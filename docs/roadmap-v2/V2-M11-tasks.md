# V2-M11 — Looping queues with a runtime limit: tasks

Goal: a queue can loop its items instead of finishing, and stops starting new passes once a configurable runtime since Start (default 5 h) has passed. Opt-in per queue, off by default. Design: [ARCHITECTURE.md](ARCHITECTURE.md) §3 decision 13.

## Progress

| Task | Status |
|---|---|
| T1 Schema, store, dispatcher and API | Done |
| T2 Queue panel | Done |
| T3 Docs, acceptance, verification and deploy | Done |
| T4 Safe Docker cleanup | See acceptance checklist |

## Tasks

### T1 — Schema, store, dispatcher and API
Append-only migration 0015 adds `loop_enabled`, `loop_max_runtime_seconds` (default 18000), `loop_started_at`, `loop_pass_started_at` and `loop_count` to `queues`. Start begins the loop clock (at the due time when scheduled). At the end of a pass the dispatcher requeues done and skipped items in one transaction unless the limit has passed, spacing passes by at least one minute through the durable schedule (also honoured by slot dispatch). `PUT /api/queues/{id}/loop {enabled, maxRuntime}`; the queue view carries `loop` only when the settings differ from the default, so V2-M1 responses keep their keys. Tests: U: dispatcher passes until the limit, a pass never cut short, needs-attention pauses without a pass, quick passes spaced and restored after restart, Pause cancels, Start requeues a finished looping queue, limit validation, API routes and validation; I: PostgreSQL loop settings, pass transaction, status guards and history rows; API round trip and Origin refusal. E2E: API-level *Loop queue runs its items again until the runtime limit*; the route is also in the shared Origin list via `routes.json`.

### T2 — Queue panel
A *Loop queue* form: *Loop the queue* checkbox, *Stop starting passes after* input (default `5h`, validated client- and server-side), *Save limit* when changed, and the current pass with when looping stops. Start stays enabled on a finished looping queue. Tests: U: Vitest for the form, validation, Save limit, Start on finished looping queues, and `queueControls`/duration helpers. I: n/a (the API is covered in T1). E2E: desktop *Loop a queue until its runtime limit*, phone *Loop a queue on phone; Pause cancels the pending pass*.

### T3 — Docs, acceptance, verification and deploy
README, v1 ARCHITECTURE §9 route, v2 ARCHITECTURE decision 13 and ROADMAP; run `make lint test`, e2e `tsc`, `make gitleaks`, then `make deploy` and a health check. E2E: T1/T2 scenarios; browser run is on demand only.

### T4 — Safe Docker cleanup
Follow the v2 Rules and V1 M7 T15 procedure; skip and record if anything is in use. Tests/E2E: n/a (operations only).
