# V2-M12 — Agent marks in queue items: tasks

Goal: show the selected agent logo while selecting an agent and show each agent logo in the queue item list.

## Progress

| Task | Status |
|---|---|
| T1 Queue panel and coverage | Done |
| T2 Docs, verification and deploy | Done: `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and `/api/health` passed |
| T3 Safe Docker cleanup | Pending |

## Tasks

### T1 — Queue panel and coverage
Reuse the existing AgentMark for Claude Code and Codex. Show it alongside the native Agent selector in both Add item and Edit item forms, updating immediately as the selection changes. Show it beside the agent name for agent-backed queue items; leave existing-session command items without an agent mark.

Tests: U: QueuePanel.spec.ts asserts the picker updates and both item marks render. I: n/a (presentation only; no backend or deploy configuration behavior). E2E: extend queues.spec.ts to select Claude and Codex, see the picker mark update, add each item, and verify its visible queue row mark.

### T2 — Docs, acceptance, verification and deploy
Update README and this checklist; run `make lint test`, E2E TypeScript check, and `make gitleaks`; deploy with `make deploy` and verify `/api/health`. Browser E2E remains on demand. Record owner review and the on-demand run as open.

### T3 — Safe Docker cleanup
Follow the v2 Rules and V1 M7 T15 procedure; skip and record if anything is in use. Tests/E2E: n/a (operations only).
