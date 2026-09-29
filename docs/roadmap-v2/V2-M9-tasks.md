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

## Tasks

### T1 — Append-only history schema and store
Add an independent history table with metadata snapshots for item creation, edits, status changes and deletion. Preserve queue/project labels, item position, execution mode, agent, flags, instruction or command, status, transition kind, actionable detail and timestamp. No foreign key to the live queue/item means history survives deletion. Never store transcript paths, raw hook payloads, terminal output, tokens or agent session ids. Tests: U: scan/order and snapshot privacy; I: migration on populated schema, transition snapshots and queue deletion retention. E2E: n/a (store only).

### T2 — History API and privacy boundaries
Add a bounded, newest-first authenticated API for the history records. Keep existing Origin protections on state-changing requests; return only the history DTO. Tests: U: route validation and serialization; I: authenticated reads, unauthenticated rejection and deleted queue history. E2E: add API-level history coverage through Caddy.

### T3 — History view
Add a Queue History view grouped by queue, showing item command/instruction, status changes, dates and errors. Use plain text rendering, support empty state and pagination/limit, and update from history data without polling. Tests: Vitest grouping, formatting, empty and long values; E2E: desktop and phone can open history, inspect a failed item and its status timeline after deleting its queue; session content is absent.

### T4 — Docs, acceptance, verification and deploy
Update v2 architecture and README; complete acceptance checklist; run `make lint test`, e2e `tsc`, `make gitleaks`, and deploy. Record owner checks and the full e2e run as open/on-demand. E2E: T2/T3 scenarios; full browser run remains on demand only.

### T5 — Safe Docker cleanup
Follow v2 Rules and V1 M7 T15 after deploy. Skip and record if any toolbox or test container is in use. Tests/E2E: n/a (operations only).

### T6 — Queue progress in the panel title
Show a compact colored segment for each item beside the Queue heading, with an accessible active/error summary and queued count. Done items are green, the active item uses the accent color, queued/skipped items are muted, and needs-attention items are red. Tests: U: n/a (small computed presentation state covered through the UI scenario); I: n/a (no backend behavior); E2E: extend the desktop Queue panel scenario to check queued, active and needs-attention states. E2E runs remain on demand.
