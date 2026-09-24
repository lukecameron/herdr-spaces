// Package app wires the pieces together: the poll loop that reports agent
// counts and watches for new spaces, the periodic naming pass, and the
// actions a user can invoke.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/lukecameron/herdr-spaces/internal/config"
	"github.com/lukecameron/herdr-spaces/internal/counts"
	"github.com/lukecameron/herdr-spaces/internal/herdr"
	"github.com/lukecameron/herdr-spaces/internal/naming"
	"github.com/lukecameron/herdr-spaces/internal/subagents"
)

// Session is the part of the Herdr API the app uses.
type Session interface {
	Snapshot(ctx context.Context) (herdr.Snapshot, error)
	RenameWorkspace(ctx context.Context, workspaceID, label string) error
	ReportWorkspaceTokens(ctx context.Context, workspaceID, source string, tokens map[string]*string, ttl time.Duration) error
}

// App holds what every command needs.
type App struct {
	Cfg    config.Config
	Client Session
	Store  *naming.Store
	Namer  naming.Namer
	Log    *slog.Logger
	Branch func(dir string) string
	subDir string
	known  map[string]bool
	primed bool
	tokens map[string]reportedTokens
}

// New builds an App from the environment Herdr injects.
func New(cfg config.Config, log *slog.Logger) (*App, error) {
	client, err := herdr.NewFromEnv()
	if err != nil {
		return nil, err
	}
	store, err := naming.NewStore(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("state dir: %w", err)
	}
	return &App{
		Cfg:    cfg,
		Client: client,
		Store:  store,
		Namer:  naming.ClaudeNamer{Bin: cfg.ClaudeBin, Model: cfg.Model, WorkDir: cfg.StateDir},
		Log:    log,
		Branch: newBranchLookup(),
		subDir: cfg.SubagentDir,
	}, nil
}

// Run polls the session until ctx ends, reporting counts every poll and
// naming spaces on the configured interval.
func (a *App) Run(ctx context.Context) error {
	a.Log.Info("starting", "poll", a.Cfg.Poll, "naming", a.Cfg.Naming, "interval", a.Cfg.NamingInterval, "model", a.Cfg.Model, "dry_run", a.Cfg.DryRun)

	poll := time.NewTicker(a.Cfg.Poll)
	defer poll.Stop()

	var naming <-chan time.Time
	var namingTimer *time.Timer
	if a.Cfg.Naming {
		namingTimer = time.NewTimer(a.Cfg.NamingDelay)
		defer namingTimer.Stop()
		naming = namingTimer.C
	}

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			if err := a.Poll(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				failures++
				if failures == 1 || failures%30 == 0 {
					a.Log.Warn("poll failed", "err", err, "consecutive", failures)
				}
				continue
			}
			failures = 0
		case <-naming:
			if err := a.NamingPass(ctx, false, ""); err != nil && ctx.Err() == nil {
				a.Log.Warn("naming pass failed", "err", err)
			}
			namingTimer.Reset(a.Cfg.NamingInterval)
		}
	}
}

type reportedTokens struct {
	tokens map[string]*string
	at     time.Time
}

// Poll reads one snapshot, records any space that has just appeared, and
// pushes changed count tokens.
func (a *App) Poll(ctx context.Context) error {
	snap, err := a.Client.Snapshot(ctx)
	if err != nil {
		return err
	}
	a.recordBirths(snap)
	return a.reportCounts(ctx, snap)
}

// recordBirths remembers the label of every space that appears after the
// first poll. The first poll only learns what already exists: those spaces
// may carry names the user chose, so they are never named automatically.
func (a *App) recordBirths(snap herdr.Snapshot) {
	if a.known == nil {
		a.known = map[string]bool{}
	}
	var born []herdr.Workspace
	live := map[string]bool{}
	for _, w := range snap.Workspaces {
		live[w.ID] = true
		if !a.known[w.ID] {
			a.known[w.ID] = true
			if a.primed {
				born = append(born, w)
			}
		}
	}
	for id := range a.known {
		if !live[id] {
			delete(a.known, id)
		}
	}
	a.primed = true
	if len(born) == 0 {
		return
	}
	err := a.Store.Update(func(s *naming.State) {
		for _, w := range born {
			s.Born[w.ID] = w.Label
		}
	})
	if err != nil {
		a.Log.Warn("could not record new spaces", "err", err)
		return
	}
	for _, w := range born {
		a.Log.Info("new space", "workspace", w.ID, "label", w.Label)
	}
}

// reportCounts pushes changed tokens. Unchanged tokens are refreshed on a
// slower cadence with a TTL, so a dead plugin leaves no stale counts behind.
func (a *App) reportCounts(ctx context.Context, snap herdr.Snapshot) error {
	if a.tokens == nil {
		a.tokens = map[string]reportedTokens{}
	}
	now := time.Now()
	subs := subagents.Read(a.subDir, snap, now)
	all := counts.Compute(snap, subs)

	live := map[string]bool{}
	for wsID, c := range all {
		live[wsID] = true
		tokens := counts.Tokens(c)
		prev, seen := a.tokens[wsID]
		if seen && counts.Equal(prev.tokens, tokens) && now.Sub(prev.at) < a.Cfg.TokenRefresh {
			continue
		}
		if !seen && c.Agents == 0 {
			a.tokens[wsID] = reportedTokens{tokens: tokens, at: now}
			continue
		}
		if err := a.Client.ReportWorkspaceTokens(ctx, wsID, config.Source, tokens, a.Cfg.TokenTTL); err != nil {
			return err
		}
		if !seen || !counts.Equal(prev.tokens, tokens) {
			a.Log.Debug("reported", "workspace", wsID, "counts", fmt.Sprintf("%+v", c))
		}
		a.tokens[wsID] = reportedTokens{tokens: tokens, at: now}
	}
	for wsID := range a.tokens {
		if !live[wsID] {
			delete(a.tokens, wsID)
		}
	}
	return nil
}

// NamingPass considers every eligible space, or only workspaceID when it is
// set. force names a space whatever its history, including one the user
// named by hand.
func (a *App) NamingPass(ctx context.Context, force bool, workspaceID string) error {
	snap, err := a.Client.Snapshot(ctx)
	if err != nil {
		return err
	}
	state, err := a.Store.Load()
	if err != nil {
		return err
	}
	contexts := naming.Collect(snap, a.Branch)

	live := map[string]bool{}
	for _, c := range contexts {
		live[c.WorkspaceID] = true
		if workspaceID != "" && c.WorkspaceID != workspaceID {
			continue
		}
		a.consider(ctx, c, state, force)
	}

	// Forget spaces that no longer exist.
	return a.Store.Update(func(s *naming.State) {
		for id := range s.Owned {
			if !live[id] {
				delete(s.Owned, id)
			}
		}
		for id := range s.Born {
			if !live[id] {
				delete(s.Born, id)
			}
		}
	})
}

// Eligibility is why a space may or may not be named automatically.
type Eligibility string

const (
	// EligibleOwned means the plugin named it and the work has changed.
	EligibleOwned Eligibility = "owned"
	// EligibleBorn means it appeared while the plugin ran and still has its
	// creation label.
	EligibleBorn Eligibility = "born"
	// SkipRenamedByHand means the label is not one the plugin wrote or saw at
	// creation, so the user chose it.
	SkipRenamedByHand Eligibility = "named by hand"
	// SkipUnchanged means the plugin named it and nothing has changed since.
	SkipUnchanged Eligibility = "unchanged"
	// SkipNoAgents means there is nothing to describe yet.
	SkipNoAgents Eligibility = "no agents"
	// SkipPreexisting means it existed before the plugin started, so its
	// label may be the user's.
	SkipPreexisting Eligibility = "existed before the plugin started"
	// SkipSupervisorWorker means supervisor opened the space for one of its
	// workers and labelled it "└ <worker>" itself, ordered under the space
	// that spawned it; a generated name would hide which worker it is.
	SkipSupervisorWorker Eligibility = "a supervisor worker's space"
)

// Eligible decides whether an automatic pass may name the space.
func Eligible(c naming.Context, state naming.State) (Eligibility, bool) {
	if c.Worker != "" {
		return SkipSupervisorWorker, false
	}
	if owned, ok := state.Owned[c.WorkspaceID]; ok && owned.Name == c.Label {
		if owned.Fingerprint == c.Fingerprint() {
			return SkipUnchanged, false
		}
		if !c.HasAgents() {
			return SkipNoAgents, false
		}
		return EligibleOwned, true
	}
	if _, ok := state.Owned[c.WorkspaceID]; ok {
		return SkipRenamedByHand, false
	}
	born, ok := state.Born[c.WorkspaceID]
	if !ok {
		return SkipPreexisting, false
	}
	if born != c.Label {
		return SkipRenamedByHand, false
	}
	if !c.HasAgents() {
		return SkipNoAgents, false
	}
	return EligibleBorn, true
}

func (a *App) consider(ctx context.Context, c naming.Context, state naming.State, force bool) {
	log := a.Log.With("workspace", c.WorkspaceID, "label", c.Label)
	owned, isOwned := state.Owned[c.WorkspaceID]

	if isOwned && owned.Name != c.Label {
		log.Info("released: renamed by hand", "was", owned.Name)
		_ = a.Store.Update(func(s *naming.State) { delete(s.Owned, c.WorkspaceID); delete(s.Born, c.WorkspaceID) })
		isOwned = false
	}
	if !force {
		why, ok := Eligible(c, state)
		if !ok {
			log.Debug("skipped: " + string(why))
			return
		}
	}

	description := c.Describe(isOwned || force)
	if strings.TrimSpace(description) == "" {
		log.Debug("skipped: nothing to describe")
		return
	}
	started := time.Now()
	raw, err := a.Namer.Name(ctx, description)
	if err != nil {
		log.Warn("model call failed", "err", err)
		return
	}
	label, ok := naming.Sanitize(raw)
	if !ok {
		log.Warn("model reply unusable", "reply", strings.TrimSpace(raw))
		return
	}
	record := naming.Owned{Name: label, Fingerprint: c.Fingerprint(), NamedAt: time.Now(), Model: a.Cfg.Model}

	if a.Cfg.DryRun {
		log.Info("would rename", "to", label, "took", time.Since(started).Round(time.Millisecond))
		return
	}
	if label == c.Label {
		log.Info("kept", "took", time.Since(started).Round(time.Millisecond))
		_ = a.Store.Update(func(s *naming.State) { s.Owned[c.WorkspaceID] = record })
		return
	}
	if err := a.Client.RenameWorkspace(ctx, c.WorkspaceID, label); err != nil {
		log.Warn("rename failed", "to", label, "err", err)
		return
	}
	log.Info("renamed", "to", label, "took", time.Since(started).Round(time.Millisecond))
	_ = a.Store.Update(func(s *naming.State) { s.Owned[c.WorkspaceID] = record })
}

// Release drops ownership of a space so the plugin stops renaming it.
func (a *App) Release(workspaceID string) error {
	return a.Store.Update(func(s *naming.State) { delete(s.Owned, workspaceID); delete(s.Born, workspaceID) })
}

// newBranchLookup returns a memoized git branch reader.
func newBranchLookup() func(dir string) string {
	cache := map[string]string{}
	return func(dir string) string {
		if b, ok := cache[dir]; ok {
			return b
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
		branch := ""
		var exitErr *exec.ExitError
		if err == nil {
			branch = strings.TrimSpace(string(out))
			if branch == "HEAD" {
				branch = ""
			}
		} else if !errors.As(err, &exitErr) {
			branch = ""
		}
		cache[dir] = branch
		return branch
	}
}
