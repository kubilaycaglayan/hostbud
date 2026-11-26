# V2-M16 — Skip first window nudge once

Goal: optionally skip the next upcoming slot once when saving an enabled Window nudge schedule. Design: [ARCHITECTURE.md](ARCHITECTURE.md) §3 decision 16.

## T1 — One-time skip behavior
- Carry `skipFirst` on the authenticated PUT request only. When enabled and checked, arm the second strictly future slot; do not persist or return a skip flag. Disabled schedules remain unarmed.
- **Tests:** U: service arms second slot and ordinary save arms first slot; I: n/a (existing PostgreSQL persistence path stores the computed next slot without schema changes).
- **E2E:** extend the Window nudge API scenario to send the option and verify it is not part of saved settings; type-check only.

## T2 — Settings checkbox and coverage
- Add “Skip the next scheduled time once” to the dialog and include it on save. Reopening resets it because it is a one-save option.
- **Tests:** U: dialog sends the option after enable confirmation; I: n/a (UI uses the existing API); E: extend the desktop configuration scenario to exercise the checkbox.
- **E2E:** checkbox is covered in the existing Window nudge UI scenario; type-check only.

## T3 — Docs, verification and deploy
- Update README, v2 architecture, roadmap and this acceptance checklist. Run `make lint test`, `make e2e-lint`, `make gitleaks`, deploy, and verify loopback health.
- **Tests:** U/I: T1/T2; E: T1/T2 type-check.
- **E2E:** none added; T1/T2 own the scenarios.

## T4 — Safe Docker cleanup
- Follow v2 Roadmap cleanup rules after deploy. **Skipped:** active hostbud toolbox containers and warm test targets are in use; production health and all four hostbud volumes were verified intact.
- **Tests:** n/a (cleanup only; check health/volumes if cleanup runs).
- **E2E:** none (no product behavior).
