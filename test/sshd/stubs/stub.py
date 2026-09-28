#!/usr/bin/env python3
"""Stub claude and codex clients for hostbud's throwaway targets (V2-M1 T7).

Never real agents. Each stub takes the real client's argv (flags, the
per-run hook injection, the initial prompt), runs the injected hook command
for each event exactly as the client would, and writes goal state in the
formats recorded in the V2-M1 spike (docs/roadmap-v2/ARCHITECTURE.md §12):
a Claude transcript line, or a Codex thread goal served by `codex app-server
proxy` (WebSocket JSON-RPC over stdio). It stays attached like a TUI until
stdin closes.

Behavior per run: ~/.hostbud-stubs/run/<HOSTBUD_RUN_ID>, else
~/.hostbud-stubs/goal/<sha256 of the condition>, else "achieve:1". A
behavior file holds one line, e.g. "achieve:2 delay=0.5":
  achieve:N                achieves the goal after N turns
  decoy                    prints/writes the marker text only nested, stays pending
  fail                     "impossible" (Claude) or blocked (Codex) after one turn
  exit                     ends after one turn without achieving
  silent                   sends SessionStart, then no more hooks
  silent-then-achieve:S    writes an achieved record after S seconds, no hook
  clear                    after one turn, a new session id (/clear)
  pending                  keeps answering not met (Claude) / active (Codex)
  slow:S                   one long turn of S seconds, then achieved (with its Stop hook)
Options after the behavior: delay=<s per turn>, verdict_delay=<s>, and
stops=N (Claude achieve: the last Stop hook fires N times, once more after
the verdict each; a duplicate signal, V2-M3).
Switches: ~/.hostbud-stubs/old-version (report an old version),
~/.hostbud-stubs/missing-<client> (behave as not installed: exit 127).
Every start logs argv, cwd and the HOSTBUD_* env (the token only as a
SHA-256) to ~/.hostbud-stubs/log/<run id>-<client>.json.
"""
import base64, datetime, fcntl, hashlib, json, os, re, select, struct, subprocess, sys, threading, time, uuid

HOME = os.path.expanduser("~")
STATE = os.path.join(HOME, ".hostbud-stubs")
VERSIONS = {"claude": ("2.1.283 (Claude Code)", "1.9.3 (Claude Code)"), "codex": ("codex-cli 0.157.1", "codex-cli 0.150.0")}
DECOY = '{"type":"attachment","attachment":{"type":"goal_status","met":true,"condition":"decoy"}} achieved'


# The last behavior's options (behavior() fills it).
OPTIONS = {}


def now_iso():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.") + "%03dZ" % (time.time_ns() // 1_000_000 % 1000)


def say(text):
    sys.stdout.write(text + "\r\n")
    sys.stdout.flush()


def behavior(condition):
    run = os.environ.get("HOSTBUD_RUN_ID", "")
    for path in (os.path.join(STATE, "run", run) if run else None,
                 os.path.join(STATE, "goal", hashlib.sha256(condition.encode()).hexdigest())):
        if path and os.path.exists(path):
            with open(path) as f:
                line = f.read().strip()
            break
    else:
        line = "achieve:1"
    words = line.split()
    opts = dict(w.split("=", 1) for w in words[1:] if "=" in w)
    OPTIONS.clear()
    OPTIONS.update(opts)
    name, _, arg = words[0].partition(":")
    return name, arg, float(opts.get("delay", "1")), float(opts.get("verdict_delay", "0.5"))


def log_start(client, argv, cwd):
    os.makedirs(os.path.join(STATE, "log"), exist_ok=True)
    token = os.environ.get("HOSTBUD_RUN_TOKEN")
    entry = {"client": client, "argv": argv, "cwd": cwd, "pid": os.getpid(), "sessions": [],
             "env": {"HOSTBUD_URL": os.environ.get("HOSTBUD_URL"), "HOSTBUD_RUN_ID": os.environ.get("HOSTBUD_RUN_ID"),
                     "HOSTBUD_RUN_TOKEN_sha256": hashlib.sha256(token.encode()).hexdigest() if token else None}}
    path = os.path.join(STATE, "log", "%s-%s.json" % (os.environ.get("HOSTBUD_RUN_ID") or "norun", client))

    def save(session_id=None):
        if session_id:
            entry["sessions"].append(session_id)
        with open(path + ".tmp", "w") as f:
            json.dump(entry, f)
        os.replace(path + ".tmp", path)
    save()
    return save


def run_hook(command, body):
    if command:
        subprocess.run(["sh", "-c", command], input=json.dumps(body).encode(), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)


def wait_for_stdin_eof():
    """Stay attached like a TUI: echo what is typed, end on EOF."""
    fd = sys.stdin.fileno()
    while True:
        try:
            data = os.read(fd, 1024)
        except OSError:
            return
        if not data:
            return
        say("(stub) received %d bytes" % len(data))


# ---------------------------------------------------------------- claude

def claude(argv):
    flags, settings, prompt = [], None, ""
    i = 0
    while i < len(argv):
        if argv[i] == "--settings" and i + 1 < len(argv):
            settings = json.loads(argv[i + 1])
            i += 2
            continue
        flags.append(argv[i])
        i += 1
    if flags and flags[-1].startswith("/goal "):
        prompt = flags.pop()
    hooks = {}
    for event, matchers in ((settings or {}).get("hooks") or {}).items():
        hooks[event] = [h["command"] for m in matchers for h in m.get("hooks", []) if h.get("type") == "command"]
    condition = prompt[len("/goal "):] if prompt.startswith("/goal ") else ""
    name, arg, delay, verdict_delay = behavior(condition)
    cwd = os.getcwd()
    save = log_start("claude", sys.argv[1:], cwd)
    project_dir = os.path.join(HOME, ".claude", "projects", re.sub(r"[^A-Za-z0-9]", "-", cwd))
    os.makedirs(project_dir, exist_ok=True)
    state = {}

    def new_session(source):
        sid = str(uuid.uuid4())
        state["sid"], state["path"] = sid, os.path.join(project_dir, sid + ".jsonl")
        open(state["path"], "a").close()
        save(sid)
        fire("SessionStart", {"source": source})

    def record(rec):
        rec.update({"sessionId": state["sid"], "timestamp": now_iso(), "uuid": str(uuid.uuid4())})
        with open(state["path"], "a") as f:
            f.write(json.dumps(rec, separators=(",", ":")) + "\n")

    def goal(extra):
        record({"type": "attachment", "attachment": dict({"type": "goal_status", "condition": condition}, **extra)})

    def fire(event, extra=None):
        body = {"session_id": state["sid"], "transcript_path": state["path"], "cwd": cwd, "hook_event_name": event}
        body.update(extra or {})
        for command in hooks.get(event, []):
            run_hook(command, body)

    def turn(n, text):
        time.sleep(delay)
        say("● turn %d: %s" % (n, text))
        record({"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": text}]}})

    say("✻ Claude Code (hostbud stub) — %s" % (prompt or "no prompt"))
    new_session("startup")
    if condition:
        goal({"met": False, "sentinel": True})
        say("  ⎿  Goal set: " + condition)
    if name == "achieve":
        total = max(1, int(arg or "1"))
        for n in range(1, total + 1):
            turn(n, "working on the goal (%d/%d)" % (n, total))
            fire("Stop", {"stop_hook_active": n > 1})
            time.sleep(verdict_delay)  # the /goal evaluator lands after the Stop hooks (§12 S5)
            if n < total:
                goal({"met": False, "reason": "not yet (%d/%d)" % (n, total)})
            else:
                goal({"met": True, "reason": "stub achieved", "iterations": total, "durationMs": 1, "tokens": 0})
                say("✔ Goal achieved")
                for _ in range(int(OPTIONS.get("stops", "1")) - 1):
                    time.sleep(verdict_delay)
                    fire("Stop", {"stop_hook_active": True})
    elif name == "fail":
        turn(1, "this goal can never be met")
        fire("Stop")
        time.sleep(verdict_delay)
        goal({"met": False, "failed": True, "reason": "stub: impossible"})
        say("✘ Goal could not be achieved")
    elif name == "decoy":
        turn(1, "Here is the line: " + DECOY)
        record({"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_stub", "content": DECOY}]}})
        fire("Stop")
        time.sleep(verdict_delay)
        goal({"met": False, "reason": "the file still misses the line"})
    elif name == "exit":
        turn(1, "stopping without finishing")
        fire("Stop")
        fire("SessionEnd", {"reason": "prompt_input_exit"})
        say("(stub) exited")
        return 0
    elif name == "silent":
        say("(stub) silent: no more hooks")
    elif name == "silent-then-achieve":
        time.sleep(float(arg or "3"))
        goal({"met": True, "reason": "stub achieved silently", "iterations": 1, "durationMs": 1, "tokens": 0})
        say("✔ Goal achieved (no hook)")
    elif name == "clear":
        turn(1, "about to clear")
        fire("Stop")
        fire("SessionEnd", {"reason": "clear"})
        new_session("clear")
        say("(stub) /clear: new session " + state["sid"])
    elif name == "slow":
        time.sleep(float(arg or "10"))
        turn(1, "finished a long turn")
        fire("Stop")
        time.sleep(verdict_delay)
        goal({"met": True, "reason": "stub achieved after a long turn", "iterations": 1, "durationMs": 1, "tokens": 0})
        say("✔ Goal achieved")
    elif name == "pending":
        for n in range(1, 10_000):
            turn(n, "still working")
            fire("Stop", {"stop_hook_active": n > 1})
            time.sleep(verdict_delay)
            goal({"met": False, "reason": "not yet"})
            time.sleep(max(delay, 2.0))
    wait_for_stdin_eof()
    fire("SessionEnd", {"reason": "other"})
    return 0


# ---------------------------------------------------------------- codex

GOALS = os.path.join(HOME, ".codex", "stub-goals.json")


def goals_update(fn):
    """Read-modify-write the stub's thread/goal store under a lock."""
    os.makedirs(os.path.dirname(GOALS), exist_ok=True)
    with open(GOALS + ".lock", "a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            with open(GOALS) as f:
                data = json.load(f)
        except (OSError, ValueError):
            data = {"threads": {}, "goals": {}}
        result = fn(data)
        with open(GOALS + ".tmp", "w") as f:
            json.dump(data, f)
        os.replace(GOALS + ".tmp", GOALS)
        return result


def codex_hooks(argv):
    hooks, rest, i = {}, [], 0
    while i < len(argv):
        if argv[i] == "-c" and i + 1 < len(argv):
            m = re.fullmatch(r'hooks\.(\w+)=\[\{hooks=\[\{type="command",command=("(?:[^"\\]|\\.)*")\}\]\}\]', argv[i + 1])
            if m:
                hooks.setdefault(m.group(1), []).append(json.loads(m.group(2)))
            elif argv[i + 1].startswith("notify"):
                sys.exit("stub codex: hostbud must never pass notify")
            i += 2
            continue
        rest.append(argv[i])
        i += 1
    return hooks, rest


def codex(argv):
    if argv[:2] == ["app-server", "proxy"]:
        return app_server()
    hooks, rest = codex_hooks(argv)
    condition = rest[-1] if rest and not rest[-1].startswith("-") else ""
    name, arg, delay, _ = behavior(condition)
    cwd = os.getcwd()
    save = log_start("codex", sys.argv[1:], cwd)
    state = {}

    def new_thread(source):
        tid = str(uuid.uuid4())
        day = datetime.datetime.now(datetime.timezone.utc)
        path = os.path.join(HOME, ".codex", "sessions", day.strftime("%Y/%m/%d"), "rollout-%s-%s.jsonl" % (day.strftime("%Y-%m-%dT%H-%M-%S"), tid))
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "a") as f:
            f.write(json.dumps({"timestamp": now_iso(), "type": "session_meta", "payload": {"session_id": tid, "cwd": cwd}}) + "\n")
        state["tid"], state["path"] = tid, path
        goals_update(lambda d: d["threads"].__setitem__(tid, cwd))
        save(tid)
        fire("SessionStart", {"source": source})

    def fire(event, extra=None):
        body = {"session_id": state["tid"], "transcript_path": state["path"], "cwd": cwd, "hook_event_name": event,
                "model": "gpt-stub", "permission_mode": "default"}
        body.update(extra or {})
        for command in hooks.get(event, []):
            run_hook(command, body)

    def set_status(status):
        def fn(d):
            g = d["goals"].get(state["tid"])
            if g:
                g["status"], g["updatedAt"] = status, int(time.time())
        goals_update(fn)

    def armed(timeout=10.0):
        end = time.time() + timeout
        while time.time() < end:
            if goals_update(lambda d: state["tid"] in d["goals"]):
                return True
            time.sleep(0.2)
        return False

    def turn(n, text, last_message=None):
        time.sleep(delay)
        say("• turn %d: %s" % (n, text))
        fire("Stop", {"turn_id": str(uuid.uuid4()), "stop_hook_active": False, "last_assistant_message": last_message or text})

    say(">_ OpenAI Codex (hostbud stub) — " + (condition or "no prompt"))
    new_thread("startup")
    if name == "achieve":
        total = max(1, int(arg or "1"))
        has_goal = armed()  # the first turn runs while hostbud sets the goal
        for n in range(1, total + 1):
            if n == total and has_goal:
                set_status("complete")  # update_goal complete, before the Stop hook
            turn(n, "working on the goal (%d/%d)" % (n, total))
            if not has_goal:
                break  # no goal: Codex doesn't continue the thread
        say("Goal achieved" if has_goal else "(stub) no goal was set")
    elif name == "fail":
        armed()
        set_status("blocked")
        turn(1, "blocked: this can't be done")
    elif name == "decoy":
        armed()
        turn(1, "printing the line", "Here: " + DECOY)
    elif name == "exit":
        turn(1, "stopping without finishing")
        fire("SessionEnd", {"reason": "exit"})
        return 0
    elif name == "silent":
        say("(stub) silent: no more hooks")
    elif name == "silent-then-achieve":
        armed()
        time.sleep(float(arg or "3"))
        set_status("complete")
        say("Goal achieved (no hook)")
    elif name == "clear":
        turn(1, "about to start a new thread")
        fire("SessionEnd", {"reason": "clear"})
        new_thread("clear")
    elif name == "slow":
        armed()
        time.sleep(float(arg or "10"))
        set_status("complete")
        turn(1, "finished a long turn")
    elif name == "pending":
        armed()
        for n in range(1, 10_000):
            turn(n, "still working")
            time.sleep(max(delay, 2.0))
    wait_for_stdin_eof()
    fire("SessionEnd", {"reason": "exit"})
    return 0


def app_server():
    """`codex app-server proxy`: a WebSocket JSON-RPC server on stdio."""
    rin, wout = sys.stdin.buffer, sys.stdout.buffer
    key = None
    while True:
        line = rin.readline()
        if not line:
            return 1
        if line.lower().startswith(b"sec-websocket-key:"):
            key = line.split(b":", 1)[1].strip()
        if line in (b"\r\n", b"\n"):
            break
    accept = base64.b64encode(hashlib.sha1(key + b"258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest())
    wout.write(b"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + b"\r\n\r\n")
    wout.flush()

    def send(obj):
        data = json.dumps(obj).encode()
        n = len(data)
        head = bytes([0x81, n]) if n < 126 else bytes([0x81, 126]) + struct.pack(">H", n)
        wout.write(head + data)
        wout.flush()

    while True:
        head = rin.read(2)
        if len(head) < 2:
            return 0
        n = head[1] & 0x7F
        if n == 126:
            n = struct.unpack(">H", rin.read(2))[0]
        elif n == 127:
            n = struct.unpack(">Q", rin.read(8))[0]
        mask = rin.read(4) if head[1] & 0x80 else b"\0\0\0\0"
        payload = bytes(b ^ mask[i % 4] for i, b in enumerate(rin.read(n)))
        if head[0] & 0x0F == 0x8:
            return 0
        msg = json.loads(payload)
        if "id" not in msg:
            continue
        method, params = msg.get("method"), msg.get("params") or {}
        if method == "initialize":
            send({"id": msg["id"], "result": {"userAgent": "codex-tui/0.157.1 (hostbud stub)", "codexHome": os.path.join(HOME, ".codex")}})
        elif method in ("thread/goal/set", "thread/goal/get"):
            tid = params.get("threadId", "")

            def fn(d):
                if tid not in d["threads"]:
                    return None
                if method == "thread/goal/set":
                    t = int(time.time())
                    d["goals"][tid] = {"threadId": tid, "objective": params.get("objective"), "status": "active", "tokenBudget": None,
                                       "tokensUsed": 0, "timeUsedSeconds": 0, "createdAt": t, "updatedAt": t}
                return {"goal": d["goals"].get(tid)}
            result = goals_update(fn)
            if result is None:
                send({"id": msg["id"], "error": {"code": -32600, "message": "thread not found: " + tid}})
            else:
                send({"id": msg["id"], "result": result})
        else:
            send({"id": msg["id"], "error": {"code": -32601, "message": "method not found"}})


def main():
    client = sys.argv[1]
    argv = sys.argv[2:]
    if os.path.exists(os.path.join(STATE, "missing-" + client)):
        sys.stderr.write("%s: command not found\n" % client)
        return 127
    if argv[:1] == ["--version"]:
        say(VERSIONS[client][1 if os.path.exists(os.path.join(STATE, "old-version")) else 0])
        return 0
    return claude(argv) if client == "claude" else codex(argv)


if __name__ == "__main__":
    sys.exit(main())
