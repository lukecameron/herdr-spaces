#!/usr/bin/env python3
"""Claude Code hook for the Herdr Spaces plugin.

Registered for SubagentStart, SubagentStop, and SessionEnd. It keeps one file
per Claude Code session under the Herdr Spaces state directory listing the
subagents currently running, and reports the count as a `subagents` token on
the Herdr pane so it can be shown in the Agent sidebar. The plugin reads the
same files to roll the count up to each Space.

Outside Herdr the hook exits immediately.
"""

import fcntl
import json
import os
import subprocess
import sys
import time

SOURCE = "lukecameron.spaces"
TOKEN_TTL_MS = 6 * 60 * 60 * 1000


def state_dir():
    home = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local", "state")
    return os.path.join(home, "herdr-spaces", "subagents")


def read_record(path):
    try:
        with open(path, encoding="utf-8") as handle:
            data = json.load(handle)
        if isinstance(data, dict):
            return data
    except (OSError, ValueError):
        pass
    return {}


def write_record(path, record):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as handle:
        json.dump(record, handle)
    os.replace(tmp, path)


def report(pane_id, count):
    herdr = os.environ.get("HERDR_BIN_PATH") or "herdr"
    args = [herdr, "pane", "report-metadata", pane_id, "--source", SOURCE]
    if count > 0:
        args += ["--token", "subagents=%d" % count, "--ttl-ms", str(TOKEN_TTL_MS)]
    else:
        args += ["--clear-token", "subagents"]
    try:
        subprocess.run(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5, check=False)
    except (OSError, subprocess.SubprocessError):
        pass


def main():
    if os.environ.get("HERDR_ENV") != "1":
        return
    pane_id = os.environ.get("HERDR_PANE_ID")
    if not pane_id:
        return
    try:
        hook_input = json.load(sys.stdin)
    except ValueError:
        return
    if not isinstance(hook_input, dict):
        return

    event = hook_input.get("hook_event_name")
    session_id = hook_input.get("session_id")
    agent_id = hook_input.get("agent_id")
    if not session_id or not isinstance(session_id, str) or "/" in session_id or session_id.startswith("."):
        return

    directory = state_dir()
    os.makedirs(directory, exist_ok=True)
    path = os.path.join(directory, session_id + ".json")

    with open(path + ".lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)

        record = read_record(path)
        agents = set(a for a in record.get("agents", []) if isinstance(a, str))

        if event == "SubagentStart" and agent_id:
            agents.add(agent_id)
        elif event == "SubagentStop" and agent_id:
            agents.discard(agent_id)
        elif event == "SessionEnd":
            # A subagent's own SessionEnd carries its agent_id; only the main
            # session ending clears everything.
            if agent_id:
                return
            agents = set()
        else:
            return

        if agents:
            write_record(path, {"pane_id": pane_id, "agents": sorted(agents), "updated": time.time()})
        else:
            for stale in (path, path + ".lock"):
                try:
                    os.remove(stale)
                except OSError:
                    pass

    report(pane_id, len(agents))


if __name__ == "__main__":
    try:
        main()
    except Exception:  # never fail the agent because of a display hook
        pass
