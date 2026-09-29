# V2-M6 — Queue dependencies on active goals: tasks

Goal: allow a newly created queue to wait for a currently active, hostbud-tracked run to achieve its goal. A manually started tmux session is not eligible because hostbud has no structured goal binding for it. Scope and switch: [ROADMAP.md](ROADMAP.md#v2-m6--queue-after-active-goal-opt-in); design: [ARCHITECTURE.md](ARCHITECTURE.md).

## Progress

| Task | Status |
|---|---|
| T1 Schema and store | Implemented; append-only queue link and active same-machine run validation |
| T2 Queue service and dispatcher | Implemented; achievement releases dependents, failure pauses, stale can achieve late |
| T3 API and UI | Implemented; optional selector and visible dependency state |
| T4 Tests and docs | Implemented; Go, Vitest, API E2E, README and architecture coverage written |
| T5 Acceptance and deploy | Implemented; `make lint test`, gitleaks and deploy passed; E2E run remains on demand |
| T6 Safe Docker cleanup | Skipped; active toolbox and production stack are in use |

## T1 — Schema and store

Append `queues.after_run_id` (nullable) and store validation for an active same-machine run. Never delete or mutate the predecessor run. Tests: U migration sequence and scan; I append-only migration on populated DB and same-machine validation. E2E: n/a (storage only).

## T2 — Queue service and dispatcher

Accept an optional active `afterRunId` when creating a queue. A running dependent queue does not start until that run reaches `achieved`; achievement wakes it. Failed/exited/cancelled predecessors pause the dependent queue with an actionable reason. Stale predecessors remain eligible for a late achievement. Restart re-evaluates dependencies. Tests: U UI and service behavior; I dispatcher with stub goal runs and restart. E2E: *Queue waits for active goal* and *Unsuccessful predecessor pauses dependent queue* (API-level).

## T3 — API and UI

Expose eligible active runs in queue list data and add an opt-in selector to queue creation, defaulting to no dependency. Show the selected predecessor and waiting state. Tests: U API validation and Vitest default/selection; I Origin-checked API through real store; E2E: same T2 scenarios cover the create API and rendered queue status.

## T4 — Tests and docs

Update v2 architecture, roadmap, README, and docs consistency checks. Tests: U docs check. E2E: n/a (docs only).

## T5 — Acceptance and deploy

Run `make lint test`, E2E `tsc`, `make gitleaks`, and `make deploy`; no full E2E run (on demand only). E2E: no new scenarios.

## T6 — Safe Docker cleanup

Follow the v2 Rules and V1 M7 T15 procedure. Tests/E2E: n/a (operations only).
