# V2-M1 — Proof of concept: one queue, sequential: tasks

Goal: the owner creates one queue for a project, adds items (agent, flags, `/goal …` instruction) and presses Start. hostbud runs each item in its own interactive tmux session, exactly like a session the owner starts by hand. It starts the next item only when the client's own `/goal` evaluator has recorded the queued goal as achieved, as a structured record bound to that run. Anything else (a failure, an exit, silence, a changed session, a format hostbud doesn't recognise) pauses the queue and asks for the owner's attention. hostbud never kills a run's session, installs nothing on the host and changes no user config. The queue is fully operable from the desktop and the phone, and the UI follows it live from events.

This is **default behavior**, not opt-in. Scope: [ARCHITECTURE.md](ARCHITECTURE.md) (v2) §1–§9 and §10 *V2-M1* · roadmap: [ROADMAP.md](ROADMAP.md#v2-m1--proof-of-concept-one-queue-sequential) · checklist: [V2-M1-acceptance.md](V2-M1-acceptance.md).

Citations: **v2 §N** is [ARCHITECTURE.md](ARCHITECTURE.md) in this directory; **v1 ARCHITECTURE §N** is [../ARCHITECTURE.md](../ARCHITECTURE.md); **v1 ROADMAP** is [../ROADMAP.md](../ROADMAP.md).

**Out of scope** (don't build, don't half-build): several queues or parallel runs (V2-M2), notifications (V2-M3), verify commands and approval (V2-M4), LLM classification (V2-M5), multi-machine, clients other than Claude Code and Codex, the explicit-signal fallback payload (v2 §8, *Later*), queue templates.

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T0 Prerequisite: fix "session start command fails to create the session" | Not started |
| T1 Spike on this host | Not started |
| T2 Schema migration and store | Not started |
| T3 Run tokens and the hook endpoint | Not started |
| T4 Session creation for runs | Not started |
| T5 Adapter interface and the Claude Code adapter | Not started |
| T6 Codex adapter | Not started |
| T7 Stub clients on the throwaway target | Not started |
| T8 Queue service and REST API | Not started |
| T9 Dispatcher and state machines | Not started |
| T10 Queue panel (desktop) | Not started |
| T11 Queue panel on the phone | Not started |
| T12 Docs alignment | Not started |
| T13 Milestone acceptance | Not started |
| T14 Safe Docker cleanup | Not started |

## Preconditions (check before T0)

Check each one and record the result in the Progress note under the checkpoint table. A precondition that isn't met is fixed first (P3 is T0 itself); open owner items don't count against them.

| # | Precondition | How to check |
|---|---|---|
| P1 | v1 through M7 is done, including M7's full e2e run (twice green). M8 may still be in progress. | [../roadmap/M7-acceptance.md](../roadmap/M7-acceptance.md) *Definition of done* is ticked except owner items; the T13 run is recorded there. |
| P2 | The v1 obligations in v1 ARCHITECTURE §10 hold: (1) typed events on the `events` bus; (2) one session-create service accepting `env` and `startCommand`; (3) a dialect-agnostic store with append-only migrations; (4) room for token-authenticated `/api/hooks/*` that bypass the browser Origin check. | (1) `internal/events` has typed `Type` constants (`sessions.changed`, `projects.changed`); (2) `session.Service.Create(ctx, Spec{Machine, Name, Path, Env, StartCommand})` is the only creation path (T0 re-checks it); (3) `internal/store/migrations` is numbered and append-only; (4) no middleware makes the `/api/hooks/` prefix impossible to exempt (T3 builds the exemption). |
| P3 | The v1 ROADMAP *Later* bug "session start command fails to create the session" is fixed and verified. | This is **T0**. |

## Rules for this milestone

- **Order.** Work top to bottom, one task at a time, and don't start a task until the previous one is done. **T1 (spike) comes before any product code** except T0's bug fix, and its results are recorded in v2 ARCHITECTURE before T2 starts.
- **Read the coverage lines first.** Before each task, read the U/I/E coverage lines that [V2-M1-acceptance.md](V2-M1-acceptance.md) assigns to it, plus its own **Tests:** and **E2E:** lines. Write all of them in the same commit(s) as the behavior. Never leave tests or scenarios for a later task.
- **E2E runs once, in T13** (v2 ROADMAP *Rules*, owner's decision 2026-09-27). T0–T12 write their scenarios and type-check them (`tsc` for `test/e2e`), but **never** run `make e2e`, `make e2e-up` or `make e2e-run`: not per commit, not per task, not at checkpoints. T13 runs the full suite (both profiles, all v1 and v2 scenarios), fixes every failure and reruns until green. Until then an E item counts as *written*, not *passed*. This replaces, for V2-M1, the post-M7 rule in AGENTS.md and v1 ARCHITECTURE §13.1 ("required again before every commit").
- **E2E scenarios are never deferred.** Every task has an **E2E:** line. A scenario is written in the same commit as the behavior (API-level before the UI exists) and is tagged with the task that writes it, both here and in the checklist.
- **Batched test checkpoints** (continuing v1 M3–M7): each commit runs only fast checks: `go build` / `go vet` for the Go packages it touches, `vue-tsc` for the frontend, and `tsc` for the e2e suite when it changes. `make gitleaks` runs on every commit through the pre-commit hook. Full `make lint test` runs at the checkpoints below. A checkpoint failure is fixed (with a regression test if it's a bug) before the next task starts, and the checkpoint re-runs until green.
- **No real agents in automated tests.** Unit, integration and e2e tests use fixtures and the **stub `claude` / `codex` executables** from T7 on the throwaway target. Real Claude Code and Codex are used only in the T1 spike (on the host, by the agent) and in the owner's manual checks.
- **Host safety** (v1 rules, still binding):
  - hostbud never kills a run's session (v2 §3.7). The dispatcher only ever *creates* sessions. Closing one goes through the existing confirmation dialog, by the owner. The T1 spike also never kills a session: it lists the sessions it made for the owner to close.
  - Nothing is installed on the host, and no user config is changed: tmux, shell, `~/.claude`, `~/.codex`, `~/.ssh` (v2 §3.5). Hooks are injected per run only (`--settings`, `-c`). Goal state is read **read-only**.
  - Migrations are append-only and never drop or rewrite user data.
  - Every remote command goes through `sshx` from argv, shell-quoted (v2 §9). User flags are split and quoted, never interpolated.
  - Don't restart, stop or recreate the production containers before T13's deploy, and never touch the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data` or `hostbud-caddy-config`. Integration tests run against `test/sshd` and throwaway databases only.
- **Owner items never block** (AGENTS.md). Anything that needs the owner's accounts, devices or decisions (a client not logged in on the host, trusting a workspace, dropping a client after a failed spike check, the real-client end-to-end check) is recorded as **open** under *Manual checks (owner)* in the checklist and in the summary. Take the safe default the docs name and continue; never wait.
- **Public repo.** No real hostnames, IPs, usernames, home paths, session ids, thread ids or tokens in docs, fixtures or tests. Spike output and client fixtures are **redacted**: `/home/dev` paths, `example.com`, `server-a`, made-up ULIDs/UUIDs. `make gitleaks` before every commit.
- **New env vars** (`HOSTBUD_HOOK_BASE_URL` in T3, `HOSTBUD_RUN_STALE_AFTER` in T9): each goes to `.env.example` with a placeholder and a comment, to `internal/config` with validation (an invalid value is a startup error naming the variable), to the `hostbud` service's explicit `environment:` list in `docker-compose.yml`, and to v1 ARCHITECTURE §12 (T12). The M7 `check-docs.sh` / deploy-config tests must stay green.
- **Design moves go into the docs in the same commit.** When a task changes the design (the spike's findings, a new column, the 429 response, the reader choice), it updates v2 ARCHITECTURE §5/§6/§7/§8/§9 in the same commit. v1 docs are aligned in T12.
- **Docker cleanup ends the milestone** (v2 ROADMAP *Rules*, owner's request 2026-09-27). The last task (T14) frees the disk the milestone's builds and e2e run used, with the repo's own `make docker-clean` (without `CACHE=1`), after T13's deploy. It never touches the production stack, its volumes, backups, other projects' Docker objects, or work another agent is running; if something is in use, it's skipped and recorded, never forced.
- **Bugs found on the way** follow the bug-fix workflow: a failing regression test in one commit, the fix in the next.
- **Commits.** Conventional commits, small and focused; commit only this task's files. Other agents may be working in the tree: stage explicit paths (never `git add -A`) and check `git status` before touching a shared file (`docker-compose.yml`, `test/e2e/compose.yml`, `test/e2e/helpers/*.ts`, `playwright.config.ts`, `Makefile`, `.env.example`, `internal/config/config.go`, `internal/api/testdata/routes.json`). Coordinate rather than overwrite.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T0 (start-command fix) | `make lint test` | Not run |
| CP2 | T2 + T3 + T4 (schema, hooks, run sessions) | `make lint test`, `scripts/compose-config.sh` (new env var reaches only `hostbud`) | Not run |
| CP3 | T5 + T6 + T7 (adapters, stubs) | `make lint test`, e2e `tsc` | Not run |
| CP4 | T8 + T9 (queue API, dispatcher) | `make lint test` **three times in a row** (flake check: timers and concurrent signals), e2e `tsc` | Not run |
| CP5 | T10 + T11 + T12 (panel, docs) | `make lint test`, `vue-tsc`, e2e `tsc`, `make gitleaks`, docs consistency check | Not run |
| CP6 | T13 (milestone acceptance) | `make e2e` (full suite, both profiles) until green; then `make lint test` after the last fix; `make gitleaks`; `make deploy` | Not run |
| — | T14 (Docker cleanup) | no test run; post-cleanup health check only | Not run |

**Progress note:** (record preconditions P1–P3 here when checked, and each checkpoint's result.)

**What e2e can and can't reach.**
- **Real agents:** never. Every run in e2e uses the T7 stubs on `hostbud-e2e-target`. A stub's behavior (achieve after N turns, decoy only, fail, exit, go silent, change session id) is set per run by a behavior file the runner writes on the target over SSH (`helpers/stubs.ts`).
- **Hooks:** the stub runs the exact hook command that hostbud injected, so the e2e path is real end to end: tmux env → `curl` in the target → `hostbud-e2e-caddy` loopback site → `hostbud-e2e-app` → dispatcher → adapter read over SFTP/`sshx` → next session. The e2e app sets `HOSTBUD_HOOK_BASE_URL` to Caddy's loopback site by service name, because `127.0.0.1` inside the target isn't Caddy.
- **Stale:** `hostbud-e2e-app` gets a short `HOSTBUD_RUN_STALE_AFTER` (seconds), so a silent stub goes stale inside the scenario's timeout.
- **Restart safety:** the ctl service restarts `hostbud-e2e-app` (a new ctl action, T9) during *Two items in order*.
- **Out of e2e:** log contents (U/I log-hygiene tests), the token in the host's process list (I, T4), the user's real `~/.claude`/`~/.codex` files (I checksums in T4/T9 against `test/sshd`), and real client formats (fixtures from the T1 spike, U/I in T5/T6). Real clients end to end are the owner's check.

---

## T0 — Prerequisite: fix "session start command fails to create the session"

Runs are created through exactly the path that the v1 ROADMAP *Later* bug says is broken (v2 §10 prerequisite; ROADMAP precondition P3). Fix it first, inside the single service, so every entry point (the UI dialog, the API, the dispatcher later) benefits.

- **Reproduce** first on the e2e target (through the UI and `POST /api/machines/host/sessions` with `startCommand`), then on the host (create a session with a start command from the UI, in a scratch folder; leave the session open for the owner to close; never kill it). Record the exact symptom (error shown, session missing, session created then gone).
- **Find the root cause** in the session-create path, i.e. `tmux new-session … <command>` built by `sshx`. Candidates to rule in or out, each with evidence:
  - quoting: the start command is double-quoted or passed as one argv element where tmux expects a shell command string (or the reverse);
  - the path: `-c <dir>` resolution for `~/…`, spaces, a missing directory;
  - the shell used for the command: tmux runs it with `default-shell -c`; a login-shell dependency (PATH) makes the command not found;
  - the session exiting at once because the command ends (the session is created and vanishes before the inventory sees it, or `remain-on-exit` is off);
  - the name/collision logic or the `ready()` precondition rejecting it.
- **Fix it in `session.Service.Create`** (the single session-create service, v1 ARCHITECTURE §10 obligation 2). Confirm and document that:
  - the service accepts `{machine, name, path, env, startCommand}` (`session.Spec`);
  - `env` becomes `tmux new-session -e KEY=VALUE` per entry (tmux ≥ 3.2; the service already refuses env on older tmux with an actionable error). Add `env` support if any part is missing;
  - the command runs in the given path with the user's normal shell environment, and the session is visible in the next `sessions.changed`.
  - The start command stays a string (the user typed it); T4 adds the argv form for runs, quoted through `sshx`.
- **Close the *Later* item** in the v1 ROADMAP (move it to the fixed list or strike it with the fixing commit).
- **Bug-fix workflow:** the failing regression test (I against `test/sshd`) in one commit, the fix in the next.

**Tests:** U (Go): command construction for start commands with spaces, single and double quotes, `$`, flags (`claude --model x`), and a `~/…` path; env entries become one `-e KEY=VALUE` each, quoted; a name collision still suffixes. I (Go, `test/sshd`): a session created with a start command exists after creation, runs in the given path (`#{pane_current_path}`), and the command's output is visible in `tmux capture-pane -p`; a start command containing quotes works; a command that ends at once gives an actionable error or keeps the session per the fix (whichever the fix defines), never a silent success.

**E2E:** add `start-command.spec.ts` (desktop) and `start-command.phone.spec.ts` (`iphone-13-pro`) **(T0) Session with start command:** open New session, choose a folder in the file browser, type a start command that prints a marker (e.g. `sh -c 'echo HOSTBUD_START_OK; exec sh'`), create; the session appears under its project and the terminal shows the marker. Also add the API-level variant to `sessions` API tests if a spec exists for it (`api.api.spec.ts`): `POST /api/machines/host/sessions` with `startCommand` → the session exists on the target with the right path. Type-check only.

**Done:** the root cause is written in the commit message and the v1 ROADMAP *Later* item is closed; a session with a start command is created, runs the command in the chosen path, and is visible; `env` becomes `-e` entries; CP1 green; the scenarios compile.

## T1 — Spike on this host (first; no product code)

Answer v2 §11 Q1–Q6 and the other unknowns with **both real clients** before building (v2 §10 *T1 spike*). Run each check in a scratch directory the owner doesn't use for work (e.g. a new folder under the home directory, never a real project). The spike **creates sessions but never kills one**: every session it made is listed in the summary for the owner to close from the UI. It changes no user config: take a checksum of `~/.claude/settings.json`, `~/.claude/settings.local.json` (if present), `~/.codex/config.toml` and any `~/.codex` hooks file **before and after**, and record that they match.

If a client isn't installed or isn't logged in on the host, don't log it in: record that as an open owner item and run what can be run (the safe default below applies to that client's checks).

| Check | Answers | What to record |
|---|---|---|
| **S1** Per-run hook injection: Claude `--settings '<json>'`, Codex `-c 'hooks.<Event>=[…]'` | v2 §11 Q1 | The exact working syntax for `SessionStart`, `Stop` and `SessionEnd` in each client. Whether Codex inline hooks **merge** with the user's `~/.codex` hooks and don't replace them (test with a harmless user-level hook present in a scratch `CODEX_HOME` copy, never by editing the real one). That the user's settings files are unchanged afterwards (checksums before and after). |
| **S2** Slash command as the initial prompt: `claude '/goal …'`, `codex '/goal …'` | v2 §11 Q2 | Whether both TUIs execute `/goal` from the argument (not as plain text), and that the goal is active afterwards (Claude's goal indicator / transcript, Codex's `thread_goals` row with `status='active'`). |
| **S3** Workspace trust | v2 §11 Q3 | Whether `--settings` hooks run in a directory Claude Code hasn't trusted yet (and the Codex equivalent: an untrusted project / sandbox prompt). If not, the exact actionable message hostbud shows (text for T10's error), and how the owner trusts a folder once. |
| **S4** `SessionStart` id capture | v2 §5.1 step 4 | The fields in each client's hook stdin (`session_id`, `transcript_path`, `cwd`, `hook_event_name`, `model`, `permission_mode`, `turn_id`, …), captured with a local sink (`nc -l` on loopback or a hook command that appends stdin to a file in the scratch dir). What a `/clear`, a resume and a compaction do to the id (does `SessionStart` fire again, with which id?). For Codex: whether the hook's `session_id` **is** the `thread_goals.thread_id`. |
| **S5** Goal-record formats | v2 §2, §5.3, §11 Q5 | Real Claude `goal_status` lines for *met*, intermediate *not met*, and *impossible* (how "impossible" is distinguished from not-met: a field or the reason). Whether the line (or its envelope) carries a **timestamp**. Codex `thread_goals` rows through `active`, `complete`, `blocked`, `paused`, `usage_limited` (and `budget_limited` if reachable). The **client versions** used (`claude --version`, `codex --version`). Redact and save the samples as fixture candidates for T5/T6. |
| **S6** Decoy text | v2 §2 risks | Have the agent print and quote `{"type":"attachment","attachment":{"type":"goal_status","met":true,…}}` and the word "achieved" (in its reply, in a file it reads, in a tool result). Confirm the marker appears only **nested** (inside tool results or message content), never as a top-level record. Same for Codex (objective text or messages mentioning `complete`). |
| **S7** Codex reader | v2 §7, §11 Q4 | Option A: host `sqlite3`/`python3` read-only query (`file:…/goals_1.sqlite?mode=ro`, behavior with WAL while Codex writes: does a read-only open see committed rows, does it need `-wal`/`-shm` access, does it ever block the writer). Option B: the rollout JSONL at `transcript_path` (is the goal status a structured record there?). **Pick one** and give the reasons. Note the host dependency, if any, and its install hint. |
| **S8** tmux env / token exposure | v2 §9 | Whether `tmux new-session -e HOSTBUD_RUN_TOKEN=…` shows up in `ps -eo args` on the host, and for how long (sample in a loop during creation). Whether `set-environment` fed through stdin (`tmux source-file -` with `set-environment -t …`) or a similar stdin route avoids it, and whether the variable then reaches the session's first process (it must be set **before** the start command runs). |
| **S9** Stale window | v2 §11 Q6 | The longest gap between `Stop` hooks seen in real goal runs with background work (sub-agents, long builds). Confirm or change the 90-minute default for `HOSTBUD_RUN_STALE_AFTER`. |
| **S10** Hook reachability | v2 §8 | From a shell on the host, `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` through Caddy works (from inside a tmux session, as a hook would run it). From the e2e target, the equivalent base URL (Caddy's loopback site by service name) works; this sets the T3/T7 `HOSTBUD_HOOK_BASE_URL` value. |

- **Output:** a new section **"12. Spike results"** in v2 [ARCHITECTURE.md](ARCHITECTURE.md), with:
  - one entry per check (S1–S10): what was run (redacted), what happened, the conclusion;
  - the client versions tested (these become `MinVersion()` in T5/T6 unless a lower version is proven);
  - the chosen answer to each §11 question (Q1–Q6), and §11 marked as answered with links to the entries;
  - any design change written into v2 §5 / §7 / §9 **in the same commit** (for example: the Codex reader, the token-passing method, how "impossible" is recognised, timestamp vs. line order).
- **Redaction:** all host-specific values are replaced: `/home/dev` paths, made-up ULIDs/UUIDs, `example.com`. No real session ids, thread ids, tokens, usernames or project names.
- **If a check fails:** record it and write the alternative into v2 §7 before T2. The alternative must keep the §3 decisions: interactive TUI, no host install, no user config changes, no `send-keys` typing. If no such alternative exists for one client, **safe default:** V2-M1 continues with the other client only. The dropped client is recorded as an **open owner decision** (checklist *Manual checks (owner)*), its adapter task (T5 or T6) is reduced to "refuse the agent with an actionable message", and its e2e scenarios are marked n/a with the reason. Never block on the decision.
- **Leftovers:** list every session the spike created (names only, redacted in the repo; real names only in the chat summary) for the owner to close.

**Tests:** n/a: research only. The recorded formats become fixtures in T5/T6 and drive the stubs in T7.

**E2E:** none: nothing reachable. The recorded formats drive the stub clients in T7.

**Done:** v2 ARCHITECTURE §12 answers S1–S10 and §11 Q1–Q6 with tested versions, redacted; design changes are in §5/§7/§9; user config checksums match before and after; leftover sessions are listed; any dropped client is an open owner decision.

## T2 — Schema migration and store

The v2 tables from v2 §6, append-only, with the only SQL in `store` (archtest keeps it there).

- **One append-only migration** (the next free number in `internal/store/migrations`, e.g. `0005_queues.sql`) creating:
  - `queues(id, machine_id FK, project_id FK, name, status, created_at, updated_at)` with `CHECK(status IN ('idle','running','paused','finished'))`;
  - `queue_items(id, queue_id FK, position, agent, flags TEXT, instruction TEXT, status, created_at, updated_at)` with `CHECK(agent IN ('claude','codex'))`, `CHECK(status IN ('queued','running','done','needs_attention','skipped'))` and **`UNIQUE(queue_id, position)`**;
  - `runs(id, item_id FK, machine_id, session_name, agent_session_id NULL, client_version NULL, token_hash, status, started_at, ended_at NULL, last_signal_at NULL, detail TEXT NULL, transcript_offset BIGINT NULL)` with `CHECK(status IN ('starting','running','achieved','failed','exited','stale','cancelled'))`;
  - `run_events(id, run_id FK, source, kind, payload_json, created_at)` with `CHECK(source IN ('hook','poller','timer','user','llm'))`;
  - **`machine_id` on every table** (queues directly; `queue_items` inherits through its queue per v2 §6 *and* gets its own column so every table carries it; runs and run_events carry it for the later multi-machine case);
  - indexes on `runs(item_id)`, `runs(status)` and `run_events(run_id, created_at)`;
  - foreign keys that **never cascade-delete runs or events** implicitly beyond what the queue-delete rule allows (T8 refuses deleting a queue with an active run; deleting a queue removes its items, runs and events in one transaction, explicitly).
- **Ids:** `runs.id` is a **ULID** (v2 §8, it appears in `HOSTBUD_RUN_ID` and the hook URL). Other ids follow the existing store convention.
- **Token:** `runs.token_hash` holds the SHA-256 only (32 bytes, `BYTEA`), never the token.
- **Payload cap:** `run_events.payload_json` is stored with a size cap of 64 KiB (the same as the hook body cap); an oversized payload is refused by the store with a typed error (the endpoint answers 413 before it gets there). `transcript_path` is kept inside the stored hook JSON (v2 §6 audit trail).
- **Transcript offset:** the per-run byte offset for the incremental transcript read (v2 §7) is the nullable `runs.transcript_offset` column in this same migration. Record it as a design addition in v2 §6 in this commit.
- **Store methods** (the only SQL; all take the caller's context and obey M7's statement/lock timeouts):
  - queue CRUD: create, get, list (by machine/project), update name/status, delete (explicit transaction: items, runs, events, queue);
  - item CRUD: create (appended at the next position), get, list by queue ordered by position, update (agent, flags, instruction, status), delete (positions re-densified in the same transaction);
  - **transactional reorder** that keeps positions dense `1..n` and respects `UNIQUE(queue_id, position)` (e.g. shift to negative temporaries, then renumber);
  - run create (with token hash, `starting`, `started_at`);
  - **guarded status transitions:** `UPDATE runs SET status = $new … WHERE id = $id AND status = $expected` returning whether a row changed, so two concurrent signals can't double-advance; the same guard for item and queue status changes;
  - run lookup by id, by token hash, and by active item (the non-terminal run of an item);
  - list active runs (for restart recovery, T9);
  - `last_signal_at`, `agent_session_id`, `client_version`, `detail`, `transcript_offset`, `ended_at` setters;
  - event append (`run_events`), and list events of a run (newest first, bounded) for the panel's "why".
- The superseded `tasks` / `machine_capacity` sketch from v1 ARCHITECTURE §10 is **not** created (capacity arrives in V2-M2 T1).
- v2 ARCHITECTURE §6: the final columns (including `transcript_offset`, the extra `machine_id`s and the run status CHECK) in this commit.

**Tests:** U (Go, store against the test PostgreSQL as the store's existing tests do): status CHECKs reject bad values for queues, items, runs, sources and agents; `UNIQUE(queue_id, position)` holds; reorder keeps positions dense for moves up, down, first↔last, and a no-op; delete re-densifies; guarded transitions refuse a wrong source state and report "not changed"; two goroutines racing the same guarded transition → exactly one wins; payload over 64 KiB refused; lookup by token hash; queue delete removes items/runs/events and nothing else. I (Go): the migration applies on a **copy of a v1 database with data** (users, sessions, projects, UI state, rate-limit rows from 0001–0004 seeded by a fixture) and keeps every v1 row unchanged (row counts and a checksum per table before/after); applying the migration twice is a no-op; no earlier migration file changed (the archtest/append-only check).

**E2E:** none reachable yet (no endpoint). The schema is exercised by T3's *Hook endpoint* (via the seed helper) and T8's *Queue API* onward. Add `helpers/db.ts` functions to seed a queue, item and run (with a known token) for T3's API scenario. Type-check only.

**Done:** one append-only migration creates the four tables as designed, v1 data survives it, the store has every method listed with guarded transitions, and v2 §6 matches.

## T3 — Run tokens and the hook endpoint

The machine-to-server endpoint that agent hooks call (v2 §8, §9; v1 ARCHITECTURE §10 obligation 4).

- **Token** (`internal/queue` or a small `internal/runtoken` helper; no SQL outside `store`):
  - 32 random bytes from `crypto/rand`, encoded **base64url** (no padding) for the env var;
  - only the **SHA-256** is stored (`runs.token_hash`);
  - compared in **constant time** (`subtle.ConstantTimeCompare` on the hashes) after looking the run up by id;
  - **scoped to one run**: a token is only valid on its own `run_id`'s URL;
  - **revoked when the run is finished with**: `achieved`, `failed`, `exited`, `cancelled` (the hash is cleared or the run is terminal and the endpoint answers 410). It is **not** revoked on `stale`, because a late `achieved` must still be accepted (v2 §5.4).
- **Endpoint** `POST /api/hooks/{run_id}/{event}` with `event ∈ {session_start, turn_end, session_end}` (v2 §8):
  - `Authorization: Bearer <token>` is the **only** credential. The route is exempt from the cookie session, the Origin allowlist and the Tailscale identity gate (M7 T8), and this exemption is explicit (a named allowlist in the middleware chain with a reason, and an archtest/route-table rule that nothing else is exempt);
  - order of checks: unknown event → 404; unknown run → **404**; bad or missing token → **401**; run ended (terminal, not `stale`) → **410**; body over 64 KiB → **413**; not JSON → 400; accepted → **204**;
  - body: capped at 64 KiB (read through a limited reader), must be valid JSON (`Content-Type: application/json`, as M7's rule for bodies requires; the injected hook command sends it);
  - **rate-limited per run** (a token bucket keyed by run id; value a constant documented in v1 ARCHITECTURE §15 in T12, proposed 60 requests/minute with a burst of 20). Exceeding it answers **429** with no side effect. 429 is a design addition to v2 §8's response list: record it there in this commit;
  - on accept, it **only** writes a `run_events` row (`source='hook'`, `kind=<event>`, `payload_json` = the body as sent, `transcript_path` kept) and notifies the dispatcher (a channel/interface the T9 dispatcher implements; until T9 a no-op). It **never runs a command** (v2 §9) and never reads the host synchronously in the request (the dispatcher does that).
  - The route is added to `internal/api/testdata/routes.json` with a new `token_auth: true` attribute, so M7's shared route tests treat it correctly: the Origin test expects it to be **exempt** (and the drift test fails if the router and the file differ); the auth table expects bearer, not cookie.
- **`HOSTBUD_URL` for sessions:** default `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}` (through Caddy on loopback, v2 §8). New override **`HOSTBUD_HOOK_BASE_URL`** so the e2e target can reach Caddy by service name:
  - `.env.example`: `HOSTBUD_HOOK_BASE_URL=` (empty = default) with a comment;
  - `internal/config`: read and validated (empty, or an absolute `http`/`https` URL without a path, query or fragment); an invalid value is a startup error naming the variable;
  - `docker-compose.yml`: added to the `hostbud` service's `environment:` list only.
- **Caddy:** confirm the loopback site proxies `/api/hooks/*` unchanged, including the `Authorization` header, and that no header rewriting or Tailscale gate applies there. **No new published port.** The domain site may serve it too (the token is the only credential), but `HOSTBUD_URL` never points at it in V2-M1.
- **Logging:** never log tokens, `Authorization` headers or hook bodies at info. Log the run id and event at **debug** only; 401/429 at info with the run id only (no token, no remote details beyond what M7 already logs).
- **Security checklist:** the AGENTS.md item "(v2) Hook endpoints use per-run tokens (stored hashed)" becomes **active** from this task (the box is ticked in T12 when all of it holds).
- v2 ARCHITECTURE §8 (429, check order, `HOSTBUD_HOOK_BASE_URL`), §9 (rate-limit value).

**Tests:** U (Go): token generation (length, alphabet, uniqueness over many draws), hashing, constant-time compare path; each response code (`204`, `401` missing and wrong token, `401` for another run's token, `404` unknown run and unknown event, `410` for each finished state, `413` at 64 KiB + 1, `400` non-JSON, `429` after the burst); revocation for `achieved`/`failed`/`exited`/`cancelled` and **none** for `stale` (a hook on a stale run is `204`); a `204` writes exactly one `run_events` row with the body and notifies the dispatcher fake; no cookie and no Origin needed, and a foreign Origin doesn't change the answer; config parsing of `HOSTBUD_HOOK_BASE_URL` (empty, valid, with path → error, garbage → error); route table has the hook route with `token_auth`. I (Go): through the **real Caddy config** (the M7 Caddy integration harness), a hook POST with a token reaches the app (`204`) and the `Authorization` header arrives intact; one without a token gets `401` with no cookie or Origin needed; the **log-hygiene test** (M7 T10) is extended: a hook call with a known token and body leaves neither in the logs at info; `internal/deploytest`: `HOSTBUD_HOOK_BASE_URL` reaches only the `hostbud` service.

**E2E:** add `hooks.api.spec.ts` (desktop, API-level, through `hostbud-e2e-caddy`) **(T3) Hook endpoint:** a run seeded through `helpers/db.ts` with a known token accepts `session_start` with its token (`204`) and the event is recorded; wrong token → `401`; another run's token → `401`; unknown run → `404`; an ended run → `410`; a 65 KiB body → `413`; the request carries a foreign `Origin` header and no cookie and is still `204` (Origin doesn't matter); a burst beyond the limit → `429`. Type-check only.

**Done:** tokens are random, hashed, constant-time compared, run-scoped and revoked on finish but not on stale; the endpoint answers exactly per v2 §8 (+429), is exempt only from cookie/Origin/Tailscale, is capped and rate-limited, only records and notifies; `HOSTBUD_HOOK_BASE_URL` is configured and documented; logs stay clean; the scenario compiles.

## T4 — Session creation for runs

A run's session is created through the single session-create service (v2 §5.1 step 3), never by a separate code path.

- The run-session builder calls `session.Service.Create` with:
  - `machine`: the host (`machine_id` from the queue);
  - `path`: the queue's project path;
  - `name`: `<project>-q<position>` (the project's display name sanitized to a valid tmux session name as v1 does; `position` is the item's position), with collisions resolved by the **v1 M6 rule** (`-1`, `-2`, …);
  - `env`: `HOSTBUD_URL` (the `HOSTBUD_HOOK_BASE_URL` override or the loopback default), `HOSTBUD_RUN_ID` (the ULID), `HOSTBUD_RUN_TOKEN` (the plain token, only in memory);
  - `startCommand`: the adapter's **argv** (T5/T6), passed to `sshx` as argv and shell-quoted there, never joined by string concatenation. If `Spec.StartCommand` is a string today, add an argv field (`StartArgv []string`) to `session.Spec` handled by the same `Create` function (one service, two input forms; the UI keeps the string form).
- **Apply the S8 result:**
  - if a stdin-fed `set-environment` hides the token from the process list **and** the variable reaches the first process, set `HOSTBUD_RUN_TOKEN` that way (and `HOSTBUD_URL`/`HOSTBUD_RUN_ID` with `-e`);
  - if not, keep `-e` for all three and record the known limitation (v2 §9: the token is briefly visible in the host's process list; acceptable on a single-user host) in the README security notes (written in T12).
- The session name is recorded on the run (`runs.session_name`). The run starts in **`starting`**; `started_at` is set before the session is created, so any record the new session writes is "after start".
- A creation failure (tmux missing, path gone, env unsupported on old tmux) sets the run `failed` with the service's actionable message in `detail` (T9 then pauses the queue). Nothing retries automatically.
- The client **version check** (T5/T6) runs before creation; a too-old or missing client fails the run the same way, with its actionable message.

**Tests:** U (Go): name derivation (spaces, dots and colons sanitized; position appended), collision suffixing against an existing inventory (`-1`, `-2`); env map contents (exactly the three keys; `HOSTBUD_URL` default vs. override); argv passed **unchanged** to `sshx` (a fake runner records it), including flags with quotes and spaces; creation failure → run `failed` with `detail`; the token appears in no log line and in no field except the env passed to the service. I (Go, `test/sshd`): the created session has the three variables in `tmux show-environment -t '=<name>'` and runs in the project path (`#{pane_current_path}`); the first process sees them (the stub prints them); if S8 chose the stdin method, a `ps -eo args` sampler running during creation never sees the token; the user's `~/.claude` / `~/.codex` fixture files in the target home are unchanged (checksums) after a run session is created.

**E2E:** no standalone scenario: run sessions are only reachable through the queue. T9's *Two items in order* asserts the session name, the project path and the `HOSTBUD_*` env through the target (`helpers/target.ts`). Type-check only.

**Done:** run sessions are created only through the single service, with the right name, path, env and argv; the token-passing method follows S8 (limitation recorded if `-e` is kept); failures are actionable and fail the run; tests written.

## T5 — Adapter interface and the Claude Code adapter

`internal/agents` (v2 §7), with the Claude Code adapter first.

- **Interface and registry:** the `Adapter` interface and `GoalState` from v2 §7 (`Kind`, `MinVersion`, `BuildCommand`, `ParseHook`, `ReadGoalState`), `Binding` (session/thread id, transcript path), and a registry keyed by `Kind()`. An unknown agent kind is refused at item create/edit (T8) and at run start.
- **`BuildCommand`:**
  - argv = `claude`, the user flags, `--settings '<hooks json>'`, then `'/goal <condition>'`, in that order;
  - flags are split into arguments **shell-style** (a quoting-aware splitter; unbalanced quotes are a validation error at item create/edit) and never interpolated (v2 §3.2);
  - the hooks JSON contains the **single hook command** from v2 §7 for `SessionStart`, `Stop` and `SessionEnd`, each mapped to the endpoint events `session_start`, `turn_end`, `session_end`, with the exact syntax recorded in S1;
  - the token appears only as **`$HOSTBUD_RUN_TOKEN`**, never literally, in both the argv and the settings JSON (the same for `$HOSTBUD_URL` and `$HOSTBUD_RUN_ID`);
  - the hook command always exits 0 and prints nothing (`-o /dev/null`, `|| true`, `--max-time 5`), so it can't block or steer the agent.
- **`ParseHook`:** extracts `session_id` and `transcript_path` from the forwarded body (field names per S4). Rejects bodies without a session id (the dispatcher treats that as `unknown` ⇒ needs attention).
- **`ReadGoalState`:**
  - **checks `transcript_path`** (v2 §9, untrusted): absolute, `path.Clean`ed, under the remote user's `~/.claude/projects/` (home from the machine record), and its **SFTP `realpath` still under it** (no symlink escape). A failing path ⇒ `unknown` with a reason;
  - reads it **read-only over SFTP**, **incrementally** from the stored byte offset (`runs.transcript_offset`), only complete lines (a truncated last line is left for the next read and the offset isn't advanced past it); the new offset is saved;
  - parses **each line as JSON** and accepts only **top-level** records with `type == "attachment"` and `attachment.type == "goal_status"`; text search is never used;
  - applies the **v2 §5.3 binding**: the transcript belongs to the bound `session_id` (the path comes from this run's bound hook and the record's session field, if present, matches); the record is **after `run.started_at`** (its timestamp, or line order plus hook arrival time, per S5); `attachment.condition` equals the queued condition (the instruction text after `/goal `) after **whitespace normalization** (trim, collapse runs of whitespace);
  - result mapping: `met: true` ⇒ **`achieved`**; the "impossible" record (as identified in S5) ⇒ **`failed`**; intermediate `met: false` ⇒ **`pending`**; unparseable, unknown shape, missing fields, wrong condition only when it's the latest record ⇒ **`unknown`**; no goal record yet ⇒ `pending`;
  - the SFTP read is timeout-bounded by `HOSTBUD_SFTP_TIMEOUT` (M7), and a timeout is `unknown`-for-now (retried on the next signal) rather than a failure, unless the run has ended (then `unknown`).
- **Version check:** `claude --version` over `sshx` before the run starts; the result is recorded as `runs.client_version`. Below `MinVersion()` (the version tested in S5) or not found ⇒ an actionable error (e.g. "Claude Code 2.1.0 or newer is needed on the host; found 1.9.3 — update with `claude update`" / "claude not found on the host — install Claude Code first") and the run is not started.
- **Instruction validation** on item create/edit (used by T8): the PoC requires `/goal ` followed by a non-empty condition (v2 §3.2); the condition is stored as typed and normalized only for comparison.
- **Fixtures** (`internal/agents/testdata/claude/<version>/`), redacted, from S5/S6: achieved, pending (intermediate not met), failed (impossible), **decoy** (the marker JSON and the word "achieved" nested inside tool results and message content, no top-level record), stale record from before start, wrong session, changed condition, truncated last line.

**Tests:** U (Go): argv building (flags with quotes and spaces, empty flags, unbalanced quotes → error, **no literal token** anywhere in argv or JSON, the three hooks present with the right event names); `ParseHook` (valid, missing session id, extra fields ignored); the **§5.3 matrix** against each fixture per supported version (achieved ⇒ achieved; pending; failed; decoy ⇒ never achieved; before start ⇒ not achieved; wrong session ⇒ not achieved; changed condition ⇒ not achieved; truncated last line ⇒ ignored until complete); whitespace normalization; **path validation** (relative, `..`, outside the tree, another user's home, symlink escape through a fake realpath); **incremental offset** (second read starts where the first stopped, a new line appended between reads is seen once); version parsing and comparison; instruction validation. I (Go, `test/sshd`): `ReadGoalState` over real SFTP against a fixture transcript placed under `/home/dev/.claude/projects/…` (achieved, decoy, pending), incremental across two appends; a symlink under the projects dir pointing outside is refused; `claude --version` through `sshx` against a fake `claude` on the target.

**E2E:** none standalone: the adapter is reached only through runs. T9's scenarios use the stub `claude` from T7 and include the decoy case. Type-check only.

**Done:** the adapter builds the command exactly as S1 proved, never exposes the token literally, reads the transcript read-only, incrementally and safely, accepts only bound, structured, top-level records with the queued condition, maps every state, checks the version, and has per-version fixtures including decoys.

## T6 — Codex adapter

The second adapter, with the reader chosen in S7 (v2 §7).

- **`BuildCommand`:** argv = `codex`, the user flags, `-c 'hooks.SessionStart=[…]' -c 'hooks.Stop=[…]' -c 'hooks.SessionEnd=[…]'`, then `'/goal <condition>'`. The syntax is exactly as proven in S1 (and merges with the user's hooks, per S1). **Never `-c notify=…`** (v2 §2: it would replace the user's notifier). Same flag splitting, same single hook command, same `$HOSTBUD_RUN_TOKEN` reference as T5.
- **`ParseHook`:** the **thread id** from the hook body (as recorded in S4: `session_id` or another field), plus `transcript_path`. Rejects bodies without it.
- **`ReadGoalState`** uses the reader chosen in S7, keyed by the bound thread id:
  - `status == 'complete'` with `updated_at_ms` after `run.started_at` and `objective` equal to the queued condition (whitespace-normalized) ⇒ **`achieved`**;
  - `blocked` ⇒ **`failed`**;
  - `active`, `paused`, `usage_limited`, `budget_limited` ⇒ **`pending`** (Codex usage limits resume on their own; a long pause surfaces as `stale`, v2 §5.3);
  - no row, a row for another objective, an `updated_at_ms` before start with `complete`, anything else ⇒ **`unknown`** (a row that simply isn't complete yet is `pending`).
- **If S7 chose a host tool** (`sqlite3` or `python3`):
  - the query opens the database **read-only** (`file:$CODEX_HOME/goals_1.sqlite?mode=ro`, or the S7-proven equivalent; `$CODEX_HOME` defaults to `~/.codex`), selects only the needed columns for one `thread_id` bound as a parameter (not interpolated), is built from argv through `sshx`, and is **timeout-bounded** (`HOSTBUD_EXEC_TIMEOUT`);
  - a missing tool is an actionable error, e.g. "sqlite3 not found on the host — install with `sudo apt install sqlite3`" (the run is not started; the error shows in the panel, T10).
- **If S7 chose the rollout JSONL:** the same path validation as T5 (absolute, cleaned, under the remote user's `~/.codex/` data dir, `realpath` check), read-only SFTP, incremental with `transcript_offset`, line-by-line JSON parsing of the structured goal record only.
- **Version check** (`codex --version`) and `client_version` as in T5, with `MinVersion()` from S5.
- **Fixtures** (`internal/agents/testdata/codex/<version>/`), redacted: a `goals_1.sqlite` (or rollout files) with rows/records for complete, blocked, active, paused, usage_limited, budget_limited, a complete row before start, a different objective, and **decoy text** in the objective or messages (e.g. the objective or a message containing "complete" or the Claude marker JSON).

**Tests:** U (Go): argv (the three `-c hooks.*` overrides, no `notify`, flags quoted, no literal token); `ParseHook`; the **status matrix** from fixture rows or rollout files per supported version, including decoy text; the read-only query's argv (parameters bound, `mode=ro`); missing-tool error mapping; version check. I (Go, `test/sshd`): the reader against a fixture `goals_1.sqlite` (or rollout file) under `/home/dev/.codex/` on the target: complete ⇒ achieved, blocked ⇒ failed, active ⇒ pending; the database file's checksum is unchanged after reads (read-only); with the host tool removed from `PATH` in a variant target, the actionable error.

**E2E:** none standalone; T9 runs a stub `codex` item (*Mixed clients*). Type-check only.

**Done:** the Codex adapter builds the S1-proven command without `notify`, reads goal state read-only with the S7 reader, maps every status, gives actionable errors for a missing tool or old client, and has per-version fixtures including decoys. (If S1–S7 dropped Codex, this task instead refuses `codex` items with an actionable message and records the open owner decision.)

## T7 — Stub clients on the throwaway target

The e2e and integration harness that stands in for real agents (v2 §10 *E2E*; v2 ROADMAP *Rules*: no real agents in automated tests).

- **Stub executables** `claude` and `codex` added to the `test/sshd` image (so both `hostbud-e2e-target` and the integration target have them), installed on `PATH` for the `dev` user. They are tiny scripts, not real clients. Each one:
  - accepts **the same argv shape** as the real client: user flags (ignored, but logged to a per-run file for assertions), the hook injection (`--settings '<json>'` for Claude; `-c 'hooks.<Event>=[…]'` for Codex), and the initial prompt `'/goal <condition>'`;
  - **extracts the injected hook command and runs it** for each event (`SessionStart` at start, `Stop` after each simulated turn, `SessionEnd` on exit), feeding it a hook JSON body in the S4 shape (session/thread id, `transcript_path`, `cwd`, `hook_event_name`), so the injection is exercised end to end, including the env var references;
  - writes goal state in the **exact formats recorded in S5/S7**: a Claude transcript line (top-level `goal_status` record) under `/home/dev/.claude/projects/<dir>/<session>.jsonl`, or Codex state (a `goals_1.sqlite` row via the target's `sqlite3`, or a rollout record, per S7) under `/home/dev/.codex/`;
  - **stays attached in the foreground like a TUI** (prints a banner and simulated turn output, reads stdin) until its behavior says to finish, so the session behaves like an interactive run and "Open session" shows live output;
  - answers `--version` with a version ≥ the adapter's `MinVersion()` (and a switch to report an old version, for the version-check test).
- **Scripted behaviors**, chosen per run by a behavior file the runner writes on the target over SSH (keyed by project path or run id; the stub reads `HOSTBUD_RUN_ID` from its env), each with a turn delay:
  - `achieve:N` — achieve after N turns (`met:true` / `complete`);
  - `decoy` — print and write the marker text only nested (in simulated tool-result/message lines), never a top-level record, and keep running;
  - `fail` — the "impossible" record (Claude) / `blocked` (Codex);
  - `exit` — exit without achieving (`SessionEnd` then the process ends; the session ends too);
  - `silent` — `SessionStart`, then no more hooks (for stale); optionally `silent-then-achieve:S` to write an achieved record after S seconds without a hook until the next `Stop` (for *Late achieved after stale*);
  - `clear` — a second `SessionStart` with a different session id (a `/clear`);
  - `pending` — keep answering turns with `met:false` / `active`.
- **E2E compose:** `hostbud-e2e-app` gets `HOSTBUD_HOOK_BASE_URL` pointing at Caddy's loopback site by service name (the value found in S10), and a short **`HOSTBUD_RUN_STALE_AFTER`** in seconds (e.g. `20s`); the target can reach that Caddy address on the e2e network.
- **Test helpers** in `test/e2e/helpers/stubs.ts` (and additions to `target.ts`): set a stub behavior; read a run's session env (`tmux show-environment -t '=<name>'`) and path through the target; read the stub's per-run argv log; list sessions on the target.
- A ctl action to restart `hostbud-e2e-app` (`/app/restart`) for T9's restart scenario (`test/e2e/ctl/server.mjs`, `helpers/ctl.ts`).

**Tests:** U: n/a: test tooling (the stubs are exercised by I and E). I (Go, `test/sshd`): **stub-vs-adapter drift test**: run each stub with each behavior against the T5/T6 adapters: the adapter's `BuildCommand` argv is accepted by the stub, the stub's hook calls reach a test HTTP sink with the expected events and bodies, and the records it writes are parsed by `ReadGoalState` as the behavior intends (`achieve` ⇒ achieved, `decoy` ⇒ never achieved, `fail` ⇒ failed, `pending` ⇒ pending), so stub and adapter can't drift apart.

**E2E:** none of its own; this task provides the harness the T9–T11 scenarios run on. The suite still type-checks.

**Done:** both stubs mimic the real argv, run the injected hooks, write the S5/S7 formats, stay attached, and support every behavior; the e2e app is configured with the hook base URL and a short stale window; helpers and the restart action exist; the drift test is written.

## T8 — Queue service and REST API

`internal/queue` service plus routes (v2 §4). The exact request/response shapes are fixed here and copied to v1 ARCHITECTURE §9 in T12.

- **Service** (`internal/queue`): queue and item CRUD, reorder, start / pause / resume, item retry / skip / mark done (the retry/skip/mark-done state effects are completed by the T9 dispatcher; the service validates and delegates). It publishes a typed **`queue.changed`** event (new `events.Type`, payload: queue id and a snapshot or version) on every change (v2 §4).
- **REST routes**, cookie-authenticated and Origin-checked like every state-changing v1 route (M7 limits apply: 64 KiB JSON through `decode`, `Content-Type: application/json`, 30 s deadline, `Cache-Control: no-store`):
  - `GET /api/queues` (list, with items and each item's latest run summary), `POST /api/queues` `{projectId, name}`;
  - `GET /api/queues/{id}`, `PATCH /api/queues/{id}` `{name}`, `DELETE /api/queues/{id}`;
  - `POST /api/queues/{id}/items` `{agent, flags, instruction}` (appended at the end);
  - `PATCH /api/queue-items/{id}` `{agent?, flags?, instruction?}`, `DELETE /api/queue-items/{id}`;
  - `PUT /api/queues/{id}/order` `{itemIds: [...]}` (must be exactly the queue's `queued` items, in the new order, after all non-queued ones);
  - `POST /api/queues/{id}/start`, `/pause`, `/resume`;
  - `POST /api/queue-items/{id}/retry`, `/skip`, `/mark-done`;
  - errors use the v1 `{error, hint}` shape with actionable text.
  - All routes are added to `internal/api/testdata/routes.json` (state-changing, auth required, JSON body where applicable), so M7's shared Origin, limit and auth tests cover them automatically.
- **Validation:** `agent` must be a registered adapter kind (T5 registry); `flags` must split cleanly (balanced quotes); `instruction` must be `/goal ` followed by a non-empty condition (T5); the project must exist on the host machine.
- **Editing rules:**
  - only `queued` items can be edited, deleted or moved;
  - `running` and `done` items (and `skipped`) are read-only; `needs_attention` items can't be edited either (Retry re-queues them first);
  - deleting a queue with an active run is refused with an actionable message ("A run is still active in this queue — pause it and wait for the run to end, or mark its item done/skip it first").
- **Queue actions:** `start` from `idle` (or `finished` with new queued items) ⇒ `running`; `pause` from `running` ⇒ `paused` (never touches the running session); `resume` from `paused` ⇒ `running`. Invalid transitions answer 409 with a message naming the current state.
- **Owner overrides** (retry / skip / mark done) are only allowed from `needs_attention` (409 otherwise), except that T9 additionally lets them end an item's still-active run as `cancelled` (v2 §5.4) — the service enforces the state rule; T9 implements the effect.
- **PoC limit:** one queue per installation (v2 §3.3). Creating a second one is refused (409) with a message that names V2-M2 ("V2-M1 supports one queue; several queues arrive with V2-M2").

**Tests:** U (Go): service rules (editability by status for every status, reorder validation, one-queue limit and its message, queue state transitions valid/invalid, owner overrides only from `needs_attention`, delete refused with an active run), validation (agent, flags, instruction), **event publication** (`queue.changed` on every mutating call, none on reads). I (Go): routes against the real store and the real middleware chain: happy paths, 409s, 400s; **Origin rejection** for a foreign or missing Origin on every state-changing queue route (through M7's shared route-table test), unauthenticated → 401; the route-table drift test passes with the new entries.

**E2E:** add `queue.api.spec.ts` (desktop, API-level through Caddy) **(T8) Queue API:** sign in; create a queue for a project; add three items; reorder them; edit a queued item; delete one; start and pause (the stub behavior for item 1 is `pending`, so nothing advances); a second queue is refused with the V2-M2 message; editing a running item is refused. The new routes join M7's shared route list, so `origin.api.spec.ts` (**M7 T12**) also rejects a foreign Origin on each of them. Type-check only.

**Done:** the service and all routes exist with the shapes fixed and documented, editing and state rules hold, the one-queue limit names V2-M2, every change publishes `queue.changed`, routes are Origin-checked and authenticated through the shared route list, and the scenario compiles.

## T9 — Dispatcher and state machines

`internal/queue` dispatcher implementing v2 §5 end to end.

- **Start (§5.1):** when a queue is `running` and has **no active run**, take its first `queued` item (lowest position) ⇒ item `running`; create a run (fresh token, **`starting`**, `started_at`); run the adapter's version check; build the argv (`BuildCommand`); create the session (T4). A failure at any step ⇒ run `failed` with the actionable `detail`.
- **Signals (§5.2)**, each a `run_events` row and a guarded transition:
  - `session_start`: `ParseHook` ⇒ bind `agent_session_id` (and store `transcript_path` for the adapter) ⇒ run **`running`**. A second `session_start` for the same run with a **different** id (e.g. `/clear`) ⇒ **`needs_attention`**: the run can no longer be tracked, so it ends as `exited` with `detail` "the agent session changed (/clear or restart) — the goal can't be tracked" (session left alive, token revoked); the same id again is ignored;
  - `turn_end`: update `last_signal_at`, then `adapter.ReadGoalState(binding)`: `achieved` ⇒ run **`achieved`**; `failed` ⇒ run **`failed`**; `pending` ⇒ still `running`; `unknown` ⇒ **`needs_attention`**: the run ends as `exited` with `detail` (e.g. "unrecognised goal-state format for Claude Code 2.x — check the session"), session left alive. Mapping both untrackable cases to `exited` + `detail` is a design detail recorded in v2 §5.4 in this task;
  - `session_end`: read the goal state **once more**; achieved ⇒ `achieved`; otherwise ⇒ **`exited`**;
  - the run's session **disappearing from `sessions.changed`** (the v1 inventory poller's event; `source='poller'`) is handled like `session_end`;
  - **no signal for `HOSTBUD_RUN_STALE_AFTER`** (a per-run timer from `last_signal_at`, or `started_at` while `starting`; `source='timer'`) ⇒ **`stale`**. It only flags the run; nothing is killed, and the token stays valid.
- **Transitions (§5.4):**
  - **only `achieved` advances**: item ⇒ `done`, token revoked, then the next `queued` item starts;
  - every other terminal run state (`failed`, `exited`, `stale`, `cancelled` not caused by an owner action, and the needs-attention cases above) sets the item **`needs_attention`** (with the run's `detail`) and the queue **`paused`**;
  - a **late `achieved`** that arrives after a run went `stale` (a hook on the stale run, or the `sessions.changed`/restart read) marks the item **`done`**, but the queue **stays `paused`** until the owner resumes it;
  - no `queued` items left after a `done`/`skipped` ⇒ queue **`finished`**;
  - **pause never touches the running session**: the current run continues and is still tracked (an `achieved` while paused marks the item done; nothing new starts until resume);
  - resume ⇒ `running` and, if no run is active, start the next item.
- **Owner actions:**
  - **retry** ⇒ item `queued` (same position) and, when the queue is resumed/running, a **new run** with a new token and a **new session** (collision-suffixed); the old session stays open;
  - **skip** ⇒ item `skipped`;
  - **mark done** ⇒ item `done` (owner override; `run_events` `source='user'`);
  - an action on an item whose run is **still active** (e.g. stale) ends that run as **`cancelled`** (token revoked, **session left open**) before applying the action.
  - The queue stays `paused` after an owner action; the owner presses Resume (so a mistaken click can't start the next agent).
- **No new tmux poll loop:** the dispatcher reacts only to hook signals, `sessions.changed` and its timers (v2 §4). It never calls `capture-pane`, `send-keys` or any kill command.
- **Concurrency:** one dispatcher goroutine (or a per-queue mutex) serializes signals; the store's guarded transitions make a duplicate or concurrent `turn_end` advance **once**.
- **Restart safety:** on app start, load active runs (`starting`, `running`, `stale`), **re-arm** their stale timers from `last_signal_at`, and give each `running`/`stale` run **one `ReadGoalState`**, so a goal achieved (or a session lost) during the restart is picked up; runs whose session is gone are handled as `session_end` once the first inventory arrives.
- **Audit and events:** every transition writes a `run_events` row (`source` = `hook` / `poller` / `timer` / `user`, `kind` = the transition) and publishes a typed **`run.changed`** event (run id, item id, queue id, status, detail); item/queue changes also publish `queue.changed`.
- **New env var `HOSTBUD_RUN_STALE_AFTER`** (Go duration, default `90m` or the S9 value; validated range, e.g. 10 s–24 h) in `.env.example` (with a comment), `internal/config`, and the `hostbud` service's `environment:` list.

**Tests:** U (Go, fake adapter, fake session service, fake clock, real store or store fake): the **full transition table**: every §5.2 row (`session_start` bind, second different id, same id ignored, `turn_end` × {achieved, failed, pending, unknown}, `session_end` × {achieved, not}, session missing, stale timer), every §5.4 edge (achieved advances; each other terminal ⇒ needs_attention + paused; late achieved after stale ⇒ done + paused; finished when empty; pause doesn't cancel or kill; resume starts next; retry new run and token; skip; mark done; action on active run ⇒ cancelled with token revoked), **a concurrent double `turn_end` advancing once**, **restart recovery** (reload, re-arm, one read, missed achieved picked up), **no kill/send-keys call on any transition** (the fake session service fails the test if one is attempted), `run_events` row and `run.changed` per transition, config parsing of `HOSTBUD_RUN_STALE_AFTER`. I (Go, `test/sshd` + stub `claude`): one item runs from `starting` to `achieved` through real hooks (a test HTTP server standing in for the endpoint, or the real app), the item turns `done` and the next session is created; a `fail` stub pauses the queue and its session is **still alive** afterwards; the user's `~/.claude` / `~/.codex` fixture files on the target are unchanged after the runs; `internal/deploytest`: `HOSTBUD_RUN_STALE_AFTER` reaches only `hostbud`.

**E2E:** add `queue-runs.api.spec.ts` (desktop, API-level, stubs), each scenario tagged (T9):
- **Two items in order:** item 1 `achieve:2`, item 2 `achieve:1`; item 2's session appears only after item 1's stub writes its achieved record; check both sessions' names (`<project>-q1`, `<project>-q2`), project path and `HOSTBUD_*` env on the target (token value not asserted literally beyond presence); the queue ends `finished`. Variant: **restart `hostbud-e2e-app`** (ctl `/app/restart`) while item 1 is running, and let the stub achieve during the restart; tracking resumes and item 2 starts.
- **Decoy text does not advance:** item 1 `decoy`; after several turns the queue is still on item 1, item 1 is `running`, no second session exists.
- **Needs attention:** separately for `fail`, `exit`, `silent` (stale) and `clear` (changed session id): the item turns `needs_attention` with the expected `detail`, the queue is `paused`, and the run's session **is still alive** on the target (except `exit`, where the stub itself ended it).
- **Late achieved after stale:** `silent-then-achieve`: the run goes stale, then achieves; the item is `done`, the queue stays `paused` until `resume`, after which item 2 starts.
- **Retry and skip:** a `fail` item is retried (new run, new session suffixed, old session still there) and achieves; another `fail` item is skipped and the queue moves on after resume.
- **Mixed clients:** a stub `claude` item then a stub `codex` item, both achieving, in order.
Type-check only.

**Done:** the dispatcher implements every §5.1/§5.2/§5.4 rule, only a bound achieved record advances, everything else pauses with a reason, no session is ever killed, concurrent signals advance once, restarts recover, every transition is audited and evented, `HOSTBUD_RUN_STALE_AFTER` is configured, and the scenarios compile.

## T10 — Queue panel (desktop)

A minimal **Queue panel** (v2 §10) in the Vue app.

- **Entry:** opened from the app header (a "Queue" button, keyboard-reachable, also in the command palette), as a side panel or dialog consistent with the v1 layout.
- **Contents:**
  - **pick the project** (projects of the host machine; creating the queue on first use with a default name);
  - **items list** in order: position, agent, flags (truncated), instruction (the `/goal` condition), a **status badge** with the item status, the run state and its `detail` (why it needs attention), and the session name;
  - **add, edit, delete** items (inline form or dialog): **agent select** (`claude`, `codex`), **flags text field**, **instruction text field** (prefilled `/goal `);
  - **reorder** by drag (`vue-draggable-plus`, as in the v1 tree) **and** by keyboard (move up/down buttons or shortcuts), queued items only;
  - **Start / Pause / Resume** with the queue status shown;
  - **Retry / Skip / Mark done** on `needs_attention` items; **Mark done asks for confirmation**, because it overrides the evaluator; Skip also confirms;
  - **"Open session"** on an item with a run opens the run's session in a terminal tab (the v1 tab mechanism).
- **Live updates** come from `queue.changed` / `run.changed` over `/ws/events` only (a Pinia store with event reducers); no polling, no reload. On reconnect, the store refetches `GET /api/queues` once (as v1 stores do after a WS reconnect).
- **Errors are actionable** and shown in the panel next to the item or form: untrusted workspace (the S3 text), client missing or too old (T5/T6 text), missing reader tool (T6 text), validation errors (flags, `/goal ` required), the one-queue limit, 409 state errors.
- **Autocomplete is off** on these inputs (M8 policy: `autocomplete="off"` and the related attributes the M8 helper sets).
- Read-only rendering for `running`, `done`, `skipped` items (no edit/delete/drag handles).

**Tests:** U (Vitest): the panel store's **event reducers** (`queue.changed`, `run.changed`, out-of-order and duplicate events, reconnect refetch); **form validation** (`/goal ` required, empty condition, unbalanced quotes in flags); **button availability per status** (edit/delete/drag only on `queued`; Retry/Skip/Mark done only on `needs_attention`; Start/Pause/Resume per queue status); **confirmation on Mark done** (and Skip); autocomplete attributes present; "Open session" emits the open-tab action with the session name. I: n/a: the API is covered in T8/T9; this task is frontend only.

**E2E:** add `queue.spec.ts` (desktop) **(T10) Queue panel:** open the panel from the header; pick the project; build a two-item queue in the UI (item 1 stub `achieve:2`, item 2 stub `pending`); reorder by drag and by keyboard; Start; watch item 1 turn `done` and item 2 start **without a reload**; open item 2's session in a tab and see the stub's output; a `needs_attention` item (a third item with `fail`, added before start) shows its reason, and **Retry** and **Skip** work from the panel; Mark done shows a confirmation. Type-check only.

**Done:** the desktop panel covers every control listed in v2 §10, updates only from events, shows actionable errors, keeps autocomplete off, and the scenario compiles.

## T11 — Queue panel on the phone

The same panel in the phone layout (v1 M5 phone UI).

- A **drawer or full-screen sheet** instead of the side panel, opened from the phone header/menu.
- **Touch-sized controls** (≥ 44 px targets), the item form as a sheet, status badges readable at phone width, no horizontal scroll.
- **Reorder** by touch drag or by move up/down buttons (both available; buttons are the reliable path).
- **"Open session"** closes the sheet and switches to the **single-terminal view** on the run's session.
- Confirmation dialogs (Mark done, Skip) use the phone confirmation sheet.

**Tests:** U (Vitest): responsive layout components (sheet vs. panel by breakpoint, move buttons present on phone, open-session switches to the single-terminal view). I: n/a: frontend only.

**E2E:** add `queue.phone.spec.ts` (`iphone-13-pro`) **(T11) Queue panel (phone):** open the sheet; create two items; reorder with the move buttons; Start; see status changes live; open a run's session (single-terminal view shows the stub output); **Retry** a `needs_attention` item. Type-check only.

**Done:** the panel works on the phone profile with touch-sized controls and the single-terminal handoff, and the scenario compiles.

## T12 — Docs alignment

Make every doc match what was built (v2 ROADMAP T12).

- **v1 [ARCHITECTURE.md](../ARCHITECTURE.md):**
  - **§10** becomes a short pointer to `docs/roadmap-v2/`, **keeping the v1 obligations** (1)–(4) (the old `tasks`/`machine_capacity` sketch, the "ship hook scripts + install instructions" line and the `send`-style dispatcher text are removed, since v2 installs nothing and uses per-run injection);
  - **§9** lists the queue routes (T8 shapes) and the hook route (T3, token-auth, response codes);
  - **§12** lists `HOSTBUD_RUN_STALE_AFTER` and `HOSTBUD_HOOK_BASE_URL`;
  - **§14** replaces `internal/{llm,orchestrator}` with `internal/{queue,agents}` (and notes `internal/llm` for V2-M5);
  - **§15** lists the hook body cap (64 KiB), the per-run rate limit, the adapter read timeouts (SFTP/exec), and the stale window.
  - §8 (persistence) mentions the v2 tables and the append-only migration.
- **v1 [ROADMAP.md](../ROADMAP.md):** the *v2 — Orchestration* section (V2.1–V2.5) is replaced by a link to [ROADMAP.md](ROADMAP.md) (v2). The start-command bug leaves *Later* (done in T0; check it's gone).
- **v2 [ARCHITECTURE.md](ARCHITECTURE.md):** the status line says **V2-M1 is implemented** (with the date), the §12 spike results are kept, and §5–§9 match the code (429, `transcript_offset`, reader choice, token method).
- **v2 [ROADMAP.md](ROADMAP.md):** status line updated (V2-M1 done / in acceptance), linking this file and the checklist.
- **README:** a **"Queues"** usage section: creating a queue, the instruction format (`/goal <condition>`), agents and flags, what "needs attention" means and the owner actions (Retry, Skip, Mark done, Resume), trusting a workspace (S3), the client **minimum versions**, that hostbud never closes run sessions, and the **token process-list note** if T4 kept `-e`; the new env vars in the configuration table.
- **AGENTS.md:** the v2 security checklist item **"(v2) Hook endpoints use per-run tokens (stored hashed)"** is ticked; **"v2 is design-only"** in *Scope* is replaced by a pointer to `docs/roadmap-v2/ROADMAP.md`; the v2 e2e once-per-milestone rule stays.
- **`.env.example`:** both new vars present with comments (added in T3/T9; verified here).

**Tests:** the v1 M7 **docs consistency check** (`check-docs.sh`) covers the new env vars (in `.env.example`, compose `environment:`, ARCHITECTURE §12) and the new routes (in §9 and `routes.json`); it's extended if it doesn't already check routes. E: n/a (docs).

**E2E:** none (docs only).

**Done:** v1 ARCHITECTURE §8/§9/§10/§12/§14/§15, v1 ROADMAP, v2 ARCHITECTURE and ROADMAP status, README, AGENTS.md and `.env.example` all match the build; the docs check is green (CP5).

## T13 — Milestone acceptance

The milestone's single e2e run and the acceptance walk (v2 ROADMAP *Rules*, T13).

1. **The milestone's e2e run:** `make e2e`, the **full suite** (both profiles — desktop Chromium and iPhone 13 Pro — all v1 and v2 scenarios), for the first time in V2-M1.
2. **Triage every failure** (never skip, retry-until-green or weaken an assertion): a product bug ⇒ bug-fix workflow (regression test commit, then fix commit); a stale v1 scenario broken by an intended change ⇒ fix the scenario with the reason in the commit; a flaky scenario ⇒ fix the wait/condition, not the timeout alone.
3. **Fast loop while fixing:** `make e2e-up` and `make e2e-run ARGS="<spec> -g '<name>' --project=<profile>"` for the failing scenario, then the spec file, then the whole suite with `make e2e`. **Rerun the full suite until green.**
4. **After the last fix:** `make lint test` green; `make gitleaks` clean.
5. **Deploy:** *(host)* `make deploy`; check `docker compose ps` (containers healthy), `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health`, the migration applied with v1 data intact (sessions, projects, sign-in still work), the Queue panel opens. If the app fails to start, roll back to the previous deploy commit and `make deploy`, report, and fix before retrying (data is untouched: the migration is append-only).
6. **Walk the criteria** in [V2-M1-acceptance.md](V2-M1-acceptance.md) and tick each one whose tests pass; mark the E items as passed at this run (date, commit). Review the §12 spike section for criterion 2.
7. **Owner checks:** the real-client check and the other *Manual checks (owner)* are the owner's; list them as **open** in the summary unless the owner has done them (don't wait).
8. **Summary to the owner:** what changed; **new env vars** `HOSTBUD_RUN_STALE_AFTER` (optional; default `90m` or the S9 value) and `HOSTBUD_HOOK_BASE_URL` (optional; production leaves it empty); manual steps on the host (trust the project directory in each client if S3 requires it; install `sqlite3` if S7 chose it and it's missing; close the spike's leftover sessions); the e2e results (counts, green run); the open owner items (real clients end to end, trust, version match, leftover sessions, any dropped-client decision).

**Tests:** whatever the triage adds (regression tests per the bug-fix workflow).

**E2E:** the full suite run (both profiles), until green; no new scenarios except regressions.

**Done:** `make e2e` green (full suite, both profiles) with no skips or weakened assertions; `make lint test` green; gitleaks clean; deployed and healthy; every checklist criterion ticked or its owner item listed as open; summary delivered.

## T14 — Safe Docker cleanup

Free the disk the milestone's builds and its e2e run used (v2 ROADMAP *Rules*: every v2 milestone ends with a Docker cleanup), **without touching the running deployment, its data, other projects, or work another agent may be doing at the same time.** This is the last step of the milestone, after T13's deploy and checks. It follows v1 M7 T15's procedure ([../roadmap/M7-tasks.md](../roadmap/M7-tasks.md#t15--safe-docker-cleanup)).

1. **Check that nothing is in use.** If any of these hold, skip the cleaning (steps 3–5), record "cleanup skipped: <reason>" in Progress and the summary, and treat the task as done. Cleanup can run again later:
   - a `make` test/lint/build or e2e run is in progress from this or another session (`pgrep -af 'scripts/tool.sh|docker exec hostbud-tools|test/e2e/run.sh|docker compose .*hostbud'`);
   - a `hostbud-tools-*` container is running a process other than its idle entrypoint (`docker top <container>` for each one listed by `docker ps --filter label=hostbud.tools=1`);
   - the `hostbud-e2e` stack is up (`docker compose -p hostbud-e2e ps -q` is non-empty) and `pgrep` shows a run using it. If it's up but idle (a leftover from T13's `e2e-up` fast loop), it's this milestone's own: `make e2e-down` removes it and its volumes, but only after confirming no run is active;
   - a `docker build` or `docker compose build` for hostbud is running (`pgrep -af 'docker (compose )?build'`);
   - `make restore` or `make restore-check` is running (`pgrep -af 'pg_restore|restore-check'`).
2. **Record the before state:** `docker system df`; `docker compose ps` (the production `hostbud`, `hostbud-caddy`, `hostbud-postgres` must be running and healthy before and after); `docker volume ls --filter name=hostbud`; `git worktree list`.
3. **Remove this milestone's own leftovers only:** a worktree this milestone created in the scratch directory (only if `git -C <it> status --porcelain` is empty, with `git worktree remove`, never `--force`, then `git worktree prune`); any `hostbud_restore_check_%` database left over, dropped by exact name only if no restore-check is running. Anything else belongs to someone else: leave it.
4. **Clean with the repo's own target:** `make docker-clean` **without** `CACHE=1`. Before running it, read the `docker-clean` recipe in the Makefile and confirm it removes only hostbud's disposable artifacts: the e2e stack and its images (`hostbud-e2e-*:local`), the toolbox containers labelled `hostbud.tools=1`, and dangling images labelled `hostbud.image=1`. If V2-M1 added e2e images (for example a rebuilt `test/sshd` image with the stub clients), extending the recipe to remove them is expected. If it removes anything else, don't run it: record the difference in Progress and the summary as an open owner item and finish the rest of the task.
5. **Never, in this task:**
   - `docker system prune`, `docker volume prune`, `docker image prune -a` without the `hostbud.image=1` label filter, `docker builder prune` (all projects' build cache; `CACHE=1` does this), or `docker network prune`;
   - removing the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data` or `hostbud-caddy-config`, or anything with another project's prefix;
   - stopping, recreating or removing the production containers, or the images they run from (`hostbud:local`, `hostbud-caddy:local`, the pinned `postgres` image);
   - removing the toolbox *images* (`hostbud-toolbox-*`), the `test/sshd` integration targets (`make test-down` is not part of cleanup; they stay warm for speed), or anything under `.cache/`, `data/` or `backups/`;
   - killing or closing any tmux session on the host, including the spike's and the runs' leftover sessions (those are the owner's to close, T1/T13);
   - deleting any file in `backups/`.
   Build cache and the rest go only if the owner asks explicitly, as a separate step.
6. **Verify after:** `docker compose ps` shows the same three production containers up and healthy; `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` answers `{"status":"ok"}`; `docker volume ls --filter name=hostbud` still lists the four production volumes; the `hostbud-test-sshd` containers are still there if they were before; `git worktree list` shows only the main tree plus worktrees that aren't this milestone's; `docker system df` again. Report the space reclaimed (before/after) in the summary.
7. Commit only the Progress table update (this task changes no code).

**Tests:** n/a (operations only, no behavior change). The post-cleanup health check and `docker compose ps` are the verification.

**E2E:** n/a: nothing reachable changes. T13 already ran the suite, and cleanup must not start a new run.

**Done:** hostbud's disposable Docker artifacts and this milestone's worktree/restore leftovers are removed (or the skip is recorded with its reason); the deployment, every volume and every backup are intact and healthy; nothing outside hostbud was touched; reclaimed space is reported.

---

## New configuration (owner fills in `.env` if needed)

| Var | Added in | Default | Meaning |
|---|---|---|---|
| `HOSTBUD_HOOK_BASE_URL` | T3 | empty = `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}` | Override for `HOSTBUD_URL` inside run sessions. Production normally leaves it empty; e2e sets it to Caddy's loopback site by service name. |
| `HOSTBUD_RUN_STALE_AFTER` | T9 | `90m` (or the S9 value) | No-signal window before a run is `stale` (a flag only; nothing is killed). |

## Traceability: v2 ROADMAP V2-M1 → these tasks

| ROADMAP V2-M1 item | Task |
|---|---|
| Prerequisite: start-command bug (P3) | T0 |
| Spike S1–S10, "12. Spike results", redaction, failed-check rule | T1 |
| Migration and store (tables, CHECKs, UNIQUE, indexes, ULID, hash, payload cap, offset column, guarded transitions, no `tasks`/`machine_capacity`) | T2 |
| Tokens, hook endpoint, response codes, cap, rate limit, exemptions, `HOSTBUD_HOOK_BASE_URL`, Caddy, logging, checklist item | T3 |
| Run sessions through the single service, name/collision, env, argv, S8 token method | T4 |
| Adapter interface, registry, Claude `BuildCommand`/`ParseHook`/`ReadGoalState`, path checks, incremental read, binding, version, instruction validation | T5 |
| Codex adapter, no `notify`, S7 reader, read-only query, missing-tool error, version | T6 |
| Stub clients, behaviors, e2e app config, helpers | T7 |
| Queue service, routes, editing rules, one-queue limit, `queue.changed` | T8 |
| Dispatcher: start, signals, transitions, owner actions, no poll loop, restart safety, audit, `run.changed`, `HOSTBUD_RUN_STALE_AFTER` | T9 |
| Queue panel desktop (all controls, events only, actionable errors, autocomplete off) | T10 |
| Queue panel phone | T11 |
| Docs alignment (v1 ARCHITECTURE §9/§10/§12/§14/§15, v1 ROADMAP, v2 status, README, AGENTS.md) | T12 |
| Milestone e2e run, lint/test, gitleaks, deploy, criteria walk | T13 |
| Docker cleanup at the end of the milestone (v2 *Rules*) | T14 |
| Manual checks (owner) | [V2-M1-acceptance.md](V2-M1-acceptance.md#manual-checks-owner-backlog-not-blockers) |
