package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallClaudeHookIsIdempotentAndKeepsOtherHooks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "hooks", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "claude", hookFile), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	existing := `{"model":"x","note":"a & b <c>","hooks":{"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"other"}]}],"SubagentStop":[{"matcher":"Explore","hooks":[{"type":"command","command":"keep-me"}]}]}}`
	if err := os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		dest, err := InstallClaudeHook(root)
		if err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
		if _, err := os.Stat(dest); err != nil {
			t.Fatalf("hook not copied: %v", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A third install must not rewrite the file at all.
	before, _ := os.Stat(filepath.Join(configDir, "settings.json"))
	if _, err := InstallClaudeHook(root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(configDir, "settings.json"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("settings.json was rewritten although the hooks were already registered")
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "x" {
		t.Error("unrelated settings must survive")
	}
	if !bytes.Contains(data, []byte(`"a & b <c>"`)) {
		t.Errorf("HTML characters must not be escaped, got %s", data)
	}
	hooks := settings["hooks"].(map[string]any)
	if n := len(hooks["SessionStart"].([]any)); n != 1 {
		t.Errorf("SessionStart entries = %d, want 1 (untouched)", n)
	}
	for _, event := range hookEvents {
		entries := hooks[event].([]any)
		ours := 0
		for _, e := range entries {
			if mentionsHook(e) {
				ours++
			}
		}
		if ours != 1 {
			t.Errorf("%s has %d of our entries, want exactly 1 after two installs", event, ours)
		}
	}
	if n := len(hooks["SubagentStop"].([]any)); n != 2 {
		t.Errorf("SubagentStop entries = %d, want 2 (existing kept, ours added)", n)
	}
}
