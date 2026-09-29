# M8 E2E triage log

## Pass 01 — requested full suite

- **Commit under test:** `9952650` plus the M8 working-tree state.
- **Result:** 324 passed, 131 failed, 72 skipped, 0 flaky (527 total).
- **Playwright duration:** 48.6 minutes; approximately 49 minutes wall time.
- **Detailed report:** [2026-09-29-v1-m8-pass-01.md](../e2e-triage/2026-09-29-v1-m8-pass-01.md).
- **Main recurring finding:** 29 API scenarios hit the single-queue guard because the main API-only request fixture bypassed scenario reset. Fixed in `12d22a3` by sharing `page.request`; two harness regression scenarios are written and `make e2e-lint` passes (the scenarios remain unrun under the one-pass instruction). The 2-case LLM API Origin mismatch is also fixed by deriving `mutate()`’s default Origin from the active test base URL. The rest of the failure inventory remains open.
- **Next:** fix the report’s groups from highest frequency down. Do not run another full pass unless the owner asks.
