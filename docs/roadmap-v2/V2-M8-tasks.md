# V2-M8 — Scheduled queue starts and execution targets: tasks

Goal: support durable relative-delay queue starts and queue items that either start a tracked agent session or dispatch a command into a selected existing session. Design: [ARCHITECTURE.md](ARCHITECTURE.md) §3 decision 11.

## Progress

| Task | Status |
|---|---|
| T1 Schema and store | Done |
| T2 Execution modes and dispatcher | Done |
| T3 API and UI | Done |
| T4 Documentation, acceptance and deploy | Done |
| T5 Safe Docker cleanup | Done: cleanup ran; production volumes intact; 0B reclaimed |

## Tasks

### T1 — Append-only schema and store
Persist `queues.scheduled_at`, plus each item's execution mode and optional existing-session target. Scheduled values are absolute UTC instants computed server-side from bounded relative durations. Existing rows default to agent mode. Tests: U scheduler and mode validation; I migration over a populated v2 schema and schedule survives a new store instance. E2E: n/a (API/UI consume in T3).

### T2 — Execution modes and dispatcher
Delay dispatch until the persisted due time, restore timers on restart, and cancel pending work when paused. For `agent` items retain the existing tracked-run flow. For `session` items validate the target session and send the command as one safely quoted argument plus Enter through `sshx`; mark done only after successful tmux dispatch. Never kill or close sessions. Tests: U fake-clock due/cancel/recovery and no dispatch before due; I against `test/sshd` for delayed dispatch, existing-session command delivery, shell metacharacters and target disappearance. E2E: *Delayed start and existing session target* (API-level through Caddy).

### T3 — API and queue panel
Add validated delay controls to Start (including examples `15m` and `4h14m`), show the scheduled time/countdown, and let each item select agent/new-session or existing-session/command with a session picker. Tests: U API validation/serialization and Vitest form behavior; I authenticated API request, Origin rejection and persisted queue/item view; E2E: desktop and phone *Delayed start and existing session target*, plus new-agent mode remains covered by prior run scenarios.

### T4 — Documentation, acceptance and deploy
Update README and both architecture references, finish acceptance checklist, run `make lint test`, e2e `tsc`, `make gitleaks`, then `make deploy`. E2E: T2/T3 scenarios; full browser run is on demand only.

### T5 — Safe Docker cleanup
Follow the v2 Rules and V1 M7 T15 procedure. Tests/E2E: n/a (operations only).
