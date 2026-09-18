package subagents

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lukecameron/herdr-spaces/internal/herdr"
)

func write(t *testing.T, dir, session, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, session+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func snapshot(status string) herdr.Snapshot {
	return herdr.Snapshot{Agents: []herdr.Agent{{
		PaneID:       "w1:p1",
		WorkspaceID:  "w1",
		Agent:        "claude",
		AgentStatus:  status,
		AgentSession: &herdr.AgentSession{Value: "sess-1"},
	}}}
}

func TestReadCountsLiveSubagentsForWorkingAgent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sess-1", `{"pane_id":"w1:p1","agents":["a","b"],"updated":1}`)

	got := Read(dir, snapshot("working"), time.Now())
	if got["w1:p1"] != 2 {
		t.Errorf("got %v, want w1:p1=2", got)
	}
}

func TestReadIgnoresIdleAgent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sess-1", `{"pane_id":"w1:p1","agents":["a"],"updated":1}`)

	if got := Read(dir, snapshot("idle"), time.Now()); len(got) != 0 {
		t.Errorf("idle agent should have no subagents, got %v", got)
	}
}

func TestReadDeletesStaleRecords(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "gone", `{"pane_id":"w9:p1","agents":["a"],"updated":1}`)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "gone.json"), old, old); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "recent", `{"pane_id":"w9:p2","agents":["a"],"updated":1}`)

	got := Read(dir, snapshot("working"), time.Now())
	if len(got) != 0 {
		t.Errorf("unmatched sessions should not count, got %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.json")); !os.IsNotExist(err) {
		t.Error("stale record should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "recent.json")); err != nil {
		t.Error("recent unmatched record should be kept")
	}
}

func TestReadMissingDir(t *testing.T) {
	if got := Read(filepath.Join(t.TempDir(), "missing"), snapshot("working"), time.Now()); got != nil {
		t.Errorf("missing dir should yield nil, got %v", got)
	}
}
