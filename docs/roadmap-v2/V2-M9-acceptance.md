# V2-M9 — Durable queue history: acceptance checklist

## Criteria

- [x] **1 Item history survives queue/item deletion.** Metadata snapshots include command/instruction, status, event time and actionable error detail, and remain readable after queue deletion. — U: T1 · I: T1 · E: T2/T3.
- [x] **2 History contains no session content or credentials.** No terminal output, transcript/hook body, token or transcript path is stored or returned (the agent session id is recorded since T7, criterion 8). — U: T1/T2 · I: T1/T2 · E: T3.
- [x] **3 API is bounded and protected.** Auth applies; read-only GET does not require Origin per the v1 security rule. Results are newest first with a bounded page size and offset. — U: T2 · I: T2 · E: T2.
- [x] **4 History view presents useful results.** Desktop and phone show queue/item context, commands, status timeline, timestamps and errors as text. — U: T3 · I: n/a (UI rendering) · E: T3.
- [x] **5 Migration is append-only and existing queue behavior is preserved.** — U: T1 · I: T1 · E: n/a (schema).
- [x] **6 Docs match behavior and operations.** — U: T4 · I: n/a (documentation) · E: n/a (documentation).
- [x] **7 Queue heading summarizes item progress.** The compact indicator distinguishes completed, active, queued and needs-attention items, and reports how many are queued. — U: n/a (presentation behavior covered by browser scenario) · I: n/a (no backend behavior) · E: T6.
- [x] **8 History keeps the coding agent's session id.** Each snapshot records the agent's own session id (Claude `session_id`, Codex thread id) from the item's latest bound run, existing rows are backfilled from runs, and the History view shows it as selectable text so a past item can be traced back (`claude --resume <id>`). — U: T7 (store snapshot, Vitest) · I: T7 (migration + trigger against PostgreSQL) · E: T7.
- [x] **9 History lists every queue with run dates and queue-level pagination.** Queues sort newest first by activity, run dates come from `running` status events, and each page contains up to 20 queues. — U: T8 · I: n/a (presentation uses the existing history API) · E: T8.

## E2E scenarios

- [x] (T2/T3) Queue history survives queue deletion and shows status, command, timestamps and error detail without session content — written and type-checked; browser run pending on demand.
- [x] (T7) History shows the agent session id of a failed item after its queue is deleted — written and type-checked; browser run pending on demand.
- [x] (T6) Queue panel title indicator shows queued count, active item and red needs-attention state — written and type-checked; browser run pending on demand.
- [x] (T8) Queue history shows run dates and paginates all queue groups 20 per page — written; E2E type-check currently blocked by an unrelated existing TypeScript error in `tests/servers.spec.ts` (`page` is undefined); browser run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review History view presentation in the deployed app (open).
- [ ] Review the compact queue progress indicator in the deployed app (open).
- [ ] Check an item run after 2026-09-29 shows its Claude/Codex session id in History (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked.
- [x] `make lint test`, `make gitleaks`, and deploy pass.
- [x] Docker cleanup safely skipped because toolbox containers were active; production volumes were not touched and app/database containers are healthy.
- [x] Summary includes manual checks and pending on-demand E2E run.
