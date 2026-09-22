package naming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukecameron/herdr-spaces/internal/herdr"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Dotfiles Ghostty Config", "Dotfiles Ghostty Config", true},
		{"\"Slate Review 5547\"\n", "Slate Review 5547", true},
		{"  \n**Harness Review Agents.**\n\nExplanation here", "Harness Review Agents", true},
		{"one two three four five six seven", "one two three four five", true},
		{"", "", false},
		{"\n \n", "", false},
		{"Label: something", "", false},
		{"I cannot name this workspace", "", false},
	}
	for _, c := range cases {
		got, ok := Sanitize(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Sanitize(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func sample() herdr.Snapshot {
	return herdr.Snapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "dotfiles"}, {ID: "w2", Label: "empty"}},
		Tabs: []herdr.Tab{
			{ID: "w1:t1", WorkspaceID: "w1", Label: "1 · claude › Ghostty opacity"},
			{ID: "w1:t2", WorkspaceID: "w1", Label: "2 · Shell"},
		},
		Panes: []herdr.Pane{
			{ID: "w1:p1", WorkspaceID: "w1", CWD: "/home/me/dotfiles"},
			{ID: "w1:p2", WorkspaceID: "w1", CWD: "/home/me/dotfiles", ForegroundCWD: "/home/me/dotfiles/dot_config"},
		},
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", WorkspaceID: "w1", Agent: "claude", AgentStatus: "working", CWD: "/home/me/dotfiles", Title: "Ghostty opacity"},
		},
	}
}

func TestCollect(t *testing.T) {
	branch := func(dir string) string {
		if filepath.Base(dir) == "dotfiles" {
			return "main"
		}
		return ""
	}
	ctxs := Collect(sample(), branch)
	if len(ctxs) != 2 {
		t.Fatalf("got %d contexts, want 2", len(ctxs))
	}
	c := ctxs[0]
	if c.WorkspaceID != "w1" || c.Label != "dotfiles" {
		t.Errorf("unexpected identity %+v", c)
	}
	if want := []string{"dot_config", "dotfiles"}; !equal(c.Dirs, want) {
		t.Errorf("dirs = %v, want %v", c.Dirs, want)
	}
	if want := []string{"main"}; !equal(c.Branches, want) {
		t.Errorf("branches = %v, want %v", c.Branches, want)
	}
	if want := []string{"claude › Ghostty opacity", "Shell"}; !equal(c.Tabs, want) {
		t.Errorf("tabs = %v, want %v", c.Tabs, want)
	}
	if want := []string{"claude, working: Ghostty opacity"}; !equal(c.Agents, want) {
		t.Errorf("agents = %v, want %v", c.Agents, want)
	}
	if !c.HasAgents() || ctxs[1].HasAgents() {
		t.Error("HasAgents is wrong")
	}
}

func TestDescribeIncludesLabelOnlyWhenAsked(t *testing.T) {
	c := Collect(sample(), nil)[0]
	if strings.Contains(c.Describe(false), "Current label") {
		t.Error("default label should not be described")
	}
	if !strings.Contains(c.Describe(true), "Current label: dotfiles") {
		t.Error("owned label should be described")
	}
	if !strings.Contains(c.Describe(false), "- claude, working: Ghostty opacity") {
		t.Errorf("agents missing from description:\n%s", c.Describe(false))
	}
}

func TestFingerprintIgnoresLabel(t *testing.T) {
	a := Collect(sample(), nil)[0]
	b := a
	b.Label = "Something Else"
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("label must not affect the fingerprint")
	}
	b.Agents = append(b.Agents, "codex, idle")
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("agent changes must change the fingerprint")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(s *State) { s.Owned["w1"] = Owned{Name: "A", Fingerprint: "f"}; s.Born["w3"] = "slate" }); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(s *State) { delete(s.Owned, "missing"); s.Owned["w2"] = Owned{Name: "B"} }); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Owned["w1"].Name != "A" || got.Owned["w2"].Name != "B" || len(got.Owned) != 2 {
		t.Errorf("unexpected owned %+v", got.Owned)
	}
	if got.Born["w3"] != "slate" || len(got.Born) != 1 {
		t.Errorf("unexpected born %+v", got.Born)
	}
}

func TestStoreReadsLegacyOwnedFile(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"w1":{"name":"Old Name","fingerprint":"abc"}}`
	if err := os.WriteFile(filepath.Join(dir, "owned.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Owned["w1"].Name != "Old Name" || len(got.Born) != 0 {
		t.Errorf("legacy load = %+v", got)
	}
	if err := st.Update(func(s *State) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "owned.json")); !os.IsNotExist(err) {
		t.Error("legacy file should be removed after the first write")
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Error("state.json should exist after the first write")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
