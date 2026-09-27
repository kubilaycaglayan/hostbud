# M8 — Interface density and input behavior: tasks

Goal: make the main controls, project tree and file browser compact and easy to scan without sacrificing accessible names, focus visibility or touch usability. This milestone is planned after M7.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m8--interface-density-and-input-behavior) · checklist: [M8-acceptance.md](M8-acceptance.md).

## Rules for this milestone

- Work top to bottom. Before each task, read its U/I/E coverage in [M8-acceptance.md](M8-acceptance.md) and write those tests/scenarios with the behavior.
- Add or update E2E scenarios in the same task as each UI behavior change. Follow the E2E run policy active after M7; do not run the suite early if the M7 full-run checkpoint has not happened yet.
- During visual implementation, run the app and inspect desktop and phone screenshots of the affected screens before and after. Use those screenshots to tune spacing and verify that names are more prominent, controls are compact, and the file dialog uses the viewport efficiently. This inspection is part of implementation, not an owner check.
- Keep accessible names, visible focus, keyboard operation and touch usability. Do not shrink hit areas below the app's phone target requirements; use compact visual padding while retaining a usable target.
- Preserve the existing login screen's password autocomplete behavior exactly. Disable browser autocomplete/autofill on every other application input.
- Conventional commits, small and focused. No backend, schema, migration or environment changes are expected.

## T1 — Header action placement and compact controls

- Move New session and Browse files into the top app bar, immediately after the host name. Remove duplicate locations if present.
- Reduce excess inner padding in New session, Browse files and the left-bar collapse/expand controls by at least 50% from their current visual padding, with further reduction where the icon/text and focus treatment remain clear.
- Keep accessible names, tooltips, visible focus and responsive layout intact.

**Tests:** U: frontend component tests cover the controls' placement/order, action wiring, accessible names and compact visual padding classes; I: n/a (presentation-only, no server behavior); E: T1 header actions are reachable by keyboard and pointer on desktop and phone, and open the expected dialogs.

**E2E:** Add/update T1 *Header actions and compact controls* (desktop and phone): New session and Browse files appear after the host name, open their expected UI, and remain keyboard operable.

## T2 — Compact, name-first project tree

- Reduce excess horizontal gap between project reorder/drag affordance and project expand/collapse control, and reduce row padding/margins across project and session rows.
- Remove session chevrons when sessions have no expandable children; retain expand affordances only where a row actually expands (such as lazily loaded windows/panes).
- Make the session name the first and most prominent content in each session row; keep secondary actions available without crowding the name.
- Preserve reorder, collapse, row actions, keyboard tree navigation, focus visibility and phone touch targets.

**Tests:** U: Vitest covers row structure/affordances (no inert session chevron), name-first order, action availability and compact spacing classes; I: n/a (presentation-only); E: T2 *Compact tree* checks names, project controls, real reorder/collapse/open actions and keyboard access on desktop and phone.

**E2E:** Add T2 *Compact tree* (desktop and phone): verify project reorder/collapse controls work, session rows have no inert chevron, session names remain prominent, and selecting a session still opens it.

## T3 — Compact file browser and autocomplete policy

- Reduce the Browse files dialog's outer width/height and excessive inner padding so it no longer stretches its content across an unnecessarily large blank area. Keep it responsive and within the viewport.
- Reduce spacing between file/folder rows while preserving readable labels and usable item targets; retain breadcrumbs, navigation, create-folder and project/session actions.
- Set `autocomplete="off"` on all application inputs except the login-screen password input. Preserve that input's current attributes and browser behavior unchanged.

**Tests:** U: Vitest covers compact dialog sizing/spacing classes, item action wiring, and the autocomplete policy across rendered forms, including an assertion that login password attributes are unchanged; I: n/a (frontend-only behavior); E: T3 *Compact file browser* checks navigation, directory and action behavior with compact screenshots/bounds on desktop and phone; T3 *No browser autocomplete outside login password* checks form attributes and the unchanged login password behavior.

**E2E:** Add T3 *Compact file browser* (desktop and phone): screenshot/bounds confirm dialog fits the viewport compactly with close item spacing; navigate folders, create a folder and invoke its project/session actions. Add T3 *No browser autocomplete outside login password* (desktop): inspect every application input's autocomplete attributes and confirm login password markup/behavior remains unchanged.

## T4 — Drag to reorder open tabs

- Let the user drag a tab directly to a new position in the existing tab bar. Do not add a drag icon/handle or redesign tab styling.
- Keep the existing behavior where newly opened tabs append to the end. Reordering changes only tab order; it must not change which tab is active, close or reopen terminals, or detach their tmux clients.
- Persist the custom order through the existing per-account `layout` UI state, including reload and app restart. Keep close, activation, keyboard navigation and the compact phone tab switcher in sync with the same order.

**Tests:** U: T4 layout-store reorder operation preserves tab identities, active tab and pane state, appends newly opened tabs after a custom order, and serializes/restores order (Vitest); tab bar drag/drop updates order without activation or terminal remount (component test). I: n/a: persistence uses the existing M3 `ui_state/layout` API and deploy configuration, whose integration coverage already exists; no server contract changes. E: T4 *Custom tab order* drags tabs on desktop and phone, confirms the active tab/terminal attachments remain stable, and checks order after reload and app restart.

**E2E:** Add T4 *Custom tab order* (desktop and phone): open at least three sessions, drag a tab to a new position, verify the order and active terminal remain correct, reload and restart the app to verify the order persists; confirm no drag handle was added and tab styling remains unchanged.

## Done

- [ ] M8 acceptance criteria and their U/I/E coverage are complete.
- [ ] `make lint test` and `make gitleaks` are green; E2E scenarios pass under the post-M7 run policy.
- [ ] Desktop and phone screenshots were inspected during implementation; README/ARCHITECTURE updated only if user-facing behavior or design changes warrant it.
- [ ] Summary lists changes, any environment variables (expected none), host steps (expected none), and any open owner items.
