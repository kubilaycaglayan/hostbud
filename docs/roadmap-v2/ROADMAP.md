# hostbud v2 — Roadmap (agent task queue)

Status: **V2-M1 implemented** (acceptance: [V2-M1-acceptance.md](V2-M1-acceptance.md)); **V2-M2 implemented** (opt-in, `HOSTBUD_PARALLEL_QUEUES`, off by default; [V2-M2-tasks.md](V2-M2-tasks.md) · [V2-M2-acceptance.md](V2-M2-acceptance.md); its e2e run is on demand); **V2-M3 implemented** (opt-in per account, off by default; Web Push needs `HOSTBUD_VAPID_*`; [V2-M3-tasks.md](V2-M3-tasks.md) · [V2-M3-acceptance.md](V2-M3-acceptance.md); its e2e run is on demand); **V2-M4 implemented** (opt-in per item, both gates empty by default; `HOSTBUD_VERIFY_TIMEOUT`; [V2-M4-tasks.md](V2-M4-tasks.md) · [V2-M4-acceptance.md](V2-M4-acceptance.md); its e2e run is on demand); **V2-M5 implemented** (opt-in, empty provider is off; [V2-M5-tasks.md](V2-M5-tasks.md) · [V2-M5-acceptance.md](V2-M5-acceptance.md); full e2e run is on demand and owner checks remain open); **V2-M6 implemented** (opt-in per queue; [V2-M6-tasks.md](V2-M6-tasks.md) · [V2-M6-acceptance.md](V2-M6-acceptance.md); E2E run is on demand and owner review remains open); **V2-M7 implemented** (queue and item lifecycle timestamps; [V2-M7-tasks.md](V2-M7-tasks.md) · [V2-M7-acceptance.md](V2-M7-acceptance.md); E2E run and owner review remain open); **V2-M8 implemented** (durable delayed starts and per-item execution targets; [V2-M8-tasks.md](V2-M8-tasks.md) · [V2-M8-acceptance.md](V2-M8-acceptance.md); browser run and owner review remain open, cleanup skipped because hostbud toolbox containers were active); **V2-M9 implemented** (durable queue item history and History view; [V2-M9-tasks.md](V2-M9-tasks.md) · [V2-M9-acceptance.md](V2-M9-acceptance.md); browser run and owner review remain open, cleanup skipped because hostbud toolbox containers were active); **V2-M10 implemented** (plain prompts with per-agent completion tracking; [V2-M10-tasks.md](V2-M10-tasks.md) · [V2-M10-acceptance.md](V2-M10-acceptance.md); browser run and owner review remain open, cleanup skipped because hostbud toolbox and test containers were active); **V2-M11 implemented** (opt-in per queue: looping queues with a runtime limit; [V2-M11-tasks.md](V2-M11-tasks.md) · [V2-M11-acceptance.md](V2-M11-acceptance.md); browser run and owner review remain open). V2-M1 breakdown: [V2-M1-tasks.md](V2-M1-tasks.md) · [V2-M1-acceptance.md](V2-M1-acceptance.md). Design source of truth: [ARCHITECTURE.md](ARCHITECTURE.md) in this directory (cited below as **v2 §N**). The v1 documents are cited as **v1 ARCHITECTURE §N** ([../ARCHITECTURE.md](../ARCHITECTURE.md)) and v1 ROADMAP ([../ROADMAP.md](../ROADMAP.md)).

This roadmap replaces the *v2 — Orchestration* section of the v1 ROADMAP (V2.1–V2.5). That section and v1 ARCHITECTURE §10 were aligned with it in V2-M1 T12 and now point here.

---

## V2-M12 — Agent marks in queue items

**Goal.** Make each agent-backed queue item identifiable at a glance by showing its Claude Code or Codex mark in the agent picker and queue list. Breakdown: [V2-M12-tasks.md](V2-M12-tasks.md).

---

## V2-M11 — Looping queues with a runtime limit

**Goal.** A queue can loop: when its last item ends, it runs all its items again instead of finishing. It stops starting new passes once a configurable runtime since Start (default 5 h) has passed. Opt-in per queue, off by default. Breakdown: [V2-M11-tasks.md](V2-M11-tasks.md); design: v2 §3 decision 13.

---

## V2-M10 — Plain prompts and per-agent completion tracking

**Goal.** Let queue items use ordinary one-line prompts. Codex completion remains tied to Codex's native thread goal. Plain Claude prompts pause for owner review after a turn because Claude exposes no trusted completion status for an ordinary prompt. Existing Claude queue items beginning with `/goal ` retain their current behavior.

### T1 — Plain instruction and completion lifecycle
- Accept ordinary non-empty one-line instructions in UI and API; launch Claude with the prompt unchanged and retain legacy `/goal ` handling for existing items.
- Keep Codex's app-server thread goal lifecycle, using the plain instruction as its objective.
- On plain Claude `Stop` or `SessionEnd`, mark the run exited, move the item to `needs_attention`, pause its queue and show that the user must review and mark done or retry. Never treat a turn-end hook or model text as proof of success.
- **Tests:** U: validation and command construction accept plain instructions; dispatcher confirms turn end pauses and does not advance, while legacy Claude and Codex goal behavior remains; I: queue API accepts a plain prompt and integration exercises Claude turn-end handling.
- **E2E:** add a queue-panel scenario showing a plain Claude prompt, manual-review status and confirmed Mark done.

### T2 — Docs, acceptance, verification and deploy
Update README and v2 architecture, finish acceptance checklist, run `make lint test`, e2e TypeScript check, `make gitleaks`, and deploy/health check. Record owner review and on-demand e2e run as open.

### T3 — Safe Docker cleanup
Follow the v2 Rules and V1 M7 T15 after deploy; skip and record if any toolbox/test container is active.

---

## Rules (inherited from v1, apply to every v2 milestone)

- **Order.** Work milestone by milestone, task by task, top to bottom. A milestone starts only when the previous one meets its acceptance criteria. Open owner items don't count against that (AGENTS.md, *Owner items never block agents*).
- **Owner items never block.** Real-agent checks on the host, approvals and decisions are the owner's backlog. Take the safe default the docs name, record the item as **open** under the milestone's *Manual checks (owner)* and in the summary, and continue.
- **Breakdown files.** When a milestone is started, its tasks and checklist are written to `docs/roadmap-v2/V2-M<n>-tasks.md` and `docs/roadmap-v2/V2-M<n>-acceptance.md`, in the same format as `docs/roadmap/M*-tasks.md` / `M*-acceptance.md`. The task lists and criteria below are the starting point for those files.
- **Three test layers per criterion.** Every acceptance criterion has a coverage line with **U** (unit: Go with fakes, Vitest), **I** (integration: against `test/sshd` or the real deploy config) and **E** (e2e), each naming the task that writes it. **n/a** needs a one-line reason; "manual" only for what automation can't observe.
- **E2E scenarios are never deferred.** Every task has an **E2E:** line. Scenarios are written in the same commit as the behavior (API-level before the UI exists).
- **E2E runs only on demand** (owner's decision, 2026-09-27; replaces the earlier "once per milestone" rule). This is the default for every v2 planning file and every `V2-M*-tasks.md` / `V2-M*-acceptance.md`:
  - `make e2e` (and `e2e-up` / `e2e-run`) is **never** run while implementing a feature or working on a task, checkpoint or milestone, including its last task. The only e2e check per commit is that the suite type-checks (`tsc`);
  - a full run (both profiles, all v1 and v2 scenarios) happens only when the owner asks for one. After every full pass, create and commit a report under `docs/e2e-triage/`, and link it from the milestone's triage index before another full run. Record its command/commit, counts, duration, every failed test title/profile, error summary and classification. Batch-fix the whole inventory with focused checks; the next full run again waits for the owner's request;
  - a milestone's definition of done never waits on a run. E items in its checklist count as written (type-checked) until an on-demand run passes them; a pending run is listed as open in the summary.
- **Docker cleanup at the end of every milestone** (owner's request, 2026-09-27). Each v2 milestone's **last task** is a *Safe Docker cleanup*, after the milestone's deploy. It follows v1 M7 T15's procedure ([../roadmap/M7-tasks.md](../roadmap/M7-tasks.md#t15--safe-docker-cleanup)):
  - first check nothing is in use (a `make`/e2e/build/restore run from any session, a busy toolbox container). If something is, skip, record "cleanup skipped: <reason>" in Progress and the summary, and don't force it;
  - clean only with the repo's own `make docker-clean` (without `CACHE=1`): the e2e stack and its images, the toolbox containers, dangling `hostbud.image=1` images, plus the milestone's own clean worktrees and restore-check leftovers. When a milestone adds e2e services or images (stubs, `hostbud-e2e-pushfake`, `hostbud-e2e-llmfake`), the recipe is extended to remove them;
  - never prune globally (`docker system/volume/network/builder prune`), never touch the production containers, their images, the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data`, `hostbud-caddy-config`, `backups/`, other projects' objects, the toolbox images or the warm `test/sshd` targets, and never close a tmux session;
  - verify afterwards that the production stack is healthy (`/api/health`) and the volumes are intact, and report the reclaimed space in the summary.
- **No real agents in automated tests.** e2e and integration use **stub `claude` / `codex` executables** on the throwaway target (v2 §10). Real Claude Code and Codex runs are owner checks.
- **Host safety** (v1 rules, still binding):
  - hostbud never kills a run's session (v2 §3.7); closing one goes through the existing confirmation dialog;
  - nothing is installed on the host, and no user config is changed: tmux, shell, `~/.claude`, `~/.codex`, `~/.ssh` (v2 §3.5);
  - migrations are append-only and never drop user data;
  - every remote command goes through `sshx` from argv (v2 §9).
- **Public repo.** No real hostnames, paths, usernames or tokens in docs, fixtures or tests. Client fixtures use `/home/dev`, `example.com`, `server-a`. Run `make gitleaks` before every commit.
- **Everything after V2-M1 is opt-in** (v2 §3.9). Each opt-in milestone ships switched off by default, and switching it off again restores V2-M1 behavior.
- **V2-M9 exception, explicitly requested by the owner:** metadata history recording is always on because durable storage is the requested behavior. It does not change queue dispatch; the History view is read-only.
- **V2-M10 exception, explicitly requested by the owner:** plain one-line prompts become the default queue instruction. Codex uses its native thread goal status; plain Claude prompts require manual review at turn end. Existing Claude items prefixed with `/goal ` retain their prior behavior.

---

## Preconditions (before V2-M1 starts)

| # | Precondition | Why |
|---|---|---|
| P1 | v1 through M7 is done (M7's full e2e run is on demand and may still be open). M8 may still be in progress. | v2 relies on the M7 hardening (timeouts, CSP, headers). |
| P2 | v1 obligations in v1 ARCHITECTURE §10 still hold: (1) typed events on the `events` bus, (2) one session-create service accepting `env` and `startCommand`, (3) dialect-agnostic store with append-only migrations, (4) room for token-authenticated `/api/hooks/*` that bypass the browser Origin check. | v2 §4 and §5.1 build directly on them. V2-M1 T0 re-checks (2). |
| P3 | The v1 ROADMAP *Later* bug "session start command fails to create the session" is fixed and verified. | Runs are created through exactly that path (v2 §10). This is V2-M1 **T0**. |

---

## V2-M1 — Proof of concept: one queue, sequential

**Default behavior** (not opt-in). Scope: v2 §1–§9 and §10 *V2-M1*.

**Goal.** The owner creates one queue for a project, adds items (agent, flags, `/goal …` instruction), and presses Start. hostbud runs each item in its own interactive tmux session. It starts the next item only when the client's own `/goal` evaluator has recorded the queued goal as achieved, bound to that run. Anything else pauses the queue and asks for attention.

**Out of scope for V2-M1:** several queues or parallel runs (V2-M2), notifications (V2-M3), verify commands and approval (V2-M4), LLM classification (V2-M5), multi-machine, and clients other than Claude Code and Codex.

### Tasks

#### T0 — Prerequisite: fix "session start command fails to create the session"
- Reproduce the v1 ROADMAP *Later* bug on the e2e target first, then on the host (create a session with a start command from the UI).
- Find the root cause in the session-create path (`tmux new-session … <command>` built by `sshx`: quoting, the path, the shell used for the command, or the session exiting at once because the command ends).
- Fix it inside the **single session-create service**, so every entry point benefits. Confirm the service accepts `{machine, name, path, env, startCommand}`, and that `env` becomes `tmux new-session -e KEY=VALUE` (v1 obligation 2). Add `env` support if it's missing.
- Close the *Later* item in the v1 ROADMAP.
- **Tests:** U: command construction for start commands with spaces, quotes and flags; I: against `test/sshd`, a session created with a start command exists, runs in the given path, and the command's output is visible in `capture-pane`; E: see below.
- **E2E:** add *Session with start command* (desktop and phone): create a session with a start command in a chosen folder; the session appears under its project and the command's output shows in the terminal.

#### T1 — Spike on this host (first; no product code)
Run each check with **both** real clients, in a scratch directory the owner does not use for work. The spike creates sessions but never kills one: sessions it made are listed in the summary for the owner to close.

| Check | Answers | What to record |
|---|---|---|
| S1 Per-run hook injection: Claude `--settings '<json>'`, Codex `-c 'hooks.<Event>=[…]'` | v2 §11 Q1 | Exact working syntax for `SessionStart`, `Stop`, `SessionEnd`. Whether Codex inline hooks **merge** with the user's `~/.codex` hooks and don't replace them. That the user's settings files are unchanged afterwards (checksum before and after). |
| S2 Slash command as the initial prompt: `claude '/goal …'`, `codex '/goal …'` | v2 §11 Q2 | Whether both TUIs execute `/goal` from the argument, and that the goal is active afterwards. |
| S3 Workspace trust | v2 §11 Q3 | Whether `--settings` hooks run in a directory Claude Code hasn't trusted yet (and the Codex equivalent). If not, the exact actionable message hostbud shows. |
| S4 `SessionStart` id capture | v2 §5.1 step 4 | The fields in each client's hook stdin (`session_id`, `transcript_path`, `cwd`, …), captured with a local `nc`/file sink. What a `/clear` or resume does to the id. |
| S5 Goal-record formats | v2 §2, §5.3, §11 Q5 | Real Claude `goal_status` lines (met, not met, impossible). Whether they carry a timestamp. Codex `thread_goals` rows through `complete`, `blocked`, `paused`, `usage_limited`. The client versions used. |
| S6 Decoy text | v2 §2 risks | Have the agent print or quote `{"type":"attachment","attachment":{"type":"goal_status","met":true,…}}` and "achieved". Confirm it appears only **nested** (inside tool results or message content), never as a top-level record. |
| S7 Codex reader | v2 §7, §11 Q4 | Host `sqlite3`/`python3` read-only query (`file:…?mode=ro`, WAL behavior while Codex writes) **or** the rollout JSONL at `transcript_path`. Pick one and give the reasons. Note the host dependency if any. |
| S8 tmux env / token exposure | v2 §9 | Whether `tmux new-session -e HOSTBUD_RUN_TOKEN=…` shows up in `ps`, and for how long. Whether `set-environment` fed through stdin (or a `source-file` from stdin) avoids it. |
| S9 Stale window | v2 §11 Q6 | The longest turn gap seen in real goal runs with background work. Confirm or change the 90-minute default for `HOSTBUD_RUN_STALE_AFTER`. |
| S10 Hook reachability | v2 §8 | From a shell on the host, `curl` to `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` through Caddy works. From the e2e target, the equivalent base URL works (see T3). |

- **Output:** a new section **"12. Spike results"** in [ARCHITECTURE.md](ARCHITECTURE.md), with one entry per check, the client versions tested, and the chosen answer to each §11 question. Any design change goes into v2 §5/§7/§9 in the same commit. Redact all host-specific values: `/home/dev` paths and made-up ids only.
- **If a check fails:** record it and write the alternative into v2 §7 before T2. The alternative must keep the §3 decisions (interactive TUI, no host install, no user config changes, no `send-keys` typing). If no such alternative exists for one client, **safe default:** V2-M1 continues with the other client only. The dropped client is recorded as an open owner decision.
- **Tests:** n/a (research only; the recorded formats become fixtures in T5/T6).
- **E2E:** none (nothing reachable); the recorded formats drive the stub clients in T7.

#### T2 — Schema migration and store
- One append-only migration creating `queues`, `queue_items`, `runs`, `run_events` as in v2 §6. Include:
  - `machine_id` on every table;
  - CHECK constraints for status, agent and source;
  - `UNIQUE(queue_id, position)`;
  - indexes on `runs(item_id)`, `runs(status)` and `run_events(run_id, created_at)`.
- `runs.id` is a ULID (v2 §8). `runs.token_hash` holds SHA-256 only.
- `run_events.payload_json` is stored with a size cap (64 KiB, the same as the hook body cap). `transcript_path` is kept (v2 §6).
- Store methods, the only SQL (archtest keeps SQL in `store`):
  - queue CRUD;
  - item CRUD and transactional reorder;
  - run create and status transitions (guarded `UPDATE … WHERE status = <expected>`, so concurrent signals can't double-advance);
  - run lookup by id, by token hash and by active item;
  - event append;
  - the transcript byte offset per run (v2 §7, incremental read) — a nullable `runs` column added in this same migration, recorded as a design addition in v2 §6.
- The superseded `tasks`/`machine_capacity` sketch from v1 ARCHITECTURE §10 is **not** created.
- **Tests:** U: status CHECKs reject bad values, reorder keeps positions dense, guarded transitions refuse a wrong source state; I: migration applies on a copy of a v1 database with data and keeps every v1 row (append-only check); E: n/a (no endpoint yet).
- **E2E:** none reachable yet; covered from T3 (hooks) and T8 (queue API).

#### T3 — Run tokens and the hook endpoint
- Token: 32 random bytes, base64url. Only the SHA-256 is stored. Compared in constant time. Scoped to one run.
- Revoked when the run is **finished with**: `achieved`, `failed`, `exited`, `cancelled`. It is **not** revoked on `stale`, because a late `achieved` must still be accepted (v2 §5.4).
- `POST /api/hooks/{run_id}/{event}` with `event ∈ {session_start, turn_end, session_end}` (v2 §8):
  - responses `204` / `401` / `404` / `410` / `413`, exactly as in v2 §8;
  - body is capped at 64 KiB and must be JSON;
  - exempt from the cookie session, the Origin allowlist and the Tailscale identity gate — the bearer token is the only credential;
  - rate-limited per run;
  - writes only a `run_events` row (`source='hook'`) and notifies the dispatcher. It never runs a command (v2 §9).
- `HOSTBUD_URL` for sessions: default `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}` (v2 §8). A new override, **`HOSTBUD_HOOK_BASE_URL`**, is needed so the e2e target can reach Caddy by service name. Add it to `.env.example` (empty = default) and read it in `internal/config`.
- Caddy: confirm the loopback site proxies `/api/hooks/*` unchanged (the `Authorization` header passes through). No new published port.
- Logging: never log tokens or hook bodies at info. Log the run id and event at debug.
- The AGENTS.md security checklist item "(v2) Hook endpoints use per-run tokens (stored hashed)" becomes active from this task.
- **Tests:** U: token generation, hashing, constant-time compare, each response code, body cap, rate limit, revocation per terminal state (and none on `stale`); I: through the real Caddy config, a hook POST with a token reaches the app, and one without a token gets `401` with no cookie or Origin needed; log-hygiene test extended to hook tokens; E: see below.
- **E2E:** add *Hook endpoint* (API-level, through Caddy): a seeded run accepts `session_start` with its token (`204`); wrong token `401`; unknown run `404`; ended run `410`; 65 KiB body `413`; a foreign `Origin` header does not matter.

#### T4 — Session creation for runs
- Runs are created through the single session-create service (v2 §5.1 step 3):
  - `machine`: the host;
  - `path`: the queue's project path;
  - `name`: `<project>-q<position>`, with collisions resolved by the v1 M6 rule (`-1`, `-2`, …);
  - `env`: `HOSTBUD_URL`, `HOSTBUD_RUN_ID`, `HOSTBUD_RUN_TOKEN`;
  - `startCommand`: the adapter's argv.
- Apply the S8 result: if a stdin-fed `set-environment` hides the token from the process list, use it for `HOSTBUD_RUN_TOKEN`. If not, keep `-e` and record the known limitation (v2 §9) in README security notes.
- The session name is recorded on the run. The run starts in `starting`.
- **Tests:** U: name derivation and collision suffixing, env map contents, argv passed unchanged to `sshx`; I: against `test/sshd`, the created session has the three variables in `tmux show-environment -t '=<name>'` and runs in the project path; if S8 chose the stdin method, the token never appears in `ps` output during creation; E: covered by T9 (runs start through the dispatcher).
- **E2E:** no standalone scenario: sessions for runs are only reachable through the queue, and T9's *Three items in order* asserts the session name, path and env through the target.

#### T5 — Adapter interface and the Claude Code adapter
- `internal/agents`: the `Adapter` interface and `GoalState` from v2 §7, plus a registry keyed by `Kind()`.
- `BuildCommand`:
  - argv = `claude`, the user flags, `--settings '<hooks json>'`, then `'/goal <condition>'`;
  - flags are split into arguments shell-style and never interpolated (v2 §3.2);
  - the hooks JSON contains the single hook command from v2 §7 for `SessionStart`, `Stop` and `SessionEnd`;
  - the token appears only as `$HOSTBUD_RUN_TOKEN`, never literally.
- `ParseHook`: extracts `session_id` and `transcript_path`. Rejects bodies without a session id.
- `ReadGoalState`:
  - checks `transcript_path` (v2 §9): absolute, cleaned, under the remote user's `~/.claude/projects/`, and its SFTP `realpath` still under it (no symlink escape);
  - reads it read-only over SFTP, incrementally from the stored byte offset;
  - parses each **line as JSON** and accepts only top-level `type=="attachment"`, `attachment.type=="goal_status"` records;
  - applies the §5.3 binding: bound session, after `run.started_at` (timestamp, or line order plus hook arrival time per S5), condition equal to the queued one after whitespace normalization;
  - result mapping: `met:true` ⇒ `achieved`; the "impossible" record ⇒ `failed`; intermediate `met:false` ⇒ `pending`; unparseable or unknown shape ⇒ `unknown`.
- Version check: `claude --version` over `sshx` before the run starts. It is recorded as `runs.client_version`. Below `MinVersion()` ⇒ an actionable error, and the run is not started.
- Instruction validation on item create/edit: the PoC requires `/goal ` followed by a non-empty condition (v2 §3.2).
- **Tests:** U: argv building (flags with quotes, no literal token), `ParseHook`, the §5.3 matrix against fixture transcripts per supported version (achieved, pending, failed, **decoy text** with the marker words nested, stale record from before start, wrong session, changed condition, truncated last line), path validation (relative, `..`, outside the tree, symlink escape), incremental offset; I: `ReadGoalState` over SFTP against `test/sshd` with a fixture transcript under `/home/dev/.claude/projects/`; E: see T9.
- **E2E:** none standalone: the adapter is reached only through runs. T9's scenarios use the stub `claude` from T7 and include the decoy case.

#### T6 — Codex adapter
- `BuildCommand`: argv = `codex`, the user flags, `-c 'hooks.SessionStart=[…]' -c 'hooks.Stop=[…]' -c 'hooks.SessionEnd=[…]'`, then `'/goal <condition>'`. The syntax is as proven in S1. Never `-c notify=…` (v2 §2).
- `ParseHook`: the thread id from the hook body (as recorded in S4), plus `transcript_path`.
- `ReadGoalState` uses the reader chosen in S7:
  - `status=='complete'` with `updated_at_ms` after start and `objective` equal ⇒ `achieved`;
  - `blocked` ⇒ `failed`;
  - `active`, `paused`, and Codex's `usageLimited` / `budgetLimited` wire statuses (or legacy underscore spellings) ⇒ `pending`;
  - anything else ⇒ `unknown`.
- If S7 chose a host tool (`sqlite3`/`python3`): the query opens the database read-only, is built from argv through `sshx`, and is timeout-bounded. A missing tool is an actionable error (e.g. "sqlite3 not found on the host — install with `sudo apt install sqlite3`").
- Version check and `client_version` as in T5.
- **Tests:** U: argv, `ParseHook`, the status matrix from fixture rows or rollout files per supported version (including decoy text in the objective or messages); I: the reader against `test/sshd` with a fixture `goals_1.sqlite` (or rollout file) under `/home/dev/.codex/`; E: see T9.
- **E2E:** none standalone; T9 runs a stub `codex` item.

#### T7 — Stub clients on the throwaway target
- Add stub `claude` and `codex` executables to the `test/sshd` image used by `hostbud-e2e-target`. They are tiny scripts, not real clients. Each one:
  - accepts the same argv shape as the real client (flags, the hook injection, the initial prompt) and **runs the injected hook command** for each event, so injection is exercised end to end;
  - writes records in the exact formats recorded in S5/S7: a Claude transcript line or Codex state;
  - stays attached in the foreground like a TUI until told to finish, so the session behaves like an interactive run.
- A scripted behavior is chosen per run, controlled by a file the runner writes on the target over SSH:
  - achieve after N turns;
  - print decoy text only;
  - fail ("impossible" / `blocked`);
  - exit without achieving;
  - go silent (for stale);
  - change the session id (`/clear`).
- `hostbud-e2e-app` gets `HOSTBUD_HOOK_BASE_URL` pointing at Caddy's loopback site by service name, and a short `HOSTBUD_RUN_STALE_AFTER` (seconds).
- Test helpers in `test/e2e/` to set a stub behavior and read a run's session env and path through the target.
- **Tests:** U: n/a (test tooling); I: the stubs' output is parsed by the T5/T6 adapters in an integration test, so stub and adapter can't drift; E: used by T9–T11.
- **E2E:** none of its own; this task provides the harness the T9–T11 scenarios run on. The suite still type-checks.

#### T8 — Queue service and REST API
- `internal/queue` service: queue and item CRUD, reorder, start / pause / resume, item retry / skip / mark done. It publishes `queue.changed` on every change (v2 §4).
- REST routes, cookie-authenticated and Origin-checked like every state-changing v1 route. The exact shapes are fixed in the V2-M1 task file and in v1 ARCHITECTURE §9:
  - `GET/POST /api/queues`, `GET/PATCH/DELETE /api/queues/{id}`;
  - `POST /api/queues/{id}/items`, `PATCH/DELETE /api/queue-items/{id}`, `PUT /api/queues/{id}/order`;
  - `POST /api/queues/{id}/start|pause|resume`;
  - `POST /api/queue-items/{id}/retry|skip|mark-done`.
- Editing rules:
  - only `queued` items can be edited, deleted or moved;
  - `running` and `done` items are read-only;
  - deleting a queue with an active run is refused with an actionable message.
- PoC limit: one queue per installation (v2 §3.3). Creating a second one is refused with a message that names V2-M2.
- **Tests:** U: service rules (editability by status, reorder, one-queue limit, owner overrides only from `needs_attention`), event publication; I: routes against the real store with Origin rejection for foreign origins; E: see below.
- **E2E:** add *Queue API* (API-level through Caddy): create a queue for a project, add three items, reorder, edit a queued item, delete one, start and pause. A foreign Origin is rejected on every state-changing queue route (added to M7's shared route list).

#### T9 — Dispatcher and state machines
- `internal/queue` dispatcher implements v2 §5:
  - **start** (§5.1): the first `queued` item of a `running` queue with no active run ⇒ new run (token, `starting`), then T4 session creation;
  - **signals** (§5.2):
    - `session_start` binds the agent session id ⇒ `running`; a second `session_start` with a different id ⇒ `needs_attention`;
    - `turn_end` updates `last_signal_at` and calls `ReadGoalState`;
    - `session_end` reads the state once more, then `exited` if not achieved;
    - the run's session disappearing from `sessions.changed` is handled like `session_end`;
    - no signal for `HOSTBUD_RUN_STALE_AFTER` ⇒ `stale`;
  - **transitions** (§5.4):
    - only `achieved` advances;
    - every other terminal run state sets the item `needs_attention` and the queue `paused`;
    - a late `achieved` after `stale` marks the item `done`, but the queue stays paused;
    - no `queued` items left ⇒ queue `finished`;
    - pause never touches the running session;
  - **owner actions**:
    - retry ⇒ item `queued` and a new run (new session; the old one stays open);
    - skip ⇒ `skipped`;
    - mark done ⇒ `done`;
    - an action on an item whose run is still active ends that run as `cancelled` (token revoked, session left open).
- No new tmux poll loop: the dispatcher reacts to hooks, `sessions.changed` and a timer only (v2 §4).
- Restart safety: on app start, active runs are reloaded, their stale timers are re-armed, and each running run gets one `ReadGoalState`, so a signal missed during a restart can't stall the queue.
- Every transition writes a `run_events` row (`source` = hook / poller / timer / user) and publishes `run.changed`.
- New env var **`HOSTBUD_RUN_STALE_AFTER`** (Go duration, default `90m`, or the S9 value) in `.env.example` and `internal/config`.
- **Tests:** U: the full transition table with a fake adapter and clock (every §5.2 row, every §5.4 edge, a concurrent double `turn_end` advancing once, restart recovery); I: with `test/sshd` and the stub `claude`, one item runs from `starting` to `achieved` and the next session is created; E: see below.
- **E2E:** add (API-level, stubs):
  - *Three items in order*: each next item's session appears only after the previous stub writes its achieved record, and the queue ends `finished`. Check the session names, project path and `HOSTBUD_*` env on the target;
  - *Decoy text does not advance*: the queue stays on item 1;
  - *Needs attention*: failed, exited, stale and changed session id each pause the queue with the item `needs_attention`, and the session is still alive;
  - *Late achieved after stale*: the item is `done`, and the queue stays paused until resumed;
  - *Retry and skip*;
  - *Mixed clients*: a stub `claude` item then a stub `codex` item.

#### T10 — Queue panel (desktop)
- A minimal **Queue panel** (v2 §10), opened from the app header:
  - pick the project;
  - add, edit, delete and reorder items (drag, as in the v1 tree; keyboard reorder too);
  - per item: agent select, flags text field, instruction text field;
  - Start / Pause / Resume;
  - per-item status badge with the run state and `detail` (why it needs attention);
  - Retry / Skip / Mark done on `needs_attention` items (Mark done asks for confirmation, since it overrides the evaluator);
  - "Open session" opens the run's session in a terminal tab.
- Updates come from `queue.changed` / `run.changed` over `/ws/events` only; no polling.
- Errors are actionable: untrusted workspace (S3), client missing or too old, missing reader tool (T6).
- Autocomplete is off on these inputs (M8 policy).
- **Tests:** U: Vitest for the panel store (event reducers), form validation (`/goal ` required), button availability per status, confirmation on Mark done; I: n/a (the API is covered in T8/T9); E: see below.
- **E2E:** add *Queue panel* (desktop): build a two-item queue in the UI, start it, watch item 1 turn done and item 2 start without a reload, open item 2's session in a tab and see the stub's output; a needs-attention item shows its reason, and Retry/Skip work from the panel.

#### T11 — Queue panel on the phone
- The panel works in the phone layout: a drawer or full-screen sheet, touch-sized controls, and reorder by touch drag or move buttons. "Open session" switches to the single-terminal view.
- **Tests:** U: responsive layout components; I: n/a; E: see below.
- **E2E:** add *Queue panel (phone)* (iPhone 13 Pro profile): create two items, start, see item 1 turn done and item 2 start and finish live, open a run's session, Retry a needs-attention item.

#### T12 — Docs alignment
- v1 [ARCHITECTURE.md](../ARCHITECTURE.md):
  - §10 becomes a short pointer to this directory, keeping the v1 obligations;
  - §9 lists the queue and hook routes;
  - §12 lists `HOSTBUD_RUN_STALE_AFTER` and `HOSTBUD_HOOK_BASE_URL`;
  - §14 replaces `internal/{llm,orchestrator}` with `internal/{queue,agents}` (and `internal/llm` for V2-M5);
  - §15 lists the hook body cap, the rate limit and the adapter read timeouts.
- v1 [ROADMAP.md](../ROADMAP.md): the *v2* section is replaced by a link to this roadmap. The start-command bug leaves *Later* (T0).
- [ARCHITECTURE.md](ARCHITECTURE.md) (v2): the status line says V2-M1 is implemented, with the spike results kept.
- README: a "Queues" usage section (creating a queue, instruction format, what needs attention means, trusting a workspace, the client minimum versions) and the token process-list note if T4 kept `-e`.
- AGENTS.md: the v2 security checklist item is ticked. "v2 is design-only" is replaced by a pointer to this roadmap.
- **Tests:** the v1 M7 docs consistency check covers the new env vars and routes; E: n/a (docs).
- **E2E:** none (docs only).

#### T13 — Milestone acceptance
- `make lint test` green, e2e `tsc` clean, gitleaks clean, deploy with `make deploy`. No e2e run (on demand only).
- Walk the criteria below and tick each one whose tests pass.
- The real-client check is the owner's (see *Manual checks*).
- **E2E:** no new scenarios; no run (on demand only).

#### T14 — Safe Docker cleanup
- The milestone's last step, after T13's deploy (see *Rules*, Docker cleanup): check nothing is in use, `make docker-clean` without `CACHE=1` (recipe read first; extended for any new e2e images), remove only this milestone's clean worktrees and restore-check leftovers, never prune globally or touch production, volumes, backups, other projects or tmux sessions; verify the stack is healthy and report reclaimed space.
- **Tests:** n/a (operations only); the post-cleanup health check is the verification.
- **E2E:** n/a (nothing reachable changes; must not start a new run).

### Acceptance criteria (V2-M1)
Each line: criterion — coverage.

1. **Prerequisite fixed:** a session created with a start command exists, runs the command, in the chosen path. — U: T0 · I: T0 · E: T0 *Session with start command*.
2. **Spike recorded:** v2 ARCHITECTURE has a "Spike results" section answering §11 Q1–Q6 with tested client versions. — U/I/E: n/a (a document; reviewed in T13).
3. **Schema is append-only and keeps v1 data.** — U: T2 · I: T2 (migrate a populated v1 copy) · E: n/a (not user-visible; covered indirectly by every T9 scenario).
4. **Hook endpoint contract** (`204/401/404/410/413`, 64 KiB cap, per-run rate limit, no cookie or Origin needed, tokens stored hashed and revoked at run end but not on stale). — U: T3 · I: T3 (through Caddy) · E: T3 *Hook endpoint*.
5. **No secrets in logs or argv:** tokens never logged, never literal in the command or settings JSON. — U: T3, T5, T6 · I: T3 log-hygiene, T4 (`ps` check if S8 allows) · E: n/a (logs aren't observable from the browser).
6. **Runs start correctly:** session `<project>-q<pos>` (collision-suffixed) in the project path with `HOSTBUD_*` env, through the single session-create service. — U: T4 · I: T4 · E: T9 *Three items in order*.
7. **Only a bound, structured achieved record advances the queue** (bound session, after start, equal condition; Codex row equivalent). — U: T5, T6 (fixture matrices) · I: T5, T6 (SFTP / reader against `test/sshd`), T9 · E: T9 *Three items in order*, *Mixed clients*.
8. **Decoy text never advances.** — U: T5, T6 decoy fixtures · I: T7 (stub-vs-adapter test) · E: T9 *Decoy text does not advance*.
9. **Fail closed:** failed, exited, stale, a changed session id and unknown formats set `needs_attention` and pause the queue; the session keeps running. — U: T9 · I: T9 · E: T9 *Needs attention*.
10. **Late achieved after stale** marks the item done and keeps the queue paused. — U: T9 · I: n/a (timer semantics are fully covered with a fake clock in U) · E: T9 *Late achieved after stale*.
11. **Owner controls:** Start/Pause/Resume, Retry (new run, old session kept), Skip, Mark done (confirmed); pause never touches the running session. — U: T8, T9, T10 · I: T8 · E: T9 *Retry and skip*, T10 *Queue panel*.
12. **hostbud never kills a run's session, and changes no user config.** — U: T9 (no kill calls on any transition) · I: T4/T9 (sessions survive every terminal state; the user's `~/.claude` / `~/.codex` files are unchanged) · E: T9 *Needs attention* (session alive).
13. **Queue API is Origin-checked and authenticated.** — U: T8 · I: T8 · E: T8 *Queue API*.
14. **UI updates from events only** (no reload, no polling). — U: T10 · I: n/a (frontend behavior) · E: T10 *Queue panel*.
15. **Panel usable on desktop and phone.** — U: T10, T11 · I: n/a · E: T10, T11.
16. **Restart safety:** after `hostbud` restarts mid-run, the run resumes tracking, and a goal achieved during the restart is picked up. — U: T9 · I: T9 · E: T9 (restart `hostbud-e2e-app` during *Three items in order*).
17. **Docs aligned** (v1 ARCHITECTURE §9/§10/§12/§14/§15, v1 ROADMAP, README, AGENTS.md, `.env.example`). — U/I: T12 docs consistency check · E: n/a.

### Manual checks (owner; backlog, not blockers)
- **Real clients end to end** (v2 §10 *Accept*): on the host, a real Claude Code item followed by a real Codex item. The second starts only after the first's `/goal` is achieved, and never on decoy output (ask the first agent to print the marker text before finishing).
- Trust the project directory once in each client if S3 shows it's required.
- Confirm the tested client versions match what's installed on the host after upgrades.
- Close the spike's and the check's leftover sessions from the UI.

### New configuration (owner fills in `.env` if needed)
- `HOSTBUD_RUN_STALE_AFTER`: no-signal window before a run is `stale` (default `90m`, or the S9 value).
- `HOSTBUD_HOOK_BASE_URL`: override for `HOSTBUD_URL` inside run sessions. Empty = `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}`. Production normally leaves it empty.

**Accept (summary):** with stub clients in e2e and real clients as an owner check, a queue runs items strictly in order, advances only on a bound, structured goal-achieved record, pauses with a clear reason on anything else, never kills a session, and is fully operable from desktop and phone.

---

## V2-M2 — Multiple queues and parallel runs (opt-in)

Scope: v2 §10 *V2-M2*, v2 §3.3 and §3.9. Breakdown: [V2-M2-tasks.md](V2-M2-tasks.md) · [V2-M2-acceptance.md](V2-M2-acceptance.md).

**Goal.** Several queues can run at once. Each queue stays strictly sequential. A configurable per-machine cap limits active runs; it defaults to 2 while parallel queues are enabled and is ignored while they are off.

**Switch:** multiple queues are enabled per installation (`HOSTBUD_PARALLEL_QUEUES`, default `false`). With the switch off, the V2-M1 one-queue limit stays and V2-M1 behavior is unchanged.

### Tasks
- **T1 Schema:** append-only migration adding `machine_capacity(machine_id PK, max_concurrent_runs NULL)` plus `queues.waiting_since` for slot order. Stored NULL/unset uses the runtime default of 2 when parallel queues are enabled; T9 records this follow-up behavior.
- **T2 Several queues:** lift the one-queue limit when enabled. A project may have more than one queue. The panel warns when two running queues share a project directory, because agents may edit the same files.
- **T3 Session naming:** `<project>-q<position>` collides across queues of one project. Switch to `<project>-<queue>-q<position>` for queues after the first (the first keeps the V2-M1 name), still collision-suffixed.
- **T4 Dispatcher with slots:** one active run per queue as before, plus a machine-wide count of active runs (`starting`, `running`, `stale`; stale still occupies the slot because the session is alive).
  - When the cap is reached, the next item stays `queued` with the reason "waiting for a free slot".
  - Slots go to queues in the order they started waiting (queue start or resume, or its previous run ending; FIFO), so no queue starves.
  - The cap holds under concurrent signals and after a restart.
  - Owner action on a stale run frees its slot.
- **T5 API and panel:**
  - queue list and switcher, create/rename/delete queue;
  - per-machine cap setting (Settings, Origin-checked);
  - a "waiting for slot" state on items;
  - desktop and phone.
- **T6 Docs:** v1 ARCHITECTURE §9/§12, README "Queues" (parallel runs, cap), this roadmap's status.
- **T7 Milestone acceptance:** `make lint test`, e2e `tsc`, gitleaks, `make deploy`, and tick the criteria below (no e2e run; on demand only).
- **T8 Safe Docker cleanup:** as in *Rules* (after T7's deploy; `make docker-clean` without `CACHE=1`; production, volumes and backups untouched; reclaimed space reported).

**E2E** (per task, stubs):
- T2/T5 *Two queues in parallel*: both queues' first items run at once, and each queue stays sequential;
- T4 *Cap of one*: the second queue waits with "waiting for a free slot" and starts when the first queue's run achieves;
- T3 *Name collision*: two queues on one project get distinct session names;
- T2 *Same-directory warning*; T4 *Slots in start order*, *Stale holds a slot*, *Cap after restart*;
- T5 panel on desktop and phone.

**Accept:**
1. With the switch on, two queues advance independently, each sequential. — U: T4 · I: T4 · E: T2/T5 *Two queues in parallel*.
2. The cap is respected (a stale run holds its slot; also under concurrent signals and after a restart), and slots go FIFO by the order queues started waiting, so none starves. — U: T4 · I: T4 · E: T4 *Cap of one*, *Slots in start order*, *Stale holds a slot*, *Cap after restart*.
3. With the switch off, V2-M1 behavior is unchanged. — U: T2 · I: T2 · E: the V2-M1 suite still green.
4. Session names never collide across queues; the first queue keeps the V2-M1 name. — U: T3 · I: T3 · E: T3 *Name collision*.
5. The migration is append-only and keeps V2-M1 data. — U: T1 · I: T1 · E: n/a (indirect through T4).
6. Two running queues on one project directory show a warning. — U: T2, T5 · I: n/a (pure logic over stored paths) · E: T2 *Same-directory warning*, T5.
7. Queues and the cap are manageable from desktop and phone; the cap route is Origin-checked; the UI updates from events. — U: T5 · I: T5 · E: T5.
8. Docs aligned. — U/I: T6 docs check · E: n/a.

**Manual checks (owner):** two real agents in parallel on separate projects.

---

## V2-M3 — Notifications (opt-in)

Scope: v2 §10 *V2-M3*.

**Goal.** Tell the owner when an item is done, when an item needs attention, and when a queue has finished, even when hostbud isn't open.

**Switch:** per account, off by default. Turned on in Settings, which asks for browser notification permission.

Breakdown: [V2-M3-tasks.md](V2-M3-tasks.md) · [V2-M3-acceptance.md](V2-M3-acceptance.md). It adds **T0** (schema, per-account switch, VAPID config) before T1.

### Tasks
- **T1 In-app notifications:** while the app is open, `run.changed` / `queue.changed` produce browser `Notification`s for the three events (none for intermediate states).
  - Text: project name, item position and outcome only. No instruction text, paths or pane output.
  - A click focuses the app and opens the item.
- **T2 Web Push:** notifications while the app is closed, including the installed PWA on iOS (16.4+, home-screen install only).
  - `push_subscriptions` table (append-only migration; per account; endpoint and keys; deleted on `404` or `410` from the push service).
  - VAPID keys from new env vars `HOSTBUD_VAPID_PUBLIC_KEY`, `HOSTBUD_VAPID_PRIVATE_KEY`, `HOSTBUD_VAPID_SUBJECT` (placeholders in `.env.example`; a `make` target generates a pair in a container).
  - The M5 service worker gets `push` and `notificationclick` handlers. Its cache rules are unchanged (never `/api/*` or `/ws/*`).
  - The CSP is reviewed so it still allows no third-party origins.
- **T3 Outbound delivery:** push is sent from the server with a timeout and retries (bounded). A failure is logged without the endpoint URL at info. This needs outbound HTTPS from the container, documented in README.
- **T4 Settings UI:** per-account toggle, a per-event choice (done / needs attention / queue finished), "send test notification". Desktop and phone.
- **T5 Docs:** README (enabling, iOS requirements), v1 ARCHITECTURE §11/§12/§15.
- **T6 Milestone acceptance:** `make lint test`, e2e `tsc`, gitleaks, `make deploy`, and tick the criteria below (no e2e run; on demand only).
- **T7 Safe Docker cleanup:** as in *Rules*; the `docker-clean` recipe also removes the `hostbud-e2e-pushfake` image.

**E2E:**
- T1 *In-app notification*: Chromium, permission granted; a stub item achieves and a notification with the expected title is shown (observed through a notification spy in E2E builds);
- T2 *Push subscription*: subscribing stores a subscription. A stub run's completion makes the server POST an encrypted payload to a fake push endpoint (a new `hostbud-e2e-pushfake` service), and the payload decrypts to the expected title;
- T3 *Expired subscription*: the fake endpoint answers `410`/`404` and the subscription is removed;
- T4 settings on desktop and phone, off by default.

**Accept:**
1. Off by default; no permission prompt until the owner opts in; a denied or revoked permission is shown and handled. — U: T4 · I: T0 (a fresh account reads off) · E: T4.
2. The three events notify in-app and through push, with no sensitive content. — U: T1, T2 · I: T2/T3 (fake push endpoint) · E: T1, T2.
3. Expired subscriptions (`404`/`410`) are removed. — U: T3 · I: T3 · E: T3.
4. With every account off, behavior is exactly V2-M2's. — U: T1, T2 · I: T0 · E: T1.
5. Missing or invalid VAPID keys turn push off with an actionable message, not a startup error. — U: T0 · I: T0 · E: T0.
6. Each device and account gets its own notifications, and duplicate signals or a restart never send one twice. — U: T2, T3 · I: T2, T3 · E: T2, T3.
7. The docs are aligned (T5). — U/I: T5 docs check · E: n/a (documents).

**Manual checks (owner):** push on the installed iPhone PWA and on a desktop browser with hostbud closed.

---

## V2-M4 — Completion gates (opt-in, per item)

Scope: v2 §10 *V2-M4*, and the "evaluator talked into met" risk in v2 §2.

**Goal.** Before an achieved item counts as done, optionally require a **verify command** that exits 0 and/or the owner's **approval**.

**Switch:** per item; both gates empty by default, so behavior is V2-M3's (and V2-M1's with the earlier switches off).

Breakdown: [V2-M4-tasks.md](V2-M4-tasks.md) · [V2-M4-acceptance.md](V2-M4-acceptance.md).

### Tasks
- **T1 Schema:** append-only migration adding `queue_items.verify_command TEXT NULL` and `queue_items.requires_approval BOOL DEFAULT false`, plus new item states `verifying` and `awaiting_approval` (a new migration replaces the CHECK constraint; no data changes).
- **T2 Verify runner:** after `achieved`, when `verify_command` is set, the item goes to `verifying`.
  - hostbud runs the command itself on the host, in the project directory, through `sshx` from argv (split like flags; never through the run's tmux session).
  - It is timeout-bounded (new env var `HOSTBUD_VERIFY_TIMEOUT`, default `10m`).
  - Exit 0 ⇒ next gate. Non-zero or timeout ⇒ `needs_attention`, and the queue is paused.
  - The output tail (capped, e.g. last 16 KiB) goes into `run_events` under a new `source='verify'` (added to the CHECK in the T1 migration) and is shown in the panel. It is never logged at info.
- **T3 Approval:** when `requires_approval` is set, the item goes to `awaiting_approval`, and the queue waits (not paused).
  - Approve ⇒ `done` and advance.
  - Reject ⇒ `needs_attention`.
  - Both are owner actions, recorded in `run_events` (`source='user'`).
- **T4 Owner actions:**
  - "Re-run verify" on a needs-attention item whose run achieved, without rerunning the agent;
  - the existing Retry reruns the agent;
  - Mark done still overrides.
- **T5 Panel:** gate fields on the item form, gate status and verify output in the item view, Approve/Reject buttons. Desktop and phone.
- **T6 Docs:** README (writing verify commands, what runs where), v1 ARCHITECTURE §12/§15.
- **T7 Milestone acceptance:** `make lint test`, e2e `tsc`, gitleaks, `make deploy`, and tick the criteria below (no e2e run; on demand only).
- **T8 Safe Docker cleanup:** as in *Rules*.

**E2E** (stubs):
- T2 *Verify fails then passes*: verify `test -f done.flag` fails ⇒ needs attention, and the output is shown; the runner creates the file on the target; Re-run verify ⇒ done, and after Resume the queue advances (owner actions keep the queue paused, V2-M1);
- T2 *Verify timeout* (a short timeout);
- T3 *Approval*: the queue waits in `awaiting_approval`; Approve advances; Reject ⇒ needs attention;
- T5 panel on desktop and phone.

**Accept:**
1. With no gates, V2-M3 behavior is unchanged. — U: T2 · I: T2 · E: the V2-M1–V2-M3 suites green.
2. A verify command gates advancement on exit 0, with a timeout and visible output. — U: T2 · I: T2 (against `test/sshd`) · E: T2.
3. Approval gates advancement. — U: T3 · I: n/a (no host interaction) · E: T3.
4. Verify runs in the project directory from argv through `sshx`, never through `send-keys`. — U: T2 · I: T2 · E: n/a (the command path isn't visible to the browser; I covers it).
5. The migration is append-only: it adds columns and widens the CHECKs, existing rows stay valid, a second apply is a no-op. — U: T1 · I: T1 · E: n/a (indirect through T2).
6. A restart never loses a gated item or runs verify twice, and racing owner actions apply once. — U: T2, T3, T4 · I: T2 · E: T2, T3, T4.
7. Gates interact with slots and notifications as decided in the breakdown. — U: T2, T3 · I: n/a (dispatcher logic over the store; U uses the test database) · E: T2, T3.
8. Gates are editable per a defined rule and usable on desktop and phone; the new routes are Origin-checked. — U: T1, T5 · I: T1 · E: T1, T5.
9. The docs are aligned (T6). — U/I: T6 docs check · E: n/a (documents).

**Manual checks (owner):** a real item gated by the project's own `make test`.

---

## V2-M5 — LLM stale-run supervisor (opt-in)

Scope: v2 §10 *V2-M5*, v2 §2 option 6, v1 ARCHITECTURE §10 *Supervisor LLM*.

**Goal.** For runs that have gone quiet, classify what the pane shows (working, waiting for input, blocked, finished, failed) so the owner knows where to look. **It can flag a run, never advance it**, not even on "finished".

**Switch:** off unless `HOSTBUD_LLM_PROVIDER` is set, so behavior is V2-M4's (and V2-M1's with the earlier switches off).

Breakdown: [V2-M5-tasks.md](V2-M5-tasks.md) · [V2-M5-acceptance.md](V2-M5-acceptance.md).

### Tasks
- **T1 Provider interface:** `internal/llm` with `Classifier`; OpenAI first. Env vars:
  - `HOSTBUD_LLM_PROVIDER` (empty = off);
  - `HOSTBUD_LLM_MODEL`;
  - `OPENAI_API_KEY`;
  - `HOSTBUD_LLM_BASE_URL` (optional, for the e2e fake).
  Calls are timeout-bounded, retried with a bound, and never log the key or pane text at info.
- **T2 Trigger:** a run with no signal for `HOSTBUD_LLM_QUIET_AFTER` (default shorter than the stale window, e.g. `20m`) and on `stale`. hostbud reads `tmux capture-pane -p -t '=<name>' -S -200` through `sshx`.
  - Per-run budget: at most N classifications per hour (env `HOSTBUD_LLM_MAX_PER_RUN_HOUR`, default 2).
- **T3 Result handling:** the label (`running | waiting_input | blocked | completed | failed | unknown`) and a short reason are stored as `run_events` with `source='llm'`, and a `run.changed` flag is published.
  - The item and queue status never change because of it. A `completed` label is shown as "looks finished — check and Mark done".
- **T4 Privacy:** pane text leaves the host for the provider. Settings and README say so explicitly.
  - Optional secret scrubbing (token-like strings, `KEY=value`) before sending, on by default.
  - The supervisor is off until the provider is configured.
- **T5 Panel:** a flag badge with the label, reason and time on the item. Desktop and phone. Integrates with V2-M3 when notifications are on ("needs your input").
- **T6 Docs:** README, v1 ARCHITECTURE §10/§12/§14/§15 (package `internal/llm`).
- **T7 Milestone acceptance:** `make lint test`, e2e `tsc`, gitleaks, `make deploy`, and tick the criteria below (no e2e run; on demand only).
- **T8 Safe Docker cleanup:** as in *Rules*; the `docker-clean` recipe also removes the `hostbud-e2e-llmfake` image.

**E2E:**
- T2/T3 *Quiet run is flagged*: a new `hostbud-e2e-llmfake` service answers with a fixed label; a stub run goes silent and gets flagged `waiting_input` in the panel, and the queue does **not** advance;
- T3 *Completed label never advances*: the fake answers `completed`, and the item stays running;
- T2 budget respected;
- T5 badge on desktop and phone.

**Accept:**
1. Off unless configured: with no provider, V2-M4 behavior is unchanged (no `capture-pane`, outbound request or new events). — U: T1, T2 · I: T1, T2 · E: T1; the V2-M1–V2-M4 suites green with no provider set.
2. A missing, partial or invalid provider config turns the supervisor off with an actionable message, never a startup error. — U: T1 · I: T1 · E: T1.
3. Quiet runs are classified from `capture-pane` and flagged, within budget. — U: T2 · I: T2 (against `test/sshd` and a fake provider) · E: T2/T3.
4. `capture-pane` runs through `sshx` from argv with a validated exact target, is size-capped, and handles a gone or renamed session. — U: T2 · I: T2 · E: T2.
5. Only eligible runs are classified (never gated items or ended runs); the budget and quiet timer survive a restart without extra calls. — U: T2 · I: T2 · E: T2.
6. A classification never advances the queue or changes an item's status (or a run, gate or slot), even when it races a real signal or an owner action. — U: T3 · I: T3 · E: T3 *Completed label never advances*.
7. Provider failures and prompt injection in pane text end as `unknown` or a bounded retry, never a status change. — U: T1, T3 · I: T1 · E: T3.
8. No key or pane text in logs at info; scrubbing applied before sending; pane text never stored or shown beyond the label and reason. — U: T1, T4 · I: T1 log-hygiene · E: T4 (logs aren't observable in the browser).
9. Flags show on desktop and phone and, with notifications on, notify once per run and label within the V2-M3 payload allowlist. — U: T5 · I: n/a (UI and notify builder) · E: T5.
10. The docs are aligned (T6). — U/I: T6 docs check · E: n/a (documents).

**Manual checks (owner):** one real OpenAI classification of a quiet real agent session; review cost settings.

---

## V2-M6 — Queue after an active goal (opt-in)

Goal: when creating a queue, optionally select an active hostbud-tracked agent run and hold the new queue until that run's structured `/goal` is achieved. This supports chaining a fresh queue after work already in progress.

Scope and task breakdown: [V2-M6-tasks.md](V2-M6-tasks.md) · [V2-M6-acceptance.md](V2-M6-acceptance.md).

**Switch:** per queue. No selected predecessor means existing behavior. The selection is limited to active, hostbud-tracked runs on the same machine. Manually started tmux sessions are not selectable because hostbud cannot observe their structured goal state.

1. A new queue persists its optional predecessor run; existing queues are unchanged. — U/I: T1 · E: T2.
2. Starting a dependent queue waits for predecessor achievement, including a late achievement after stale and restart recovery. — U/I: T2 · E: T2 *Queue waits for active goal*.
3. A failed, exited or cancelled predecessor pauses its dependent queue; it never releases it. — U/I: T2 · E: T2 *Unsuccessful predecessor pauses dependent queue*.
4. The create form defaults to no dependency and lists eligible active goals when present. — U: T3 · I: T3 · E: T3 *No dependency by default*.

## V2-M7 — Queue lifecycle timestamps

Record and expose the first start time and finish time for each queue, plus first start and latest terminal end for each item. Retry preserves an item's original start and clears its prior end; a queue restarted after finishing preserves its first start and clears its prior end.

Scope and task breakdown: [V2-M7-tasks.md](V2-M7-tasks.md) · [V2-M7-acceptance.md](V2-M7-acceptance.md).

1. Database lifecycle values and guarded transition behavior. — U/I: T1 · E: T2.
2. API/UI display and browser scenario. — U/I/E: T2 *Queue lifecycle timestamps*.

## Later (not scheduled)

- **Explicit-signal fallback for other clients** (v2 §2 option 2, §8): a documented `{"hostbud_goal":{"status":"achieved","condition":"…"}}` payload on `session_end`/`turn_end`, for clients without readable goal state. Never used for Claude Code or Codex.
- **More agent clients** via the run protocol (v2 §8). Each one needs: an `Adapter`, fixtures for each supported version (achieved, pending, failed, decoy), an e2e stub binary, and documented flags and minimum version.
- **Multi-machine runs:** together with v1 *Later* multi-machine. Targets must reach `HOSTBUD_URL` (the tailnet), host-key trust comes through the UI, and `machine_id` is already in every v2 table.
- **Queue templates**, e.g. "M1…Mn from `docs/roadmap/`" generated from a project's roadmap files.
- Carried over from the old v1 ROADMAP v2 list: tmux control-mode push instead of polling; git status per project. (The "Postgres option" item is dropped: v1 already uses PostgreSQL.)

---

## Traceability: v2 ARCHITECTURE → this roadmap

Every section and step of [ARCHITECTURE.md](ARCHITECTURE.md), and where it's delivered.

| v2 section | Item | Delivered in |
|---|---|---|
| §1 Context | Queue per project, items in order, own agent/flags/instruction | V2-M1 T2, T8, T10 |
| | 1. Owner creates a queue and adds items | V2-M1 T8 (API), T10/T11 (UI) |
| | 2. First item starts in its own tmux session with the instruction | V2-M1 T4, T5/T6 (`BuildCommand`), T9 |
| | 3. Next item starts when the goal is achieved | V2-M1 T9 |
| | 4. Repeat until empty or attention needed | V2-M1 T9 (`finished`, `needs_attention` ⇒ `paused`) |
| | First milestone is the PoC; rest optional | V2-M1 default; V2-M2–M5 opt-in |
| §2 Research | Claude `/goal`, `goal_status` record, hooks, `--settings`, errors clear/pause goal | V2-M1 T1 S1/S2/S5, T5 |
| | Codex `/goal`, `thread_goals`, inline hooks, no `notify` | V2-M1 T1 S1/S5/S7, T6 |
| | Option 1 (chosen) | V2-M1 T3, T5, T6, T9 |
| | Option 2 (explicit contract) | Later (explicit-signal fallback) |
| | Option 3 (headless) | Rejected; runs stay interactive (V2-M1 T4) |
| | Option 4 (polling without hooks) | Rejected as primary; hooks trigger reads (V2-M1 T9) |
| | Option 5 (tmux exit = failure only) | V2-M1 T9 (`sessions.changed` ⇒ `exited`) |
| | Option 6 (LLM classifier) | V2-M5 |
| | Risk: marker text in unrelated output | V2-M1 T1 S6, T5/T6 decoy fixtures, T9 *Decoy* e2e |
| | Risk: stale or unrelated goal | V2-M1 T5/T6 binding, T9 |
| | Risk: format changes between versions | V2-M1 T5/T6 (per-version fixtures, `client_version`, `unknown` ⇒ attention) |
| | Risk: hook never fires | V2-M1 T9 (stale timer, `sessions.changed`) |
| | Risk: owner clears/replaces the goal | V2-M1 T5/T6 (condition match), T9 |
| | Risk: evaluator talked into "met" | Accepted for PoC; V2-M4 gates |
| §3 Decisions | 1 Interactive TUI in tmux | V2-M1 T4, T7 (stubs stay attached) |
| | 2 Item = agent + flags + instruction; argv built by hostbud; initial prompt argument; no `send-keys` | V2-M1 T5, T6, T8 (validation) |
| | 3 A queue belongs to one project; single queue in PoC | V2-M1 T8; lifted in V2-M2 |
| | 4 Done = client's own `/goal` achieved | V2-M1 T5, T6, T9 |
| | 5 Per-run hooks + structured state, read-only, nothing installed | V2-M1 T1, T3, T5, T6 |
| | 6 Fail closed | V2-M1 T9 |
| | 7 Never kill a run's session | V2-M1 T9 (criterion 12) |
| | 8 Adapters + run protocol | V2-M1 T5, T6; Later (more clients) |
| | 9 Everything after PoC opt-in | V2-M2–M5 switches |
| §4 Components | Queue panel, REST + events WS | V2-M1 T8, T10, T11 |
| | Queue service, hook endpoint, events bus, inventory poller | V2-M1 T8, T3, T9 |
| | Dispatcher, session-create service, adapters via `sshx` | V2-M1 T9, T4, T5/T6 |
| | Packages `internal/queue`, `internal/agents`; SQL only in `store` | V2-M1 T2, T5, T8; T12 (§14 docs) |
| | Events `queue.changed`, `run.changed`; no UI polling | V2-M1 T8, T9, T10 |
| | No new tmux poll loop | V2-M1 T9 |
| §5.1 Starting | 1 First queued item of a running queue with no active run | V2-M1 T9 |
| | 2 Run with a 32-byte token, SHA-256 stored | V2-M1 T3 |
| | 3 Session-create service: machine, path, name, env, startCommand | V2-M1 T0 (service), T4 |
| | 4 `starting` until `SessionStart`; binding recorded | V2-M1 T9, T1 S4 |
| §5.2 Signals | `SessionStart` (incl. second id ⇒ attention) | V2-M1 T9 |
| | `Stop` ⇒ `ReadGoalState` mapping | V2-M1 T9, T5, T6 |
| | `SessionEnd` | V2-M1 T9 |
| | Session missing from `sessions.changed` | V2-M1 T9 |
| | Stale after `HOSTBUD_RUN_STALE_AFTER` | V2-M1 T9, T1 S9 |
| §5.3 Binding | Claude: top-level record, bound session, after start, equal condition | V2-M1 T5, T1 S5 (timestamp) |
| | Codex: row for bound thread, `complete`, after start, equal objective | V2-M1 T6 |
| | `failed` / `pending` mapping, usage limits ⇒ pending ⇒ stale | V2-M1 T5, T6, T9 |
| §5.4 State machines | queue / item / run states and transitions | V2-M1 T2 (CHECKs), T9 |
| | Only `achieved` advances; others ⇒ attention + paused | V2-M1 T9 |
| | Pause never touches the session | V2-M1 T9 |
| | Late achieved after stale | V2-M1 T9, T3 (token kept on stale) |
| | `cancelled` run state | V2-M1 T9 (owner action on an item with an active run) |
| §6 Schema | `queues`, `queue_items`, `runs`, `run_events`; `machine_id` everywhere | V2-M1 T2 |
| | `run_events` payload cap, `transcript_path` kept, audit trail | V2-M1 T2, T3 |
| | Replaces the v1 §10 sketch; capacity with V2-M2 | V2-M1 T2, T12; V2-M2 T1 |
| §7 Adapter | `Adapter` interface, `GoalState` | V2-M1 T5 |
| | Claude command, SFTP incremental read, path under `~/.claude/projects/` | V2-M1 T5 |
| | Codex command, reader decided in spike | V2-M1 T1 S7, T6 |
| | Hook command: always exits 0, prints nothing, token via env | V2-M1 T5, T6, T7 (stubs run it) |
| §8 Run protocol | Env vars `HOSTBUD_URL`, `HOSTBUD_RUN_ID`, `HOSTBUD_RUN_TOKEN` | V2-M1 T3 (+ `HOSTBUD_HOOK_BASE_URL`), T4 |
| | Endpoint, events, body, response codes, no Origin/cookie | V2-M1 T3 |
| | Goal-state contract: structured state only | V2-M1 T5, T6 |
| | Adding a client | Later (more clients) |
| | Explicit payload fallback | Later |
| §9 Security | Token size, hashing, constant-time, scope, revocation, not logged | V2-M1 T3 |
| | Endpoint rate-limited and body-capped; cannot run commands | V2-M1 T3 |
| | `transcript_path` untrusted: absolute, cleaned, under data dir, read-only SFTP | V2-M1 T5 (and T6 if the rollout reader is chosen) |
| | argv through `sshx`; flags split and quoted | V2-M1 T5, T6, T4 |
| | Token in the process list; spike checks `set-environment` | V2-M1 T1 S8, T4 |
| | No destructive tmux action; dispatcher only creates | V2-M1 T9 (criterion 12) |
| §10 Milestones | V2-M1 prerequisite (start-command bug) | V2-M1 T0 |
| | V2-M1 T1 spike (all six checks) and recording results | V2-M1 T1 (S1–S8, plus S9/S10) |
| | V2-M1 Build (migration/store, hooks/tokens, adapters, dispatcher, API/events, panel) | V2-M1 T2–T11 |
| | V2-M1 E2E (stubs; in order, decoy, attention, retry/skip, desktop/phone) | V2-M1 T7, T9, T10, T11 |
| | V2-M1 Accept (real Claude then Codex) | V2-M1 *Manual checks (owner)* |
| | V2-M2 multiple queues, per-machine cap | V2-M2 |
| | V2-M3 notifications | V2-M3 |
| | V2-M4 verify command / manual approval | V2-M4 |
| | V2-M5 LLM supervisor, flag only, pluggable provider | V2-M5 |
| §11 Open questions | Q1 Codex inline hooks and merging | V2-M1 T1 S1 |
| | Q2 Slash command as initial prompt | V2-M1 T1 S2 |
| | Q3 Workspace trust | V2-M1 T1 S3, T10 (actionable error) |
| | Q4 Codex reader | V2-M1 T1 S7, T6 |
| | Q5 Claude record timestamp | V2-M1 T1 S5, T5 |
| | Q6 Stale window default | V2-M1 T1 S9, T9 |
