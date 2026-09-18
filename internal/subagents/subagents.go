// Package subagents reads the records the Claude Code hook keeps about live
// subagents and reconciles them against what Herdr can see.
package subagents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lukecameron/herdr-spaces/internal/herdr"
)

// Record is one file in the subagent directory, named <session_id>.json.
type Record struct {
	PaneID  string   `json:"pane_id"`
	Agents  []string `json:"agents"`
	Updated float64  `json:"updated"`
}

// staleAfter is how long a record whose session Herdr no longer shows is kept
// before it is deleted. Sessions are reported to Herdr at start, so a record
// without a matching pane belongs to a session that ended without a hook.
const staleAfter = 24 * time.Hour

// Read returns the number of live subagents per pane. A record counts only
// while the matching agent is working or blocked: an idle agent has no
// subagents running whatever the hook last recorded.
func Read(dir string, s herdr.Snapshot, now time.Time) map[string]int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	byShortSession := make(map[string]herdr.Agent, len(s.Agents))
	for _, a := range s.Agents {
		if a.AgentSession != nil && a.AgentSession.Value != "" {
			byShortSession[a.AgentSession.Value] = a
		}
	}

	out := map[string]int{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		sessionID := strings.TrimSuffix(name, ".json")

		agent, ok := byShortSession[sessionID]
		if !ok {
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > staleAfter {
				_ = os.Remove(path)
			}
			continue
		}
		if agent.AgentStatus != "working" && agent.AgentStatus != "blocked" {
			continue
		}

		var rec Record
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &rec) != nil {
			continue
		}
		if n := len(rec.Agents); n > 0 {
			out[agent.PaneID] += n
		}
	}
	return out
}
