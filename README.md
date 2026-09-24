# Herdr Spaces

A [Herdr](https://herdr.dev) plugin that shows how many agents are running in
each Space and, every so often, asks a model to name the Space after the work
in it.

Two things it adds to the Space sidebar:

- **Agent counts.** Every Space gets `$agents`, `$working`, `$blocked`,
  `$subagents`, and `$agent_summary` tokens, kept current from the session
  every two seconds. Put the ones you want in your sidebar layout.
- **Generated names.** On a ten minute cycle, Spaces that were created while
  the plugin was running and still carry the label they were born with, or that
  this plugin named earlier, are sent to Claude Code for a short label. A Space
  you named yourself is never touched, and neither is any Space that already
  existed when the plugin started, nor one another tool opened for a worker
  and marked with a `worker` token in its metadata, since that tool labels
  it itself.

Subagents are counted for Claude Code through a small hook, because Herdr
cannot see them on its own. Other agents report zero subagents.

## Install

Requires Herdr 0.9.0 or later, Go 1.24 or later to build, and Claude Code on
your `PATH` if you want names. Herdr compiles the plugin from source when it
installs it.

```sh
herdr plugin install lukecameron/herdr-spaces
```

Plugins start with the Herdr server, so stop it once and reopen Herdr:

```sh
herdr server stop
herdr
```

### Show the counts

Add tokens to the Space rows in `~/.config/herdr/config.toml`. This keeps the
default layout and appends a dimmed summary:

```toml
[ui.sidebar.spaces]
rows = [
  ["state_icon", "workspace", { token = "$agent_summary", dim = true }],
  ["branch", "git_status"],
]
```

Or show a compact count that turns red when something is blocked:

```toml
[ui.sidebar.spaces]
rows = [
  ["state_icon", "workspace"],
  [
    "branch",
    "git_status",
    { token = "$working", fg = "#a6e3a1" },
    { token = "$blocked", fg = "#f38ba8", bold = true },
    { token = "$subagents", dim = true },
  ],
]
```

Run `herdr server reload-config` after editing. Tokens with a zero value are
not reported, so a Space with nothing running shows nothing extra.

### Count Claude Code subagents

```sh
herdr plugin action invoke lukecameron.spaces.install-claude-hook
```

This copies `hooks/claude/herdr-spaces-subagents.py` into `~/.claude/hooks`
and registers it in `~/.claude/settings.json` for `SubagentStart`,
`SubagentStop`, and `SessionEnd`. Existing hooks are kept. It takes effect in
sessions started afterwards.

The hook also sets a `$subagents` token on the pane, so you can show it on
Agent rows too:

```toml
[ui.sidebar.agents]
rows = [
  ["state_icon", "machine", "workspace", "tab"],
  ["agent", { token = "$subagents", dim = true }],
]
```

## How naming works

Herdr does not record whether a label was typed by you or assigned from the
directory name, so the plugin cannot tell a hand-picked `slate` from a default
`slate`. It therefore only names a Space whose history it has watched:

- **Born under the plugin.** A Space that appears while the plugin is running
  has its label recorded at that moment. While the label is unchanged, the
  Space is a candidate. The moment you rename it, it is yours for good.
- **Named by the plugin.** After the plugin names a Space, it keeps the name in
  step with the work as long as the label is still the one it wrote.
- **Everything else is left alone.** Spaces that existed before the plugin
  started, and Spaces you have renamed, are never touched by the automatic
  pass. Use `name-now` if you want one of those named.

Every ten minutes the plugin builds a short description of each candidate:
directory names, branches, tab labels, and what each agent is working on. A
Space is only sent to the model when it has at least one agent and its
description has changed since it was last named. The description goes to
`claude -p` with a fixed system prompt asking for two to four words, and the
reply becomes the label. The plugin's own names, and the birth labels it is
watching, live in `state.json` under its state directory.

Two actions let you override that:

```sh
herdr plugin action invoke lukecameron.spaces.name-now   # name the current Space now, whatever its history
herdr plugin action invoke lukecameron.spaces.release    # stop following the current Space
```

Bind them like any plugin action:

```toml
[[keys.command]]
key = "prefix+n"
type = "plugin_action"
command = "lukecameron.spaces.name-now"
description = "name this space"
```

`name-now` also puts the Space under the plugin's care, so later passes keep
its name current until you rename it again.

The model call uses your existing Claude Code login. It runs with no tools, no
hooks, and no session persistence, and takes a few seconds.

## Configuration

Settings live in `config.env` inside the directory printed by
`herdr plugin config-dir lukecameron.spaces`. See
[`config.env.example`](config.env.example). The file is read once at startup.

| Setting | Default | What it does |
| --- | --- | --- |
| `HERDR_SPACES_POLL` | `2s` | How often the session is read for counts |
| `HERDR_SPACES_NAMING` | `true` | Generate names at all |
| `HERDR_SPACES_NAMING_INTERVAL` | `10m` | How often Spaces are considered for a name |
| `HERDR_SPACES_NAMING_DELAY` | `30s` | Wait after startup before the first naming pass |
| `HERDR_SPACES_MODEL` | `haiku` | Model passed to `claude --model` |
| `HERDR_SPACES_CLAUDE_BIN` | `claude` | The Claude Code binary |
| `HERDR_SPACES_DRY_RUN` | `false` | Log proposed names without renaming |
| `HERDR_SPACES_DEBUG` | `false` | Log at DEBUG |

Logs are in `herdr plugin log list --plugin lukecameron.spaces`.

## Limits

- Subagent counts cover Claude Code only. Codex and other agents have no hook
  that reports subagents.
- A Claude Code session that is killed while subagents run never sends
  `SubagentStop`. The count is ignored while the agent is idle and the record
  is removed once the session is gone, so a stale count lasts at most until the
  agent finishes its turn.
- The plugin starts with the server. Installing or enabling it needs
  `herdr server stop` before it runs.

## Development

```sh
go test ./...
go build -o herdr-spaces ./cmd/herdr-spaces
herdr plugin link "$PWD"
```

Run it by hand from any Herdr pane to watch it work without installing:

```sh
HERDR_PLUGIN_STATE_DIR=/tmp/herdr-spaces HERDR_SPACES_DRY_RUN=true HERDR_SPACES_DEBUG=true ./herdr-spaces run
```

## License

MIT
