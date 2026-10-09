# V2-M14 — Terminal connection diagnostics

## T1 — Measure the interactive terminal path
- Add an on-demand diagnostics panel to Terminal actions.
- When open, measure application ping and remote SSH `true` round trips, input-to-ack round trip, hostbud PTY write duration, browser WebSocket buffered bytes, received terminal byte count, and xterm output processing through the next browser animation frame.
- Add a copyable diagnostic report with a capture time and connection state; keep it free of typed text and terminal output.
- Preserve the last WebSocket close code and reason across automatic reconnects so slow-client and host-silence closes are visible in the panel and report.
- Log safe terminal detach reason labels (including abrupt WebSocket disconnects without a close frame), close codes and attach duration without machine/session names or raw error text.
- Input probes contain only a numeric correlation id; acknowledgments contain the id and PTY write duration. Never record keystrokes, output, session names or host names.
- Explain what each measurement can and cannot localize, including that time spent by the remote shell/application after PTY write is not measured.
- **Tests:** U: protocol parsing, browser input/ping/SSH-probe acknowledgment, and reconnect persistence for close details; I: websocket input probe is acknowledged after the PTY write, remote SSH probe returns success, and first-output timeout logs its safe reason; E: diagnostics panel reports input and remote SSH measurements and copies a report without typed text.
- **E2E:** add the diagnostics panel scenario in `test/e2e/tests/terminal.spec.ts` in this task; type-check it, but do not run the suite unless requested.

## T2 — Documentation and acceptance
- Update v1 architecture and README terminal usage.
- Run `make lint test`, E2E TypeScript check and `make gitleaks`; deploy and check `/api/health`.
- **Tests:** U/I: T1; E: T1. Record open owner verification against a real added server and the on-demand browser run.
- **E2E:** none added; T1 owns the scenario and this task changes documentation/operations only.

## T3 — Safe Docker cleanup
- After deploy, follow the V2 Roadmap safe-cleanup rule and V1 M7 T15. Skip if a toolbox, test target or other relevant container is active. Never prune globally or touch production containers, volumes, backups, other projects or tmux sessions.
- **Tests:** n/a (cleanup operation; verify production health and volume presence afterward if cleanup runs).
- **E2E:** none (no product behavior).
