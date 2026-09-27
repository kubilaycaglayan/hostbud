# hostbud v2 — Agent task queue (architecture decision)

Status: **design only**. Nothing here is implemented, and no start date is set. This document supersedes the v2 sketch in [ARCHITECTURE.md §10](../ARCHITECTURE.md#10-v2-readiness--orchestration) and the *v2* section of [ROADMAP.md](../ROADMAP.md). Those will be aligned with it in a follow-up. The v1 obligations in §10 still apply.

---

## 1. Context

The owner works on codebases whose plans are already split into milestones and hands them to coding agents (Claude Code, Codex) **one milestone per agent session**. One goal never covers several milestones. Today each next session is started by hand after the previous one finishes, which can take hours.

v2 adds a **queue**:

1. The owner creates a queue for a project and adds items in order, for example "M1", "M2", "M3". Each item has its own agent, flags and initial instruction, typically `/goal work on milestone 2 per docs/roadmap/M2-tasks.md`.
2. hostbud starts the first item in its own tmux session, running the agent with that instruction.
3. When hostbud detects that the item's goal is **achieved**, it starts the next item in a new session.
4. This repeats until the queue is empty, or until something needs the owner's attention.

The first v2 milestone is a **proof of concept** of exactly this: one queue, sequential. Everything after it is optional.

---

## 2. Research: how can hostbud tell an item is done?

Both clients the owner uses have a native `/goal` command. It keeps the agent working turn after turn until an evaluator judges a completion condition met.

**Claude Code** (checked on 2.1.x):
- `/goal <condition>` is a session-scoped, prompt-based `Stop` hook. After every turn a small model returns *not yet met*, *met* or *impossible*.
- On *met* or *impossible*, Claude Code clears the goal and writes a **structured top-level transcript record** (JSONL, one object per line):
  ```json
  {"type":"attachment","attachment":{"type":"goal_status","met":true,"condition":"…","reason":"…","durationMs":0,"iterations":0,"tokens":0}}
  ```
  Records with `met:false` and a `reason` are written for intermediate verdicts.
- Hooks: `SessionStart`, `Stop`, `SessionEnd`, `Notification`, and others. Every hook gets JSON on stdin with `session_id`, `transcript_path`, `cwd` and `hook_event_name`.
- Extra settings, including hooks, can be passed **per invocation** with `--settings '<json|file>'`, so user settings files are never touched.
- Unrecoverable errors (auth, credits, context overflow, model unavailable) clear the goal with a warning. Rate/usage limits *pause* it.

**Codex** (checked on 0.157.x):
- `/goal <objective>` stores one goal per thread. The model marks it done by calling the `update_goal` tool with `status: complete`.
- State lives in `$CODEX_HOME/goals_1.sqlite`:
  ```sql
  thread_goals(thread_id PK, goal_id, objective,
               status CHECK(status IN ('active','paused','blocked','usage_limited','budget_limited','complete')),
               token_budget, tokens_used, time_used_seconds, created_at_ms, updated_at_ms)
  ```
- Hooks are enabled by default: `SessionStart`, `SessionEnd`, `Stop`, `UserPromptSubmit`, `Pre/PostToolUse`, `PermissionRequest`, `Subagent*`, `Pre/PostCompact`, `Interrupt`.
  - Shared stdin fields: `session_id`, `cwd`, `hook_event_name`, `model`, `transcript_path`, `permission_mode` (plus `turn_id` for turn events).
  - Hooks can be declared inline under `[hooks]` in config, so they can be passed per run with `-c` overrides. Hook layers merge.
- Codex also has a single `notify` program setting, called on turn completion. hostbud does **not** use it: a per-run `-c notify=…` would *replace* the user's own notifier instead of adding to it.
- A known working pattern is a turn-end notification followed by a read-only look-up of `thread_goals.status` by thread id.

**Options considered**

| # | Option | Verdict |
|---|---|---|
| 1 | Agent hooks call hostbud; hostbud reads the client's structured goal state | **Chosen.** Uses the client's own judge, keeps sessions interactive, needs no install on the host. |
| 2 | Explicit contract: the instruction tells the agent to run `hostbud-signal done` | Rejected as the primary signal: it depends on model compliance. Any client could still adopt it later through the run protocol (§8). |
| 3 | Headless run (`claude -p`, `codex exec`); process exit = done | Rejected: runs must stay interactive. Codex `/goal` in `exec` is also not a stable contract. |
| 4 | Poll transcripts/state files without hooks | Rejected as primary: polling cost and no turn boundary. Hooks trigger the same reads instead. |
| 5 | tmux pane/process exit | Only a *failure* signal (session gone ⇒ needs attention), never a success signal. |
| 6 | LLM classifier on `capture-pane` output | Optional later (V2-M5), and it only flags runs, never advances the queue. |

**Risks of the hook approach and their mitigations**

| Risk | Mitigation |
|---|---|
| The text "achieved" or `goal_status` shows up in unrelated output (logs, tool results, file contents quoted in the transcript) | Never search text. Parse each transcript line as JSON and accept only **top-level** records of the exact shape above. Codex state comes from the SQLite row. |
| A stale or unrelated goal (an earlier session, another goal in the same thread) | Bind the run (§5.3): same session/thread id as captured by this run's `SessionStart` hook, record newer than run start, condition equal to the queued one. |
| Internal formats change between client versions | One adapter per client, with fixture tests per supported version; the client version is recorded per run. An unknown format means `unknown`, which means **needs attention**. |
| A hook never fires (crash, hooks disabled, untrusted workspace) | A stale-run timer, plus the tmux poller noticing the session is gone. Both lead to needs attention. |
| The owner clears or replaces the goal by hand | The condition no longer matches, so the queue does not advance. |
| The evaluator is talked into "met" | Accepted for the PoC (the owner's decision). Optional verify commands or approval come in V2-M4. |

---

## 3. Decisions

1. **Runs are interactive TUI sessions** in tmux, exactly like sessions the owner starts by hand. The owner can attach, watch and type at any time.
2. **A queue item is an agent, extra flags and an instruction.**
   - `agent` is `claude` or `codex`.
   - `flags` is free text, for example `--dangerously-skip-permissions`, `--yolo` or `--model …`. It is split into arguments and shell-quoted by `sshx`.
   - `instruction` is, in the PoC, a `/goal …` line.
   - hostbud builds the whole command, including hook injection, and passes the instruction as the client's **initial prompt argument**. There is no `send-keys` typing and no timing guesswork.
3. **A queue belongs to one project.** All its items run in that project's directory. The PoC has a single queue.
4. **Done means the client's own `/goal` was achieved.** There is no additional gate in the PoC.
5. **Detection is per-run hooks plus structured goal state.**
   - hostbud injects hooks for that run only (Claude `--settings`, Codex `-c`). The hooks forward the hook's stdin JSON to hostbud.
   - On each signal hostbud reads the goal state **read-only** from the host over `sshx`.
   - Nothing is installed on the host, and no user config (tmux, shell, agent settings, `~/.ssh`) is modified.
6. **Fail closed.** Only a bound, structured "achieved" record advances the queue. Anything else leads to `needs_attention`, and the queue pauses until the owner acts.
7. **hostbud never kills a run's session.** Finished sessions stay open until the owner closes them, which goes through the usual confirmation.
8. **Client adapters and a documented run protocol** (§7, §8) let other agent clients be added without changing the queue core.
9. **Everything after the PoC is opt-in.** Parallel queues, notifications, completion gates and the LLM supervisor are all off by default.

---

## 4. Components

```
 Queue panel (Vue) ── REST + events WS ──► api
                                           │
            ┌──────────────────────────────┼─────────────────────────┐
            ▼                              ▼                         ▼
      queue service                  hook endpoint             events bus ◄── inventory poller
   (CRUD, reorder, start/pause)   POST /api/hooks/:run_id        (sessions.changed)
            │                              │                         │
            └──────────► dispatcher ◄──────┴─────────────────────────┘
                         (state machine, one active run per queue)
                               │                 │
                    session-create service   agent adapter (claude | codex)
                    (tmux new-session -e …)  BuildCommand / ReadGoalState via sshx
```

- New packages: `internal/queue` (service and dispatcher) and `internal/agents` (adapters). SQL stays in `store`.
- Every state change publishes typed events: `queue.changed` and `run.changed`. The UI updates from events, never from polling.
- The dispatcher reacts to hook signals, `sessions.changed` and a stale timer. It adds no new poll loop against tmux.

---

## 5. Run lifecycle

### 5.1 Starting an item
1. The dispatcher takes the first `queued` item of a `running` queue that has no active run.
2. It creates a run with a fresh random token (32 bytes; only its SHA-256 is stored).
3. It calls the single session-create service:
   - `machine`: host;
   - `path`: the queue's project path;
   - `name`: `<project>-q<position>` (collisions are resolved as in v1);
   - `env`: `HOSTBUD_URL`, `HOSTBUD_RUN_ID`, `HOSTBUD_RUN_TOKEN`;
   - `startCommand`: `adapter.BuildCommand(item, run)`.
4. The run is `starting` until the `SessionStart` hook arrives. hostbud records that hook's `session_id` / thread id as the run's **binding**, and the run becomes `running`.

### 5.2 Signals
| Signal | Effect |
|---|---|
| `SessionStart` hook | Bind the agent session id; the run becomes `running`. A second `SessionStart` for the same run with a different id (for example a `/clear`) leads to `needs_attention`. |
| `Stop` hook (every turn end) | Update `last_signal_at`, then `adapter.ReadGoalState(binding)`: `achieved` ⇒ `achieved`; `failed` ⇒ `failed`; `pending` ⇒ still running; `unknown` ⇒ `needs_attention`. |
| `SessionEnd` hook | Read the goal state once more. If not achieved, the run is `exited`. |
| Session missing from `sessions.changed` | Same as `SessionEnd`. |
| No signal for `HOSTBUD_RUN_STALE_AFTER` (default 90 min) | `stale`. It only flags the run; nothing is killed. |

### 5.3 Binding: what counts as "achieved"
A goal record is accepted only if **all** of these hold:
- **Claude:**
  - it is a top-level record with `type == "attachment"`, `attachment.type == "goal_status"` and `attachment.met == true`;
  - it comes from the transcript of the bound `session_id`;
  - its timestamp is after `run.started_at`;
  - `attachment.condition` equals the queued condition (the instruction text after `/goal `, whitespace-normalized).
- **Codex:** the `thread_goals` row for the bound `thread_id` has `status == 'complete'`, `updated_at_ms` after `run.started_at`, and `objective` equal to the queued condition.
- `failed` means Claude's `met == false` "impossible" record, or Codex's `blocked`. `paused`, `usage_limited` and `budget_limited` stay `pending`. Codex usage limits resume on their own; a long pause then surfaces as `stale`.

### 5.4 State machines
```
queue:   idle ──start──► running ──(no queued items)──► finished
                           │  ▲
                 pause /   │  │ resume
          run not achieved ▼  │
                          paused

item:    queued ──► running ──► done
                       │
                       ├──► needs_attention ──retry──► queued (new run)
                       │                      ──skip───► skipped
                       │                      ──mark done──► done (owner override)
run:     starting ► running ► achieved | failed | exited | stale | cancelled
```
- Only `achieved` advances the queue.
- Every other terminal run state sets the item to `needs_attention` and the queue to `paused`.
- Pausing a queue never touches the running session: the current run continues, and no new item starts.
- An `achieved` record that arrives after a run went `stale` still marks the item `done`, but the queue stays paused until the owner resumes it.

---

## 6. Schema sketch (append-only migration)

```sql
queues(id, machine_id FK, project_id FK, name, status CHECK(status IN ('idle','running','paused','finished')),
       created_at, updated_at)
queue_items(id, queue_id FK, position, agent CHECK(agent IN ('claude','codex')), flags TEXT,
            instruction TEXT, status CHECK(status IN ('queued','running','done','needs_attention','skipped')),
            created_at, updated_at)
runs(id, item_id FK, machine_id, session_name, agent_session_id NULL, client_version NULL,
     token_hash, status, started_at, ended_at NULL, last_signal_at NULL, detail TEXT NULL)
run_events(id, run_id FK, source CHECK(source IN ('hook','poller','timer','user','llm')),
           kind, payload_json, created_at)
```

- `machine_id` is kept everywhere for multi-machine later.
- `run_events` stores the forwarded hook JSON with a size cap and with `transcript_path` kept. These are the audit trail for "why did the queue advance?".
- This replaces the `tasks`/`runs`/`machine_capacity` sketch in ARCHITECTURE §10. Capacity arrives with V2-M2.

---

## 7. Agent adapter interface

```go
type Adapter interface {
    Kind() string                                   // "claude", "codex"
    MinVersion() string
    BuildCommand(item Item, run Run) ([]string, error) // argv: client, user flags, hook injection, initial prompt
    ParseHook(event string, body []byte) (Binding, error) // session/thread id, transcript path
    ReadGoalState(ctx context.Context, m Machine, b Binding, run Run) (GoalState, error)
}
type GoalState struct {
    Status    string // achieved | pending | failed | unknown
    Condition string
    At        time.Time
    Reason    string
}
```

**Claude Code**
```
claude <flags> --settings '{"hooks":{"SessionStart":[…],"Stop":[…],"SessionEnd":[…]}}' '/goal <condition>'
```
- `ReadGoalState` reads the transcript at the bound `transcript_path` over SFTP, read-only and incrementally (it keeps a byte offset per run).
- The path must resolve under the remote user's `~/.claude/projects/`.

**Codex**
```
codex <flags> -c 'hooks.SessionStart=[…]' -c 'hooks.Stop=[…]' -c 'hooks.SessionEnd=[…]' '/goal <condition>'
```
- `ReadGoalState` does a read-only query of `thread_goals` by the bound thread id. Whether that happens through the host's `sqlite3`/`python3` or through the rollout JSONL at `transcript_path` is decided in the V2-M1 spike.

**Hook command** (identical for all events and clients; no file is installed on the host):
```sh
curl -fsS --max-time 5 -o /dev/null -X POST \
  -H "Authorization: Bearer $HOSTBUD_RUN_TOKEN" -H 'Content-Type: application/json' \
  --data-binary @- "$HOSTBUD_URL/api/hooks/$HOSTBUD_RUN_ID/<event>" || true
```
- It always exits 0 and prints nothing, so it can never block or steer the agent.
- The token is referenced as an env var. It never appears literally in the command line or the settings JSON.

---

## 8. Run protocol (for other agent clients)

Any agent client can join the queue if it can (a) be started with an initial prompt, (b) run a shell command on lifecycle events, and (c) expose a goal state that hostbud can read.

**Environment** (set on the tmux session by hostbud):

| Variable | Meaning |
|---|---|
| `HOSTBUD_URL` | Base URL reachable from the target. v1 host: `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}` (through Caddy on loopback). |
| `HOSTBUD_RUN_ID` | ULID of the run. |
| `HOSTBUD_RUN_TOKEN` | Per-run bearer token. Valid only for this run's endpoint, and revoked when the run ends. |

**Endpoint:** `POST $HOSTBUD_URL/api/hooks/$HOSTBUD_RUN_ID/<event>`
- `<event>` is one of `session_start`, `turn_end`, `session_end`. Clients map their own names: Claude `SessionStart`/`Stop`/`SessionEnd`, Codex the same.
- The body is the client's hook JSON, forwarded as is (maximum 64 KiB). It must contain the client's session id, and should contain `transcript_path`.
- Responses:
  - `204` if accepted;
  - `401` for a bad token;
  - `404` for an unknown run;
  - `410` if the run has ended;
  - `413` if the body is too large.
- The browser Origin check and the cookie session do not apply here. The bearer token is the only credential.

**Goal-state contract:** the adapter must be able to answer, for a bound session, "is the goal with this exact condition achieved, and when?" from **structured state** written by the client (a typed transcript record, a database row or a status file). Printed text must never be matched.

**Adding a client:** implement `Adapter`, add fixture files for each supported client version (achieved, pending, failed, decoy text containing the marker words), add an e2e stub binary, and document the flags and minimum version.

Fallback for clients without a readable goal state: an explicit `session_end` / `turn_end` payload of `{"hostbud_goal":{"status":"achieved","condition":"…"}}`, sent by a client-side command. This is optional, and deliberately not used for Claude or Codex.

---

## 9. Security

- Hook tokens are 32 random bytes and stored as SHA-256. Comparison is constant-time. A token is scoped to one run and revoked at run end. Tokens are never logged.
- The hook endpoint is exempt from browser-Origin and cookie auth, so it is rate-limited per run and body-capped. It only writes `run_events` and triggers adapter reads; it cannot run commands.
- `transcript_path` from a hook body is untrusted. It must be absolute, cleaned, and under the client's data directory of the remote user. It is opened read-only through SFTP.
- Commands are built from argv via the single `sshx` helper (shell-quoted). User flags are split and quoted, never interpolated.
- Known limitation: `tmux new-session -e HOSTBUD_RUN_TOKEN=…` briefly shows the token in the host's process list. This is acceptable on a single-user host. The spike checks whether `set-environment` fed through stdin avoids it.
- The existing rule holds: no destructive tmux action without owner confirmation. The dispatcher only ever *creates* sessions.

---

## 10. Milestones

**V2-M1 — Proof of concept: one queue, sequential** (default behavior)
- **Prerequisite:** fix the ROADMAP *Later* bug "session start command fails to create the session", since runs depend on that path.
- **T1 spike (first):** on this host, with both clients, verify:
  - per-run hook injection (`--settings` / `-c hooks…`);
  - a slash command as the initial prompt argument (`claude '/goal …'`, `codex '/goal …'`);
  - `SessionStart` id capture;
  - the goal-record formats, including the decoy-text case;
  - the Codex reader;
  - the tmux env/token exposure.
  Record the results in this document before building.
- **Build:**
  - migration and store;
  - hook endpoint and tokens;
  - Claude and Codex adapters with fixtures;
  - dispatcher and state machine;
  - queue REST API and events;
  - a minimal **Queue panel**: pick project, add/edit/delete/reorder items, Start/Pause, Retry/Skip/Mark done, per-item status, and open a run's session in a tab.
- **E2E:** stub `claude`/`codex` executables on the throwaway target take the hook settings, POST hook calls and write fixture goal records. No real agent runs in tests. Scenarios:
  - two items run in order;
  - decoy text does not advance the queue;
  - failed/exited/stale leads to needs attention and a paused queue;
  - retry and skip;
  - panel operation on desktop and phone.
- **Accept:** with a real Claude item followed by a real Codex item (an owner check), the second starts only after the first's `/goal` is achieved, and never on decoy output.

**V2-M2 — Multiple queues and parallel runs** (opt-in): several queues, each still sequential; an optional per-machine cap on concurrent runs.

**V2-M3 — Notifications** (opt-in): browser/push notifications on item done, needs attention and queue finished.

**V2-M4 — Completion gates** (opt-in, per item): a verify command run by hostbud in the project directory (exit 0 required), and/or manual approval, before advancing.

**V2-M5 — LLM stale-run supervisor** (opt-in): classifies `capture-pane` output for runs without a signal. It can flag a run, never advance it. The provider is pluggable, as in ARCHITECTURE §10.

---

## 11. Open questions (settle in the V2-M1 spike)

1. Does Codex accept inline hooks through `-c hooks.<Event>=[…]` in interactive mode, and do they merge with the user's `~/.codex` hooks?
2. Do both TUIs execute a slash command passed as the initial prompt argument?
3. Do per-invocation `--settings` hooks run in a workspace Claude Code hasn't trusted yet? If not, the owner must trust the project once; hostbud reports that as an actionable error.
4. Codex reader: host `sqlite3`/`python3` (a dependency on the host) or the rollout JSONL (a format to verify)?
5. Does Claude's transcript record carry a timestamp, or must hostbud use line order plus hook arrival time?
6. Stale window default: is 90 minutes right for long goal turns with background work?
