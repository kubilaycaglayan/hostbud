# V2-M9 — Durable queue history: acceptance checklist

## Criteria

- [x] **1 Item history survives queue/item deletion.** Metadata snapshots include command/instruction, status, event time and actionable error detail, and remain readable after queue deletion. — U: T1 · I: T1 · E: T2/T3.
- [x] **2 History contains no session content or credentials.** No terminal output, transcript/hook body, token, transcript path or agent session id is stored or returned. — U: T1/T2 · I: T1/T2 · E: T3.
- [x] **3 API is bounded and protected.** Auth applies; read-only GET does not require Origin per the v1 security rule. Results are newest first with a bounded page size and offset. — U: T2 · I: T2 · E: T2.
- [x] **4 History view presents useful results.** Desktop and phone show queue/item context, commands, status timeline, timestamps and errors as text. — U: T3 · I: n/a (UI rendering) · E: T3.
- [x] **5 Migration is append-only and existing queue behavior is preserved.** — U: T1 · I: T1 · E: n/a (schema).
- [x] **6 Docs match behavior and operations.** — U: T4 · I: n/a (documentation) · E: n/a (documentation).

## E2E scenarios

- [x] (T2/T3) Queue history survives queue deletion and shows status, command, timestamps and error detail without session content — written and type-checked; browser run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review History view presentation in the deployed app (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked.
- [x] `make lint test`, `make gitleaks`, and deploy pass.
- [x] Docker cleanup safely skipped because toolbox containers were active; production volumes were not touched and app/database containers are healthy.
- [x] Summary includes manual checks and pending on-demand E2E run.
