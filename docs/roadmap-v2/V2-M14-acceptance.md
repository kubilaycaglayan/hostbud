# V2-M14 — Terminal connection diagnostics: acceptance checklist

## Criteria

- [x] **1. Diagnostics are opt-in and content-safe.** The modeless panel keeps the terminal interactive, only probes while open, reports no input or terminal content, and stops probing when closed. — U: T1 (term protocol and client); I: T1 (websocket input probe); E: T1 (diagnostics panel and modeless interaction).
- [x] **2. Measurements separate the major latency stages.** The panel reports browser/server WebSocket round trip, hostbud-to-added-server SSH probe round trip, input-to-PTY acknowledgment, hostbud PTY write duration, browser WebSocket buffered bytes, xterm output processing through the next animation frame, and the last WebSocket close code/reason across reconnects, with guidance on interpretation and limits. Its copyable report excludes typed text and terminal output. Safe detach reasons, SSH probe timings and thresholded slow input/ping warnings are logged without machine/session identifiers or input content. — U: T1 (client metrics and reconnect persistence); I: T1 (PTY/remote SSH acknowledgments and logging); E: T1 (visible values and content-free copied report).
- [ ] **3. Docs and deployed app are aligned.** README and architecture explain usage and limits; lint/tests pass, E2E is type-checked, gitleaks is clean, and deployed health is healthy. — U/I/E: n/a (docs/operations; T2 records checks). E2E type-check, gitleaks, docs check, build and deployed health passed. The full lint target has existing errors in `web/src/stores/tree.spec.ts`; the full test target has existing failures in terminal UI and tree/project store tests. The diagnostics API and Go integration tests passed.

- [x] **4. Keepalive waits do not pause terminal output.** A client withholding pong still receives new terminal output before the existing ping timeout. — U: T4 `TestBridgeOutputContinuesWhilePongPending`; I: T4 `TestIntegrationOutputContinuesWhilePongPending` against `test/sshd`; E: T4 *keepalive pong delay does not pause terminal output through Caddy*. Go unit and SSH integration regressions pass with race detection; E2E lint/type-check passes. The unit regression also verifies that withholding pong still disconnects at the ping timeout.

## E2E scenarios

- [x] (T1) Desktop *connection diagnostics: input probes report PTY acknowledgment without exposing text* — written; type-check and on-demand run pending.
- [x] (T4) Desktop *keepalive pong delay does not pause terminal output through Caddy* — written and type-checked; execution on demand.
- [ ] (on demand) Full suite run — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Use the deployed diagnostics panel on a real added server during a noticeable typing lag and share the displayed measurements for diagnosis (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

- [ ] (T4) Check ordinary shell and agent typing after deployment on the affected device; capture diagnostics if stalls remain.

## Definition of done

- [x] Unit/integration tests and E2E scenario are written; E2E TypeScript check passed.
- [x] `make gitleaks`, docs consistency, deploy and health check passed; full lint/test remains open due the failures recorded above.
- [x] Safe Docker cleanup skipped because `make test` left the hostbud toolbox and throwaway integration target active; production containers/volumes were left intact.
- [x] Summary includes owner checks, pending on-demand E2E run and any new environment variables.

## T4 verification — 12/10/2026

- `make lint` passed (existing frontend style warnings only).
- Full Go unit/integration suite passed with race detection. The unit regression was also rerun after adding ping-timeout coverage; it passes.
- E2E lint and TypeScript check passed; no browser scenarios were run.
- `make web-test`: 733 passed, 6 failed across unchanged `tree.servers.spec.ts`, `projects.spec.ts`, and `TerminalView.spec.ts`. These frontend-only failures remain outside this backend correction. Shell tests passed.
- Investigation evidence and limits: [latency investigation](V2-M14-latency-investigation.md).
- `make deploy` passed; hostbud and PostgreSQL are healthy and `/api/health` through loopback Caddy returned HTTP 200. `make gitleaks` passed.
- Safe Docker cleanup skipped: toolbox and integration containers remain active; no global pruning or production-volume cleanup performed.
- No new environment variables or manual host changes. Owner device observation and on-demand E2E remain open.
