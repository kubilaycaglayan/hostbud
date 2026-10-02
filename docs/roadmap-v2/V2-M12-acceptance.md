# V2-M12 — Agent marks in queue items: acceptance checklist

## Criteria

- [x] **1 Agent selection and queue visibility.** Add and Edit item forms show the selected Claude Code or Codex mark, and it updates with the selection; agent-backed queue items show the matching mark beside the agent name. Existing-session command items have no agent mark. — U: T1 (QueuePanel.spec.ts) · I: n/a (presentation only; no backend/deploy-config behavior) · E: T1 (queues.spec.ts).
- [x] **2 Docs and deploy.** README documents the queue marks; lint, unit/integration tests, E2E type-check, gitleaks and deployed health check pass. — U: n/a (documentation/operations) · I: T2 (deploy health check) · E: n/a (documentation/operations).

## E2E scenarios

- [x] (T1) Desktop *Agent marks follow the selected agent and appear in queue items* — written and type-checked; browser run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review agent marks in the deployed Queue panel (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] Unit/integration tests pass; E2E scenario is written and type-checked.
- [x] `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and health check pass.
- [x] Docker cleanup was safely skipped because hostbud toolbox containers and warm test targets were active; production volumes were not touched.
- [x] Summary includes owner checks and pending on-demand E2E run.
