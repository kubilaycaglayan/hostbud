# V2-M6 — Queue after active goal: acceptance checklist

## Criteria

- [x] **1 A queue can be linked to an active tracked run at creation.** Same-machine active run only; no dependency remains the default. — U: T1 (migration/scan) · I: T1 (PostgreSQL validates live run and rejects completed run) · E: T2 *Queue waits for active goal*.
- [x] **2 The queue waits until the predecessor goal is achieved, including across restart and late achievement after stale.** — U: T2 (dispatcher wait/release) · I: T2 (stub goal runs) · E: T2 *Queue waits for active goal*.
- [x] **3 Failed, exited or cancelled predecessor does not silently release the queue.** Dependent queue pauses with an actionable explanation. — U: T2 (terminal transition) · I: T2 (stub failure) · E: T2 *Unsuccessful predecessor pauses dependent queue*.
- [x] **4 Existing queues and default queue creation preserve current behavior.** — U: T2,T3 (Vitest) · I: T3 (real API) · E: T3 *No dependency by default*.
- [x] **5 Docs and opt-in behavior are aligned.** — U: T4 · I: n/a (docs check) · E: n/a (docs only).

## E2E scenarios

- [x] (T2) Queue waits for active goal — written/type-checked; run pending on demand.
- [x] (T2) Unsuccessful predecessor pauses dependent queue — written/type-checked; run pending on demand.
- [x] (T3) No dependency by default — written/type-checked; run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review the queue creation flow and waiting state on the deployed app (open).

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked. Full browser run remains open until requested.
- [x] `make lint test`, `make gitleaks`, and deploy pass.
- [x] Docker cleanup skipped: active toolbox is in use; production stack and volumes untouched; no space reclaimed.
- [x] Summary includes manual checks and the pending on-demand E2E run.
