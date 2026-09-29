# V2-M10 — Plain prompts and per-agent completion tracking: acceptance checklist

## Criteria

- [x] **1 Queue accepts ordinary one-line instructions.** The UI and API accept plain prompts; Claude receives the prompt unchanged. — U: T1 · I: T1 · E: T1.
- [x] **2 Completion tracking follows the agent contract.** Codex keeps native thread-goal completion; plain Claude enters needs_attention after a turn and never auto-advances; legacy Claude `/goal ` items retain goal tracking. — U: T1 · I: T1 (plain prompt API path) · E: T1.
- [x] **3 Manual review can advance a plain Claude item safely.** Mark done requires confirmation; retry remains available; no terminal session is killed. — U: T1 · I: T1 · E: T1.
- [x] **4 Docs explain the different completion behavior.** — U: n/a (documentation) · I: n/a (documentation) · E: n/a (documentation).
- [x] **5 A new queue can attach to any existing session.** Tracked or not, the queue's first item waits while the session's agent works and starts once it is idle or gone; later items are not gated; the session is never touched. — U: T4 · I: T4 · E: T4.

## E2E scenarios

- [x] (T1) Desktop queue panel creates a plain Claude prompt, shows needs-attention after its turn, and marks it done only after confirmation — written and type-checked; browser run pending on demand.
- [x] (T4) Queue attached to an existing non-queue session waits while it works and starts once idle (API and desktop) — written and type-checked; browser run pending on demand.
- [ ] (on demand) Full suite green on every e2e app and both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Review the plain-prompt Claude workflow in the deployed app (open).
- [ ] Attach a queue to a hand-started Claude/Codex session in the deployed app (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] U/I tests pass; E2E scenarios are written and type-checked.
- [x] `make lint test`, `make gitleaks`, and deploy pass; `/api/health` reports healthy.
- [x] Docker cleanup safely skipped because hostbud toolbox containers and warm test targets were active; production volumes were not touched.
- [x] Summary includes owner checks and the pending on-demand E2E run.
