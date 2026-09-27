# V2-M1 — Proof of concept: one queue, sequential: acceptance checklist

V2-M1 is done when every box is ticked, except the *Manual checks (owner)* list and the *Manual* notes on criteria, which are the owner's backlog and never block (AGENTS.md, *Owner items never block agents*). Tasks: [V2-M1-tasks.md](V2-M1-tasks.md). Roadmap: [ROADMAP.md](ROADMAP.md#v2-m1--proof-of-concept-one-queue-sequential). Design: [ARCHITECTURE.md](ARCHITECTURE.md) (v2 §1–§9, §10 *V2-M1*).

V2-M1 lets the owner run one queue of agent items for a project, strictly in order. Each item (agent, flags, `/goal …` instruction) runs in its own interactive tmux session, created through the single session-create service. The next item starts only when the client's own `/goal` evaluator has written a **structured achieved record bound to that run**: bound session, after the run started, the same condition. Everything else (failed, exited, stale, a changed session id, an unknown format) pauses the queue with a reason. hostbud never kills a run's session, installs nothing on the host and changes no user config. The queue is operable from desktop and phone, and the UI follows it from events only.

It adds one append-only migration (`queues`, `queue_items`, `runs`, `run_events`), the token-authenticated hook route `POST /api/hooks/{run_id}/{event}`, the queue REST routes, the typed events `queue.changed` and `run.changed`, the packages `internal/queue` and `internal/agents`, and two optional env vars (`HOSTBUD_HOOK_BASE_URL`, `HOSTBUD_RUN_STALE_AFTER`). Authentication, the Origin allowlist (for every route except the hook route), `sshx` as the only exec path, `store` as the only SQL, the single session service, M7's limits and the event-driven UI rules all still apply.

Setup for the manual checks: `make deploy` on the host (T13); real Claude Code and Codex installed and logged in on the host; a scratch project directory; a desktop browser on the port-forward path and the owner's iPhone on `https://${HOSTBUD_DOMAIN}`.

## Test coverage rule

Same as v1 ([../roadmap/M1-acceptance.md](../roadmap/M1-acceptance.md#test-coverage-rule)), with the v2 run rule: every criterion names its **U** (unit: Go with fakes, Vitest), **I** (integration: `-tags=integration` Go tests against `test/sshd`, PostgreSQL, or the real deploy/Caddy config) and **E** (e2e) tests and the task that writes each. **n/a** needs a one-line reason, and "manual" is used only where automation can't observe the behavior (real agent clients, the owner's devices). Tests land in the task named, in the same commit as the behavior.

**No real agents in automated tests**: U uses fixtures recorded in the T1 spike, I and E use the T7 stub `claude` / `codex` executables on the throwaway target.

**E2E runs once, in T13** (v2 ROADMAP *Rules*): scenarios written in T0–T12 are type-checked when written and **run for the first time in T13**, with the full suite (both profiles, all v1 and v2 scenarios). A ticked box means its U/I tests pass and its E scenario passed in the T13 run. Until T13, an E item counts as *written*, not *passed*.

Each criterion carries the number of the v2 ROADMAP V2-M1 acceptance criterion it details, as **(R*n*)**. All 17 are covered; see the traceability table at the end.

## Preconditions

- [ ] **P1** v1 through M7 is done, including M7's full e2e run (twice green); M8 may still be in progress. Recorded in the tasks file's Progress note.
- [ ] **P2** The v1 obligations in v1 ARCHITECTURE §10 hold: typed events on the `events` bus; one session-create service accepting `env` and `startCommand`; dialect-agnostic store with append-only migrations; room for token-authenticated `/api/hooks/*` that bypass the browser Origin check. Recorded in the Progress note (T0 re-checks the session service).
- [ ] **P3** The start-command bug is fixed: see *Prerequisite* below (T0).

## Prerequisite: session start command (T0)

- [ ] **(R1)** A session created with a start command exists after creation, runs the command, and runs in the chosen path. Start commands with spaces, quotes, `$` and flags work. The root cause is named in the fix commit.
  - U: T0 command construction for start commands with spaces, quotes, `$`, flags and a `~/…` path (Go).
  - I: T0 against `test/sshd`: the session exists, `#{pane_current_path}` is the chosen path, and the command's output is in `capture-pane` (Go).
  - E: T0 *Session with start command* (desktop and `iphone-13-pro`).
- [ ] **(R1, R6)** The single session-create service accepts `{machine, name, path, env, startCommand}`, and each `env` entry becomes one `tmux new-session -e KEY=VALUE` (quoted). Every entry point (UI, API, dispatcher) uses it.
  - U: T0 env entries → `-e` arguments; T4 run sessions call the same service (fake records the call) (Go).
  - I: T0/T4 `tmux show-environment -t '=<name>'` shows the variables (Go, `test/sshd`).
  - E: T9 *Two items in order* (run sessions' env checked on the target).
- [ ] The v1 ROADMAP *Later* item "session start command fails to create the session" is closed.
  - U/I: n/a: a document change; its behavior is covered above.
  - E: n/a: a document change.

## Spike (T1)

- [ ] **(R2)** v2 ARCHITECTURE has a section **"12. Spike results"** with one entry per check S1–S10, the **client versions tested**, and an answer to each §11 question:
  - Q1 Codex inline hooks through `-c hooks.<Event>=[…]` work interactively, and whether they merge with the user's hooks (S1);
  - Q2 both TUIs execute `/goal` passed as the initial prompt argument (S2);
  - Q3 whether `--settings` hooks run in an untrusted workspace, and the actionable message if not (S3);
  - Q4 the Codex reader chosen (host `sqlite3`/`python3` or rollout JSONL), with reasons and any host dependency (S7);
  - Q5 whether Claude's goal record carries a timestamp, or line order plus hook arrival time is used (S5);
  - Q6 the stale window default confirmed or changed (S9);
  - plus: the exact hook syntax per client (S1), the hook stdin fields and what `/clear`/resume does to the id (S4), the goal-record formats for met / not met / impossible and the Codex statuses (S5), the decoy result (S6), the token process-list finding (S8), and hook reachability from the host and the e2e target (S10).
  - U/I/E: n/a: a document; reviewed in T13 against this list.
- [ ] Design changes from the spike are in v2 §5 / §7 / §9 in the same commit; a failed check has its alternative written in v2 §7 before T2 (keeping interactive TUI, no host install, no user config change, no `send-keys`), or the client is dropped as an **open owner decision** (safe default).
  - U/I/E: n/a: documentation; reviewed in T13.
- [ ] The spike changed no user config: checksums of the client settings files before and after are recorded and equal. It killed no session; every session it created is listed for the owner.
  - U/I/E: n/a: a one-off host procedure; manual evidence recorded in §12 (the automated equivalent for runs is under *Host safety*).
- [ ] All spike output in the repo is redacted: `/home/dev` paths, made-up ids, `example.com`; no real session ids, thread ids, tokens, usernames or project names.
  - U: n/a: a document.
  - I: `make gitleaks` clean on the commit.
  - E: n/a.

## Schema and store (T2)

- [ ] **(R3)** One **append-only** migration creates `queues`, `queue_items`, `runs` and `run_events` as in v2 §6, with `machine_id` on every table, and applies on a copy of a populated v1 database, keeping every v1 row unchanged. No earlier migration file changes. The v1 §10 `tasks` / `machine_capacity` sketch is not created.
  - U: T2 schema shape (tables, columns, no `tasks`/`machine_capacity`) (Go).
  - I: T2 migrate a populated v1 copy: row counts and per-table checksums equal before and after; a second apply is a no-op; the append-only check passes (Go, PostgreSQL).
  - E: n/a: not user-visible; covered indirectly by every T9 scenario (and the T13 deploy keeps v1 data).
- [ ] CHECK constraints reject bad values: queue status (`idle|running|paused|finished`), item status (`queued|running|done|needs_attention|skipped`), agent (`claude|codex`), run status (`starting|running|achieved|failed|exited|stale|cancelled`), event source (`hook|poller|timer|user|llm`). `UNIQUE(queue_id, position)` holds. Indexes exist on `runs(item_id)`, `runs(status)` and `run_events(run_id, created_at)`.
  - U: T2 each CHECK and the UNIQUE constraint rejected with a typed error; index presence (Go).
  - I: T2 migration applied on PostgreSQL (same run as above).
  - E: n/a: constraints aren't reachable with bad values through the API (T8 validates first).
- [ ] `runs.id` is a ULID; `runs.token_hash` holds only the SHA-256; `run_events.payload_json` is capped at 64 KiB and keeps `transcript_path`; `runs.transcript_offset` (nullable) stores the incremental read offset and is recorded in v2 §6.
  - U: T2 ULID format, hash-only storage, 64 KiB + 1 payload refused, offset setter (Go).
  - I: T2 round-trip on PostgreSQL.
  - E: T3 *Hook endpoint* (a recorded event keeps the body).
- [ ] Store methods cover queue CRUD, item CRUD, **transactional reorder keeping positions dense**, run create, lookups by id / token hash / active item, active-run listing, event append and listing, and **guarded transitions** (`UPDATE … WHERE status = <expected>`) that report "not changed" on a wrong source state, so two concurrent signals can't double-advance. All SQL is in `store`.
  - U: T2 reorder (up, down, first↔last, no-op), delete re-densifies, guarded transition refuses a wrong state, two racing goroutines → one winner (Go); archtest keeps SQL in `store`.
  - I: T2 on PostgreSQL (the race test runs against the real database).
  - E: T8 *Queue API* (reorder and delete), T9 scenarios (transitions).

## Hook endpoint and run tokens (T3)

- [ ] **(R4)** Tokens are 32 random bytes (base64url), stored only as SHA-256, compared in constant time, and **scoped to one run** (another run's token is `401`).
  - U: T3 generation (length, alphabet, uniqueness), hashing, constant-time compare, cross-run token rejected (Go).
  - I: T3 through Caddy with a seeded run (Go).
  - E: T3 *Hook endpoint*.
- [ ] **(R4)** Tokens are **revoked when the run is finished with** (`achieved`, `failed`, `exited`, `cancelled`) — the endpoint answers `410` — and **not** on `stale`, where a hook is still `204`, so a late achieved is accepted.
  - U: T3 revocation per terminal state and none on `stale` (Go).
  - I: T9 a stale run accepts a later hook (Go, `test/sshd`).
  - E: T3 *Hook endpoint* (ended run `410`); T9 *Late achieved after stale*.
- [ ] **(R4)** `POST /api/hooks/{run_id}/{event}` with `event ∈ {session_start, turn_end, session_end}` answers exactly: `204` accepted, `401` bad or missing token, `404` unknown run (or event), `410` ended run, `413` body over 64 KiB; non-JSON is `400`; beyond the per-run rate limit it answers `429` with no side effect (429 recorded in v2 §8).
  - U: T3 each response code, the body cap at 64 KiB + 1, the rate limit burst (Go).
  - I: T3 through the real Caddy config (Go).
  - E: T3 *Hook endpoint*.
- [ ] **(R4)** The hook route needs **no cookie and no Origin**, and is exempt from the Tailscale identity gate; the bearer token is the only credential. The exemption is explicit (one named allowlist) and no other route is exempt. `routes.json` lists it with `token_auth`, and M7's shared Origin/auth tests treat it accordingly.
  - U: T3 middleware exemption list and the route-table rule; a foreign Origin doesn't change the answer (Go).
  - I: T3 through Caddy: no cookie, no Origin → `204` with the token, `401` without it; the `Authorization` header reaches the app intact (Go).
  - E: T3 *Hook endpoint* (foreign Origin, no cookie).
- [ ] A hook request only writes one `run_events` row (`source='hook'`, the body as sent, `transcript_path` kept) and notifies the dispatcher. It never runs a command and never reads the host inside the request.
  - U: T3 one row per `204`, dispatcher fake notified, no runner call (Go).
  - I: T9 end to end through the stub (Go, `test/sshd`).
  - E: T3 *Hook endpoint* (event recorded).
- [ ] `HOSTBUD_URL` in run sessions defaults to `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}` (Caddy on loopback), overridable with `HOSTBUD_HOOK_BASE_URL` (empty = default; validated absolute http(s) URL without path). It's in `.env.example` with a comment, in `internal/config`, and only in the `hostbud` service's compose `environment:`. Caddy proxies `/api/hooks/*` on the loopback site unchanged; no new published port.
  - U: T3 config parsing (empty, valid, with path → error, garbage → error); T4 env value default vs. override (Go).
  - I: T3 `internal/deploytest` (var reaches only `hostbud`; published ports unchanged); T12 `check-docs.sh` (Go/shell).
  - E: T9 *Two items in order* (`HOSTBUD_URL` on the target equals the e2e override, and hooks arrive through it).

## No secrets in logs or argv

- [ ] **(R5)** Tokens, `Authorization` headers and hook bodies are never logged at info; the run id and event are logged at debug only.
  - U: T3 handler logs with a capturing logger contain no token or body at info (Go).
  - I: T3 log-hygiene test (M7 T10's) extended to a hook call with a known token and body (Go).
  - E: n/a: logs aren't observable from the browser.
- [ ] **(R5)** The token never appears literally in the agent command line or the settings JSON / `-c` overrides: only `$HOSTBUD_RUN_TOKEN` (and `$HOSTBUD_URL`, `$HOSTBUD_RUN_ID`) are referenced.
  - U: T5 Claude argv and settings JSON, T6 Codex argv, both asserting the token string is absent (Go).
  - I: T7 stub argv log contains no token (Go, `test/sshd`).
  - E: n/a: the argv isn't visible to the browser; I covers it.
- [ ] **(R5)** Token exposure in the host's process list follows S8: if the stdin `set-environment` method works, the token never appears in `ps` during creation; if not, `-e` is kept and the known limitation is in the README security notes.
  - U: T4 the chosen method's command construction (Go).
  - I: T4 `ps -eo args` sampler during creation never sees the token (only if S8 chose the stdin method; otherwise n/a with the README note as evidence) (Go, `test/sshd`).
  - E: n/a: the host process list isn't observable from the browser.

## Runs start correctly (T4)

- [ ] **(R6)** A run's session is created through the **single session-create service** with name `<project>-q<position>` (collision-suffixed `-1`, `-2`, … by the v1 M6 rule), in the queue's project path, with `HOSTBUD_URL`, `HOSTBUD_RUN_ID` and `HOSTBUD_RUN_TOKEN` in its environment, and the adapter's argv passed unchanged to `sshx` (shell-quoted there, never string-concatenated). The session name is recorded on the run, and the run starts in `starting` with `started_at` set before the session exists.
  - U: T4 name derivation and suffixing, env map (exactly three keys), argv passed unchanged to a fake runner, `starting` + `started_at` ordering (Go).
  - I: T4 against `test/sshd`: `tmux show-environment -t '=<name>'` has the three variables, `#{pane_current_path}` is the project path, the first process sees the variables (Go).
  - E: T9 *Two items in order* (session names, path and `HOSTBUD_*` env checked on the target).
- [ ] A creation failure (tmux missing, path gone, env unsupported, client missing or too old) fails the run with the actionable message in `detail`, and nothing retries automatically.
  - U: T4 failure → run `failed` with `detail`; T5/T6 version-check errors (Go).
  - I: T5/T6 `--version` through `sshx` against a fake old client on the target (Go).
  - E: T9 *Needs attention* covers the pause; the message text is covered by U.

## Adapters (T5, T6)

- [ ] `internal/agents` defines `Adapter` and `GoalState` as in v2 §7, with a registry keyed by `Kind()`; an unknown agent kind is refused at item create/edit and at run start.
  - U: T5 registry lookups, unknown kind refused (Go).
  - I: n/a: pure in-process lookup.
  - E: T8 *Queue API* (an unknown agent is `400`).
- [ ] **Claude `BuildCommand`:** argv = `claude`, the user flags split shell-style (unbalanced quotes are a validation error), `--settings '<hooks json>'` with the single v2 §7 hook command for `SessionStart`/`Stop`/`SessionEnd` mapped to `session_start`/`turn_end`/`session_end` (syntax per S1), then `'/goal <condition>'` as the initial prompt argument. No `send-keys` typing.
  - U: T5 argv building (quoted flags, empty flags, unbalanced quotes, three hooks with the right events, the prompt last) (Go).
  - I: T7 stub-vs-adapter drift test: the stub accepts the argv and runs the injected hooks (Go, `test/sshd`).
  - E: T9 *Two items in order* (stub `claude`).
- [ ] **Codex `BuildCommand`:** argv = `codex`, the user flags, `-c 'hooks.SessionStart=[…]' -c 'hooks.Stop=[…]' -c 'hooks.SessionEnd=[…]'` (syntax per S1), then `'/goal <condition>'`; **never `-c notify=…`**.
  - U: T6 argv (three overrides, no `notify`, quoted flags) (Go).
  - I: T7 drift test with the stub `codex` (Go, `test/sshd`).
  - E: T9 *Mixed clients*.
- [ ] The hook command always exits 0, prints nothing, is bounded (`--max-time 5`), and references the token as an env var, so it can never block or steer the agent.
  - U: T5/T6 the hook command string (`-o /dev/null`, `|| true`, `--max-time`) (Go).
  - I: T7 the stub runs it against an unreachable URL and continues (Go, `test/sshd`).
  - E: T9 scenarios (hooks arrive through Caddy).
- [ ] `ParseHook` extracts the session id (Claude) / thread id (Codex, field per S4) and `transcript_path`; a body without an id is rejected, which the dispatcher treats as `unknown` ⇒ needs attention.
  - U: T5, T6 valid, missing id, extra fields ignored (Go).
  - I: T7 drift test (stub bodies parse) (Go).
  - E: T9 *Two items in order* (binding happens through real hook bodies).
- [ ] `transcript_path` is untrusted: it must be absolute, cleaned, under the remote user's `~/.claude/projects/` (Claude) or `~/.codex/` (Codex rollout, if chosen), and its SFTP `realpath` must still be under it (no symlink escape). It's opened read-only over SFTP. A failing path is `unknown`.
  - U: T5 (and T6 if rollout) relative, `..`, outside the tree, another user's home, symlink escape through a fake realpath (Go).
  - I: T5 a symlink under the projects dir pointing outside is refused over real SFTP (Go, `test/sshd`).
  - E: n/a: a crafted path can't be produced through the stubs without breaking the harness; U and I cover it.
- [ ] The Claude transcript is read **incrementally** from `runs.transcript_offset`, only complete lines; a truncated last line waits for the next read.
  - U: T5 second read starts at the saved offset, an appended line is seen once, a truncated line isn't consumed (Go).
  - I: T5 two appends over real SFTP (Go, `test/sshd`).
  - E: T9 *Two items in order* (`achieve:2` needs a second read).
- [ ] The Codex reader is the one chosen in S7. If it's a host tool, it opens the database read-only (`mode=ro`), binds the thread id as a parameter, runs from argv through `sshx` under the exec timeout, and a missing tool is an actionable error (e.g. "sqlite3 not found on the host — install with `sudo apt install sqlite3`"). The database file is unchanged by reads.
  - U: T6 query argv (`mode=ro`, bound parameter), missing-tool error mapping (Go).
  - I: T6 against a fixture `goals_1.sqlite` (or rollout file) under `/home/dev/.codex/`: checksum unchanged after reads; tool removed from `PATH` → the actionable error (Go, `test/sshd`).
  - E: T9 *Mixed clients*.
- [ ] Each run records `client_version` from `claude --version` / `codex --version` over `sshx`; below `MinVersion()` (the S5-tested version) or missing is an actionable error, and the run isn't started.
  - U: T5, T6 version parsing and comparison, error text (Go).
  - I: T5/T6 against a fake client on the target reporting an old version (Go, `test/sshd`).
  - E: n/a: the stubs always report a supported version in e2e; the failure path is covered by U/I and T10's error display (Vitest).

## Only a bound, structured achieved record advances (T5, T6, T9)

- [ ] **(R7)** **Claude:** a record is accepted as achieved only if it is a **top-level** JSON line with `type == "attachment"`, `attachment.type == "goal_status"` and `attachment.met == true`, from the transcript of the **bound** `session_id`, **after `run.started_at`** (timestamp or line order + hook arrival, per S5), with `attachment.condition` equal to the queued condition after whitespace normalization. The mapping is: `met:true` ⇒ `achieved`; "impossible" ⇒ `failed`; intermediate `met:false` or no record yet ⇒ `pending`; unparseable or unknown shape ⇒ `unknown`.
  - U: T5 the §5.3 matrix against fixtures per supported version: achieved, pending, failed, record before start, wrong session, changed condition, truncated last line, unknown shape (Go).
  - I: T5 `ReadGoalState` over SFTP against fixture transcripts on `test/sshd`; T9 one item from `starting` to `achieved` with the stub (Go).
  - E: T9 *Two items in order*.
- [ ] **(R7)** **Codex:** the `thread_goals` row (or S7 equivalent) for the **bound** thread id with `status == 'complete'`, `updated_at_ms` after `run.started_at` and `objective` equal to the queued condition ⇒ `achieved`; `blocked` ⇒ `failed`; `active`, `paused`, `usage_limited`, `budget_limited` ⇒ `pending`; anything else (no row, other objective, complete before start) ⇒ `unknown`.
  - U: T6 the status matrix from fixture rows or rollout files per supported version (Go).
  - I: T6 reader against `test/sshd` fixtures; T9 with the stub `codex` (Go).
  - E: T9 *Mixed clients*.
- [ ] **(R7)** Signals drive the reads: `turn_end` and `session_end` call `ReadGoalState` with the run's binding; the result, not the hook itself, decides the transition. Only `achieved` advances the queue.
  - U: T9 transition table with a fake adapter (Go).
  - I: T9 real hooks from the stub → reads → next session created (Go, `test/sshd`).
  - E: T9 *Two items in order*, *Mixed clients*.

## Decoy text never advances

- [ ] **(R8)** Marker text anywhere except a top-level record never advances: the `goal_status` JSON and the word "achieved" nested inside tool results or message content (Claude), or "complete"/marker text in the objective or messages (Codex), are ignored. Text search is never used; each line is parsed as JSON.
  - U: T5, T6 decoy fixtures (recorded in S6) ⇒ never `achieved` (Go).
  - I: T7 stub-vs-adapter drift test with the `decoy` behavior (Go, `test/sshd`).
  - E: T9 *Decoy text does not advance*.

## Fail closed (T9)

- [ ] **(R9)** `failed`, `exited`, `stale`, a **second `session_start` with a different id** (e.g. `/clear`) and an **`unknown`** goal state each set the item `needs_attention` with a readable `detail` and the queue `paused`, and the run's **session keeps running** (unless the agent itself exited it).
  - U: T9 each case in the transition table with a fake adapter and clock; the same `session_start` id twice is ignored (Go).
  - I: T9 a `fail` stub pauses the queue and its session is still alive on `test/sshd` (Go).
  - E: T9 *Needs attention* (fail, exit, silent/stale, clear; session alive).
- [ ] `session_end` reads the goal state once more before deciding: achieved ⇒ `achieved`, otherwise `exited`. The run's session disappearing from `sessions.changed` is handled the same way (`source='poller'`).
  - U: T9 `session_end` × {achieved, not}, session missing from inventory (Go).
  - I: T9 the stub's `exit` behavior on `test/sshd` (Go).
  - E: T9 *Needs attention* (exit case).
- [ ] No signal for `HOSTBUD_RUN_STALE_AFTER` (from `last_signal_at`, or `started_at` while `starting`) sets the run `stale`: a flag only; nothing is killed and the token stays valid. `HOSTBUD_RUN_STALE_AFTER` (Go duration, default `90m` or the S9 value, validated range) is in `.env.example` with a comment, in `internal/config`, and only in the `hostbud` service's compose `environment:`.
  - U: T9 stale timer with a fake clock, timer re-armed on each signal; config parsing and range (Go).
  - I: T9 `internal/deploytest` (var reaches only `hostbud`); T12 `check-docs.sh`.
  - E: T9 *Needs attention* (silent stub goes stale with the short e2e window).

## Late achieved after stale (T9)

- [ ] **(R10)** An `achieved` record that arrives after the run went `stale` marks the item `done`, but the queue **stays paused** until the owner resumes it; after Resume the next item starts.
  - U: T9 with a fake clock: stale, then a `turn_end` reading achieved ⇒ item done, queue paused; resume ⇒ next run (Go).
  - I: n/a: the timer semantics are fully covered with a fake clock in U; T9's I test covers the hook path itself.
  - E: T9 *Late achieved after stale*.

## Queue state machine and completion (T9)

- [ ] Start takes the first `queued` item (lowest position) of a `running` queue with **no active run**; there is never more than one active run per queue.
  - U: T9 start selection and the one-active-run invariant under concurrent triggers (Go).
  - I: T9 two items on `test/sshd`: the second session doesn't exist until the first achieves (Go).
  - E: T9 *Two items in order*.
- [ ] A concurrent or duplicate `turn_end` advances the queue **exactly once** (guarded transitions).
  - U: T9 two concurrent `turn_end`s with achieved ⇒ one next run (Go); T2 store race.
  - I: T9 duplicate hook POSTs against the real app and store (Go).
  - E: n/a: timing can't be forced reliably from the browser; U and I cover it.
- [ ] When no `queued` items remain after a `done`/`skipped`, the queue becomes `finished`.
  - U: T9 (Go).
  - I: T9 (Go, `test/sshd`).
  - E: T9 *Two items in order* (ends `finished`).
- [ ] Every transition writes a `run_events` row with its `source` (`hook`, `poller`, `timer`, `user`) and publishes a typed `run.changed` event; item and queue changes publish `queue.changed`. This is the audit trail for "why did the queue advance?".
  - U: T9 one row and one event per transition (Go).
  - I: T9 rows present after an end-to-end run (Go).
  - E: T10 *Queue panel* (the panel updates from these events).
- [ ] The dispatcher adds **no new tmux poll loop**: it reacts only to hooks, `sessions.changed` and its timers, and never calls `capture-pane` or `send-keys`.
  - U: T9 the fake session/tmux layer fails on any call other than create; archtest or a grep test that `internal/queue` doesn't import the poller loop or call `capture-pane`/`send-keys` (Go).
  - I: n/a: a structural property; U covers it.
  - E: n/a: not observable from the browser.

## Owner controls (T8, T9, T10)

- [ ] **(R11)** **Start / Pause / Resume:** `idle` (or `finished` with new queued items) ⇒ `running`; `running` ⇒ `paused`; `paused` ⇒ `running` (and starts the next item if no run is active). Invalid transitions answer `409` naming the current state. **Pause never touches the running session**: the run continues and is still tracked, and nothing new starts.
  - U: T8 service transitions valid/invalid; T9 pause keeps the run tracked (an achieved while paused marks the item done, nothing starts) (Go).
  - I: T8 routes against the real store (Go).
  - E: T8 *Queue API* (start, pause); T10 *Queue panel*.
- [ ] **(R11)** **Retry** on a `needs_attention` item re-queues it and, when the queue runs, starts a **new run** with a new token and a **new session** (collision-suffixed); the **old session stays open**.
  - U: T9 retry ⇒ `queued`, new run, new token, no kill call (Go).
  - I: T9 on `test/sshd`: both sessions exist after the retry (Go).
  - E: T9 *Retry and skip*; T10 *Queue panel*; T11 *Queue panel (phone)*.
- [ ] **(R11)** **Skip** ⇒ `skipped`; **Mark done** ⇒ `done` (owner override, `source='user'`), **confirmed** in the UI because it overrides the evaluator. Owner overrides are only allowed from `needs_attention` (`409` otherwise). An action on an item whose run is still active ends that run as **`cancelled`** (token revoked, session left open). The queue stays paused after an owner action until Resume.
  - U: T8 overrides only from `needs_attention`; T9 cancelled with token revoked and no kill; T10 Mark done and Skip confirmations (Go, Vitest).
  - I: T8 routes (Go).
  - E: T9 *Retry and skip*; T10 *Queue panel* (Mark done confirmation).

## Queue API (T8)

- [ ] **(R13)** The queue routes exist with the shapes fixed in T8 and documented in v1 ARCHITECTURE §9: `GET/POST /api/queues`, `GET/PATCH/DELETE /api/queues/{id}`, `POST /api/queues/{id}/items`, `PATCH/DELETE /api/queue-items/{id}`, `PUT /api/queues/{id}/order`, `POST /api/queues/{id}/start|pause|resume`, `POST /api/queue-items/{id}/retry|skip|mark-done`. Errors use `{error, hint}` with actionable text.
  - U: T8 handlers (Go).
  - I: T8 routes against the real store (Go).
  - E: T8 *Queue API*.
- [ ] **(R13)** Every queue route requires the cookie session (`401` otherwise), and every state-changing one checks Origin (`403` for a foreign or missing Origin, no side effect). They're in `routes.json`, so M7's limit, Origin and auth tables cover them (64 KiB JSON, `Content-Type`, 30 s deadline, `no-store`).
  - U: T8 route-table drift test with the new entries (Go).
  - I: T8 Origin rejection and unauthenticated `401` per route (Go).
  - E: T8 *Queue API*; M7 T12 *Origin allowlist on every route* (reads the same `routes.json`).
- [ ] Validation: `agent` is a registered kind; `flags` split cleanly; `instruction` is `/goal ` plus a non-empty condition; the project exists on the host machine. Invalid input is `400` with an actionable message.
  - U: T5 instruction/flags validation, T8 handler validation (Go).
  - I: T8 (Go).
  - E: T8 *Queue API*; T10 *Queue panel* (form errors).
- [ ] Editing rules: only `queued` items can be edited, deleted or moved; `running`, `done`, `skipped` and `needs_attention` items are read-only; reorder keeps positions dense; deleting a queue with an active run is refused with an actionable message.
  - U: T8 editability for every status, reorder validation, delete refusal (Go).
  - I: T8 (Go).
  - E: T8 *Queue API* (editing a running item refused).
- [ ] PoC limit: one queue per installation; creating a second is refused (`409`) with a message naming V2-M2.
  - U: T8 (Go).
  - I: T8 (Go).
  - E: T8 *Queue API*.

## UI (T10, T11)

- [ ] **(R14)** The panel updates only from `queue.changed` / `run.changed` over `/ws/events`: no polling and no reload; after a WS reconnect it refetches once.
  - U: T10 store event reducers (out-of-order, duplicate events, reconnect refetch); no interval timers in the store (Vitest).
  - I: n/a: frontend behavior.
  - E: T10 *Queue panel* (item 1 turns done and item 2 starts without a reload).
- [ ] **(R15)** Desktop panel (opened from the header and the command palette): pick the project; add, edit, delete items (agent select, flags field, instruction field prefilled `/goal `); reorder by drag and by keyboard (queued items only); Start / Pause / Resume; per-item status badge with the run state and `detail`; Retry / Skip / Mark done on `needs_attention` items; "Open session" opens the run's session in a terminal tab; read-only rendering for running/done/skipped items.
  - U: T10 button availability per status, form validation, open-session action (Vitest).
  - I: n/a: frontend only; the API is covered in T8/T9.
  - E: T10 *Queue panel*.
- [ ] **(R15)** Phone panel: a drawer or full-screen sheet, touch-sized controls (≥ 44 px), no horizontal scroll, reorder by touch drag or move buttons, confirmation sheets, and "Open session" switches to the single-terminal view.
  - U: T11 responsive layout components (Vitest).
  - I: n/a: frontend only.
  - E: T11 *Queue panel (phone)*.
- [ ] Errors in the panel are actionable: untrusted workspace (S3 text), client missing or too old, missing reader tool, validation errors, the one-queue limit, `409` state errors.
  - U: T10 error rendering for each error shape (Vitest).
  - I: n/a: the messages come from U-tested server code (T5, T6, T8).
  - E: T10 *Queue panel* (a needs-attention reason is shown).
- [ ] Autocomplete is off on the panel's inputs (M8 policy).
  - U: T10 attribute check (Vitest).
  - I: n/a: frontend markup.
  - E: covered by M8's autocomplete scenario pattern if it enumerates inputs; otherwise n/a (U covers it).

## Host safety

- [ ] **(R12)** hostbud **never kills a run's session**: no transition, owner action (retry, skip, mark done, cancel) or pause issues `kill-session`, `kill-server`, `send-keys` or a detach; the dispatcher only creates sessions. Closing one is the owner's, through the existing confirmation dialog.
  - U: T9 the fake session layer fails the test on any kill/send-keys call on every transition (Go).
  - I: T4/T9 sessions survive every terminal state on `test/sshd` (fail, stale, cancelled, retry) (Go).
  - E: T9 *Needs attention* (session alive), *Retry and skip* (old session still there).
- [ ] **(R12)** hostbud **changes no user config and installs nothing on the host**: hooks are injected per run only; `~/.claude` and `~/.codex` files (and tmux, shell, `~/.ssh`) are unchanged after runs; goal state is read read-only.
  - U: T5/T6 no write operations in the adapters (SFTP opened read-only; query `mode=ro`) (Go).
  - I: T4/T9 checksums of the fixture `~/.claude` / `~/.codex` files on `test/sshd` equal before and after runs; T6 the goals database checksum is unchanged (Go).
  - E: n/a: host files aren't observable from the browser; I covers it. (The real-host check is the spike's S1 checksum and the owner's manual check.)
- [ ] Every remote command (session create, version checks, the Codex query, SFTP reads) goes through `sshx` from argv, shell-quoted; user flags are split and quoted, never interpolated.
  - U: T4/T5/T6 argv through a fake runner; archtest: no exec outside `sshx` (Go).
  - I: T4 flags with quotes reach the stub intact (argv log) (Go, `test/sshd`).
  - E: n/a: the command path isn't visible to the browser.

## Restart safety (T9)

- [ ] **(R16)** After `hostbud` restarts mid-run, active runs are reloaded, their stale timers re-armed from `last_signal_at`, and each `running`/`stale` run gets one `ReadGoalState`, so a goal achieved during the restart is picked up and the queue advances; a session lost during the restart is handled as `session_end`.
  - U: T9 restart recovery with a fake clock and adapter (Go).
  - I: T9 restart the dispatcher against `test/sshd` while the stub achieves (Go).
  - E: T9 *Two items in order* (restart variant: `hostbud-e2e-app` restarted through ctl).

## Test harness (T7)

- [ ] Stub `claude` and `codex` executables on the `test/sshd` image accept the real argv shape, run the injected hook command for each event, write goal state in the exact S5/S7 formats, stay attached like a TUI, and support the behaviors `achieve:N`, `decoy`, `fail`, `exit`, `silent`, `silent-then-achieve`, `clear`, `pending`, chosen per run by a behavior file. `hostbud-e2e-app` has `HOSTBUD_HOOK_BASE_URL` (Caddy's loopback site by service name) and a short `HOSTBUD_RUN_STALE_AFTER`. Helpers set behaviors and read a run's env and path; ctl can restart the app.
  - U: n/a: test tooling.
  - I: T7 stub-vs-adapter drift test for every behavior (Go, `test/sshd`).
  - E: used by T9–T11; the suite type-checks.

## Docs (T12)

- [ ] **(R17)** v1 ARCHITECTURE: §10 is a pointer to `docs/roadmap-v2/` keeping obligations (1)–(4); §9 lists the queue and hook routes; §12 lists `HOSTBUD_RUN_STALE_AFTER` and `HOSTBUD_HOOK_BASE_URL`; §14 has `internal/{queue,agents}` instead of `internal/{llm,orchestrator}` (with `internal/llm` noted for V2-M5); §15 lists the hook body cap, the rate limit, the adapter read timeouts and the stale window; §8 mentions the v2 tables.
  - U/I: T12 docs consistency check (`check-docs.sh`: env vars in `.env.example`, compose and §12; routes in §9 and `routes.json`).
  - E: n/a: documentation.
- [ ] **(R17)** v1 ROADMAP: the *v2 — Orchestration* section is replaced by a link to the v2 roadmap; the start-command bug is gone from *Later*.
  - U/I: T12 docs check (link present).
  - E: n/a: documentation.
- [ ] **(R17)** v2 ARCHITECTURE's status says V2-M1 is implemented, the spike results are kept, and §5–§9 match the code; the v2 ROADMAP status is updated.
  - U/I/E: n/a: reviewed in T13.
- [ ] **(R17)** README has a "Queues" section: creating a queue, the `/goal <condition>` instruction format, agents and flags, what needs attention means and the owner actions, trusting a workspace, client minimum versions, that hostbud never closes run sessions, the token process-list note if `-e` was kept, and the new env vars.
  - U/I: T12 docs check (env vars listed).
  - E: n/a: documentation.
- [ ] **(R17)** AGENTS.md: the security item "(v2) Hook endpoints use per-run tokens (stored hashed)" is ticked; "v2 is design-only" is replaced by a pointer to the v2 roadmap. `.env.example` has both new vars with comments.
  - U/I: T12 docs check.
  - E: n/a: documentation.

## E2E scenarios (`make e2e`, simulated user)

Profiles: `desktop-chromium` and `iphone-13-pro` (plus the existing domain profile for the v1 suite). All run against the throwaway `hostbud-e2e-target` with the T7 stub clients only, **never the real host and never a real agent**. New files: `start-command.spec.ts`, `start-command.phone.spec.ts`, `hooks.api.spec.ts`, `queue.api.spec.ts`, `queue-runs.api.spec.ts`, `queue.spec.ts`, `queue.phone.spec.ts`; helpers `helpers/stubs.ts` and additions to `db.ts`, `target.ts`, `ctl.ts`. Each scenario is tagged with the task that writes it. Written and type-checked in T0–T11; **run for the first time in T13**, with every v1 scenario.

- [ ] **(T0) Session with start command:** create a session with a start command in a chosen folder; it appears under its project and the command's output shows in the terminal (desktop and `iphone-13-pro`); API variant: the session exists on the target with the right path.
- [ ] **(T3) Hook endpoint:** a seeded run accepts `session_start` with its token (`204`) and the event is recorded; wrong token `401`; another run's token `401`; unknown run `404`; ended run `410`; 65 KiB body `413`; a burst beyond the limit `429`; a foreign `Origin` and no cookie don't matter (desktop, API-level through Caddy).
- [ ] **(T8) Queue API:** create a queue for a project, add three items, reorder, edit a queued item, delete one, start and pause; a second queue is refused naming V2-M2; editing a running item is refused; a foreign Origin is rejected on every state-changing queue route (via M7's shared route list) (desktop, API-level).
- [ ] **(T9) Two items in order:** item 2's session appears only after item 1's stub writes its achieved record; session names, project path and `HOSTBUD_*` env checked on the target; the queue ends `finished`. Restart variant: `hostbud-e2e-app` restarted mid-run, the achieved record written during the restart is picked up (desktop, API-level, stubs).
- [ ] **(T9) Decoy text does not advance:** the queue stays on item 1, no second session exists (desktop, API-level).
- [ ] **(T9) Needs attention:** fail, exit, stale (silent) and changed session id (`clear`) each pause the queue with the item `needs_attention` and its reason, and the session is still alive (except the self-exited one) (desktop, API-level).
- [ ] **(T9) Late achieved after stale:** the item is `done`, the queue stays paused until resumed, then item 2 starts (desktop, API-level).
- [ ] **(T9) Retry and skip:** retry starts a new run in a new session (old one still there) that achieves; skip moves on after resume (desktop, API-level).
- [ ] **(T9) Mixed clients:** a stub `claude` item then a stub `codex` item, both achieving, in order (desktop, API-level).
- [ ] **(T10) Queue panel:** build a two-item queue in the UI, reorder by drag and keyboard, start, watch item 1 turn done and item 2 start without a reload, open item 2's session in a tab and see the stub's output; a needs-attention item shows its reason; Retry and Skip work; Mark done asks for confirmation (desktop).
- [ ] **(T11) Queue panel (phone):** create two items, reorder with move buttons, start, see status changes live, open a run's session (single-terminal view), Retry a needs-attention item (`iphone-13-pro`).
- [ ] **(T13) Full suite:** every v1 and v2 scenario in both profiles green in the milestone's single run, with no skips, retries-until-green or weakened assertions.

## Manual checks (owner; backlog, not blockers)

These are the owner's backlog: they don't hold back V2-M1's done state or the next milestone, and no agent waits for them. Record the date and the result here when the owner does one; an unchecked item stays open in the summary. Agents add items here when they hit something only the owner can do or decide, and keep going.

- [ ] **Real clients end to end** (v2 §10 *Accept*): on the host, a real Claude Code item followed by a real Codex item. The second starts only after the first's `/goal` is achieved, and **never on decoy output** (ask the first agent to print the marker text, e.g. the `goal_status` JSON and "achieved", before finishing).
- [ ] Trust the project directory once in each client if S3 shows it's required.
- [ ] Confirm the tested client versions (v2 §12) match what's installed on the host after upgrades; if a client upgrade changes a format, the run shows needs attention (fail closed) and the adapter needs new fixtures.
- [ ] Close the spike's and the real-client check's leftover sessions from the UI (hostbud never closes them).
- [ ] Install the Codex reader's host tool (e.g. `sqlite3`) if S7 chose one and it's missing.
- [ ] (Only if the spike dropped a client) decide whether to accept V2-M1 with one client or wait for an alternative.
- [ ] Try the Queue panel on the iPhone over the domain (installed PWA): create, start, open a run's session.

## Definition of done

- [ ] Preconditions P1–P3 recorded.
- [ ] Every functional and security criterion above is satisfied: its U/I tests pass and its E scenario passed in the T13 run.
- [ ] `make e2e` (full suite, both profiles, all v1 and v2 scenarios) is green in T13, and this checklist records the pass (date, commit).
- [ ] `make lint test` green after the last fix; `make gitleaks` clean; no secrets, real hostnames, IPs, usernames, home paths, session ids or tokens tracked (spike output and fixtures redacted).
- [ ] Docs aligned: v1 ARCHITECTURE (§8, §9, §10, §12, §14, §15), v1 ROADMAP, v2 ARCHITECTURE (status, §12 spike results, §5–§9) and ROADMAP status, README "Queues", AGENTS.md, `.env.example` (the two new vars); one append-only migration.
- [ ] *(host)* `make deploy` done; the stack is healthy, the migration applied with v1 data intact, the Queue panel opens.
- [ ] T14 safe Docker cleanup done (or skipped with the reason recorded): production stack, all volumes and backups intact and healthy; only hostbud's disposable artifacts and this milestone's leftovers removed; nothing outside hostbud touched; reclaimed space reported.
- [ ] Summary delivered: what changed; new env vars `HOSTBUD_HOOK_BASE_URL` and `HOSTBUD_RUN_STALE_AFTER` (both optional); manual steps on the host (trust workspaces, reader tool, leftover sessions); e2e results; open owner items.

## Traceability: v2 ROADMAP V2-M1 criteria → this checklist

| ROADMAP criterion | Section here | Tasks |
|---|---|---|
| R1 Prerequisite fixed | *Prerequisite: session start command* | T0 |
| R2 Spike recorded | *Spike* | T1 (reviewed T13) |
| R3 Schema append-only, keeps v1 data | *Schema and store* | T2 |
| R4 Hook endpoint contract | *Hook endpoint and run tokens* | T3 |
| R5 No secrets in logs or argv | *No secrets in logs or argv* | T3, T4, T5, T6 |
| R6 Runs start correctly | *Runs start correctly*; *Prerequisite* (single service) | T0, T4, T9 |
| R7 Only a bound, structured achieved record advances | *Only a bound, structured achieved record advances*; *Adapters* | T5, T6, T9 |
| R8 Decoy text never advances | *Decoy text never advances* | T5, T6, T7, T9 |
| R9 Fail closed | *Fail closed* | T9 |
| R10 Late achieved after stale | *Late achieved after stale* | T3, T9 |
| R11 Owner controls | *Owner controls* | T8, T9, T10 |
| R12 Never kills a session, no user config changes | *Host safety* | T4, T5, T6, T9 |
| R13 Queue API Origin-checked and authenticated | *Queue API* | T8 |
| R14 UI updates from events only | *UI* | T10 |
| R15 Panel usable on desktop and phone | *UI* | T10, T11 |
| R16 Restart safety | *Restart safety* | T9 |
| R17 Docs aligned | *Docs* | T12 |
| Owner check (real Claude then Codex) | *Manual checks (owner)* | — |
| Docker cleanup at milestone end (v2 *Rules*) | *Definition of done* | T14 |
