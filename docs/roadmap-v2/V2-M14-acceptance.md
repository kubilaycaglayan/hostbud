# V2-M14 — Terminal connection diagnostics: acceptance checklist

## Criteria

- [x] **1. Diagnostics are opt-in and content-safe.** The modeless panel keeps the terminal interactive, only probes while open, reports no input or terminal content, and stops probing when closed. — U: T1 (term protocol and client); I: T1 (websocket input probe); E: T1 (diagnostics panel and modeless interaction).
- [x] **2. Measurements separate the major latency stages.** The panel reports browser/server WebSocket round trip, hostbud-to-added-server SSH probe round trip, input-to-PTY acknowledgment, hostbud PTY write duration, browser WebSocket buffered bytes, xterm output processing through the next animation frame, and the last WebSocket close code/reason across reconnects, with guidance on interpretation and limits. Its copyable report excludes typed text and terminal output. Safe detach reason labels and durations are also logged without machine/session identifiers. — U: T1 (client metrics and reconnect persistence); I: T1 (PTY and remote SSH acknowledgments plus safe detach logging); E: T1 (visible values and content-free copied report).
- [ ] **3. Docs and deployed app are aligned.** README and architecture explain usage and limits; lint/tests pass, E2E is type-checked, gitleaks is clean, and deployed health is healthy. — U/I/E: n/a (docs/operations; T2 records checks). E2E type-check, gitleaks, docs check, build and deployed health passed. The full lint target has existing errors in `web/src/stores/tree.spec.ts`; the full test target has existing failures in terminal UI and tree/project store tests. The diagnostics API and Go integration tests passed.

## E2E scenarios

- [x] (T1) Desktop *connection diagnostics: input probes report PTY acknowledgment without exposing text* — written; type-check and on-demand run pending.
- [ ] (on demand) Full suite run — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Use the deployed diagnostics panel on a real added server during a noticeable typing lag and share the displayed measurements for diagnosis (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [x] Unit/integration tests and E2E scenario are written; E2E TypeScript check passed.
- [x] `make gitleaks`, docs consistency, deploy and health check passed; full lint/test remains open due the failures recorded above.
- [x] Safe Docker cleanup skipped because `make test` left the hostbud toolbox and throwaway integration target active; production containers/volumes were left intact.
- [x] Summary includes owner checks, pending on-demand E2E run and any new environment variables.
