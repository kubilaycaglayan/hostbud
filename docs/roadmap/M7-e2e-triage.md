# M7 E2E triage log

This file records each complete T13 suite pass and the failures that must be
resolved before another full pass. Focused scenario and spec runs remain part
of triage; they are not counted as full passes.

## Pass 1 — initial full run

- **Commit:** `d7096c3`
- **Result:** 15 passed, 300 failed, 49 skipped (364 total).
- **Duration:** Playwright reported approximately 1.4 hours.
- **Logs:** clean (245 markers checked).

**Inventory limitation:** the original run's 300 per-test failure rows and
artifacts were not preserved before the next run. `test/e2e/run.sh` empties
`test/e2e/results` at every run, and the first-run notes retained only the
aggregate counts and categories below. The exact 300 test titles/errors cannot
be recovered from the available checkout state. This is an incomplete record
of Pass 1; Pass 2 must be documented per scenario before another full run.

### Findings

| Finding | Classification | Status / evidence |
| --- | --- | --- |
| Docker build failed when `web/dist` was absent because the Vite closeBundle hook expected the directory. | Product/build bug | Fixed by `3063ca7`; test-first coverage in `38c5d8d`. |
| E2E Caddy exited because its read-only root filesystem had no writable `/data` or `/config`. | Harness configuration | Fixed by `d334d70`; regression check in `3313897`. |
| Fake Tailscale LocalAPI returned an invalid HTTP status line, so identity verification failed. | Harness configuration | Fixed in `d334d70` (`HTTP/1.1 200 OK`). |
| Empty session inventory serialized as `null` after project placement, causing a client `.map` exception. | Product/API bug | Fixed by `d8092f8`; regression test first in `06c09b3`. |
| Bundled fonts used `data:` URLs, rejected by the same-origin CSP. | Product/build bug | Fixed by `8528892`; regression test first in `861965c`. |
| A new account's initial theme-state GET returned 404 and was treated as a browser error, although missing initial state is normal. | E2E fixture issue | Fixed in `d7096c3`; fixture accepts the initial layout/tree/theme 404s. |
| A typed session name collided between inventory and tmux create; the service returned 409 instead of retrying with the next suffix. | Product/session bug | Fixed by `9f840e3`; regression test first in `8cfc256`. Focused action/API scenarios pass. |
| Rename E2E expected a dialog with a “New name” field, while the current tree UI uses inline editing. | Stale scenario | Updated in `9f840e3`; focused action spec passed all 5 desktop tests. |
| The broad run recorded 185 browser-console/failed-request errors, 107 tests timing out at 30 seconds, 5 at 90 seconds, and 3 null `.some` TypeErrors. | Multiple; investigate from Pass 2 artifacts | Awaiting the complete Pass 2 report to associate each with a scenario and root cause. |
| 49 scenarios were runtime-skipped because they target other profiles (desktop/phone or loopback/domain). | Test-suite policy violation | Still open. Preserve coverage while ensuring no scenarios are reported skipped, per T13. |

## Pass 2 — post-fix full run

- **Commit under test:** `37c009b`.
- **Result:** 128 passed, 187 failed, 49 skipped, 0 flaky (364 total).
- **Duration:** approximately 68 minutes wall-clock; Playwright reported 1.3 hours.
- **Detailed report:** [2026-09-27-v1-m7-pass-02.md](../e2e-triage/2026-09-27-v1-m7-pass-02.md).
- **Next:** triage and batch-fix every failure and eliminate runtime skips before another full run.

## Remaining verification

- `make lint test` has not passed: Go lint passed, but `scripts/test-readonly-image.sh`
  reported that its disposable SSH target status was `unreachable` (expected `ok`).
  Investigate and record whether this is a test environment or product issue.
- E2E suite lint/type-check passed after correcting the inline-rename scenario.
- Linked-worktree `make gitleaks` reports 0 commits scanned because the toolbox
  cannot resolve the host `.git/worktrees` path. Run a full-history scan from the
  main checkout after task commits are available there.
