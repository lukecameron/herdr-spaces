package naming

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SystemPrompt is what the model is told about its job.
const SystemPrompt = `You label terminal workspaces for a developer who runs several AI coding agents at once. Each workspace holds one project or task.

Reply with only the label: two to four words in Title Case, no punctuation, no quotes, no explanation.

Describe the work, not the tools. Prefer the project or repository name followed by the task, for example "Dotfiles Ghostty Config" or "Slate Review 5547". Use the branch or ticket id when it identifies the task. Do not include words like Claude, Codex, agent, tab, or workspace.

A current label may be given. Keep it unless the work has clearly changed.`

// Namer asks something for a label.
type Namer interface {
	Name(ctx context.Context, description string) (string, error)
}

// ClaudeNamer runs the Claude Code CLI in print mode. Nothing but the label
// is requested, so no tools, hooks, or session files are involved.
type ClaudeNamer struct {
	Bin     string
	Model   string
	WorkDir string
	Timeout time.Duration
}

// Name runs one completion and returns its raw text.
func (n ClaudeNamer) Name(ctx context.Context, description string) (string, error) {
	timeout := n.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-p",
		"--model", n.Model,
		"--no-session-persistence",
		"--output-format", "text",
		"--tools", "",
		"--disable-slash-commands",
		"--settings", `{"hooks":{}}`,
		"--system-prompt", SystemPrompt,
		"--", description,
	}
	cmd := exec.CommandContext(ctx, n.Bin, args...)
	cmd.Dir = n.WorkDir
	cmd.Env = cleanEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s: %w: %s", n.Bin, err, msg)
	}
	return stdout.String(), nil
}

// cleanEnv drops the variables that would make the CLI think it runs inside a
// Herdr pane or another Claude Code session.
func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "HERDR_") || strings.HasPrefix(key, "CLAUDECODE") || strings.HasPrefix(key, "CLAUDE_CODE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
