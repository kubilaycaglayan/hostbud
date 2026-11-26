# V2-M15 — Fixed-time window nudge

Goal: one opt-in daily schedule, independent of queues, periodically replaces `window-nudge-chore` and nudges Claude or Codex in one chosen machine and directory. Design: [ARCHITECTURE.md](ARCHITECTURE.md) §3 decision 15.

## T1 — Durable schedule model and UTC+03:00 clock
- Add an append-only PostgreSQL migration for one singleton configuration and occurrence claims/results. Keep SQL in `internal/store`; disabled by default. Persist machine id, absolute directory, provider, preset, next due time, enabled state and bounded last-run status/detail. Do not store prompts, terminal output or agent output.
- Implement the five fixed daily schedules in a fixed UTC+03:00 offset: `01,06,11,16,21`; `02,07,12,17,22`; `03,08,13,18,23`; `04,09,14,19,00`; `05,10,15,20,00`.
- Enable schedules at the next strictly future listed time. Never run immediately. On startup and after a clock jump, advance past elapsed slots without firing them. Claim each exact UTC occurrence transactionally so only one app instance can own it. Never replay a claimed occurrence after restart.
- **Tests:** U: preset validation, UTC+03:00 conversion, next strict future slot, midnight rollover, identical daily slots, clock jump and missed-slot skipping, duplicate/concurrent occurrence claims and restart recovery; I: migration append-only, singleton persistence, transactional claims and missed-slot advancement against PostgreSQL.
- **E2E:** n/a for this internal model; the saved next occurrence is exposed and checked in T2/T4, while clock edges are deterministic U/I coverage.

## T2 — Authenticated API and runtime wiring
- Add authenticated `GET /api/window-nudge` and `PUT /api/window-nudge` for saved settings, active state, next slot and last outcome. Validate registered machine, reachable path, provider (`claude` or `codex`) and preset; state-changing requests require the standard Origin allowlist. The API never permits caller-supplied past/due timestamps.
- Wire service startup independently of `HOSTBUD_QUEUE_DISPATCHER`; it is disabled unless the saved configuration is enabled. Publish `window_nudge.changed` when configuration or occurrence outcome changes.
- **Tests:** U: validation, Origin/auth, response privacy and event publication; I: API and durable configuration round trip against PostgreSQL; E: API-level *Configure and arm a window nudge* saves provider, target, schedule, enabled state and future next slot through Caddy; shared Origin route inventory includes the PUT route.
- **E2E:** add the API scenario in this task and type-check only.

## T3 — Scheduled session replacement and provider nudge
- At a successfully claimed occurrence, kill only the exact `window-nudge-chore` session on the configured machine if present; then create a session with that exact name in the configured directory and start the selected CLI (`claude` or `codex`) through the shared session-create service. Strict-name creation must fail on collision instead of generating a suffix.
- After session creation returns its actual name, send exactly `hi, please kill this tmux session {session-name}` to that session with `SendCommand`. The configured name is fixed as `window-nudge-chore`. No overlap or late retries; a failed occurrence is recorded and the next normal slot remains scheduled. The UI confirmation in T4 explains that each occurrence kills anything currently running in this exact session.
- **Tests:** U: strict-name collision, exact-name-only kill, order kill→create→prompt, provider argv and message interpolation, missing session handling, failure recording, and no retry; I: repeated replacement, path/provider/prompt delivery and adjacent-session preservation against `test/sshd` with stub clients.
- **E2E:** n/a for the due-time side effect; deterministic replacement and tmux safety are exercised against the isolated SSH integration target. T2/T4 cover the user-facing API and arming.

## T4 — Top-bar button and configuration dialog
- Add a top-bar **Window nudge** button. It opens a dialog to choose one machine, directory, provider and schedule. Reuse the filesystem browser to select the directory. Saving disabled settings does not kill a session.
- Enabling requires an in-app confirmation that names `window-nudge-chore` and says every scheduled occurrence ends that session before replacing it. The button is orange-filled only while enabled. Enabling waits for the next slot; disabling cancels future slots and leaves the current tmux session alone. Show the next scheduled time and last outcome.
- **Tests:** U (Vitest): provider/machine/path/schedule selection, preset display in UTC+03:00, confirm/cancel, orange active state, next-slot presentation, disable preserving the session, and errors/retry; I: n/a (the routes are exercised in T2/T3); E: desktop and phone *Configure and arm window nudge* and *Confirm recurring session replacement; disable leaves current session open*.
- **E2E:** add scenarios in this task; type-check only.

## T5 — Documentation, full verification and deploy
- Update README usage and both architecture/roadmap references. Record that the fixed UTC+03:00 offset does not follow host timezone or DST, that missed times are skipped, and the exact-session replacement confirmation.
- Run `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and loopback `/api/health`. Update acceptance and roadmap status. Leave real-provider and browser suite runs open for owner/on-demand work.
- **Tests:** U/I: T1–T4; E: T2/T4. Verification only; no new feature tests.
- **E2E:** none added; T2/T4 own scenarios.

## T6 — Safe Docker cleanup
- Follow the v2 Roadmap cleanup rule and v1 M7 T15 after deploy. Skip and record if any toolbox/test target is active. Never prune globally or touch production containers, volumes, backups, other projects or tmux sessions.
- **Tests:** n/a (cleanup operation; only repo-owned disposable objects; verify production health and volume presence if cleanup runs).
- **E2E:** none (no product behavior).
