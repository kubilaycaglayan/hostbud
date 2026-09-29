# V2-M10 — Plain prompts and per-agent completion tracking: tasks

Goal: let queue items use ordinary instructions while preserving evidence-based advancement. Codex uses its native thread goal; plain Claude items pause for owner review after a turn. Stored Claude items beginning with `/goal ` retain their existing tracking behavior.

## Progress

| Task | Status |
|---|---|
| T1 Plain instruction and completion lifecycle | Done |
| T2 Docs, acceptance, verification and deploy | Done: `make lint test`, E2E type-check, `make gitleaks`, deploy and health check passed |
| T3 Safe Docker cleanup | Done: skipped because hostbud toolbox containers and warm test targets were active |
| T4 Attach a new queue to an existing session | Done: `make lint test`, E2E type-check and gitleaks passed; browser run remains on demand |

## Tasks

### T1 — Plain instruction and completion lifecycle
Accept ordinary one-line instructions through UI/API and pass plain prompts to Claude. Continue accepting legacy `/goal ` Claude items and preserve their tracked completion. Codex receives the plain instruction and uses the app-server goal API. For plain Claude, the first Stop or SessionEnd closes hostbud's run tracking, moves the item to needs_attention and pauses the queue; the owner reviews and chooses Mark done or Retry. No turn-end signal or model-generated text counts as completion. Tests: U: prompt validation, adapter command construction and dispatcher behavior; I: PostgreSQL-backed queue API accepts a plain Claude prompt while empty prompts remain invalid. E2E: add desktop Queue-panel coverage showing plain prompt creation, needs-attention after Claude's turn, and confirmed Mark done.

### T2 — Docs, acceptance, verification and deploy
Update README and v2 architecture, complete acceptance, run `make lint test`, check e2e with `tsc`, run `make gitleaks`, deploy and check health. E2E: T1 scenario; browser run remains on demand only. Record manual owner checks as open.

### T3 — Safe Docker cleanup
Follow v2 Rules and V1 M7 T15 after deploy. Skip and record if anything is in use. Tests/E2E: n/a (operations only).

### T4 — Attach a new queue to an existing session
Let *Start after* name any existing tmux session, not only an active queue run. Persist it in `queues.after_session` (append-only migration 0014). The first item waits until the session is idle per the inventory snapshot (v2 ARCHITECTURE §3 decision 12); only the first item is gated. Tests: U: store link validation (integration), dispatcher readiness rules and first-item-only gating, API body passthrough, Vitest form option and dependency text; I: PostgreSQL persistence and invalid links. E2E: API-level *Queue attached to an existing non-queue session starts once that session is idle* and desktop *Attach a new queue to an existing non-queue session*.
