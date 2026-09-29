# E2E triage reports

Create one report for **every full `make e2e` run**, whether it passes or fails.
Do not create a full-pass report for a focused `make e2e-run` scenario/spec
check.

## File naming

Use `YYYY-MM-DD-<milestone>-pass-NN.md`, for example
`2026-09-27-v1-m7-pass-02.md`. Increment `NN` for every full suite execution
within that milestone. Keep each run's report as a separate file and link it
from that milestone's triage index.

## Required report contents

- Milestone, pass number, date, start–end time, full command and commit under test.
- Write all times in UTC+3 (the container clock is UTC: add 3 hours).
  Quote captured error output verbatim, even when it contains UTC timestamps.
- Exact Playwright passed, failed, skipped, flaky and total counts.
- Skipped should be 0: profiles are targeted in `playwright.config.ts`
  (file names and `@desktop`/`@phone`/`@loopback` tags), and runtime
  `test.skip()` is a lint error. List every skipped scenario with its reason
  (for example a `test.fixme`); treat an unexplained skip as a failure to fix.
- Elapsed wall time (and Playwright-reported duration if different).
- One row for **every failed scenario**, including its full test title,
  profile, concise error summary, classification, and confirmed or suspected
  shared cause. Mark unclassified causes as open; don't omit a failure because
  it appears related to another row.
- Results/artifacts location and any environment/setup failure.
- For fixes, link the fix commit(s) and focused checks. State explicitly when
  an issue is still open.

Generate and commit the report as soon as a full suite ends, before another
run. The E2E driver clears `test/e2e/results` at the beginning of the next
run. Do not start that next full run until the current report is complete and
committed, and all failures from the current pass have been fixed as a batch.
Focused checks can run during the batch.

Keep reports safe for this public repository: use only fixture values such as
`/home/dev`, `example.com` and generated `example.test` emails. Do not copy
secrets, real host values, private paths, raw application logs or credentials.

## Template

```markdown
# <Milestone> E2E pass <NN>

- Date:
- Time(UTC+3):
- Command:
- Commit:
- Result: <passed> passed, <failed> failed, <skipped> skipped, <flaky> flaky (<total> total)
- Elapsed wall time:
- Playwright duration:
- Artifacts:

## Failures

| Test title | Profile | Error summary | Class | Shared cause / status |
| --- | --- | --- | --- | --- |
|  |  |  |  |  |

## Environment notes

<Setup failures or `None`>

## Fix batch

<Fix commits and focused verification, or `Pending`>
```
