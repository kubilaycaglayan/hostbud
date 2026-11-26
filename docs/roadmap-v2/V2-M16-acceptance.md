# V2-M16 — Skip first window nudge once: acceptance checklist

## Criteria

- [x] **1. Saving an enabled schedule with Skip first consumes exactly the next future slot.** The following slot becomes `nextRunAt`; the option is not persisted or returned, and later saves/runs are unaffected. — U: T1; I: T1 (existing computed next-slot persistence); E: T1 API scenario (written/type-checked; on-demand run open).
- [x] **2. The dialog exposes and submits the one-time option.** It survives enable confirmation for the current save and resets when the dialog is opened again. — U: T2; I: n/a (same API path as criterion 1); E: T2 desktop and phone scenarios (written/type-checked; on-demand run open).
- [x] **3. Delivery and documentation checks pass.** README and architecture describe the one-shot semantics; `make lint test`, E2E TypeScript check, gitleaks, deploy and `/api/health` pass. — U/I: T3; E: T3 type-check.

## Manual checks (owner; backlog, not blockers)

- [ ] On the deployed UI, check Skip first with a schedule whose next slot is soon and confirm the following slot is displayed (open).
- [ ] Real-provider check and browser run remain open from V2-M15.
- [x] Docker cleanup skipped because hostbud toolbox containers and warm test targets are active; production health and all four hostbud volumes were verified intact (T4).

## Definition of done

- [x] All criteria pass; no schema migration or new environment variable is needed.
- [x] Safe Docker cleanup is skipped with its reason recorded; production health and volumes are verified.
- [x] Summary lists changes, manual steps and open owner items.
