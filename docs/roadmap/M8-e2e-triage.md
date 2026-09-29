# M8 E2E triage log

## Pass 01 — requested full suite

- **Commit under test:** `9952650` plus the M8 working-tree state.
- **Result:** 324 passed, 131 failed, 72 skipped, 0 flaky (527 total).
- **Playwright duration:** 48.6 minutes; approximately 49 minutes wall time.
- **Detailed report:** [2026-09-29-v1-m8-pass-01.md](../e2e-triage/2026-09-29-v1-m8-pass-01.md).
- **Main recurring finding:** 29 API scenarios hit the single-queue guard because the main API-only request fixture bypassed scenario reset. Fixed in `12d22a3` by sharing `page.request`; two harness regression scenarios are written and `make e2e-lint` passes (the scenarios remain unrun under the one-pass instruction). The 2-case LLM API Origin mismatch is fixed by deriving `mutate()`'s default Origin from the active test base URL. The multi-app project-reset and linked-session rename failures are fixed, along with stale attachment-indicator assertions from before M8. Commit `c476a23` corrects API reset, oversized JSON, linked rename, and attached-state expectations.
- **Fix batch update:** corrections are recorded for all 29 reported groups, worked from the most frequent downward. They include reported locator/reset/expectation issues, cached-shell fallback on Caddy 502 responses, project pin menu timing, deferred “Show keyboard” focus, shell/scroll assumptions, tree startup ordering and touch drag support, compact tree visibility after actions, theme localStorage isolation, tmux-stall retry state, permission-mode setup, the phone timeout route stub, parallel-session readiness, queue-item fallback dragging, notification permission for the active application origin, session-inventory readiness, and deterministic supervisor classification timing. The offline fallback, tree ordering, touch-drag configuration, queue drag configuration, and pin-menu behavior have unit coverage. Current checks: `make e2e-lint` passes; prior `make web-test` passed 621 tests. Scenario outcomes remain unverified because the requested loop prohibits a second pass.
- **Next:** stop here unless the owner explicitly requests another E2E pass. No second full or focused E2E run has been performed.

## Pass 02 — requested full suite

- **Commit under test:** `3fe7ae4` (clean working tree).
- **Result:** 459 passed, 7 failed, 77 skipped, 0 flaky (543 total).
- **Playwright duration:** 39.6 minutes; approximately 40 minutes wall time.
- **Detailed report:** [2026-09-29-v1-m8-pass-02.md](../e2e-triage/2026-09-29-v1-m8-pass-02.md).
- **Findings:** the pass 01 batch cleared 124 failures. Remaining: queue-history strict-mode locator (2 profiles), tree.custom client-pid checks in T4/T5, renamed-session expanded state after restart (T6), and the phone "Project tree" dialog timeout in mobile-layout T16 (2 profiles). All open.
- **Next:** fix the 7 failures as a batch; no further full run unless the owner asks.

## Pass 03 — requested full suite

- **Commit under test:** `1c14bda` (clean working tree).
- **Result:** 463 passed, 6 failed, 79 skipped, 0 flaky (548 total).
- **Playwright duration:** 40.9 minutes; 41.5 minutes wall time.
- **Detailed report:** [2026-09-29-v1-m8-pass-03.md](../e2e-triage/2026-09-29-v1-m8-pass-03.md).
- **Findings:** the pass 02 batch cleared A, B and C. Still failing: mobile-layout T16, which now hits a strict-mode palette option match (2 phone profiles). New: caret T5 Option-click is one column off (desktop), the push Expired subscription queue never finished (desktop), the queue Loop checkbox won't check (desktop), and the palette T8 phone shortcut doesn't focus the tree row (iPhone). All open.
- **Next:** fix the 6 failures as a batch; no further full run unless the owner asks.
- **Skipped scenarios (2026-09-29):** all 79 skips were profile targeting, not failures or `fixme`s. Profile targeting moved from runtime `test.skip()` to `playwright.config.ts` (`@desktop`/`@phone`/`@loopback` tags with `grepInvert`, plus the existing file-name rules), and eslint now rejects `test.skip()` in specs. `playwright test --list` drops from 551 to 472 scheduled tests (exactly the 79 skips) with all 329 unique scenarios still scheduled on their intended profiles. The duplicated per-project loops in `hardening.spec.ts` were folded into single scenarios. Type-checked (`make e2e-lint`), not run. The next requested full run should report 0 skipped.
