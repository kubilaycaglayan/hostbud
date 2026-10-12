# Terminal latency investigation — 12/10/2026

## Observations

Read-only production checks used the generated SSH config and pinned keys. Independent echo processes did not attach to, type into, or modify existing tmux sessions. No terminal contents or target identifiers were retained here.

- During the preceding 24 hours, 121 rate-limited slow WebSocket ping warnings ranged from 100.45 to 942.52 ms. Four attaches ended with a WebSocket ping failure. These logs are thresholded samples, not a latency distribution.
- No small PTY write warnings (threshold 50 ms) appeared in that window. This does not measure remote receipt or browser rendering.
- The added target had 42 command-master resets after repeated timeouts during an earlier outage window. Established SSH connections were responsive during this investigation; the prior outage's cause is unproven.
- Over approximately 40 seconds, 199 established SSH echo samples per target (excluding startup) gave:

| Target | Median | p95 | Maximum |
| --- | --- | --- | --- |
| Built-in host | 0.67 ms | 1.71 ms | 3.64 ms |
| Added server | 4.64 ms | 10.76 ms | 36.86 ms |

These use a separate channel on the long-lived SSH master and include local Docker stream transport; they do not measure the browser, interactive tmux channel, or foreground application.

Both tmux servers reported `escape-time 10`. At inspection neither machine showed sustained CPU saturation; the deploy host had no memory pressure.

## Confirmed defect and correction (T4)

The terminal bridge waited synchronously for a keepalive pong in its output-forwarding loop every 25 seconds. Terminal output accumulated during that wait, potentially until the 10-second ping timeout. This amplifies browser-path latency into a visible output freeze for either target.

`TestBridgeOutputContinuesWhilePongPending` failed against the original code because output was withheld while the peer continued reading but withheld pong. A separate serial keepalive worker fixes that coupling and retains bounded ping timeouts, cancellation, and slow-ping logging. The regression passes with race detection. Real SSH/tmux integration and Caddy E2E regressions accompany the fix.

## Validation and remaining observations

Validation and deployment results are recorded in the T4 acceptance checklist. The E2E scenario is written for on-demand execution; no E2E run was requested. No environment variables or manual host changes are needed.

Open owner observation: compare ordinary shell and agent input on the affected device after deployment, and capture Connection diagnostics during any remaining stall. The confirmed backend defect is a contributor; these checks do not establish that all reported multi-second stalls share that cause. Browser network conditions, rendering, foreground application behavior, and the earlier added-server outage remain possible additional contributors.
