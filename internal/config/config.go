// Package config reads the plugin settings from config.env and the environment.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Source is the metadata source id this plugin reports under.
const Source = "lukecameron.spaces"

// Config holds every setting.
type Config struct {
	Poll           time.Duration
	TokenRefresh   time.Duration
	TokenTTL       time.Duration
	Naming         bool
	NamingInterval time.Duration
	NamingDelay    time.Duration
	Model          string
	ClaudeBin      string
	DryRun         bool
	Debug          bool

	// StateDir holds the plugin's own files, such as the names it owns.
	StateDir string
	// SubagentDir is shared with the Claude Code hook, which cannot know the
	// plugin state directory, so it lives at a fixed path.
	SubagentDir string
}

// Load reads config.env from HERDR_PLUGIN_CONFIG_DIR, then lets the process
// environment override it.
func Load() (Config, error) {
	values := map[string]string{}
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		if err := readEnvFile(filepath.Join(dir, "config.env"), values); err != nil {
			return Config{}, err
		}
	}
	get := func(key, def string) string {
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		if v, ok := values[key]; ok {
			return v
		}
		return def
	}

	cfg := Config{
		TokenRefresh: 30 * time.Second,
		TokenTTL:     2 * time.Minute,
		Model:        get("HERDR_SPACES_MODEL", "haiku"),
		ClaudeBin:    get("HERDR_SPACES_CLAUDE_BIN", "claude"),
	}

	var err error
	if cfg.Poll, err = duration(get("HERDR_SPACES_POLL", "2s"), "HERDR_SPACES_POLL"); err != nil {
		return cfg, err
	}
	if cfg.NamingInterval, err = duration(get("HERDR_SPACES_NAMING_INTERVAL", "10m"), "HERDR_SPACES_NAMING_INTERVAL"); err != nil {
		return cfg, err
	}
	if cfg.NamingDelay, err = duration(get("HERDR_SPACES_NAMING_DELAY", "30s"), "HERDR_SPACES_NAMING_DELAY"); err != nil {
		return cfg, err
	}
	if cfg.Naming, err = boolean(get("HERDR_SPACES_NAMING", "true"), "HERDR_SPACES_NAMING"); err != nil {
		return cfg, err
	}
	if cfg.DryRun, err = boolean(get("HERDR_SPACES_DRY_RUN", "false"), "HERDR_SPACES_DRY_RUN"); err != nil {
		return cfg, err
	}
	if cfg.Debug, err = boolean(get("HERDR_SPACES_DEBUG", "false"), "HERDR_SPACES_DEBUG"); err != nil {
		return cfg, err
	}
	if cfg.Poll < 200*time.Millisecond {
		return cfg, fmt.Errorf("HERDR_SPACES_POLL must be at least 200ms")
	}
	if cfg.NamingInterval < time.Minute {
		return cfg, fmt.Errorf("HERDR_SPACES_NAMING_INTERVAL must be at least 1m")
	}

	cfg.StateDir = os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(stateHome(), "herdr-spaces")
	}
	cfg.SubagentDir = SubagentDir()
	return cfg, nil
}

// SubagentDir is where the Claude Code hook records live subagents.
func SubagentDir() string {
	return filepath.Join(stateHome(), "herdr-spaces", "subagents")
}

func stateHome() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "state")
}

func readEnvFile(path string, into map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s: cannot parse line %q", path, line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		into[strings.TrimSpace(key)] = value
	}
	return sc.Err()
}

func duration(v, key string) (time.Duration, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func boolean(v, key string) (bool, error) {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}
