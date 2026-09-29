# V2-M8 — Scheduled queue starts and execution targets: acceptance checklist

## Criteria

- [x] **1 Queue schedules are durable and bounded.** Relative delays become persisted UTC timestamps, survive restart, and do not dispatch before due; pause cancels the pending start. — U: T1/T2 · I: T1/T2 · E: T2.
- [x] **2 Existing queue behavior is preserved by default.** Existing and new items default to tracked agent sessions. — U: T1/T2 · I: T1/T2 · E: T3 (prior queue scenarios).
- [x] **3 Existing-session commands are dispatched safely.** The selected exact session must exist; shell metacharacters are passed literally as one command argument; a failed dispatch does not mark the item done; no session is killed. — U: T2 · I: T2 · E: T2/T3.
- [x] **4 API and UI expose schedule and target state.** Auth and Origin checks remain enforced; desktop and phone can set delays and select an existing session or new agent session. — U: T3 · I: T3 · E: T3.
- [x] **5 Migration is append-only and existing data remains valid.** — U: T1 · I: T1 · E: n/a (schema).
- [x] **6 Docs match behavior and operations.** — U: T4 · I: n/a (documentation) · E: n/a (documentation).

## E2E scenarios

- [x] (T2/T3) Delayed start and existing session target — scenario written and type-checked; browser run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review delayed-start and target picker presentation in the deployed queue panel (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked.
- [x] `make lint test`, `make gitleaks`, and deploy pass.
- [x] Docker cleanup completed; hostbud volumes remained intact and the app/database containers are healthy (0B reclaimed).
- [x] Summary includes manual checks and pending on-demand E2E run.
