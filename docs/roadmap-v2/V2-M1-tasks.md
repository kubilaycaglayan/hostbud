# V2-M1 — Proof of concept: one queue, sequential: tasks

Goal, scope, out-of-scope list and the base task list: [ROADMAP.md](ROADMAP.md#v2-m1--proof-of-concept-one-queue-sequential) (cited as **R T*n***). Design: [ARCHITECTURE.md](ARCHITECTURE.md) (**v2 §N**); v1 docs as **v1 ARCHITECTURE §N** / **v1 ROADMAP**. Checklist: [V2-M1-acceptance.md](V2-M1-acceptance.md).

This file does **not** repeat those documents. Each task points to its R T*n* entry (bullets, **Tests:**, **E2E:**) and adds only the decisions and details needed to build it. Read both before starting a task.

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T0 Prerequisite: start-command bug | Done (e2e written and type-checked; run on demand) |
| T1 Spike on this host | Done (results in v2 ARCHITECTURE §12; no client dropped) |
| T2 Schema migration and store | Done |
| T3 Run tokens and the hook endpoint | Done (e2e written and type-checked) |
| T4 Session creation for runs | Done (S8: env through `tmux start-server ; source-file -` on stdin) |
| T5 Adapter interface and the Claude Code adapter | Done |
| T6 Codex adapter | Done (its integration test runs the T7 stub `codex`, committed with T7) |
| T7 Stub clients on the throwaway target | Done (stubs in `test/sshd/stubs`; drift test in `internal/agents`) |
| T8 Queue service and REST API | Done (e2e written and type-checked) |
| T9 Dispatcher and state machines | Done (e2e written and type-checked) |
| T10 Queue panel (desktop) | Done (e2e written and type-checked) |
| T11 Queue panel on the phone | Done (e2e written and type-checked) |
| T12 Docs alignment | Done |
| T13 Milestone acceptance | Done (deployed 2026-09-28; e2e run on demand, pending) |
| T14 Safe Docker cleanup | Done (2026-09-28; ~4 MB reclaimed: idle toolbox containers; no e2e or dangling images existed) |
| T15 Queue panel visual density | Implemented; e2e written and type-checked, run pending (on demand) |

## Preconditions

P1–P3 are in [ROADMAP.md](ROADMAP.md#preconditions-before-v2-m1-starts). Record each check in the Progress note below. For P2, check three things:
- `internal/events` has the typed events (`sessions.changed`, `projects.changed`);
- `session.Service.Create(ctx, Spec{Machine, Name, Path, Env, StartCommand})` is the only creation path;
- `internal/store/migrations` is numbered and append-only.

P3 is T0.

## Rules for this milestone

The v2 ROADMAP *Rules* and AGENTS.md apply in full. That covers: e2e runs only on demand (never as part of a task or T13), no real agents in tests, host safety, the public repo, owner items never blocking, and Docker cleanup at the end. This milestone adds:

- **T1 comes before any product code** (T0's bug fix is the only exception). Its results are in v2 ARCHITECTURE before T2 starts.
- **Batched checkpoints** (as in v1 M3–M7). Each commit runs only fast checks:
  - `go build`/`go vet` for the Go packages it touches;
  - `vue-tsc`;
  - the e2e `tsc`;
  - gitleaks, through the pre-commit hook.

  Full suites run at the checkpoints below. A checkpoint failure is fixed (with a regression test if it's a bug) before the next task starts.
- **Production stays untouched until T13.** Don't restart or recreate the production containers, and never touch the `hostbud-*` volumes. Integration tests use `test/sshd` and throwaway databases only.
- **New env vars:** `HOSTBUD_HOOK_BASE_URL` (T3) and `HOSTBUD_RUN_STALE_AFTER` (T9). Each one goes to:
  - `.env.example`, with a comment;
  - `internal/config`, validated (a bad value is a startup error naming the var);
  - the `hostbud` service's `environment:` list only.
- **Design changes go into v2 §5–§9 in the same commit.** That covers spike findings, a new column, the 429 response and the reader choice. v1 docs are aligned in T12.
- **Bugs:** a failing regression test in one commit, the fix in the next.
- **Staging:** stage explicit paths only. Check `git status` before touching a shared file:
  - `docker-compose.yml`, `test/e2e/compose.yml`, `test/e2e/helpers/*.ts`;
  - `Makefile`, `.env.example`, `internal/config/config.go`;
  - `internal/api/testdata/routes.json`.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T0 | `make lint test` | Green (2026-09-27, after aligning two stale session race tests) |
| CP2 | T2–T4 | `make lint test`, `scripts/compose-config.sh` | Green (2026-09-27, after fixing five lint findings) |
| CP3 | T5–T7 | `make lint test`, e2e `tsc` | Green (2026-09-27, after fixing ten lint findings) |
| CP4 | T8–T9 | `make lint test` **three times in a row** (timers and concurrent signals), e2e `tsc` | Green 3/3 (2026-09-27) |
| CP5 | T10–T12 | `make lint test`, `vue-tsc`, e2e `tsc`, `make gitleaks`, docs check | Green (2026-09-27) |
| CP6 | T13 | `make lint test`, e2e `tsc`, `make gitleaks`, `make deploy` (no `make e2e`: on demand only) | Green; deployed (2026-09-28) |

**Progress note:**
- **P1** (2026-09-27): v1 M1–M7 done; M7 T13's full e2e run is on demand, M7 T15 cleanup and M8 are still open (allowed).
- **P2** (2026-09-27): `internal/events` has typed `sessions.changed` / `projects.changed`; `session.Service.Create(ctx, Spec{Machine, Name, Path, Env, StartCommand})` is the only creation path (the sessions API and `projects.Service.CreateSession` both call it); `internal/store/migrations` is numbered 0001–0004 and append-only.
- **P3 / T0** (2026-09-27): reproduced on the e2e target and on the host through hostbud's own `ssh`. Symptom: the session is created and then gone. Root cause: a tmux server started over SSH has the bare non-interactive `PATH` (`/usr/local/bin:/usr/bin:…`), so `default-shell -c <command>` couldn't find tools installed under `~/.local/bin` or an npm prefix (`claude`, `codex`); the command exited 127 and took the session with it. Quoting, `-c <dir>`, `ready()` and the name logic were ruled out (a plain `sh -c '…'` works; the path and name tests pass). Fix: `tmux.StartShell` runs the command as `"$SHELL" -lic '<command>'; exec "$SHELL" -l`, so it gets the login shell's `PATH` and the session stays open on a shell after the command ends (output visible, never a silent vanish). Verified on the host with the real `claude --version` / `codex --version`.
- **T14** (2026-09-28): nothing in use (no make/e2e/build/restore run, toolboxes idle, e2e stack down, no restore-check databases). `make docker-clean` (no `CACHE=1`; recipe unchanged: V2-M1 added no e2e images, the stubs live in the `test/sshd` image and only its current tag exists) removed the five toolbox containers, ~4 MB. Production healthy, the four volumes, the `test/sshd` targets and all backups intact. The `/tmp/hostbud-m7-t13` worktree isn't V2-M1's and was left.
- **T13** (2026-09-28): before deploying, `make backup` turned out broken in production (docker cp can't read the app's tmpfs `/tmp`); fixed with a regression test (`150cdbd`) and a backup taken. CP6 green, `make deploy` done: health ok, migration 5, v1 row counts unchanged (1 user, 6 projects, 5 links, 1 recent command, 3 UI states), queue/hook routes live, panel in the bundle.
- **T1** (2026-09-27): spike done, results in v2 ARCHITECTURE §12. Design changes: Codex gets the plain condition as its prompt and hostbud arms the goal with `thread/goal/set` (new `Adapter.Arm`); the Codex reader is `thread/goal/get` over `codex app-server proxy`; follow-up reads after a pending `turn_end` and one read before `stale`; run sessions are created with `tmux source-file -` on stdin; host commands run through the login shell; stale default 2 h; e2e hook base URL `http://hostbud-e2e-caddy:9055`. User config checksums: see §12.

**What e2e reaches:**
- Every run uses the T7 stubs.
- The injected hook command runs for real: tmux env → `curl` → `hostbud-e2e-caddy` loopback site → app → adapter read → next session.
- The e2e app sets `HOSTBUD_HOOK_BASE_URL` to Caddy by service name, and a stale window of a few seconds.
- Restart safety uses a new ctl action, `/app/restart`.

**Covered by unit/integration tests instead:** logs, the host process list, user config files, and real client formats.

---

## T0 — Prerequisite: start-command bug

Scope: R T0. Additions:
- **Reproduce first.** Try it on the e2e target, through the UI and through `POST /api/machines/host/sessions` with `startCommand`. Then try it on the host through the UI, in a scratch folder, and leave the session open. Record the symptom: an error shown, the session missing, or the session created and then gone.
- **Root-cause candidates.** Rule each in or out with evidence:
  - quoting: the command is sent as one argv element where tmux expects a shell string, or the reverse;
  - `-c <dir>` resolution for `~/…`, spaces, or a missing directory;
  - `default-shell -c` runs without the login PATH;
  - the command ends at once, so the session vanishes;
  - `ready()` or the name logic rejects the request.

  Name the cause in the fix commit.
- **`session.Spec`:** it already has `Env` (needs tmux ≥ 3.2). `StartCommand` stays a string for the UI; T4 adds the argv form.
- **Commits:** the failing integration test in one commit, the fix in the next.
- **Tests (added to R T0):**
  - U: start commands with `$` and `~/…` paths; one `-e` argument per env entry.
  - I: a command that ends at once gives an actionable error or a kept session (whichever the fix defines), never a silent success.
- **E2E:** add `start-command.spec.ts` and `start-command.phone.spec.ts`. Both use the start command `sh -c 'echo HOSTBUD_START_OK; exec sh'`. Add the API variant to `api.api.spec.ts`.
- **Done:** the bug is fixed and verified, the *Later* item is closed, and CP1 is green.

## T1 — Spike on this host

Scope: R T1 (checks S1–S10, output, redaction, and the rule for a failed check). Additions:
- **User config must not change.** Take checksums of these files before and after, and record that they match:
  - `~/.claude/settings.json` and `~/.claude/settings.local.json`;
  - `~/.codex/config.toml`, and any hooks file.
- **Client not installed or not logged in:** don't log it in. Record it as an open owner item and run the other checks.
- **Extra things to check per step:**
  - **S1:** test hook merging on a scratch copy of `CODEX_HOME` that has a harmless user hook. Never edit the real one.
  - **S2:** confirm the goal is really active: in the Claude transcript or indicator, or as a Codex `thread_goals` row with `status='active'`.
  - **S4:** check whether Codex's hook `session_id` equals `thread_goals.thread_id`, and what compaction does to the ids.
  - **S5:** record how "impossible" differs from "not met" (a field, or only the reason). Save the redacted samples as fixture candidates.
  - **S7:** for a `mode=ro` open, check whether it sees committed WAL rows, whether it needs `-wal`/`-shm` access, and whether it blocks the writer.
  - **S8:** the variable must reach the session's first process, so it has to be set before the start command runs.
  - **S10:** run the host `curl` from inside a tmux session. The e2e URL found here becomes the `HOSTBUD_HOOK_BASE_URL` value for T3 and T7.
- **Output:**
  - the tested versions become the adapters' `MinVersion()`;
  - v2 §11 is marked as answered, with links to §12.
- **Dropped client** (the safe default): its adapter task (T5 or T6) only refuses that agent with an actionable message. Its e2e scenarios become n/a, with the reason.
- **Leftover sessions:** list them for the owner. Real session names go only in the chat summary.
- **Tests/E2E:** none.
- **Done:** §12 is written and redacted, the checksums match, and the leftover sessions are listed.

## T2 — Schema migration and store

Scope: R T2 and v2 §6. Additions:
- **Migration file:** `0005_queues.sql`, or the next free number.
- **Columns and constraints:**
  - `queue_items` also gets its own `machine_id`;
  - `runs` has `CHECK status IN ('starting','running','achieved','failed','exited','stale','cancelled')`;
  - `token_hash` is `BYTEA`;
  - the offset column is `runs.transcript_offset BIGINT NULL`.
- **Deleting and reordering:**
  - deleting a queue is one explicit transaction (items, runs, events, queue), with no implicit cascades;
  - reorder shifts positions to negative temporaries, then renumbers them `1..n`;
  - deleting an item renumbers the remaining positions without gaps.
- **Store methods beyond R T2:**
  - list active runs (for restart recovery);
  - list a run's events, newest first and bounded (for the panel);
  - setters for `agent_session_id`, `client_version`, `detail`, `transcript_offset`, `last_signal_at` and `ended_at`;
  - the same guarded `UPDATE` for item and queue status.
- **Oversized payload:** a typed store error. The endpoint answers 413 before it gets that far.
- **Tests (added to R T2):**
  - U: two goroutines race one guarded transition and exactly one wins; deleting a queue removes only its own rows.
  - I: seed a v1 database from migrations 0001–0004, then migrate. Row counts and checksums per table are equal afterwards, and a second apply is a no-op.
- **E2E:** none reachable. Add seed functions to `helpers/db.ts` for a queue, an item and a run with a known token (used by T3).
- **Done:** v2 §6 matches, including the new column and the CHECK.

## T3 — Run tokens and the hook endpoint

Scope: R T3, v2 §8 and §9. Additions:
- **Order of checks:**
  1. unknown event or run → 404;
  2. bad token → 401;
  3. run ended → 410;
  4. body too large → 413;
  5. body not JSON → 400;
  6. otherwise → 204.
- **Token cases:** another run's token → 401. A hook on a `stale` run is still `204`.
- **Rate limit:** a token bucket per run (proposed: 60 per minute, burst 20). Over the limit → **429**, with no side effect. Add 429 to v2 §8 now; the limit value goes into v1 ARCHITECTURE §15 in T12.
- **Exemption from cookie/Origin checks:**
  - one named allowlist in the middleware chain, with a reason;
  - a route-table rule makes sure nothing else is exempt;
  - `routes.json` lists the route with a new `token_auth: true` attribute, so M7's Origin test expects it to be exempt and the auth table expects a bearer token.
- **Until T9:** the dispatcher notification is a no-op interface. The request never reads the host.
- **`HOSTBUD_HOOK_BASE_URL`:** empty, or an absolute http(s) URL with no path, query or fragment.
- **Logging:** 401 and 429 are logged at info with the run id only.
- **Tests (added to R T3):**
  - U: another run's token, the 400 and 429 responses, and the config validation cases.
  - I: `internal/deploytest` checks that the var reaches only `hostbud`.
- **E2E:** `hooks.api.spec.ts` covers R T3's cases, plus another run's token → 401 and a burst → 429.

## T4 — Session creation for runs

Scope: R T4. Additions:
- **Argv form:** add `StartArgv []string` to `session.Spec`, handled by the same `Create`. Quoting happens in `sshx`, never by joining strings.
- **Session name:** the project name is sanitized to a valid tmux name, as in v1.
- **`HOSTBUD_URL`:** the `HOSTBUD_HOOK_BASE_URL` override if it's set, otherwise the loopback default.
- **Order of steps:**
  - `started_at` is set **before** the session is created, so anything the session writes counts as "after start";
  - the adapter's version check runs before creation.
- **Failures:** tmux missing, the path gone, env unsupported, or the client missing or too old. Each one sets the run `failed` with the actionable message in `detail`. Nothing retries automatically.
- **Tests (added to R T4):**
  - U: the token appears only in the env passed to the service, and in no log.
  - I: the first process sees the variables (the stub prints them), and the fixture `~/.claude`/`~/.codex` checksums are unchanged.
- **E2E:** covered by T9's *Three items in order*.

## T5 — Adapter interface and the Claude Code adapter

Scope: R T5, v2 §5.3 and §7. Additions:
- **Interface:**
  - `Binding` is the session/thread id plus the transcript path;
  - an unknown agent kind is refused at item create/edit and at run start.
- **Flags:** split with a quote-aware splitter. Unbalanced quotes are a validation error.
- **Hooks:**
  - `SessionStart` / `Stop` / `SessionEnd` map to `session_start` / `turn_end` / `session_end`;
  - `$HOSTBUD_URL` and `$HOSTBUD_RUN_ID` are env references too, like the token.
- **Reading the transcript:**
  - only complete lines count, and the offset never moves past a truncated last line;
  - no goal record yet ⇒ `pending`;
  - an SFTP timeout (`HOSTBUD_SFTP_TIMEOUT`) is retried on the next signal. It becomes `unknown` only once the run has ended.
- **Error messages:**
  - "Claude Code 2.1.0 or newer is needed on the host; found 1.9.3 — update with `claude update`";
  - "claude not found on the host — install Claude Code first".
- **Conditions:** stored as typed, and normalized (trimmed, whitespace collapsed) only for comparison.
- **Fixtures:** `internal/agents/testdata/claude/<version>/`, redacted, taken from S5 and S6.
- **Tests (added to R T5):**
  - U: path validation rejects another user's home; an appended line is seen exactly once.
  - I: `claude --version` through `sshx` against a fake client.
- **E2E:** covered by T9.

## T6 — Codex adapter

Scope: R T6. Additions:
- **Thread id:** read from the field found in S4.
- **Status mapping:**
  - a row that isn't complete yet ⇒ `pending`;
  - no row, a different objective, or `complete` before the run started ⇒ `unknown`.
- **If S7 chose a host tool (sqlite):**
  - open `file:$CODEX_HOME/goals_1.sqlite?mode=ro` (`CODEX_HOME` defaults to `~/.codex`);
  - select only the needed columns, with the thread id as a bound parameter;
  - bounded by `HOSTBUD_EXEC_TIMEOUT`.
- **If S7 chose the rollout file:** T5's path checks (the file must be under `~/.codex/`), read-only SFTP, the incremental offset, and structured records only.
- **Fixtures:** `internal/agents/testdata/codex/<version>/`. Cover every status, complete-before-start, a different objective, and decoy text in the objective or messages.
- **Tests (added to R T6):** I checks that the database checksum is unchanged after reads, and that a variant with the tool missing from `PATH` gives the actionable error.
- **E2E:** covered by T9's *Mixed clients*.

## T7 — Stub clients on the throwaway target

Scope: R T7. Additions:
- **Install:** the stubs are on `PATH` for `dev` in the `test/sshd` image, so the integration target has them too.
- **Per-run setup:** each stub logs its argv to a per-run file and finds its behavior through `HOSTBUD_RUN_ID`.
- **Hook bodies:** the S4 shape (id, `transcript_path`, `cwd`, `hook_event_name`).
- **Where state is written:**
  - Claude: `/home/dev/.claude/projects/<dir>/<session>.jsonl`;
  - Codex: under `/home/dev/.codex/`, via the target's `sqlite3` or a rollout file, per S7.
- **TUI-like output:** the stubs print a banner and turn output, and read stdin.
- **`--version`:** answers a supported version. A switch makes it report an old one.
- **Behaviors** (each with a turn delay):

  | Behavior | What the stub does |
  |---|---|
  | `achieve:N` | achieves the goal after N turns |
  | `decoy` | writes the marker text only nested inside other content |
  | `fail` | "impossible" (Claude) or `blocked` (Codex) |
  | `exit` | ends without achieving |
  | `silent` | stops sending hooks |
  | `silent-then-achieve:S` | writes an achieved record after S seconds, without a hook |
  | `clear` | sends a second `SessionStart` with a new id |
  | `pending` | keeps answering not-met (Claude) or `active` (Codex) |

- **Helpers:** `test/e2e/helpers/stubs.ts` sets a behavior and reads a run's env, path, argv log and sessions. Add the ctl action `/app/restart` to `ctl/server.mjs` and `helpers/ctl.ts`.
- **Tests:** the integration test checks the stubs against the adapters for every behavior, and checks that the stubs' hook calls reach a test HTTP sink.

## T8 — Queue service and REST API

Scope: R T8. Additions (T12 copies these shapes into v1 ARCHITECTURE §9):
- **Request and response shapes:**
  - `POST /api/queues` takes `{projectId, name}`;
  - `PATCH /api/queues/{id}` takes `{name}`;
  - adding an item takes `{agent, flags, instruction}` and appends it at the end; `PATCH` on an item takes any subset of those;
  - `PUT …/order` takes `{itemIds}`, which must list exactly the `queued` items;
  - `GET /api/queues` includes items, each with its latest run summary.
- **Events:** `queue.changed` is a new `events.Type`. It's published on every change and never on reads.
- **Limits and errors:** M7's limits apply (`decode`, `Content-Type`, 30 s deadline, `no-store`). Errors use `{error, hint}`.
- **Validation:**
  - the agent is registered;
  - the flags split cleanly;
  - the instruction is `/goal <non-empty>`;
  - the project exists.
- **Editing:** `needs_attention` items can't be edited either. Retry re-queues them first.
- **Queue actions:**
  - `start` works from `idle`, or from `finished` when new items are queued;
  - an invalid transition → 409, naming the current state.
- **Owner overrides:** only from `needs_attention` (409 otherwise). T9 implements what they do.
- **Messages:**
  - deleting a queue with an active run: "A run is still active in this queue — pause it and wait for the run to end, or mark its item done/skip it first";
  - creating a second queue: "V2-M1 supports one queue; several queues arrive with V2-M2".
- **E2E:** `queue.api.spec.ts` covers R T8's scenario, plus:
  - item 1 stays `pending`, so nothing advances;
  - a second queue is refused with the V2-M2 message;
  - editing a running item is refused.

## T9 — Dispatcher and state machines

Scope: R T9, v2 §5. Additions:
- **Start order:** the item becomes `running`, then the version check runs, then `BuildCommand`, then the session is created (T4). A failure at any step ⇒ the run is `failed`, with `detail`.
- **Untrackable runs** end as **`exited`**, with `detail`. The session is left alive and the token is revoked. Record this in v2 §5.4. It covers two cases:
  - a second `session_start` with a different id: "the agent session changed (/clear or restart) — the goal can't be tracked". The same id arriving again is ignored;
  - an `unknown` goal state, e.g. "unrecognised goal-state format for Claude Code 2.x — check the session".
- **Stale timer:** counts from `last_signal_at`, or from `started_at` while the run is `starting`.
- **While the queue is paused,** the current run is still tracked. If it achieves, its item is marked done, and nothing new starts.
- **After an owner action,** the queue stays paused until Resume. A retried item gets a new token and a collision-suffixed session.
- **Never** `capture-pane` or `send-keys`.
- **Concurrency:** signals go through one dispatcher goroutine (or a per-queue mutex), plus the guarded store updates.
- **Restart safety** covers `starting`, `running` and `stale` runs. A session that's gone by the first inventory counts as `session_end`.
- **Events:**
  - `run.changed` carries the run, item and queue ids, the status and `detail`;
  - item and queue changes also publish `queue.changed`.
- **`HOSTBUD_RUN_STALE_AFTER`:** accepts 10 s to 24 h.
- **Tests (added to R T9):**
  - U: the fake session layer fails the test on any kill or send-keys call; the same `session_start` id twice is ignored; an achieved record while paused.
  - I: a `fail` stub's session stays alive; the fixture config checksums are unchanged; duplicate hook POSTs advance the queue once; `deploytest` covers the new var.
- **E2E:** `queue-runs.api.spec.ts` with R T9's scenarios:
  - *Three items in order* uses `achieve:2`, `achieve:1`, `achieve:1`. At each hand-off it checks that the next item's session appears only after the previous stub writes its achieved record, and that at most one run is active. It checks the session names `<project>-q1` / `-q2` / `-q3` and that the token is present (not its value). The queue ends `finished` with all three items `done`. A variant restarts the app through ctl during item 2.
  - *Needs attention* uses `fail`, `exit`, `silent` and `clear`.
  - *Late achieved* uses `silent-then-achieve`; after Resume, item 2 starts.
  - *Retry and skip:* after a retry, the old session still exists.

## T10 — Queue panel (desktop)

Scope: R T10. Additions:
- **Opening the panel:** a button in the header, and an entry in the command palette.
- **Items:**
  - the instruction field is prefilled with `/goal `;
  - each row shows its session name;
  - running, done and skipped items have no edit, delete or drag handles.
- **Confirmations:** Skip also asks for confirmation.
- **State:** a Pinia store. After a WS reconnect it refetches `GET /api/queues` once.
- **Errors shown:** also validation errors, the one-queue limit, and 409s.
- **E2E:** `queue.spec.ts` covers R T10's scenario with three items: item 1 `achieve:2`, item 2 `pending`, and a `fail` item added before start. Reorder is tested both by drag and by keyboard.

## T11 — Queue panel on the phone

Scope: R T11. Additions:
- **Layout:** touch targets of at least 44 px, and no horizontal scroll.
- **Sheets:** the item form is a sheet. Mark done and Skip confirm through the phone confirmation sheet.
- **E2E:** `queue.phone.spec.ts` covers R T11's scenario, reordering with the move buttons. It also checks the hand-off live: item 1 (`achieve:1`) turns done and item 2 (`achieve:1`) starts and turns done without a reload, and the queue shows `finished`. A separate `fail` item covers Retry.

## T12 — Docs alignment

Scope: R T12. Additions:
- **v1 ARCHITECTURE:**
  - §10 drops the `tasks`/`machine_capacity` sketch and the "hook scripts + install instructions" line;
  - §8 mentions the v2 tables;
  - §15 also lists the stale window.
- **v2 docs:**
  - the ROADMAP status links to these files;
  - ARCHITECTURE §5–§9 match the code: the 429 response, `transcript_offset`, the reader, the token method, and `exited` for untrackable runs.
- **README "Queues":** also says that hostbud never closes run sessions, and lists the agents, flags and owner actions.
- **Docs check:** extend `check-docs.sh` to check routes (v1 §9 against `routes.json`) if it doesn't already.

## T13 — Milestone acceptance

Scope: R T13. Additions:
- **No e2e run** (on demand only). **Fixing failures from an on-demand run:** never skip a scenario, retry until green, or weaken an assertion.
  - a product bug ⇒ the bug-fix workflow;
  - a stale scenario ⇒ fix it, with the reason in the commit;
  - a flaky scenario ⇒ fix the wait, not just the timeout.
- **Fast loop (only while fixing a requested run):** `make e2e-up` + `make e2e-run ARGS="<spec> -g '<name>' --project=<profile>"`; the next full run waits for the owner's request.
- **Deploy checks:**
  - `docker compose ps` shows everything healthy;
  - `/api/health` is ok;
  - v1 data is intact (sign-in, sessions, projects);
  - the Queue panel opens.

  If a check fails, roll back to the previous deploy commit. The migration is append-only, so no data is lost.
- **Checklist:** tick it; E items count as written until an on-demand run passes them (then record the date and commit). Review §12 for R2.
- **Summary:**
  - what changed;
  - the two optional env vars;
  - host steps: trusting workspaces, the reader tool, leftover sessions;
  - e2e results;
  - open owner items.

## T14 — Safe Docker cleanup

Follow v1 [M7 T15](../roadmap/M7-tasks.md#t15--safe-docker-cleanup) step by step (v2 ROADMAP *Rules*), with these differences:
- remove only worktrees that this milestone created;
- the `docker-clean` recipe may be extended to cover images V2-M1 changed (the `test/sshd` image with the stubs);
- never close a tmux session, including the spike's and the runs' leftovers.

If anything is in use, skip the cleanup and record why. Report the space reclaimed, and commit only the Progress update.

## T15 — Queue panel visual density

Scope: owner request (2026-09-28). Make queue editing compact and usable with longer instructions on desktop and phone:
- Use vertically resizable text areas for instructions in both Add item and Edit item; saving an edit updates the displayed item.
- Place Add item at the form's right edge.
- Replace item Edit/Delete text controls with named icon buttons; tighten move, start, retry and owner-action buttons. Keep visible focus and at least 44 px touch targets on coarse-pointer devices.
- Reduce unused action spacing within queue item cards and check the phone sheet for horizontal overflow.
- **Tests:** U: `QueuePanel.spec.ts` checks both text areas, resize affordance, compact action classes, icon actions, and form alignment. I: n/a (frontend-only). E: add the T15 compact queue form/actions scenario to `queue.spec.ts` and `queue.phone.spec.ts`; verify edit updates the displayed instruction, icon controls stay accessible, and touch targets remain usable on phone. Type-check only; E2E runs on demand.

---

## New configuration

`HOSTBUD_HOOK_BASE_URL` (T3) and `HOSTBUD_RUN_STALE_AFTER` (T9): see [ROADMAP.md](ROADMAP.md) *New configuration*.
