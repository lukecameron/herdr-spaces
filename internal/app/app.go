// Package app wires the pieces together: the poll loop that reports agent
// counts, the periodic naming pass, and the actions a user can invoke.
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

// App holds what every command needs.
type App struct {
	Cfg    config.Config
	Client *herdr.Client
	Store  *naming.Store
	Namer  naming.Namer
	Log    *slog.Logger
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

	reported := map[string]reportedTokens{}
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			if err := a.reportCounts(ctx, reported); err != nil {
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

// reportCounts reads one snapshot and pushes changed tokens. Unchanged tokens
// are refreshed on a slower cadence with a TTL, so a dead plugin leaves no
// stale counts behind.
func (a *App) reportCounts(ctx context.Context, reported map[string]reportedTokens) error {
	snap, err := a.Client.Snapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	subs := subagents.Read(a.Cfg.SubagentDir, snap, now)
	all := counts.Compute(snap, subs)

	live := map[string]bool{}
	for wsID, c := range all {
		live[wsID] = true
		tokens := counts.Tokens(c)
		prev, seen := reported[wsID]
		if seen && counts.Equal(prev.tokens, tokens) && now.Sub(prev.at) < a.Cfg.TokenRefresh {
			continue
		}
		if !seen && c.Agents == 0 {
			// Nothing to show and nothing to clear.
			reported[wsID] = reportedTokens{tokens: tokens, at: now}
			continue
		}
		if err := a.Client.ReportWorkspaceTokens(ctx, wsID, config.Source, tokens, a.Cfg.TokenTTL); err != nil {
			return err
		}
		if !seen || !counts.Equal(prev.tokens, tokens) {
			a.Log.Debug("reported", "workspace", wsID, "counts", fmt.Sprintf("%+v", c))
		}
		reported[wsID] = reportedTokens{tokens: tokens, at: now}
	}
	for wsID := range reported {
		if !live[wsID] {
			delete(reported, wsID)
		}
	}
	return nil
}

// NamingPass considers every eligible space, or only workspaceID when it is
// set. force reclaims a space the user named by hand.
func (a *App) NamingPass(ctx context.Context, force bool, workspaceID string) error {
	snap, err := a.Client.Snapshot(ctx)
	if err != nil {
		return err
	}
	owned, err := a.Store.Load()
	if err != nil {
		return err
	}
	contexts := naming.Collect(snap, newBranchLookup())

	live := map[string]bool{}
	for _, c := range contexts {
		live[c.WorkspaceID] = true
		if workspaceID != "" && c.WorkspaceID != workspaceID {
			continue
		}
		a.consider(ctx, c, owned, force)
	}

	// Forget spaces that no longer exist.
	return a.Store.Update(func(m map[string]naming.Owned) {
		for id := range m {
			if !live[id] {
				delete(m, id)
			}
		}
	})
}

func (a *App) consider(ctx context.Context, c naming.Context, owned map[string]naming.Owned, force bool) {
	log := a.Log.With("workspace", c.WorkspaceID, "label", c.Label)
	prev, isOwned := owned[c.WorkspaceID]

	if isOwned && prev.Name != c.Label {
		log.Info("released: renamed by hand", "was", prev.Name)
		_ = a.Store.Update(func(m map[string]naming.Owned) { delete(m, c.WorkspaceID) })
		isOwned = false
		if !force {
			return
		}
	}
	if !force {
		if !c.HasAgents() {
			log.Debug("skipped: no agents")
			return
		}
		if !isOwned && !naming.IsDefaultLabel(c.Label, c.Dirs) {
			log.Debug("skipped: named by hand")
			return
		}
		if isOwned && prev.Fingerprint == c.Fingerprint() {
			log.Debug("skipped: unchanged")
			return
		}
	}

	// A default label says nothing about the work, so the model is not asked
	// to keep it. A name the plugin wrote, or one the user is reclaiming with
	// force, is worth keeping when the work has not changed.
	description := c.Describe(isOwned || (force && !naming.IsDefaultLabel(c.Label, c.Dirs)))
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
	fingerprint := c.Fingerprint()
	record := naming.Owned{Name: label, Fingerprint: fingerprint, NamedAt: time.Now(), Model: a.Cfg.Model}

	if a.Cfg.DryRun {
		log.Info("would rename", "to", label, "took", time.Since(started).Round(time.Millisecond))
		return
	}
	if label == c.Label {
		log.Info("kept", "took", time.Since(started).Round(time.Millisecond))
		_ = a.Store.Update(func(m map[string]naming.Owned) { m[c.WorkspaceID] = record })
		return
	}
	if err := a.Client.RenameWorkspace(ctx, c.WorkspaceID, label); err != nil {
		log.Warn("rename failed", "to", label, "err", err)
		return
	}
	log.Info("renamed", "to", label, "took", time.Since(started).Round(time.Millisecond))
	_ = a.Store.Update(func(m map[string]naming.Owned) { m[c.WorkspaceID] = record })
}

// Release drops ownership of a space so the plugin stops renaming it.
func (a *App) Release(workspaceID string) error {
	return a.Store.Update(func(m map[string]naming.Owned) { delete(m, workspaceID) })
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
