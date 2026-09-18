package counts

import (
	"testing"

	"github.com/lukecameron/herdr-spaces/internal/herdr"
)

func TestComputeGroupsAgentsByWorkspace(t *testing.T) {
	s := herdr.Snapshot{
		Workspaces: []herdr.Workspace{{ID: "w1"}, {ID: "w2"}, {ID: "w3"}},
		Agents: []herdr.Agent{
			{PaneID: "w1:p1", WorkspaceID: "w1", AgentStatus: "working"},
			{PaneID: "w1:p2", WorkspaceID: "w1", AgentStatus: "idle"},
			{PaneID: "w1:p3", WorkspaceID: "w1", AgentStatus: "blocked"},
			{PaneID: "w2:p1", WorkspaceID: "w2", AgentStatus: "done"},
		},
	}
	got := Compute(s, map[string]int{"w1:p1": 3, "w2:p1": 9})

	if want := (Counts{Agents: 3, Working: 1, Blocked: 1, Idle: 1, Subagents: 3}); got["w1"] != want {
		t.Errorf("w1 = %+v, want %+v", got["w1"], want)
	}
	if want := (Counts{Agents: 1, Idle: 1, Subagents: 9}); got["w2"] != want {
		t.Errorf("w2 = %+v, want %+v", got["w2"], want)
	}
	if want := (Counts{}); got["w3"] != want {
		t.Errorf("w3 = %+v, want %+v", got["w3"], want)
	}
}

func TestTokensHideZeroValues(t *testing.T) {
	got := Tokens(Counts{})
	for k, v := range got {
		if v != nil {
			t.Errorf("token %s = %q, want nil", k, *v)
		}
	}
	if len(got) != 5 {
		t.Errorf("expected every token key to be present so it is cleared, got %d", len(got))
	}
}

func TestTokensSummary(t *testing.T) {
	cases := []struct {
		in   Counts
		want string
	}{
		{Counts{Agents: 1, Idle: 1}, "1 agent"},
		{Counts{Agents: 3, Working: 2, Idle: 1}, "3 agents · 2 working"},
		{Counts{Agents: 2, Working: 1, Blocked: 1, Subagents: 1}, "2 agents · 1 working · 1 blocked · 1 subagent"},
		{Counts{Agents: 5, Idle: 5, Subagents: 4}, "5 agents · 4 subagents"},
	}
	for _, c := range cases {
		got := Tokens(c.in)["agent_summary"]
		if got == nil || *got != c.want {
			t.Errorf("summary(%+v) = %v, want %q", c.in, deref(got), c.want)
		}
	}
	if v := Tokens(Counts{Agents: 4, Working: 2, Subagents: 3})["subagents"]; v == nil || *v != "3" {
		t.Errorf("subagents token = %v, want 3", deref(v))
	}
}

func TestEqual(t *testing.T) {
	a := Tokens(Counts{Agents: 2, Working: 1, Idle: 1})
	b := Tokens(Counts{Agents: 2, Working: 1, Idle: 1})
	c := Tokens(Counts{Agents: 2, Working: 2})
	if !Equal(a, b) {
		t.Error("identical tokens should be equal")
	}
	if Equal(a, c) {
		t.Error("different tokens should not be equal")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
