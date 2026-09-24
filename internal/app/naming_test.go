package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lukecameron/herdr-spaces/internal/config"
	"github.com/lukecameron/herdr-spaces/internal/herdr"
	"github.com/lukecameron/herdr-spaces/internal/naming"
)

type fakeSession struct {
	snap    herdr.Snapshot
	renames map[string]string
}

func (f *fakeSession) Snapshot(context.Context) (herdr.Snapshot, error) { return f.snap, nil }
func (f *fakeSession) RenameWorkspace(_ context.Context, id, label string) error {
	if f.renames == nil {
		f.renames = map[string]string{}
	}
	f.renames[id] = label
	f.snap.Workspaces = relabel(f.snap.Workspaces, id, label)
	return nil
}
func (f *fakeSession) ReportWorkspaceTokens(context.Context, string, string, map[string]*string, time.Duration) error {
	return nil
}

type fakeNamer struct{ calls int }

func (n *fakeNamer) Name(context.Context, string) (string, error) {
	n.calls++
	return "Model Name", nil
}

func relabel(ws []herdr.Workspace, id, label string) []herdr.Workspace {
	out := make([]herdr.Workspace, len(ws))
	copy(out, ws)
	for i := range out {
		if out[i].ID == id {
			out[i].Label = label
		}
	}
	return out
}

func agentIn(ws string) herdr.Agent {
	return herdr.Agent{PaneID: ws + ":p1", WorkspaceID: ws, Agent: "claude", AgentStatus: "working", CWD: "/x/" + ws, Title: "doing " + ws}
}

func newTestApp(t *testing.T, sess *fakeSession) (*App, *fakeNamer) {
	t.Helper()
	store, err := naming.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	namer := &fakeNamer{}
	return &App{
		Cfg:    config.Config{Model: "test", TokenRefresh: time.Minute, TokenTTL: time.Minute},
		Client: sess,
		Store:  store,
		Namer:  namer,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Branch: func(string) string { return "" },
		subDir: t.TempDir(),
	}, namer
}

func TestPreexistingSpacesAreNeverNamedAutomatically(t *testing.T) {
	sess := &fakeSession{snap: herdr.Snapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "slate"}},
		Agents:     []herdr.Agent{agentIn("w1")},
	}}
	app, namer := newTestApp(t, sess)
	ctx := context.Background()

	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 0 || len(sess.renames) != 0 {
		t.Fatalf("preexisting space was named: calls=%d renames=%v", namer.calls, sess.renames)
	}
}

func TestSpaceBornWhileRunningIsNamedUntilUserRenamesIt(t *testing.T) {
	sess := &fakeSession{snap: herdr.Snapshot{Workspaces: []herdr.Workspace{{ID: "w1", Label: "old"}}}}
	app, namer := newTestApp(t, sess)
	ctx := context.Background()
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}

	// A new space appears with Herdr's directory label and an agent.
	sess.snap.Workspaces = append(sess.snap.Workspaces, herdr.Workspace{ID: "w2", Label: "slate"})
	sess.snap.Agents = []herdr.Agent{agentIn("w2")}
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if sess.renames["w2"] != "Model Name" || namer.calls != 1 {
		t.Fatalf("born space not named: renames=%v calls=%d", sess.renames, namer.calls)
	}

	// Same work again: nothing to do.
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 1 {
		t.Fatalf("unchanged space was re-sent to the model: calls=%d", namer.calls)
	}

	// The user renames it, then the work changes. The plugin must let go.
	sess.snap.Workspaces = relabel(sess.snap.Workspaces, "w2", "Mine")
	sess.snap.Agents[0].Title = "something else"
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 1 || sess.renames["w2"] != "Model Name" {
		t.Fatalf("hand-renamed space was touched: calls=%d renames=%v", namer.calls, sess.renames)
	}
	state, _ := app.Store.Load()
	if _, owned := state.Owned["w2"]; owned {
		t.Error("ownership should be released after a manual rename")
	}
	if _, born := state.Born["w2"]; born {
		t.Error("birth record should be dropped after a manual rename")
	}
}

func TestSpaceRenamedBeforeFirstPassIsLeftAlone(t *testing.T) {
	sess := &fakeSession{snap: herdr.Snapshot{Workspaces: []herdr.Workspace{{ID: "w1", Label: "old"}}}}
	app, namer := newTestApp(t, sess)
	ctx := context.Background()
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	sess.snap.Workspaces = append(sess.snap.Workspaces, herdr.Workspace{ID: "w2", Label: "slate"})
	sess.snap.Agents = []herdr.Agent{agentIn("w2")}
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	// User names it before the naming pass ever runs.
	sess.snap.Workspaces = relabel(sess.snap.Workspaces, "w2", "My Space")
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 0 {
		t.Fatalf("space renamed by the user before the first pass was named: calls=%d", namer.calls)
	}
}

func TestForceNamesAnything(t *testing.T) {
	sess := &fakeSession{snap: herdr.Snapshot{
		Workspaces: []herdr.Workspace{{ID: "w1", Label: "My Space"}},
		Agents:     []herdr.Agent{agentIn("w1")},
	}}
	app, namer := newTestApp(t, sess)
	ctx := context.Background()
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.NamingPass(ctx, true, "w1"); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 1 || sess.renames["w1"] != "Model Name" {
		t.Fatalf("force did not name: calls=%d renames=%v", namer.calls, sess.renames)
	}
	// Now owned: a later automatic pass keeps following it as the work changes.
	sess.snap.Agents[0].Title = "new work"
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 2 {
		t.Fatalf("owned space with changed work not re-named: calls=%d", namer.calls)
	}
}

func TestEligibilityReasons(t *testing.T) {
	ctx := func(id, label string, agents bool) naming.Context {
		c := naming.Context{WorkspaceID: id, Label: label}
		if agents {
			c.Agents = []string{"claude, working"}
		}
		return c
	}
	state := naming.State{
		Owned: map[string]naming.Owned{"o": {Name: "Owned", Fingerprint: "stale"}},
		Born:  map[string]string{"b": "dir", "q": "dir"},
	}
	cases := []struct {
		c    naming.Context
		want Eligibility
		ok   bool
	}{
		{ctx("p", "anything", true), SkipPreexisting, false},
		{ctx("b", "dir", true), EligibleBorn, true},
		{ctx("b", "dir", false), SkipNoAgents, false},
		{ctx("q", "Renamed", true), SkipRenamedByHand, false},
		{ctx("o", "Owned", true), EligibleOwned, true},
		{ctx("o", "Changed", true), SkipRenamedByHand, false},
		{func() naming.Context { c := ctx("b", "dir", true); c.Worker = "c7886"; return c }(), SkipSupervisorWorker, false},
	}
	for _, tc := range cases {
		got, ok := Eligible(tc.c, state)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Eligible(%s %q) = (%s, %v), want (%s, %v)", tc.c.WorkspaceID, tc.c.Label, got, ok, tc.want, tc.ok)
		}
	}
	unchanged := ctx("o", "Owned", true)
	state.Owned["o"] = naming.Owned{Name: "Owned", Fingerprint: unchanged.Fingerprint()}
	if got, ok := Eligible(unchanged, state); got != SkipUnchanged || ok {
		t.Errorf("unchanged owned space = (%s, %v)", got, ok)
	}
}

// A space supervisor opens for one of its workers is labelled "└ <worker>"
// and marked with a worker token; it keeps that label even though it was
// born while the plugin ran and has agents in it.
func TestSupervisorWorkerSpacesKeepTheirLabel(t *testing.T) {
	sess := &fakeSession{snap: herdr.Snapshot{Workspaces: []herdr.Workspace{{ID: "w1", Label: "Conditions"}}}}
	app, namer := newTestApp(t, sess)
	ctx := context.Background()
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	sess.snap.Workspaces = append(sess.snap.Workspaces, herdr.Workspace{ID: "w2", Label: "└ c7886", Tokens: map[string]string{"worker": "c7886", "parent": "w1"}})
	sess.snap.Agents = []herdr.Agent{agentIn("w2")}
	if err := app.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.NamingPass(ctx, false, ""); err != nil {
		t.Fatal(err)
	}
	if namer.calls != 0 || sess.renames["w2"] != "" {
		t.Fatalf("a supervisor worker's space was renamed: calls=%d renames=%v", namer.calls, sess.renames)
	}
}
