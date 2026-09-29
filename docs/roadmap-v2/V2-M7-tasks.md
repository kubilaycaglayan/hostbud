# V2-M7 — Queue lifecycle timestamps: tasks

Goal: expose queue start/finish times and item start/end times through storage, API and queue UI. Design: [ARCHITECTURE.md](ARCHITECTURE.md) §6.

## Progress

| Task | Status |
|---|---|
| T1 Schema and store scans | Implemented; append-only migration and transition test pass |
| T2 API and UI | Implemented; timestamps exposed and rendered; API E2E written/type-checked |
| T3 Acceptance and deploy | Implemented; lint/test, gitleaks and deploy pass; browser run remains on demand |
| T4 Safe Docker cleanup | Skipped; production stack and toolbox containers are in use; left untouched |

## Tasks

### T1 — Append-only migration and store scans
Add nullable lifecycle columns with DB triggers for every status transition. Preserve first start across retries/restarts; clear end when work resumes. Tests: U timestamp transition behavior; I migration on existing populated schema. E2E: n/a (storage fields consumed in T2).

### T2 — API and UI
Expose lifecycle timestamps in queue and item payloads and render them in the queue panel. Tests: U JSON/API shape and Vitest rendering; I real API response. E2E: *Queue lifecycle timestamps* verifies first queue start, item start/end and queue finish are visible through the browser/API.

### T3 — Acceptance and deploy
Run `make lint test`, E2E `tsc`, `make gitleaks`, then `make deploy`; no full E2E run (on demand only). E2E: T2 scenario.

### T4 — Safe Docker cleanup
Follow the v2 Rules and V1 M7 T15 procedure. Tests/E2E: n/a (operations only).
