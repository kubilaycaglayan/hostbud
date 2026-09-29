# V2-M7 — Queue lifecycle timestamps: acceptance checklist

## Criteria

- [x] **1 Queue lifecycle is recorded.** First start survives resume/restart; finish is recorded and cleared if a finished queue starts again. — U: T1 · I: T1 · E: T2 *Queue lifecycle timestamps*.
- [x] **2 Every item records its first start and latest terminal end.** Retry preserves the first start and clears the old end; terminal completion records a new end. — U: T1 · I: T1 · E: T2 *Queue lifecycle timestamps*.
- [x] **3 API and UI expose the timestamps.** — U: T2 · I: T2 · E: T2 *Queue lifecycle timestamps*.
- [x] **4 Docs describe lifecycle semantics and append-only migration.** — U: T3 · I: n/a (documentation) · E: n/a (documentation).

## E2E scenarios

- [x] (T2) Queue lifecycle timestamps — written/type-checked; run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review lifecycle timestamp presentation in deployed queue panel (open).

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked. Full browser run remains open until requested.
- [x] `make lint test`, `make gitleaks`, and deploy pass.
- [x] Docker cleanup skipped: production stack and toolbox containers are in use; untouched.
- [x] Summary includes manual checks and pending on-demand E2E run.
