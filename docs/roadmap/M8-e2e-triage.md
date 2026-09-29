# M8 E2E triage log

## Pass 01 — requested full suite

- **Commit under test:** `9952650` plus the M8 working-tree state.
- **Result:** 324 passed, 131 failed, 72 skipped, 0 flaky (527 total).
- **Playwright duration:** 48.6 minutes; approximately 49 minutes wall time.
- **Detailed report:** [2026-09-29-v1-m8-pass-01.md](../e2e-triage/2026-09-29-v1-m8-pass-01.md).
- **Main recurring finding:** 29 API scenarios hit the single-queue guard because the main API-only request fixture bypasses scenario reset. Fix batch pending.
- **Next:** fix the report’s groups from highest frequency down. Do not run another full pass unless the owner asks.
