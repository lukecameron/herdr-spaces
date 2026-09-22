// Command herdr-spaces is a Herdr plugin that shows agent counts on every
// space and gives spaces model-generated names.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lukecameron/herdr-spaces/internal/app"
	"github.com/lukecameron/herdr-spaces/internal/config"
)

const version = "0.2.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-spaces:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd == "version" || cmd == "--version" {
		fmt.Println("herdr-spaces", version)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	switch cmd {
	case "run":
		a, err := app.New(cfg, log)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return a.Run(ctx)

	case "name-now":
		a, err := app.New(cfg, log)
		if err != nil {
			return err
		}
		wsID := workspaceArg(args)
		if wsID == "" {
			return fmt.Errorf("name-now needs a workspace: invoke it from a space or pass an id")
		}
		return a.NamingPass(context.Background(), true, wsID)

	case "release":
		a, err := app.New(cfg, log)
		if err != nil {
			return err
		}
		wsID := workspaceArg(args)
		if wsID == "" {
			return fmt.Errorf("release needs a workspace: invoke it from a space or pass an id")
		}
		if err := a.Release(wsID); err != nil {
			return err
		}
		log.Info("released", "workspace", wsID)
		return nil

	case "install-claude-hook":
		root := os.Getenv("HERDR_PLUGIN_ROOT")
		if root == "" {
			root = "."
		}
		dest, err := app.InstallClaudeHook(root)
		if err != nil {
			return err
		}
		fmt.Println("installed", dest)
		fmt.Println("Registered for SubagentStart, SubagentStop, and SessionEnd. It applies to sessions started from now on.")
		return nil

	default:
		return fmt.Errorf("unknown command %q (run, name-now, release, install-claude-hook, version)", cmd)
	}
}

// workspaceArg prefers an explicit id and falls back to the action context.
func workspaceArg(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return os.Getenv("HERDR_WORKSPACE_ID")
}
