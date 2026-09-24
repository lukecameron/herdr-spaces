// Package naming decides which spaces may be renamed, gathers what the model
// needs to know about them, and asks Claude Code for a label.
package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lukecameron/herdr-spaces/internal/herdr"
)

// Context is everything the model is told about one workspace.
type Context struct {
	WorkspaceID string
	Label       string
	Dirs        []string // unique directory basenames, sorted
	Branches    []string // unique branches, sorted
	Tabs        []string // tab labels in tab order, position prefix removed
	Agents      []string // "claude, working: <title>" per agent, in pane order
	// Worker is the supervisor worker the space was opened for, from the
	// "worker" token supervisor attaches with workspace metadata; "" for
	// any other space. Supervisor labels those spaces itself.
	Worker string
}

// tabPosition is the "1 · " prefix Auto Title puts in front of a tab label.
var tabPosition = regexp.MustCompile(`^\d+\s*[·:]\s*`)

// Collect builds a Context for every workspace. branch returns the checked-out
// branch for a directory, or "" when there is none; it may be nil.
func Collect(s herdr.Snapshot, branch func(dir string) string) []Context {
	byWorkspace := make(map[string]*Context, len(s.Workspaces))
	var order []string
	for _, w := range s.Workspaces {
		byWorkspace[w.ID] = &Context{WorkspaceID: w.ID, Label: w.Label, Worker: w.Tokens["worker"]}
		order = append(order, w.ID)
	}

	dirs := map[string]map[string]bool{}
	addDir := func(wsID, dir string) {
		if dir == "" {
			return
		}
		if dirs[wsID] == nil {
			dirs[wsID] = map[string]bool{}
		}
		dirs[wsID][dir] = true
	}
	for _, p := range s.Panes {
		addDir(p.WorkspaceID, p.Dir())
	}
	for _, t := range s.Tabs {
		c := byWorkspace[t.WorkspaceID]
		if c == nil {
			continue
		}
		if label := strings.TrimSpace(tabPosition.ReplaceAllString(t.Label, "")); label != "" {
			c.Tabs = append(c.Tabs, label)
		}
	}
	for _, a := range s.Agents {
		c := byWorkspace[a.WorkspaceID]
		if c == nil {
			continue
		}
		addDir(a.WorkspaceID, a.Dir())
		line := a.Agent
		if line == "" {
			line = "agent"
		}
		line += ", " + a.AgentStatus
		if a.Title != "" {
			line += ": " + a.Title
		}
		c.Agents = append(c.Agents, line)
	}

	for wsID, set := range dirs {
		c := byWorkspace[wsID]
		names := map[string]bool{}
		branches := map[string]bool{}
		for dir := range set {
			names[filepath.Base(dir)] = true
			if branch != nil {
				if b := branch(dir); b != "" {
					branches[b] = true
				}
			}
		}
		c.Dirs = sortedKeys(names)
		c.Branches = sortedKeys(branches)
	}

	out := make([]Context, 0, len(order))
	for _, id := range order {
		out = append(out, *byWorkspace[id])
	}
	return out
}

// HasAgents reports whether the workspace holds at least one agent.
func (c Context) HasAgents() bool { return len(c.Agents) > 0 }

// Fingerprint identifies the work in the space, independent of its label.
// A space is only sent to the model again when this changes.
func (c Context) Fingerprint() string {
	h := sha256.New()
	for _, part := range [][]string{c.Dirs, c.Branches, c.Tabs, c.Agents} {
		for _, s := range part {
			h.Write([]byte(s))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Describe renders the context as the user message for the model. The
// current label is included only when it carries meaning: a name this plugin
// wrote earlier, or one the user chose, but not the directory name Herdr
// assigned by default.
func (c Context) Describe(includeLabel bool) string {
	var b strings.Builder
	if len(c.Dirs) > 0 {
		fmt.Fprintf(&b, "Directories: %s\n", strings.Join(c.Dirs, ", "))
	}
	if len(c.Branches) > 0 {
		fmt.Fprintf(&b, "Branches: %s\n", strings.Join(c.Branches, ", "))
	}
	if len(c.Tabs) > 0 {
		b.WriteString("Tabs:\n")
		for _, t := range c.Tabs {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}
	if len(c.Agents) > 0 {
		b.WriteString("Agents:\n")
		for _, a := range c.Agents {
			fmt.Fprintf(&b, "- %s\n", a)
		}
	}
	if includeLabel && c.Label != "" {
		fmt.Fprintf(&b, "Current label: %s\n", c.Label)
	}
	return b.String()
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
