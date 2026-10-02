#!/usr/bin/env python3
"""Forward Codex or Claude Code lifecycle events into a tmux pane option.

Claude Code runs hooks inside the client's pane, so TMUX_PANE names it. Codex
runs them in its shared app-server daemon, outside tmux; then the pane is found
from the provider client processes running in the event's cwd.
"""

import json
import os
import re
import subprocess
import stat
import sys
from typing import Optional


PANE_RE = re.compile(r"^%[0-9]+$")
SESSION_RE = re.compile(r"^[A-Za-z0-9-]{1,128}$")


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


def provider_clients(provider: str, proc: str = "/proc") -> list:
    """Return (pid, tty device number, real cwd) of interactive provider client processes."""
    try:
        pids = [name for name in os.listdir(proc) if name.isdigit()]
    except OSError:
        return []
    clients = []
    for pid in pids:
        try:
            with open(os.path.join(proc, pid, "stat"), encoding="utf-8", errors="replace") as f:
                stat = f.read()
            comm = stat[stat.index("(") + 1 : stat.rindex(")")]
            tty = int(stat[stat.rindex(")") + 2 :].split()[4])
            if comm != provider or tty == 0:
                continue
            clients.append((pid, tty, os.path.realpath(os.readlink(os.path.join(proc, pid, "cwd")))))
        except (OSError, ValueError, IndexError):
            continue
    return clients


def pane_ttys() -> dict:
    """Return tmux pane ids keyed by their tty device number."""
    try:
        out = subprocess.run(
            ["tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_tty}"],
            check=False,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=2,
        ).stdout
    except (OSError, subprocess.TimeoutExpired):
        return {}
    panes = {}
    for line in out.splitlines():
        pane, _, tty = line.partition("\t")
        try:
            if PANE_RE.fullmatch(pane):
                panes[os.stat(tty).st_rdev] = pane
        except OSError:
            continue
    return panes


def claims_path() -> str:
    base = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local", "state")
    return os.path.join(base, "hostbud", "agent-panes.json")


def claim_pane(session: str, cwd_clients: list, live: set, ended: bool, path: str) -> str:
    """Pick the client ("<pane>/<pid>") for a session in its cwd, remembering the choice.

    Codex gives hooks no pane or client identity, so a session keeps the client
    it was first matched to. A new session takes the only unclaimed client in its
    cwd, or the only client there at all; anything else is ambiguous and is skipped.
    """
    import fcntl

    os.makedirs(os.path.dirname(path), mode=0o700, exist_ok=True)
    with open(path + ".lock", "a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            with open(path, encoding="utf-8") as f:
                claims = json.load(f)
            if not isinstance(claims, dict):
                claims = {}
        except (OSError, ValueError):
            claims = {}
        claims = {k: v for k, v in claims.items() if isinstance(v, str) and v in live}
        client = claims.get(session, "")
        if client not in cwd_clients:
            free = [c for c in cwd_clients if c not in claims.values()]
            if len(free) == 1:
                client = free[0]
            elif len(cwd_clients) == 1:
                client = cwd_clients[0]
            else:
                client = ""
        if ended:
            claims.pop(session, None)
        elif client:
            claims = {k: v for k, v in claims.items() if v != client or k == session}
            claims[session] = client
        tmp = path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(claims, f)
        os.replace(tmp, path)
    return client.partition("/")[0]


def find_pane(provider: str, event: dict, status: str, path: Optional[str] = None) -> str:
    """Locate the pane for hooks run outside it, such as by Codex's app-server daemon."""
    cwd = event.get("cwd")
    if not isinstance(cwd, str) or not cwd.startswith("/"):
        return ""
    clients = provider_clients(provider)
    if not clients:
        return ""
    panes = pane_ttys()
    want = os.path.realpath(cwd)
    live = {f"{panes[tty]}/{pid}" for pid, tty, _ in clients if tty in panes}
    cwd_clients = sorted(f"{panes[tty]}/{pid}" for pid, tty, where in clients if tty in panes and where == want)
    only = cwd_clients[0].partition("/")[0] if len(cwd_clients) == 1 else ""
    session = event.get("session_id")
    if not cwd_clients or not isinstance(session, str) or not SESSION_RE.fullmatch(session):
        return only
    try:
        return claim_pane(session, cwd_clients, live, status == "ended", path or claims_path())
    except (OSError, ValueError):
        return only


def codex_usage(event: dict) -> str:
    """Read only the final 1 MiB of the hook's rollout; return numeric metadata.

    Never copy transcript text into tmux, logs or hostbud. Missing/unknown usage
    is empty, not zero. Cache and reasoning subtotals are already in total_tokens.
    """
    path = event.get("transcript_path")
    if not isinstance(path, str) or not os.path.isabs(path):
        return ""
    root = os.path.realpath(os.environ.get("CODEX_HOME") or os.path.expanduser("~/.codex"))
    path = os.path.realpath(path)
    if not any(path.startswith(os.path.join(root, folder) + os.sep) for folder in ("sessions", "archived_sessions")):
        return ""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as f:
            info = os.fstat(f.fileno())
            if not stat.S_ISREG(info.st_mode):
                return ""
            start = max(0, info.st_size - 1_048_576)
            f.seek(start)
            data = f.read(1_048_576)
        lines = data.split(b"\n")[:-1]  # ignore an in-progress final record
        if start:
            lines = lines[1:]  # first record may have been cut in half
        for line in reversed(lines):
            try:
                record = json.loads(line)
            except (ValueError, UnicodeDecodeError):
                continue
            if not isinstance(record, dict) or record.get("type") != "event_msg":
                continue
            payload = record.get("payload")
            if not isinstance(payload, dict) or payload.get("type") != "token_count":
                continue
            usage = payload.get("info")
            if not isinstance(usage, dict):
                continue  # rate-limit-only events have null info
            current = usage.get("last_token_usage")
            total = usage.get("total_token_usage")
            if not isinstance(current, dict) or not isinstance(total, dict):
                return ""
            values = [current.get("total_tokens"), total.get("total_tokens"), usage.get("model_context_window") or 0]
            if not all(type(n) is int and 0 <= n <= 9_007_199_254_740_991 for n in values):
                return ""
            return ",".join(str(n) for n in values)
    except (OSError, ValueError):
        pass
    return ""


MAX_COUNT = 9_007_199_254_740_991


def usage_cache_path() -> str:
    return os.path.join(os.path.dirname(claims_path()), "claude-usage.json")


def claude_usage(event: dict, cache_path: Optional[str] = None) -> str:
    """Return "context,total,0" for the main Claude Code conversation.

    Context is the last assistant message's input (including cache) and output
    tokens; total sums them over each distinct message. Claude Code repeats a
    message's usage on every content block, so a repeated id replaces its earlier
    value. Transcripts grow without bound, so only bytes appended since the last
    hook are read; per-transcript totals and offsets live in the state directory.
    Only counts leave this function. The context window is not reported (0).
    """
    path = event.get("transcript_path")
    if not isinstance(path, str) or not os.path.isabs(path):
        return ""
    root = os.path.realpath(os.environ.get("CLAUDE_CONFIG_DIR") or os.path.expanduser("~/.claude"))
    path = os.path.realpath(path)
    if not path.startswith(os.path.join(root, "projects") + os.sep) or not path.endswith(".jsonl"):
        return ""
    cache_path = cache_path or usage_cache_path()
    import fcntl

    try:
        os.makedirs(os.path.dirname(cache_path), mode=0o700, exist_ok=True)
        with open(cache_path + ".lock", "a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            try:
                with open(cache_path, encoding="utf-8") as f:
                    cache = json.load(f)
                if not isinstance(cache, dict):
                    cache = {}
            except (OSError, ValueError):
                cache = {}
            fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
            with os.fdopen(fd, "rb") as f:
                info = os.fstat(f.fileno())
                if not stat.S_ISREG(info.st_mode):
                    return ""
                state = cache.get(path)
                fresh = {"inode": info.st_ino, "offset": 0, "total": 0, "context": -1, "id": "", "value": 0}
                if not isinstance(state, dict) or set(state) != set(fresh) or state["inode"] != info.st_ino \
                        or not all(type(state[k]) is type(fresh[k]) for k in fresh) or state["offset"] > info.st_size:
                    state = fresh
                f.seek(state["offset"])
                for line in f:
                    if not line.endswith(b"\n"):
                        break  # an in-progress record is read again next time
                    state["offset"] += len(line)
                    if b'"usage"' not in line:
                        continue
                    try:
                        record = json.loads(line)
                    except (ValueError, UnicodeDecodeError):
                        continue
                    if not isinstance(record, dict) or record.get("type") != "assistant" or record.get("isSidechain"):
                        continue
                    message = record.get("message")
                    usage = message.get("usage") if isinstance(message, dict) else None
                    if not isinstance(usage, dict):
                        continue
                    counts = [usage.get(k, 0) for k in ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "output_tokens")]
                    if not all(type(n) is int and 0 <= n <= MAX_COUNT for n in counts):
                        continue
                    value = sum(counts)
                    message_id = message.get("id") if isinstance(message.get("id"), str) else ""
                    if message_id and message_id == state["id"]:
                        state["total"] -= state["value"]
                    state["total"] = min(state["total"] + value, MAX_COUNT)
                    state["context"], state["id"], state["value"] = value, message_id[:128], value
            cache[path] = state
            # Keep the cache small: drop transcripts that no longer exist.
            cache = {k: v for k, v in cache.items() if k == path or os.path.isfile(k)}
            tmp = cache_path + ".tmp"
            with open(tmp, "w", encoding="utf-8") as f:
                json.dump(cache, f)
            os.replace(tmp, cache_path)
    except (OSError, ValueError):
        return ""
    if state["context"] < 0:
        return ""
    return f'{state["context"]},{state["total"]},0'


def report(provider: str, event: dict, pane: Optional[str] = None) -> None:
    """Set a bounded tmux user option; hook failures never affect the client."""
    if provider not in {"codex", "claude"}:
        return
    status = status_for(provider, event)
    if status is None and not (provider == "codex" and event.get("source") == "compact"):
        return
    if not pane and not os.environ.get("TMUX_PANE"):
        try:
            pane = find_pane(provider, event, status or "working")
        except Exception:  # noqa: BLE001 - a hook must never disturb the client
            return
    pane = pane or os.environ.get("TMUX_PANE", "")
    if not PANE_RE.fullmatch(pane):
        return
    options = []
    if provider == "codex":
        options.append(("@hostbud_codex_usage", codex_usage(event)))
    elif status is not None:
        options.append(("@hostbud_claude_usage", claude_usage(event)))
    if status is not None:
        options.append(("@hostbud_agent_status", status))
    for name, value in options:
        set_pane_option(pane, name, value)


def set_pane_option(pane: str, name: str, value: str) -> None:
    try:
        subprocess.run(
            ["tmux", "set-option", "-p", "-q", "-t", pane, name, value],
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
