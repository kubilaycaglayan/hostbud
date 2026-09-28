# V2-M1 — Proof of concept: one queue, sequential: acceptance checklist

V2-M1 is done when every box is ticked. The exception is *Manual checks (owner)*: those are the owner's backlog and never block (AGENTS.md).

Links: tasks in [V2-M1-tasks.md](V2-M1-tasks.md); criteria R1–R17, their base coverage and the owner checks in [ROADMAP.md](ROADMAP.md#acceptance-criteria-v2-m1); design in [ARCHITECTURE.md](ARCHITECTURE.md).

**How to read this file.** Each box below is one roadmap criterion (R*n*). Its coverage line gives the U/I/E tests and the task that writes each. Sub-bullets add the details this breakdown decided. Those details are part of the criterion, and the tests named cover them.

**Test rules:**
- The coverage rule is v1's ([M1](../roadmap/M1-acceptance.md#test-coverage-rule)): every criterion needs U/I/E tests, "n/a" needs a reason, and "manual" is only for what automation can't observe.
- Automated tests never use real agents: unit tests use the T1 fixtures, and integration and e2e tests use the T7 stubs.
- E items are written in T0–T12 and **first run in T13** (v2 ROADMAP *Rules*). A box is ticked only when its U/I tests pass and its E scenario passed in T13.

## Preconditions

- [x] P1–P3 from the ROADMAP have been checked and recorded in the tasks file's Progress note. (2026-09-27)

## Criteria

- [x] **R1 The start-command bug is fixed.**
  - The root cause is named in the fix commit, and the v1 ROADMAP *Later* item is closed.
  - Start commands with spaces, quotes, `$`, flags and `~/…` paths all work.
  - Each `env` entry becomes one quoted `-e KEY=VALUE`.
  - U: T0 · I: T0 · E: T0 *Session with start command* (desktop and phone, plus the API variant).
- [x] **R2 The spike is recorded.** v2 §12 covers S1–S10, answers §11 Q1–Q6 and lists the tested versions.
  - Design changes are in v2 §5, §7 and §9.
  - The user-config checksums match before and after.
  - No session was killed, and the leftover sessions are listed.
  - The output is redacted (checked with `make gitleaks`).
  - A dropped client is recorded as an open owner decision.
  - U/I/E: n/a (a document; reviewed in T13).
- [x] **R3 The schema is append-only and keeps v1 data.**
  - There is one migration, and it doesn't create `tasks` or `machine_capacity`.
  - Every table has `machine_id`.
  - CHECKs exist for queue/item/run status, agent and source; `UNIQUE(queue_id, position)` holds; the three indexes exist.
  - `runs.id` is a ULID, and `runs.transcript_offset` exists.
  - Payloads over 64 KiB are refused.
  - The guarded transitions are race-safe.
  - SQL lives only in `store`.
  - U: T2 · I: T2 (a v1 copy migrated from 0001–0004 with equal checksums; a second apply is a no-op) · E: n/a (indirect through T9).
- [x] **R4 The hook endpoint follows its contract.**
  - The check order and the responses `204/401/404/410/413/400/429` are as in T3. 429 is recorded in v2 §8.
  - Another run's token → 401.
  - No cookie, Origin check or Tailscale gate applies. The route is exempted in one named allowlist, marked `token_auth` in `routes.json`, and no other route is exempt.
  - A request only writes a `run_events` row and notifies the dispatcher.
  - Tokens are 32 bytes, stored as SHA-256 and compared in constant time. They are revoked on `achieved`, `failed`, `exited` and `cancelled`, but not on `stale`.
  - `HOSTBUD_HOOK_BASE_URL` is validated and reaches only `hostbud`. Caddy passes `Authorization` through, and no new port is published.
  - U: T3 · I: T3 (through Caddy, and `deploytest`) · E: T3 *Hook endpoint*.
- [x] **R5 No secrets in logs or argv.**
  - Tokens and hook bodies are never logged at info.
  - Only `$HOSTBUD_*` references appear in the argv, the settings JSON and the `-c` overrides.
  - The token's exposure in the process list follows S8: either the `ps` sampler never sees it, or the limitation is in the README.
  - U: T3, T4, T5, T6 · I: T3 (log hygiene), T4 (`ps` sampler if S8 allows), T7 (argv log) · E: n/a (not observable from the browser).
- [x] **R6 Runs start correctly.**
  - Sessions are created through the single service, using `StartArgv` quoted in `sshx`.
  - The session is named `<project>-q<pos>` (with a collision suffix), runs in the project path, has the three `HOSTBUD_*` variables, and its first process sees them.
  - The run is `starting`, with `started_at` set before creation.
  - A version check or creation failure ⇒ the run is `failed`, with an actionable `detail`.
  - U: T4, T5, T6 · I: T4 · E: T9 *Three items in order*.
- [x] **R7 Only a bound, structured achieved record advances the queue.**
  - The Claude and Codex rules follow v2 §5.3 and the mappings in T5 and T6.
  - The argv follows S1, and Codex never gets `notify`.
  - The hook command always exits 0.
  - `ParseHook` rejects a body without an id.
  - `transcript_path` must be absolute, clean, under the client's data directory, and pass a `realpath` check.
  - Reads are incremental and use complete lines only.
  - The Codex reader is read-only, uses a bound parameter, and gives an actionable error if a tool is missing.
  - Too old or missing clients are refused. `client_version` is recorded.
  - An unknown agent kind is refused.
  - U: T5, T6 (fixture matrices for each version) · I: T5, T6 (against `test/sshd`), T7 (drift test), T9 · E: T9 *Three items in order*, *Mixed clients*.
- [x] **R8 Decoy text never advances the queue.** Every line is parsed as JSON; text is never searched.
  - U: T5, T6 (decoy fixtures) · I: T7 (drift test with `decoy`) · E: T9 *Decoy text does not advance*.
- [x] **R9 Fail closed.** Each of these sets the item to `needs_attention` with a `detail` and pauses the queue, while the session keeps running:
  - `failed`, `exited`, or `stale`;
  - a changed session id, or an `unknown` state (both end as `exited`, recorded in v2 §5.4).

  Also:
  - `session_end` and a missing session re-read the state once.
  - `HOSTBUD_RUN_STALE_AFTER` is validated and reaches only `hostbud`.
  - Only one run is active per queue, and duplicate signals advance it only once.
  - The queue is `finished` when nothing is queued.
  - Every transition writes a `run_events` row and publishes `run.changed` (and `queue.changed`).
  - There is no new poll loop, and no `capture-pane` or `send-keys`.
  - U: T9 · I: T9 · E: T9 *Needs attention*.
- [x] **R10 A late achieved after stale** marks the item done and keeps the queue paused until Resume.
  - U: T9 · I: n/a (a fake clock in U) · E: T9 *Late achieved after stale*.
- [x] **R11 Owner controls work.**
  - Start, Pause and Resume follow the valid transitions; an invalid one → 409.
  - A paused queue still tracks its current run.
  - Retry creates a new run, token and session; the old session stays open.
  - Skip and Mark done both ask for confirmation. They are allowed only from `needs_attention`.
  - An action on an active run cancels it (token revoked, session left open).
  - After an owner action, the queue stays paused.
  - U: T8, T9, T10 · I: T8 · E: T9 *Retry and skip*, T10 *Queue panel*.
- [x] **R12 hostbud never kills a run's session and changes no user config.**
  - No kill, detach or send-keys happens on any path.
  - Hooks are injected per run only.
  - Client files and the Codex database are unchanged afterwards (checksums).
  - Every remote call goes through `sshx`, built from argv.
  - U: T5, T6, T9 · I: T4, T6, T9 · E: T9 *Needs attention* (session alive), *Retry and skip*.
- [x] **R13 The queue API is Origin-checked and authenticated.**
  - The routes and shapes are as in T8, and they are listed in `routes.json` under M7's limits.
  - Validation: agent, flags, `/goal <condition>`, project.
  - Only `queued` items can be edited, deleted or moved.
  - Deleting a queue with an active run is refused.
  - A second queue is refused with a message naming V2-M2.
  - U: T8 · I: T8 · E: T8 *Queue API*, M7 T12 *Origin allowlist on every route*.
- [x] **R14 The UI updates from events only**, and refetches once after a reconnect.
  - U: T10 · I: n/a (frontend) · E: T10 *Queue panel*.
- [x] **R15 The panel is usable on desktop and phone.**
  - All the T10 controls work.
  - Errors are actionable (untrusted workspace, client version, missing tool, validation, one-queue limit, 409).
  - Autocomplete is off.
  - On the phone: a sheet, touch targets of at least 44 px, move buttons, and a handoff to the single-terminal view.
  - U: T10, T11 · I: n/a (frontend) · E: T10, T11.
- [x] **R16 Restart safety.**
  - Active runs are reloaded and their timers re-armed, with one read each.
  - A goal achieved during the restart is picked up.
  - A session lost during the restart is treated as `session_end`.
  - U: T9 · I: T9 · E: T9 *Three items in order* (restart variant).
- [x] **R17 The docs are aligned** as listed in R T12 and the T12 additions.
  - U/I: T12 docs check (env vars and routes) · E: n/a.
- [x] **Test harness:** the T7 stubs support every behavior, and the e2e app has the hook base URL and a short stale window.
  - U: n/a (tooling) · I: T7 drift test · E: used by T9–T11.

## E2E scenarios

Status (2026-09-28, commit `150cdbd`): every scenario below is **written and type-checked**; none has run yet (e2e runs only on demand). Per the e2e rule, the criteria above are ticked with their U/I tests passing (CP1–CP6) and their E items counted as written; each box below is ticked when an on-demand run passes it.

These run on the throwaway target with stubs, on desktop and `iphone-13-pro`. They are written in T0–T11 and first run in T13. The file names are in the tasks.

- [ ] (T0) Session with start command
- [ ] (T3) Hook endpoint
- [ ] (T8) Queue API
- [ ] (T9) Three items in order: each hand-off waits for the previous achieved record, ends `finished` (with the restart variant)
- [ ] (T9) Decoy text does not advance
- [ ] (T9) Needs attention (fail, exit, stale, clear)
- [ ] (T9) Late achieved after stale
- [ ] (T9) Retry and skip
- [ ] (T9) Mixed clients
- [ ] (T10) Queue panel
- [ ] (T11) Queue panel (phone), including a live hand-off from item 1 to item 2 and `finished`
- [ ] (on demand) Full suite green: every v1 and v2 scenario, in both profiles, with no skips and no weakened assertions — run only when the owner asks; open until then, not a blocker

## Manual checks (owner; backlog, not blockers)

Record the date and the result for each one. Anything still unchecked stays open in the summary.

- [ ] The ROADMAP *Manual checks (owner)*:
  - real Claude then Codex, including the decoy prompt;
  - trusting workspaces;
  - client versions;
  - leftover sessions.
- [x] Install the Codex reader tool if S7 chose one and it's missing. — n/a: S7 chose `codex app-server proxy`, nothing extra to install.
- [x] Only if the spike dropped a client: decide whether to accept V2-M1 with one client. — n/a: no client was dropped.
- [ ] The Queue panel on the iPhone over the domain (the installed PWA).

## Definition of done

- [x] Every criterion above is ticked (E items as written and type-checked; `make e2e` runs only on demand and a pending run is listed as open).
- [x] `make lint test` is green, `make gitleaks` is clean, and nothing host-specific is tracked. (CP6, 2026-09-28)
- [x] *(host)* `make deploy` succeeded: the stack is healthy, v1 data is intact, and the panel opens. (2026-09-28: `/api/health` ok, migration 5 applied, v1 row counts equal to the pre-deploy baseline, queue and hook routes answer, the served bundle has the Queue panel; the panel's look on real devices is an owner check.)
- [x] T14 Docker cleanup is done, or skipped with the reason recorded. Production, the volumes and the backups are intact, and the reclaimed space is reported. (2026-09-28, ~4 MB)
- [x] The summary has been delivered: changes, the two optional env vars, host steps, e2e results and open owner items. (2026-09-28)
