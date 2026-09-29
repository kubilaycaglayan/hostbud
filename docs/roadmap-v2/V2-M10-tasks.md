# V2-M10 — Plain prompts and per-agent completion tracking: tasks

Goal: let queue items use ordinary instructions while preserving evidence-based advancement. Codex uses its native thread goal; plain Claude items pause for owner review after a turn. Stored Claude items beginning with `/goal ` retain their existing tracking behavior.

## Progress

| Task | Status |
|---|---|
| T1 Plain instruction and completion lifecycle | Done |
| T2 Docs, acceptance, verification and deploy | Done: `make lint test`, E2E type-check, `make gitleaks`, deploy and health check passed |
| T3 Safe Docker cleanup | Done: skipped because hostbud toolbox containers and warm test targets were active |

## Tasks

### T1 — Plain instruction and completion lifecycle
Accept ordinary one-line instructions through UI/API and pass plain prompts to Claude. Continue accepting legacy `/goal ` Claude items and preserve their tracked completion. Codex receives the plain instruction and uses the app-server goal API. For plain Claude, the first Stop or SessionEnd closes hostbud's run tracking, moves the item to needs_attention and pauses the queue; the owner reviews and chooses Mark done or Retry. No turn-end signal or model-generated text counts as completion. Tests: U: prompt validation, adapter command construction and dispatcher behavior; I: PostgreSQL-backed queue API accepts a plain Claude prompt while empty prompts remain invalid. E2E: add desktop Queue-panel coverage showing plain prompt creation, needs-attention after Claude's turn, and confirmed Mark done.

### T2 — Docs, acceptance, verification and deploy
Update README and v2 architecture, complete acceptance, run `make lint test`, check e2e with `tsc`, run `make gitleaks`, deploy and check health. E2E: T1 scenario; browser run remains on demand only. Record manual owner checks as open.

### T3 — Safe Docker cleanup
Follow v2 Rules and V1 M7 T15 after deploy. Skip and record if anything is in use. Tests/E2E: n/a (operations only).
