# V2-M11 — Looping queues with a runtime limit: acceptance checklist

## Criteria

- [x] **1 A looping queue runs its items again after the last one ends.** Done and skipped items are requeued in their positions and the pass is counted; a queue that doesn't loop finishes as before. — U: T1 · I: T1 · E: T1/T2.
- [x] **2 The runtime limit stops new passes.** Default 5 h, configurable per queue (1 s–30 d); counted from Start (the due time for a scheduled start); checked only between passes, so a started pass runs to its end. — U: T1 · I: T1 · E: T1/T2.
- [x] **3 Loops can't spin or run unattended past problems.** Passes start at least one minute apart through the durable schedule (survives restart); needs-attention pauses without a pass; Pause cancels a pending pass. — U: T1 · I: T1 (schedule persistence) · E: T1/T2.
- [x] **4 API and UI expose the loop.** `PUT /api/queues/{id}/loop` with Origin and auth checks; desktop and phone can toggle looping, set the limit and see the pass and when looping stops; Start restarts a finished looping queue. — U: T1/T2 · I: T1 · E: T1/T2.
- [x] **5 Existing data and responses are unchanged.** Migration 0015 is append-only with defaults (looping off); queues with default loop settings keep V2-M1's response keys. — U: T1 · I: T1 · E: n/a (schema).
- [x] **6 Docs match behavior.** — U: n/a (documentation) · I: n/a (documentation) · E: n/a (documentation).

## E2E scenarios

- [x] (T1) API *Loop queue runs its items again until the runtime limit* — written and type-checked; browser run pending on demand.
- [x] (T2) Desktop *Loop a queue until its runtime limit* and phone *Loop a queue on phone; Pause cancels the pending pass* — written and type-checked; browser run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review the loop controls in the deployed queue panel (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked.
- [x] `make lint test`, `make gitleaks`, and deploy pass; `/api/health` reports healthy; migration 0015 applied.
- [x] Docker cleanup safely skipped because hostbud toolbox containers and warm test targets were active; production volumes were not touched.
- [x] Summary includes owner checks and the pending on-demand E2E run.
