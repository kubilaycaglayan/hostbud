#!/usr/bin/env python3
"""Forward Codex or Claude Code lifecycle events into a tmux pane option."""

import json
import os
import re
import subprocess
import sys
from typing import Optional


PANE_RE = re.compile(r"^%[0-9]+$")


def status_for(provider: str, event: dict) -> Optional[str]:
    """Return the hostbud state represented by one provider hook event."""
    name = event.get("hook_event_name")
    if provider == "codex":
        if name == "SessionStart":
            return None if event.get("source") == "compact" else "blocked"
        if name == "SessionEnd":
            return "ended"
        if name in {"UserPromptSubmit", "PreToolUse", "PostToolUse"}:
            return "working"
        if name in {"PermissionRequest", "Stop", "Interrupt"}:
            return "blocked"
        return None

    if provider == "claude":
        if name == "SessionStart":
            return "blocked"
        if name == "SessionEnd":
            return "ended"
        if name in {"UserPromptSubmit", "UserPromptExpansion", "PreToolUse", "PostToolUse", "PostToolBatch"}:
            return "working"
        if name in {"PermissionRequest", "Stop", "StopFailure"}:
            return "blocked"
        if name == "Notification" and event.get("notification_type") in {
            "permission_prompt",
            "idle_prompt",
            "agent_needs_input",
        }:
            return "blocked"
    return None


def report(provider: str, event: dict, pane: Optional[str] = None) -> None:
    """Set a bounded tmux user option; hook failures never affect the client."""
    if provider not in {"codex", "claude"}:
        return
    pane = pane or os.environ.get("TMUX_PANE", "")
    status = status_for(provider, event)
    if status is None or not PANE_RE.fullmatch(pane):
        return
    try:
        subprocess.run(
            ["tmux", "set-option", "-p", "-q", "-t", pane, "@hostbud_agent_status", status],
            check=False,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=2,
        )
    except (OSError, subprocess.TimeoutExpired):
        pass


def main() -> int:
    if len(sys.argv) != 2:
        return 0
    try:
        raw = sys.stdin.buffer.read(1_048_577)
        if len(raw) > 1_048_576:
            return 0
        event = json.loads(raw)
        if isinstance(event, dict):
            report(sys.argv[1], event)
    except (json.JSONDecodeError, OSError, UnicodeDecodeError):
        pass
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
