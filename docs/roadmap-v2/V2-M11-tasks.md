# V2-M11 — Looping queues with a runtime limit: tasks

Goal: a queue can loop its items instead of finishing, and stops starting new passes once a configurable runtime since Start (default 5 h) has passed. Opt-in per queue, off by default. Design: [ARCHITECTURE.md](ARCHITECTURE.md) §3 decision 13.

## Progress

| Task | Status |
|---|---|
| T1 Schema, store, dispatcher and API | Done |
| T2 Queue panel | Done |
| T3 Docs, acceptance, verification and deploy | Done: `make lint test`, E2E type-check, `make gitleaks`, deploy and health check passed |
| T4 Safe Docker cleanup | Done: skipped because hostbud toolbox containers and warm test targets were active |
| T5 Queue default prompt (follow-up) | Implemented; U and I pass; E written and type-checked, browser run pending on demand |
| T6 Queue panel viewport sizing (follow-up) | Done: `make lint test`, E2E type-check, `make gitleaks`, deploy and health check passed |

## Tasks

### T1 — Schema, store, dispatcher and API
Append-only migration 0015 adds `loop_enabled`, `loop_max_runtime_seconds` (default 18000), `loop_started_at`, `loop_pass_started_at` and `loop_count` to `queues`. Start begins the loop clock (at the due time when scheduled). At the end of a pass the dispatcher requeues done and skipped items in one transaction unless the limit has passed, spacing passes by at least one minute through the durable schedule (also honoured by slot dispatch). `PUT /api/queues/{id}/loop {enabled, maxRuntime}`; the queue view carries `loop` only when the settings differ from the default, so V2-M1 responses keep their keys. Tests: U: dispatcher passes until the limit, a pass never cut short, needs-attention pauses without a pass, quick passes spaced and restored after restart, Pause cancels, Start requeues a finished looping queue, limit validation, API routes and validation; I: PostgreSQL loop settings, pass transaction, status guards and history rows; API round trip and Origin refusal. E2E: API-level *Loop queue runs its items again until the runtime limit*; the route is also in the shared Origin list via `routes.json`.

### T2 — Queue panel
A *Loop queue* form: *Loop the queue* checkbox, *Stop starting passes after* input (default `5h`, validated client- and server-side), *Save limit* when changed, and the current pass with when looping stops. Start stays enabled on a finished looping queue. Tests: U: Vitest for the form, validation, Save limit, Start on finished looping queues, and `queueControls`/duration helpers. I: n/a (the API is covered in T1). E2E: desktop *Loop a queue until its runtime limit*, phone *Loop a queue on phone; Pause cancels the pending pass*.

### T3 — Docs, acceptance, verification and deploy
README, v1 ARCHITECTURE §9 route, v2 ARCHITECTURE decision 13 and ROADMAP; run `make lint test`, e2e `tsc`, `make gitleaks`, then `make deploy` and a health check. E2E: T1/T2 scenarios; browser run is on demand only.

### T4 — Safe Docker cleanup
Follow the v2 Rules and V1 M7 T15 procedure; skip and record if anything is in use. Tests/E2E: n/a (operations only).

### T5 — Queue default prompt (follow-up)
Opt-in per queue, off by default (first built per machine in migration 0019 and Settings, then moved to each queue at the owner's request; 0019's columns stay unused): migration 0020 adds `default_prompt_enabled` and `default_prompt` (default `, commit regularly.`) to `queues`; `PUT /api/queues/{id}/default-prompt {enabled, text}` (one line, ≤ 1000 bytes); the queue view carries `defaultPrompt` once it differs from the default. Each queue's tab in the Queue panel gets a *Default prompt* form (checkbox saves at once, text saves with *Save default prompt*); when on, the new-item instruction starts with the selected queue's text (caret before a continuing prompt), resets to it after each add, follows a queue switch while untouched, and refuses the prefill alone. Design: ARCHITECTURE §3 decision 14. Tests: U: store (`TestQueueDefaultPrompt`), service (`TestSetDefaultPromptStoresAndPublishes`), API routes and validation (`TestQueueRoutesCallTheService`), Vitest for the helpers and the panel form and prefill; I: `queues_integration_test.go` round trip through PostgreSQL and the Origin refusal list. E2E: `queue-default-prompt.spec.ts`: API *Per queue, off by default, Origin-checked and validated* and desktop *Opting a queue in prefills its new item instruction; the item keeps what was typed*.

### T6 — Queue panel viewport sizing (follow-up)
Let the desktop Queue panel grow up to the available viewport height, leaving a small edge margin; keep long queue contents scrolling inside the panel. Tests: U: n/a (presentation sizing is browser layout); I: n/a (no backend behavior). E2E: extend `queues.spec.ts` to confirm a long queue fits a constrained viewport and its content scrolls inside the dialog. Run lint/test, E2E type-check and gitleaks; deploy and check health. E2E browser run remains on demand.
