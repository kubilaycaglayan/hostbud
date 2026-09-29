# hostbud v2 — Agent task queue (architecture decision)

Status: **V2-M1–V2-M6 implemented** (M2–M6 opt-in; full browser suites run on demand). See the per-milestone acceptance checklists for verification status and the open owner checks. This document superseded the v2 sketch in the v1 [ARCHITECTURE.md §10](../ARCHITECTURE.md#10-v2--agent-task-queue) and the *v2* section of the v1 [ROADMAP.md](../ROADMAP.md), which now point here. The v1 obligations in v1 §10 still apply. The V2-M1 spike results are in §12.

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
10. **Queue dependencies are opt-in per queue (V2-M6).** A queue may name a currently active hostbud-tracked run as its predecessor. It remains queued until that run's structured goal is achieved; stale predecessors can still achieve late. A failed, exited or cancelled predecessor pauses the dependent queue. Unlinked queues retain existing behavior. A manually started tmux session cannot be selected because hostbud has no tracked goal binding for it.

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
- **Notifier (V2-M3, `internal/notify`, opt-in per account).** The dispatcher builds one payload (`notify.Build`) for item done, needs attention and queue finished while any account is on. It rides on `queue.changed` for the open app, and, with VAPID keys set, the same transition writes one `notification_outbox` row per opted-in account whose event choice matches and one `notification_deliveries` row per subscription of that account, **in the transition's own transaction** (`TransitionQueueItemNotify`, `TransitionQueueNotify`; a signal that loses the guarded update writes nothing, and `UNIQUE(user_id, dedupe_key)` catches the rest). A single sender goroutine, woken after each commit, claims each delivery in a committed transaction before its POST (at most once: after a restart unclaimed rows are sent, claimed ones never), encrypts it (RFC 8291 `aes128gcm`, VAPID RFC 8292) and sends it with bounded retries; 404/410 delete the subscription. With every account off nothing new runs: no payload, no outbox row, no request.

---

## 5. Run lifecycle

### 5.1 Starting an item
1. The dispatcher takes the first `queued` item of a `running` queue that has no active run.
2. It creates a run with a fresh random token (32 bytes; only its SHA-256 is stored).
3. It calls the single session-create service:
   - `machine`: host;
   - `path`: the queue's project path;
   - `name`: `<project>-q<position>` for the project's first (oldest) queue, and `<project>-<queue>-q<position>` for any other (V2-M2), each part sanitized as a tmux name. Collisions are resolved as in v1; a duplicate reported by `new-session` retries the next suffix (bounded). The name is stored on the run, so renaming a queue never renames a run's session;
   - `env`: `HOSTBUD_URL`, `HOSTBUD_RUN_ID`, `HOSTBUD_RUN_TOKEN`;
   - `startCommand`: `adapter.BuildCommand(item, run)`.
4. The run is `starting` until the `SessionStart` hook arrives. hostbud records that hook's `session_id` / thread id as the run's **binding**, and the run becomes `running`.
5. The adapter **arms** the goal if the client needs it (§7, `Arm`). Codex doesn't run a slash command passed as its initial prompt (§12 S2), so hostbud sets the goal through Codex's app-server `thread/goal/set` right after binding. Claude needs nothing: its `/goal` runs from the argument.

### 5.2 Signals
| Signal | Effect |
|---|---|
| `SessionStart` hook | Bind the agent session id; the run becomes `running`. A second `SessionStart` for the same run with a different id (for example a `/clear`) leads to `needs_attention`. |
| `Stop` hook (every turn end) | Update `last_signal_at`, then `adapter.ReadGoalState(binding)`: `achieved` ⇒ `achieved`; `failed` ⇒ `failed`; `pending` ⇒ still running; `unknown` ⇒ `needs_attention`. A `pending` read is **followed up** at +2 s, +5 s, +15 s, +30 s and +60 s after the hook (cancelled by the next signal or a terminal state): Claude's `/goal` evaluator is itself a `Stop` hook running in parallel, and writes its verdict about 2 s *after* hostbud's hook fires (§12 S5). |
| `SessionEnd` hook | Read the goal state once more. If not achieved, the run is `exited`. A `SessionEnd` with `reason: "clear"` (Claude `/clear`, §12 S4) gets the detail "the agent session was cleared (/clear) — the goal can't be tracked". |
| Session missing from `sessions.changed` | Same as `SessionEnd`. |
| No signal for `HOSTBUD_RUN_STALE_AFTER` (default 2 h, §12 S9) | One last `ReadGoalState` (an `achieved` record is taken as usual), otherwise `stale`. It only flags the run; nothing is killed. |

### 5.3 Binding: what counts as "achieved"
A goal record is accepted only if **all** of these hold:
- **Claude:**
  - it is a top-level record with `type == "attachment"`, `attachment.type == "goal_status"` and `attachment.met == true`;
  - it comes from the transcript of the bound `session_id`;
  - its top-level `timestamp` is after `run.started_at` (every record carries an ISO-8601 `timestamp` and `sessionId`, §12 S5);
  - `attachment.condition` equals the queued condition (the instruction text after `/goal `, whitespace-normalized).
- **Codex:** the goal of the bound thread, read with `thread/goal/get`, has `status == 'complete'`, `createdAt` (seconds) not before `run.started_at` (hostbud set it in this run), and `objective` equal to the queued condition.
- Claude writes `{"met":false,"sentinel":true,…}` when a goal is set; that is `pending`.
- `failed` means Claude's `{"met":false,"failed":true,…}` "impossible" record, or Codex's `blocked`. `paused`, `usage_limited` and `budget_limited` stay `pending`. Codex usage limits resume on their own; a long pause then surfaces as `stale`.

### 5.4 State machines
```
queue:   idle ──start──► running ──(no queued items)──► finished
                           │  ▲
                 pause /   │  │ resume
          run not achieved ▼  │
                          paused

item:    queued ──► running ──► done
                       │  └─(achieved, gated; V2-M4)─► verifying ──► awaiting_approval ──approve──► done
                       │                                  │                 │
                       ├──► needs_attention ◄──(verify ≠ 0)┘       (reject)─┘
                       │    needs_attention ──retry──► queued (new run)
                       │                    ──skip───► skipped
                       │                    ──mark done──► done (owner override)
                       │                    ──re-run verify──► verifying (V2-M4)
run:     starting ► running ► achieved | failed | exited | stale | cancelled
```
- Only `achieved` advances the queue.
- Every other terminal run state sets the item to `needs_attention` and the queue to `paused`.
- Pausing a queue never touches the running session: the current run continues and is still tracked (an `achieved` marks its item done), and no new item starts.
- Owner overrides (Retry, Skip, Mark done) apply only to `needs_attention` items. An item whose run is still active (a `stale` run) has that run ended as `cancelled` first: its token is revoked and its session stays open. The queue stays paused until Resume; a retried item gets a new run, token and collision-suffixed session.
- Restart safety: on start the dispatcher reloads active runs, re-arms their stale timers from `last_signal_at` (or `started_at`), reads each bound run once, and advances running queues that have no active run. A run whose session is missing from the first inventory is handled like a `SessionEnd`.
- An `achieved` record that arrives after a run went `stale` still marks the item `done`, but the queue stays paused until the owner resumes it.
- A stale run sends no more hooks until its turn ends, so the owner's **Start or Resume reads each stale run of the queue once** before advancing: a goal it achieved without a hook is picked up then.
- A run hostbud can no longer track ends as **`exited`** with a `detail`, and its session stays open: a second `SessionStart` with a different id, a `/clear`, or an `unknown` goal-state format.

### 5.5 Parallel queues and run slots (V2-M2, opt-in)
With parallel queues on (the Queue panel's switch, stored in `machine_capacity.parallel_queues`; `HOSTBUD_PARALLEL_QUEUES` is the default until it is set) a project may have several queues (names unique per project, case-insensitive) and they run at once; each queue stays strictly sequential. Off (the default), V2-M1's one-queue limit and paths are unchanged; queues left over from switch-on stay listed, but one may start or resume only while no other queue is running or has an active run (409 naming the switch). No run is ever cancelled by the switch.
- **Slots.** An optional per-machine cap (`machine_capacity.max_concurrent_runs`, 1–32, NULL or no row = no cap, set in Settings) limits active runs (`starting`, `running`, `stale`). A stale run holds its slot because its session is alive; only a late achieved or an owner action (Retry, Skip, Mark done, which cancel it) frees it. `failed`/`exited` free it at once.
- **One decision point.** Slots are handed out only on the dispatcher goroutine (`dispatch`), and the store re-checks the cap when it creates each run: `CreateRunInSlot` takes a per-machine advisory lock, re-counts active runs and refuses (`ErrNoSlot`) at the cap. Concurrent hooks, owner actions and timers can't exceed it.
- **FIFO.** `queues.waiting_since` is set when a queue starts or resumes and again when its previous run ends, always behind every queue already in line (so equal timestamps still put it at the back), and cleared when it gets a run. A free slot goes to the running queue with the oldest `waiting_since` (ties by id); with a cap of 1 queues take turns and none starves.
- **Waiting reason.** Derived on every read, never stored: the head item of a running queue with no active run while the cap is reached is `waitingForSlot` ("waiting for a free slot"). The dispatcher publishes `queue.changed` whenever that changes; a cap change publishes it for every queue.
- **Cap changes.** Lowering the cap stops nothing (new runs wait); raising or clearing it dispatches at once.
- **Restart.** Active runs are rows, so the count after a restart includes them before any dispatch; `waiting_since` keeps the order. A running queue without one (a V2-M1 row) joins the back.
- **Shared directory.** When two busy queues (running, or paused with an active run) resolve to the same cleaned project path, both carry a `shared_directory` warning (start/resume responses, `GET /api/queues`, `queue.changed`). It never blocks.

### 5.6 Completion gates (V2-M4, opt-in per item)
An item may have a **verify command** (`queue_items.verify_command`, NULL = none) and/or **require approval** (`requires_approval`, default false). With neither, `achieved` ⇒ `done` in one transaction exactly as before (same events, notifications, slot release and hand-off).
- **Gates are item states; the run stays `achieved`.** `achieved` ⇒ `verifying` (if set) ⇒ `awaiting_approval` (if set) ⇒ `done`. The run's token is revoked at `achieved` and no stale timer runs on a gated item. A late achieved after stale enters the gates too; its queue stays paused.
- **Verify runs from argv through `sshx`, never through tmux.** The command is split like flags (quotes, no expansion); shell operators are literal words, so pipelines need an explicit `sh -c '…'`. hostbud runs it itself, as the host user, in its own `sshx` call: a preflight checks the project directory and coreutils' `timeout`, then a fixed `sh -c` script `cd`s into the directory and `exec`s `timeout -k 10s <HOSTBUD_VERIFY_TIMEOUT> <argv…>`, each word passed as a quoted argument. The local deadline is the timeout + 15 s.
- **Outcomes.** Exit 0 ⇒ the approval gate or `done`. Non-zero ("verify failed (exit N)"), timeout ("verify timed out after 10m"), a missing project directory, a missing `timeout` and an SSH failure ⇒ `needs_attention` with an actionable detail (also the run's `detail`), and the queue pauses.
- **Output.** stdout and stderr combined, kept as a 16 KiB tail by a ring buffer (never held whole), invalid UTF-8 replaced and ANSI and control bytes (except newline and tab) stripped. It is stored in the `verify_result` run event and shown as text in the item view (`verify`: attempt, running, outcome, exit code, duration, `truncated`, output). Info logs carry the item, attempt, exit code and duration only.
- **Claim before run.** Entering `verifying` and the `verify_started` event (`source='verify'`, attempt n) commit in one transaction before the call; `verify_result` and the next state commit together. On restart, an attempt that started without a result isn't re-run: the item needs attention ("hostbud restarted during verify — Re-run verify"). A verifying item without an open attempt starts one, once.
- **Approval.** `awaiting_approval` is stored state; nothing re-runs and no timer arms. `POST /api/queue-items/{id}/approve` ⇒ `done` and the queue advances; `…/reject` ⇒ `needs_attention` ("rejected by <account>"), queue paused. Both record a `source='user'` event with the account id.
- **Owner actions.** `POST /api/queue-items/{id}/reverify` (Re-run verify) applies to a needs-attention item whose latest run achieved and that has a verify command: a new attempt on the same run, no new session. Retry, Skip and Mark done keep V2-M1's rules (`needs_attention` only; Mark done skips remaining gates); from `verifying` or `awaiting_approval` they return 409. Owner actions keep the queue paused until Resume.
- **Guarded transitions.** Every gate result and owner action is one `UPDATE … WHERE status = <expected>`; the loser gets 409 naming the current state.
- **Queue and slots.** While an item is `verifying` or `awaiting_approval` its queue stays `running` and waits (the next item doesn't start); pausing lets the gate finish and starts nothing. `verifying` holds the queue's run slot (a command runs on the host); `awaiting_approval` frees it. `waiting_since` is set when the item leaves its gates.
- **Notifications (V2-M3).** `done` is sent when the item becomes `done`, never at `achieved`. `awaiting_approval` and a failed verify notify as needs attention (`on_attention`), with outcomes `awaiting_approval` and `verify_failed` and keys `run:<id>:approval` and `run:<id>:verify:<attempt>`. The payload allowlist is unchanged: never the command or its output.
- **Edits.** Queued items edit as before. A needs-attention item may change only its gates (a PATCH without `agent`, `flags` or `instruction`), to fix a verify command before Re-run verify. `running`, `verifying` and `awaiting_approval` refuse every edit (409). A queue with a `verifying` item can't be deleted.

### 5.7 LLM stale-run supervisor (V2-M5, opt-in)
The supervisor is constructed only when `HOSTBUD_LLM_PROVIDER=openai` and its configuration validates. It scans at a bounded interval for the latest `running` or `stale` run whose item is respectively `running` or `needs_attention`; it never reads `starting`, ended or gated items. A running run is due after `HOSTBUD_LLM_QUIET_AFTER` (default 20m) since its last hook or start, and a newly stale run is due immediately. Before capture, a transaction records `llm_started` and enforces `HOSTBUD_LLM_MAX_PER_RUN_HOUR` (default 2); unresolved claims after restart become `unknown`, never a repeated provider call.
- It captures `tmux capture-pane -p -J -t '=<session>:' -S -200` through `sshx` argv, caps output at 256 KiB and keeps 200 lines. It strips ANSI/control bytes, scrubs common secret patterns by default (`HOSTBUD_LLM_SCRUB=true`), and sends at most the last 8 KiB to the provider. This is an explicit privacy boundary: pane text leaves the host. Raw pane text and raw provider output are memory-only and never logged or stored.
- A strict structured response is one of `running`, `waiting_input`, `blocked`, `completed`, `failed`, or `unknown`, plus a one-line reason. Only a guarded `run_events(source='llm')` insert can be made; the run, item, queue, gates and slots are never updated. A result that races a hook, run end or owner action is discarded. `completed` is shown as “Looks finished — check and Mark done” and never advances the queue. Provider calls are bounded to 20 seconds per attempt, at most three attempts and 60 seconds overall; failures become `unknown`.

---

## 6. Schema sketch (append-only migration)

```sql
queues(id, machine_id FK, project_id FK, name, status CHECK(status IN ('idle','running','paused','finished')),
       created_at, updated_at, after_run_id TEXT NULL)
queue_items(id, queue_id FK, position, agent CHECK(agent IN ('claude','codex')), flags TEXT,
            instruction TEXT, status CHECK(status IN ('queued','running','done','needs_attention','skipped')),
            created_at, updated_at)
runs(id, item_id FK, machine_id, session_name, agent_session_id NULL, transcript_path NULL,
     transcript_offset BIGINT NULL, client_version NULL, token_hash BYTEA UNIQUE,
     status CHECK(status IN ('starting','running','achieved','failed','exited','stale','cancelled')),
     started_at, ended_at NULL, last_signal_at NULL, detail TEXT NULL)
run_events(id, run_id FK, source CHECK(source IN ('hook','poller','timer','user','llm')),
           kind, payload_json, created_at)
```

- `machine_id` is kept everywhere for multi-machine later, including `queue_items`; composite foreign keys keep every row on its parent's machine.
- Implemented as `internal/store/migrations/0005_queues.sql` (V2-M1 T2). Design additions: `runs.transcript_path` (the bound transcript) and `runs.transcript_offset` (bytes read so far, for incremental reads, §7); `token_hash` is the 32-byte SHA-256; `run_events.payload_json` has a 64 KiB CHECK; `UNIQUE(queue_id, position)`; indexes on `runs(item_id)`, `runs(status)` and `run_events(run_id, created_at)`.
- Nothing cascades: deleting a queue deletes its events, runs, items and row in one explicit transaction, and is refused while a run is active. A project with a queue can't be deleted (409) until its queue is.
- `run_events` stores the forwarded hook JSON with a size cap and with `transcript_path` kept. These are the audit trail for "why did the queue advance?".
- This replaces the `tasks`/`runs`/`machine_capacity` sketch in ARCHITECTURE §10.
- V2-M2 adds `internal/store/migrations/0006_parallel_queues.sql` (additions only): `machine_capacity(machine_id PK FK, max_concurrent_runs INT NULL CHECK 1–32, updated_at)`, `queues.waiting_since TIMESTAMPTZ NULL` and the unique index `queues_project_name` on `(project_id, lower(name))` (§5.5).
- V2-M4 adds `internal/store/migrations/0009_completion_gates.sql`: `queue_items.verify_command TEXT NULL` (1–4096 bytes; NULL = none) and `requires_approval BOOLEAN NOT NULL DEFAULT false`, and it widens `queue_items.status` (+ `verifying`, `awaiting_approval`) and `run_events.source` (+ `verify`), each CHECK replaced in one `DROP CONSTRAINT … ADD CONSTRAINT` statement. The new sets are strict supersets: existing rows stay valid and nothing is updated (§5.6).
- V2-M6 adds `internal/store/migrations/0010_queue_goal_dependency.sql`: nullable `queues.after_run_id`, identifying an active hostbud-tracked goal run selected when the queue is created. It intentionally has no foreign key: if the source run is deleted, its successor keeps the identifier and fails closed instead of starting. `afterRunStatus` in queue views is derived from the current run row, not stored. The `POST /api/queues` body accepts optional `afterRunId`; empty or omitted keeps existing behavior.
- V2-M3 adds `internal/store/migrations/0008_notifications.sql` (additions only): `notification_prefs(user_id PK FK, enabled DEFAULT false, on_done, on_attention, on_finished DEFAULT true)` (no row = off), `push_subscriptions(id, user_id, endpoint UNIQUE, p256dh, auth, created_at)`, `notification_outbox(id, user_id, dedupe_key, payload_json ≤ 1 KiB, created_at, UNIQUE(user_id, dedupe_key))` and `notification_deliveries(outbox_id, subscription_id, status, claimed_at, finished_at, PK(outbox_id, subscription_id))`. Outbox and delivery rows are operational and pruned after 7 days.

---

## 7. Agent adapter interface

```go
type Adapter interface {
    Kind() string                                   // "claude", "codex"
    MinVersion() string
    CheckVersion(ctx context.Context, machine string) (string, error) // via the login shell; actionable error
    BuildCommand(item store.QueueItem, run store.Run) ([]string, error) // argv: client, user flags, hook injection, initial prompt
    ParseHook(event string, body []byte) (Binding, error)              // session/thread id, transcript path, source/reason
    Arm(ctx context.Context, machine string, b Binding, run store.Run, condition string) error // after binding; Claude: no-op
    ReadGoalState(ctx context.Context, machine string, b Binding, run store.Run, condition string) (GoalState, error)
}
type GoalState struct {
    Status    string // achieved | pending | failed | unknown
    Condition string
    At        time.Time
    Reason    string
    Offset    int64  // transcript bytes read so far (stored as runs.transcript_offset)
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
codex <flags> -c 'hooks.SessionStart=[{hooks=[{type="command",command="…"}]}]' -c 'hooks.Stop=[…]' -c 'hooks.SessionEnd=[…]' '<condition>'
```
- The initial prompt is the **plain condition**, not `/goal …`: Codex sends an argument prompt to the model as a user message and never runs it as a slash command (§12 S2).
- `Arm` sets the goal on the bound thread with the app-server JSON-RPC call `thread/goal/set {threadId, objective}`, through `codex app-server proxy` (the CLI already on the host relays stdio to the running daemon's control socket; hostbud speaks WebSocket-framed JSON-RPC over that pipe). Codex's own goal loop then continues the thread at turn end until the model calls `update_goal` with `complete`. If the first turn ends before the goal is set, the run stays `pending` and surfaces as `stale`.
- `ReadGoalState` calls `thread/goal/get {threadId}` the same way (§12 S7): no `sqlite3`/`python3` dependency on the host, and it's the daemon's authoritative state. Read-only.

**Host commands** (version checks, the Codex proxy) run through the user's login shell, like start commands (`"$SHELL" -lic '<argv>'`): the clients live on the login `PATH` (`~/.local/bin`, an npm prefix), not on the bare SSH `PATH`. Output before the expected data (rc-file noise) is skipped.

**Hook command** (identical for all events and clients; no file is installed on the host):
```sh
curl -fsS --max-time 5 -o /dev/null -X POST \
  -H "Authorization: Bearer $HOSTBUD_RUN_TOKEN" -H 'Content-Type: application/json' \
  --data-binary @- "$HOSTBUD_URL/api/hooks/$HOSTBUD_RUN_ID/<event>" || true
```
- It always exits 0 and prints nothing, so it can never block or steer the agent. It needs `curl` on the host.
- The command text is the same for every run (only env references), so Codex's one-time hook trust (§12 S3) covers all later runs.
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
  - `413` if the body is too large;
  - `400` if the body isn't JSON;
  - `429` if the run's hook rate limit is exceeded (a token bucket per run; nothing is recorded).
- The browser Origin check, the cookie session, the Tailscale identity gate and the generic JSON body limits do not apply here (one named exemption, `tokenAuthRoutes` in `internal/api`, marked `token_auth` in the route table). The bearer token is the only credential.
- Checks run in this order: unknown event or run → 404, bad token → 401, ended run → 410, rate limit → 429, body → 413, not JSON → 400. The rate limit is a token bucket per run: 60 a minute, burst 20.

**Goal-state contract:** the adapter must be able to answer, for a bound session, "is the goal with this exact condition achieved, and when?" from **structured state** written by the client (a typed transcript record, a database row or a status file). Printed text must never be matched.

**Adding a client:** implement `Adapter`, add fixture files for each supported client version (achieved, pending, failed, decoy text containing the marker words), add an e2e stub binary, and document the flags and minimum version.

Fallback for clients without a readable goal state: an explicit `session_end` / `turn_end` payload of `{"hostbud_goal":{"status":"achieved","condition":"…"}}`, sent by a client-side command. This is optional, and deliberately not used for Claude or Codex.

---

## 9. Security

- Hook tokens are 32 random bytes and stored as SHA-256. Comparison is constant-time. A token is scoped to one run and revoked at run end. Tokens are never logged.
- The hook endpoint is exempt from browser-Origin and cookie auth, so it is rate-limited per run and body-capped. It only writes `run_events` and triggers adapter reads; it cannot run commands.
- `transcript_path` from a hook body is untrusted. It must be absolute, cleaned, and under the client's data directory of the remote user. It is opened read-only through SFTP.
- Commands are built from argv via the single `sshx` helper (shell-quoted). User flags are split and quoted, never interpolated.
- The token never appears in a process's argv: hostbud creates run sessions with `tmux source-file -`, feeding the `new-session … -e HOSTBUD_RUN_TOKEN=…` command line through **stdin** (§12 S8). With `-e` on the command line it was visible in the `ssh` client's argv (container processes show in the host's process list) for the length of the call.
- The existing rule holds: no destructive tmux action without owner confirmation. The dispatcher only ever *creates* sessions.
- **Notifications (V2-M3).** One builder (`internal/notify`) makes every notification, in-app and push alike: `{v, kind, key, project, position, outcome, url, title, body}`, where `title` and `body` are made only from `project`, `position` and `outcome`, and the whole payload is at most 1 KiB. Never instruction text, flags, paths, session names, run `detail`, pane output, tokens or emails. `url` is an app path (`/queues/<id>?item=<id>`); clients open it only as a same-origin path. The key (`run:<id>:done`, `run:<id>:attention`, `queue:<id>:finished:<last run id>`) is the `Notification` tag. The payload rides on `queue.changed` only while at least one account has notifications on.
- **Push endpoints (V2-M3).** A subscription endpoint must be `https`, on port 443, with a DNS host name (no IP literal, no single-label name, no credentials); the sender's client dials only public addresses (not loopback, private, link-local or the tailnet's 100.64.0.0/10), follows no redirects and is bounded at 10 s. `HOSTBUD_PUSH_TEST_ENDPOINT` (e2e only) exempts one prefix. Info logs name the subscription id and status, never the endpoint. The subscribe, unsubscribe and test routes act on the caller's own account only; an endpoint another account subscribed moves to the caller (a shared device follows who is signed in) and loses the old account's pending deliveries.

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

## 11. Open questions (answered by the V2-M1 spike, see §12)

1. Does Codex accept inline hooks through `-c hooks.<Event>=[…]` in interactive mode, and do they merge with the user's `~/.codex` hooks? **Answer (S1):** yes, `-c 'hooks.<Event>=[{hooks=[{type="command",command="…"}]}]'`, and they merge with `hooks.json` (both run). They need a one-time trust (S3).
2. Do both TUIs execute a slash command passed as the initial prompt argument? **Answer (S2):** Claude yes; Codex no. Codex gets the plain condition as its prompt and hostbud sets the goal with `thread/goal/set` (§7).
3. Do per-invocation `--settings` hooks run in a workspace Claude Code hasn't trusted yet? If not, the owner must trust the project once; hostbud reports that as an actionable error. **Answer (S3):** no: until the folder is trusted, Claude shows its trust dialog and fires no hook. A trusted parent folder counts. Codex also asks to trust the folder and, once, hostbud's hooks.
4. Codex reader: host `sqlite3`/`python3` (a dependency on the host) or the rollout JSONL (a format to verify)? **Answer (S7):** neither: `thread/goal/get` through `codex app-server proxy`.
5. Does Claude's transcript record carry a timestamp, or must hostbud use line order plus hook arrival time? **Answer (S5):** yes, a top-level ISO-8601 `timestamp` on every record.
6. Stale window default: is 90 minutes right for long goal turns with background work? **Answer (S9):** 2 h (the longest gap between stop attempts in real goal sessions was 75 min).

---

## 12. Spike results (V2-M1 T1, 2026-09-27)

Tested on the host with **Claude Code 2.1.283** and **Codex 0.157.1** (these become the adapters' `MinVersion()`), tmux 3.6, bash as the login shell. Runs used throwaway folders under an already-trusted parent (`/home/dev/dev/…`); Codex ran with a scratch `CODEX_HOME` holding a copy of the login, a harmless user hook, and a daemon started from a separate session first, like the real shared daemon. Paths and ids below are redacted or made up.

**User config.** Checksums of `~/.claude/settings.json` and `~/.codex/hooks.json` were unchanged. `~/.codex/config.toml` changed once during the spike, when the owner started a Codex session on the real `CODEX_HOME`; it was unchanged from that re-baseline to the end. No spike session touched the real Codex home (reads were read-only). No session was killed; the leftovers are listed in the V2-M1 summary.

| Check | Result |
|---|---|
| **S1** hook injection | **Claude:** `claude --settings '{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"…"}]}],"Stop":[…],"SessionEnd":[…]}}' …` works; the user's `settings.json` hooks still run too (4 hooks on one stop: user, hostbud, `/goal`). **Codex:** `-c 'hooks.SessionStart=[{hooks=[{type="command",command="…"}]}]'` (TOML inline table, the command as a basic string) works in the TUI and **merges** with the user's `hooks.json` (both ran on every `Stop`). The session's environment (`HOSTBUD_*`) reaches the hook commands of both clients, also for Codex, whose hooks run in its shared app-server daemon. |
| **S2** slash command as the prompt | **Claude:** `claude … '/goal <condition>'` runs `/goal` ("Goal set: …", a sentinel record) and works on it. **Codex:** `codex … '/goal <condition>'` is sent to the model as a plain user message; no goal row is created. **Alternative (keeps §3):** the prompt is the plain condition, and on `SessionStart` hostbud calls `thread/goal/set {threadId, objective}` over `codex app-server proxy`. Verified: the goal became `active`, Codex continued the thread in a second turn by itself, and the model marked it `complete`. Setting a goal on an **idle** thread does not start a turn. |
| **S3** workspace trust | **Claude:** in an untrusted folder the TUI shows "Quick safety check: … trust this folder?" and fires **no** hook, not even `SessionStart`; choosing "No, exit" fires none either. A folder under a trusted parent is trusted. hostbud can't see the dialog (no `capture-pane`), so a run stuck there stays `starting` until the stale timer, whose detail says: "No SessionStart hook arrived — the client may be waiting for you to trust the folder (or, for Codex, hostbud's hooks). Open the session to check." **Codex:** asks to trust the folder, then "Hooks need review: 3 hooks are new or changed" (Review / Trust all / Continue without trusting). Trust is saved by Codex in `config.toml` as `hooks.state."/<session-flags>/config.toml:<event>:0:0".trusted_hash`, keyed by event and command hash; hostbud's hook command is identical for every run, so the owner trusts it once. Hostbud never writes it. (`--dangerously-bypass-hook-trust` in the item's flags is the owner's alternative.) |
| **S4** `SessionStart` ids | **Claude** stdin: `session_id`, `transcript_path` (`/home/dev/.claude/projects/<cwd with / → ->/<session_id>.jsonl`), `cwd`, `hook_event_name`, `source` (`startup`), `model`; `Stop` adds `permission_mode`, `stop_hook_active`, `last_assistant_message`. `/clear` sends `SessionEnd` with `reason: "clear"` for the old id, then `SessionStart` with `source: "clear"` and a **new** id and transcript file. **Codex** stdin: `session_id` (equal to the goal's `threadId`), `transcript_path` (the rollout file under `$CODEX_HOME/sessions/YYYY/MM/DD/`), `cwd`, `hook_event_name`, `model`, `permission_mode`, `source`; `Stop` adds `turn_id`, `stop_hook_active`, `last_assistant_message`. Compaction was not exercised (owner check). |
| **S5** goal records | **Claude** writes one top-level record per verdict, each with `timestamp`, `sessionId`, `uuid`: goal set `{"type":"goal_status","met":false,"sentinel":true,"condition":…}`; achieved `{"met":true,"condition":…,"reason":…,"iterations":1,"durationMs":…,"tokens":…}`; impossible `{"met":false,"failed":true,"condition":…,"reason":…}` (the `failed` field tells it apart from not met). **Timing:** the verdict record is written about 1.7–2.1 s **after** hostbud's `Stop` hook fires (the evaluator is a parallel `Stop` hook), and after an achieved goal no further hook fires — hence the follow-up reads in §5.2. **Codex** goal (`thread/goal/get` or the `thread_goals` row): `status` went `active` → `complete`; `createdAt`/`updatedAt` are seconds over RPC (`*_at_ms` in SQLite). The row was `complete` before Codex's last `Stop` hook. Samples are the fixtures in `internal/agents/testdata/`. |
| **S6** decoy text | The agent printed and wrote `{"type":"attachment","attachment":{"type":"goal_status","met":true,…}} achieved`. It appeared only **nested** (inside `user`/`assistant` message content and tool results), never as a top-level record. The real achieved record of that goal carried the marker text inside its `condition`, which the condition-equality rule handles. |
| **S7** Codex reader | Chosen: **`thread/goal/get` over `codex app-server proxy`** (WebSocket-framed JSON-RPC: `initialize`, `initialized`, then the call). It needs nothing on the host besides Codex itself and reads the daemon's own state; a read-only call against the real daemon worked and changed nothing. Rejected: `sqlite3` (not on the host's standard `PATH`; only a copy from an SDK) and `python3` (a pyenv shim). A `sqlite3 'file:…?mode=ro'` read did see committed WAL rows while the daemon was writing, as a fallback note. |
| **S8** token exposure | With `tmux new-session -e HOSTBUD_RUN_TOKEN=…` on the command line, a `ps` sampler saw the token in the `ssh` client's argv (the container's processes are visible from the host) for the length of the call. With the same `new-session` line fed to **`tmux source-file -` on stdin**, the sampler never saw it, and the session's first process had the variable. hostbud uses the stdin method (§9). |
| **S9** stale window | Real Claude goal sessions: gaps between consecutive stop attempts p50 1.2 min, max 75 min; single turns up to 4 h exist, but a goal loop fires `Stop` at every stop attempt. Default **2 h**. |
| **S10** hook reachability | From a tmux session on the host, `curl http://127.0.0.1:9055/api/health` answered 200 through Caddy's loopback site (`/api/hooks/*` answered 403 before T3's exemption). `curl` is on the host. E2E: the target and Caddy share the `hostbud-e2e` network and the loopback site listens on all container interfaces, so `HOSTBUD_HOOK_BASE_URL=http://hostbud-e2e-caddy:9055`. |

**Other findings.**
- Over plain SSH, `claude`, `codex` and `node` aren't on the `PATH`. Start commands therefore run in the login shell (V2-M1 T0), and so do the adapters' host commands (§7).
- Claude's hook `transcript_path` is under the remote user's `~/.claude/projects/`; Codex's is under `$CODEX_HOME/sessions/`, but hostbud doesn't read it (S7).
- No client was dropped.
