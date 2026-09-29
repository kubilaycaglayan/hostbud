# M8 E2E triage log

## Pass 01 — requested full suite

- **Commit under test:** `9952650` plus the M8 working-tree state.
- **Result:** 324 passed, 131 failed, 72 skipped, 0 flaky (527 total).
- **Playwright duration:** 48.6 minutes; approximately 49 minutes wall time.
- **Detailed report:** [2026-09-29-v1-m8-pass-01.md](../e2e-triage/2026-09-29-v1-m8-pass-01.md).
- **Main recurring finding:** 29 API scenarios hit the single-queue guard because the main API-only request fixture bypassed scenario reset. Fixed in `12d22a3` by sharing `page.request`; two harness regression scenarios are written and `make e2e-lint` passes (the scenarios remain unrun under the one-pass instruction). The 2-case LLM API Origin mismatch is also fixed by deriving `mutate()`’s default Origin from the active test base URL. Locator fixes for duplicate toasts, project headers, terminal focus and phone scrollbar checks are written/type-checked but unrun. The multi-app project-reset and linked-session rename failures are also fixed, along with stale attachment-indicator assertions from before M8. Commit `c476a23` also corrects API reset, oversized JSON, linked rename, and attached-state expectations. Several remaining scenario groups are open pending a later requested run.
- **Fix batch update:** the 29-case queue isolation issue, the LLM Origin mismatch, reported locator/reset/expectation issues, and cached-shell fallback on Caddy 502 responses have been addressed in subsequent commits. The offline fallback has a passing unit test. Several report groups remain open because individual causes have not been established. E2E changes are type-checked; no second E2E pass has been run.
- **Next:** continue triaging remaining groups from highest frequency down. Do not run another E2E pass unless the owner asks.
