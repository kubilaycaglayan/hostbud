# V2-M9 — Durable queue history: tasks

Goal: preserve status transitions and useful queue item metadata as historical data, including after a queue or item is deleted, and let the user review outcomes without storing session content.

## Progress

| Task | Status |
|---|---|
| T1 Append-only history schema and store | Done |
| T2 History API and privacy boundaries | Done |
| T3 History view and refresh behavior | Done |
| T4 Docs, acceptance, verification and deploy | Done: `make lint test`, `make gitleaks`, deploy and health check passed |
| T5 Safe Docker cleanup | Done: skipped because hostbud toolbox containers were active; production services and volumes left untouched |
| T6 Queue progress in the panel title | Done: web lint and E2E type-check passed; browser run remains on demand |
| T7 Agent session id on history | Done: store/api integration tests, QueuePanel Vitest, lint and E2E type-check passed; browser run remains on demand |
| T8 Queue-level history pages and run dates | Done: QueuePanel Vitest; E2E scenario written, browser run remains on demand |

## Tasks

### T1 — Append-only history schema and store
Add an independent history table with metadata snapshots for item creation, edits, status changes and deletion. Preserve queue/project labels, item position, execution mode, agent, flags, instruction or command, status, transition kind, actionable detail and timestamp. No foreign key to the live queue/item means history survives deletion. Never store transcript paths, raw hook payloads, terminal output or tokens (agent session ids: see T7). Tests: U: scan/order and snapshot privacy; I: migration on populated schema, transition snapshots and queue deletion retention. E2E: n/a (store only).

### T2 — History API and privacy boundaries
Add a bounded, newest-first authenticated API for the history records. Keep existing Origin protections on state-changing requests; return only the history DTO. Tests: U: route validation and serialization; I: authenticated reads, unauthenticated rejection and deleted queue history. E2E: add API-level history coverage through Caddy.

### T3 — History view
Add a Queue History view grouped by queue, showing item command/instruction, status changes, dates and errors. Use plain text rendering, support empty state and pagination/limit, and update from history data without polling. Tests: Vitest grouping, formatting, empty and long values; E2E: desktop and phone can open history, inspect a failed item and its status timeline after deleting its queue; session content is absent.

### T4 — Docs, acceptance, verification and deploy
Update v2 architecture and README; complete acceptance checklist; run `make lint test`, e2e `tsc`, `make gitleaks`, and deploy. Record owner checks and the full e2e run as open/on-demand. E2E: T2/T3 scenarios; full browser run remains on demand only.

### T5 — Safe Docker cleanup
Follow v2 Rules and V1 M7 T15 after deploy. Skip and record if any toolbox or test container is in use. Tests/E2E: n/a (operations only).

### T6 — Queue progress in the panel title
Show a compact colored segment for each item beside the Queue heading, with an accessible active/error summary and queued count. Done items are green, the active item is yellow, queued/skipped items are muted, and needs-attention items are red. Tests: U: n/a (small computed presentation state covered through the UI scenario); I: n/a (no backend behavior); E2E: extend the desktop Queue panel scenario to check queued, active and needs-attention states. E2E runs remain on demand.

### T7 — Agent session id on history
Owner request (2026-09-29): attach the coding agent's session id to history so past items can be checked later. Append-only migration 0018 adds `queue_item_history.agent_session_id`, redefines the history trigger to copy the latest bound run's `agent_session_id` (falling back to the last recorded one, so a delete snapshot keeps it) and backfills existing rows. The API returns `agentSessionId`; the History view shows it as selectable text. Snapshots taken before the hook binds (e.g. `running`) have none. Tests: U: store snapshot keeps the id across queue deletion; Vitest renders it; I: migration + trigger against PostgreSQL (store integration). E2E: extend the T2/T3 history scenario to check the API field and the History view.

### T8 — Queue-level history pages and run dates
Show queue run dates from recorded `running` status events, order queue groups by newest history activity, and paginate 20 queues per page. Load the bounded event API in pages until all queue history is present, so every queue is reachable through pagination. Tests: U: QueuePanel verifies run dates and 20-queue pagination/order. I: n/a (presentation uses the existing history API). E2E: add a browser scenario with 21 queues and verify both pages.
