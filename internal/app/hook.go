package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookFile is the script name under the Claude Code hooks directory.
const hookFile = "herdr-spaces-subagents.py"

// hookEvents are the Claude Code events the script needs.
var hookEvents = []string{"SubagentStart", "SubagentStop", "SessionEnd"}

// InstallClaudeHook copies the subagent hook into the Claude Code config
// directory and registers it in settings.json. It is safe to run again.
func InstallClaudeHook(pluginRoot string) (string, error) {
	src := filepath.Join(pluginRoot, "hooks", "claude", hookFile)
	script, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read hook source: %w", err)
	}

	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, ".claude")
	}
	hooksDir := filepath.Join(configDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(hooksDir, hookFile)
	if err := os.WriteFile(dest, script, 0o755); err != nil {
		return "", err
	}

	settingsPath := filepath.Join(configDir, "settings.json")
	settings := map[string]any{}
	data, err := os.ReadFile(settingsPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return "", err
	default:
		if err := json.Unmarshal(data, &settings); err != nil {
			return "", fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}

	command := fmt.Sprintf("python3 '%s'", dest)
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, event := range hookEvents {
		entries, _ := hooks[event].([]any)
		entry := map[string]any{
			"matcher": "",
			"hooks":   []any{map[string]any{"type": "command", "command": command, "timeout": 10}},
		}
		replaced := false
		for i, e := range entries {
			if mentionsHook(e) {
				entries[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			entries = append(entries, entry)
		}
		hooks[event] = entries
	}
	settings["hooks"] = hooks

	// Claude Code and other tools rewrite this file too; keep its text stable
	// by not escaping HTML characters the way json.Marshal does by default.
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return "", err
	}
	tmp := settingsPath + ".herdr-spaces.tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, settingsPath); err != nil {
		return "", err
	}
	return dest, nil
}

// mentionsHook reports whether a settings.json hook entry runs our script.
func mentionsHook(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	list, _ := m["hooks"].([]any)
	for _, h := range list {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, _ := hm["command"].(string); strings.Contains(cmd, hookFile) {
			return true
		}
	}
	return false
}
