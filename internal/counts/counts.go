// Package counts turns a session snapshot into per-workspace agent counts and
// the display tokens that show them.
package counts

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lukecameron/herdr-spaces/internal/herdr"
)

// Counts describes the agents in one workspace.
type Counts struct {
	Agents    int
	Working   int
	Blocked   int
	Idle      int
	Subagents int
}

// Compute counts the agents in every workspace. subagentsByPane maps a pane id
// to the number of subagents running under the agent in that pane.
func Compute(s herdr.Snapshot, subagentsByPane map[string]int) map[string]Counts {
	out := make(map[string]Counts, len(s.Workspaces))
	for _, w := range s.Workspaces {
		out[w.ID] = Counts{}
	}
	for _, a := range s.Agents {
		c := out[a.WorkspaceID]
		c.Agents++
		switch a.AgentStatus {
		case "working":
			c.Working++
		case "blocked":
			c.Blocked++
		default:
			c.Idle++
		}
		c.Subagents += subagentsByPane[a.PaneID]
		out[a.WorkspaceID] = c
	}
	return out
}

// Tokens renders the counts as workspace metadata. Zero values map to nil so
// the token disappears from the sidebar instead of showing "0".
func Tokens(c Counts) map[string]*string {
	return map[string]*string{
		"agents":        nonZero(c.Agents),
		"working":       nonZero(c.Working),
		"blocked":       nonZero(c.Blocked),
		"subagents":     nonZero(c.Subagents),
		"agent_summary": summary(c),
	}
}

// Equal reports whether two token maps would render the same.
func Equal(a, b map[string]*string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if (va == nil) != (vb == nil) {
			return false
		}
		if va != nil && *va != *vb {
			return false
		}
	}
	return true
}

func nonZero(n int) *string {
	if n == 0 {
		return nil
	}
	s := strconv.Itoa(n)
	return &s
}

func summary(c Counts) *string {
	if c.Agents == 0 {
		return nil
	}
	parts := []string{plural(c.Agents, "agent")}
	if c.Working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", c.Working))
	}
	if c.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", c.Blocked))
	}
	if c.Subagents > 0 {
		parts = append(parts, plural(c.Subagents, "subagent"))
	}
	s := strings.Join(parts, " · ")
	return &s
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
