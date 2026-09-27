# M8 — Interface density and input behavior: tasks

Goal: make the main controls, project tree and file browser compact and easy to scan without sacrificing accessible names, focus visibility or touch usability. This milestone is planned after M7.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m8--interface-density-and-input-behavior) · checklist: [M8-acceptance.md](M8-acceptance.md).

## Progress

Update this table in the same commit that finishes a task. T2–T4 were done early, alongside M7, at the owner's request (2026-09-27); T1 and T5–T7 haven't started.

| Task | Status |
|---|---|
| T1 Header action placement and compact controls | Not started |
| T2 Compact, name-first project tree | Implemented; e2e written, not run yet |
| T3 Compact file browser and autocomplete policy | Implemented; e2e written, not run yet |
| T4 Drag to reorder open tabs | Implemented; e2e written, not run yet |
| T5 Reliable Option-click caret placement | Not started |
| T6 Readable terminal wheel scrolling | In progress (direct touch scrolling implemented; wheel readability work remains) |
| T7 Contrast in long-lived terminal clients | Not started |

**Progress note (T2–T4, 2026-09-27):** Vitest (447 tests), eslint, `vue-tsc` and the e2e `tsc` passed; deployed to the host from a clean checkout of `f664bb8` (a pre-deploy `pg_dump` is in `backups/`), and the stack came up healthy. Still open for these tasks:
- **E2E runs:** the scenarios *(T2) Compact tree*, *(T3) Compact file browser*, *(T3) No browser autocomplete outside login password* and *(T4) Custom tab order* type-check but haven't run: e2e stays paused until M7 T13. The T2 commit also updated two M6 scenarios that expanded single-window sessions (*(T4) Inline rename a session*, *(T3) Window rows follow the real terminal*).
- **Screenshot inspection:** not done yet. The only running app is production, and signing in there would need a new whitelisted account in the owner's database. The e2e scenarios save desktop and phone screenshots (`file-browser-*.png`, `tree-*.png`); inspect them at the first e2e run and tune spacing then.
- **Decisions to review:** the login screen's email field is now `autocomplete="off"` (the literal policy: only the password is exempt), and a single-window session has no chevron until its windows are loaded (the inventory reports window counts, not pane counts).

## Rules for this milestone

- Work top to bottom. Before each task, read its U/I/E coverage in [M8-acceptance.md](M8-acceptance.md) and write those tests/scenarios with the behavior.
- Add or update E2E scenarios in the same task as each UI behavior change. Follow the E2E run policy active after M7; do not run the suite early if the M7 full-run checkpoint has not happened yet.
- During visual implementation, run the app and inspect desktop and phone screenshots of the affected screens before and after. Use those screenshots to tune spacing and verify that names are more prominent, controls are compact, and the file dialog uses the viewport efficiently. This inspection is part of implementation, not an owner check.
- For the caret-placement issue, first reproduce it in the affected input, identify whether it is a single-line or multiline control, and verify the clicked caret position against the displayed text before and after the fix.
- For the scrolling issue, inspect a screen recording or repeated live captures of the running terminal while scrolling both directions through distinct and repeated output. Identify the cause before choosing a rendering, event handling or scroll-step adjustment; compare before/after at the same content and input gestures.
- For the terminal contrast issue, use the reported timeline: start a Codex client in a dark System theme, leave its session attached for an extended period, then switch the OS to light and inspect the prompt surface (including “Ask Codex to do anything”) and its text. Determine whether the colors come from xterm's palette or explicit colors chosen by the terminal client before selecting a fix. Do not assume Codex supports live theme changes.
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

## T5 — Reliable Option-click caret placement

- Fix the reported behavior where Option-clicking editable text causes the caret to jump to unrelated lines or positions. The caret must land at the character/line under the pointer in both single-line and multiline inputs.
- Preserve ordinary click, selection, typing, keyboard navigation, and the login password field's existing autocomplete behavior.
- Reproduce and inspect the actual affected input in the running app. Add regression coverage for positions near the beginning, middle and end of text and across multiple lines; do not claim this task done based only on a synthetic unit test.

**Tests:** U: T5 frontend test covers mapping the pointer location to the expected caret position across line boundaries and confirms ordinary click/selection remain unchanged (Vitest); I: n/a (browser-side input behavior, no server state); E: T5 *Option-click caret placement* (desktop): in representative single-line and multiline app text inputs, click at known characters/lines and assert `selectionStart`/caret line and column match the click, then type and verify insertion at that location.

**E2E:** Add T5 *Option-click caret placement* (desktop): exercise single-line and multiline inputs at several known positions with the platform's Option/Alt-click gesture, assert the caret location and inserted text, and verify ordinary click remains correct.

## T6 — Readable terminal wheel scrolling

- Improve the visual tracking of terminal content during mouse-wheel scrolling. The report is visual: scrolling currently moves in the correct direction and reaches the expected content, but characters can flicker and become difficult to follow, especially in repeated output.
- Reproduce the issue with both distinct lines and repeated lines, scrolling up and down at typical and faster wheel input. Inspect captures/recording before and after to identify and address the source; don't change scroll semantics or direction to hide the visual issue.
- Keep terminal input, copy-mode behavior, touch scrolling and the existing scrollback content intact. Avoid adding a setting or control unless implementation demonstrates it is necessary.
- On touch devices, a vertical swipe directly on the terminal enters copy mode and applies that swipe to scroll history; users don't need to press the Scroll history button first. Keep the button and its explicit controls available.

**Tests:** U: T6 frontend coverage for wheel-event handling/render updates or the selected scroll-step behavior (Vitest), including direction, bounded movement and no duplicated/skipped scroll requests; also cover direct touch swipe entering copy mode and applying the initial scroll; I: n/a if the fix stays in xterm/browser rendering (state the specific frontend-only reason in the test); E: T6 *Readable terminal scrolling* verifies distinct and repeated output while scrolling both directions, checks the expected text/position, and records visual comparison at the same wheel gestures; *Touch swipe scrolls history directly* verifies direct up/down gestures on a phone.

**E2E:** Add T6 *Readable terminal scrolling* (desktop): print deterministic numbered and repeated lines in the throwaway tmux target, wheel up/down at controlled and rapid intervals, assert movement/direction and visible text, and capture before/after views to compare flicker/readability. Add T6 *Touch swipe scrolls history directly* (phone): swipe up/down on the terminal, confirm copy mode starts and the same gesture scrolls the throwaway target's history. Do not use the real host's tmux.

## T7 — Contrast in long-lived terminal clients

- This is a follow-up to M6 T7 (Theme setting), prompted by a real Codex-in-tmux report; M6's completed theme-setting work remains recorded as done.
- Investigate the reported low-contrast Codex UI after a System theme switch: Codex starts in a dark hostbud/xterm theme at night, remains running for many hours, then the OS switches to light; the prompt area (reported as “Ask Codex to do anything”) can remain dark while its text is dark too.
- Separate hostbud-controlled xterm palette colors from colors explicitly rendered by Codex or another terminal client. Verify the theme change reaches an already-mounted terminal without detaching/restarting its tmux client. Improve palette or rendering behavior where hostbud controls the cause; when the client explicitly chooses colors hostbud cannot safely override, record that boundary and an actionable supported workaround instead of claiming the client updates live.
- Compare dark-to-light and light-to-dark changes with the same long-lived terminal, and inspect screenshots of the prompt surface and ordinary terminal text for readable contrast. Do not change user tmux configuration.

**Tests:** U: T7 tests palette updates and contrast for foreground/background pairs across Dark/Light/System changes, including a mounted terminal that stays attached (Vitest); I: n/a: color rendering and theme state are frontend-only and do not alter SSH/tmux state; E: T7 *Long-lived terminal contrast* changes the emulated OS theme with a running representative prompt-like TUI, asserts the terminal stays attached and hostbud palette changes, and captures the prompt/text colors in both themes. Real Codex-specific colors are recorded as a manual owner check if the e2e target cannot run Codex.

**E2E:** Add T7 *Long-lived terminal contrast* (desktop): run a deterministic prompt-like ANSI TUI on the throwaway target, leave the terminal mounted while switching System light/dark, assert palette and contrast tokens change without reconnecting, and capture both states. Never use the real host's tmux. Record the real Codex “Ask Codex to do anything” surface as a manual owner check if Codex itself is unavailable in the e2e target.

## Done

- [ ] M8 acceptance criteria and their U/I/E coverage are complete.
- [ ] `make lint test` and `make gitleaks` are green; E2E scenarios pass under the post-M7 run policy.
- [ ] Desktop and phone screenshots were inspected during implementation; README/ARCHITECTURE updated only if user-facing behavior or design changes warrant it. T5's affected inputs were reproduced and checked in the running app; T6's scroll behavior was recorded and compared before/after; T7's long-lived terminal theme change and contrast were inspected and the palette-vs-client color ownership was recorded.
- [ ] Summary lists changes, any environment variables (expected none), host steps (expected none), and any open owner items.
