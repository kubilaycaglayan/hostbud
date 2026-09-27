# M8 — Interface density and input behavior: tasks

Goal: make the main controls, project tree and file browser compact and easy to scan without sacrificing accessible names, focus visibility or touch usability. This milestone is planned after M7.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m8--interface-density-and-input-behavior) · checklist: [M8-acceptance.md](M8-acceptance.md).

## Progress

Update this table in the same commit that finishes a task. T2–T4 were done early, alongside M7, at the owner's request (2026-09-27); T1 and T5–T7 followed the same day, also ahead of M7's full e2e run.

| Task | Status |
|---|---|
| T1 Header action placement and compact controls | Implemented; e2e written, not run yet |
| T2 Compact, name-first project tree | Implemented; e2e written, not run yet |
| T3 Compact file browser and autocomplete policy | Implemented; e2e written, not run yet |
| T4 Drag to reorder open tabs | Implemented; e2e written, not run yet |
| T5 Reliable Option-click caret placement | Implemented; e2e written, not run yet |
| T6 Readable terminal wheel scrolling | Implemented; e2e written, not run yet |
| T7 Contrast in long-lived terminal clients | Implemented; e2e written, not run yet |

**Progress note (T2–T4, 2026-09-27):** Vitest (447 tests), eslint, `vue-tsc` and the e2e `tsc` passed; deployed to the host from a clean checkout of `f664bb8` (a pre-deploy `pg_dump` is in `backups/`), and the stack came up healthy. Still open for these tasks:
- **E2E runs:** the scenarios *(T2) Compact tree*, *(T3) Compact file browser*, *(T3) No browser autocomplete outside login password* and *(T4) Custom tab order* type-check but haven't run: e2e stays paused until M7 T13. The T2 commit also updated two M6 scenarios that expanded single-window sessions (*(T4) Inline rename a session*, *(T3) Window rows follow the real terminal*).
- **Screenshot inspection:** done 2026-09-27 (with T1) against a scratch build of the SPA served with mocked API responses in headless Chromium, desktop 1280×800 and iPhone 13 Pro, dark and light. The tree is name-first and compact, and the Browse files dialog fits the viewport with tight rows (44 px row actions on the phone, list scrolling inside the sheet). Cosmetic note for a later pass: on the phone the *Show hidden files* checkbox itself is 44 px square. The e2e scenarios still save screenshots for a look at the first real run.
- **Decisions to review:** the login screen's email field is now `autocomplete="off"` (the literal policy: only the password is exempt), and a single-window session has no chevron until its windows are loaded (the inventory reports window counts, not pane counts).

**Progress note (T1, T5–T7, 2026-09-27):** `make lint test` green (Go unit and integration against `test/sshd`, Vitest, shell checks, eslint, `vue-tsc`, e2e `tsc`, docs check); not deployed (M7 forbids `make deploy` before T14). Evidence per task:
- **T1:** New session and Browse files sit right after the host name. Measured 28 px buttons on desktop, and a 44 px hit area around a 28 px visual box on the phone (was 44 px with about 12 px padding). The keyboard focus ring was checked in screenshots.
- **T5:** the affected input is the terminal's line editor, not the app's `<input>`s (all single-line, native caret handling; there are no `<textarea>`s). The cause is in xterm 6's `altClickMovesCursor` (source read, and its arrow count reproduced in `altClick.spec.ts` against a bordered multiline prompt). A live Option-click check in a browser is part of the pending e2e run (*(T5) Option-click caret placement*: bash, soft-wrapped bash, and a deterministic multiline prompt on the throwaway target).
- **T6:** tmux's copy-mode output was recorded in a throwaway target container and replayed into xterm 6 in headless Chromium, with the rendered rows captured every frame. Before: 8–10 torn frames out of about 20 (tmux `mouse on`), and 3-row jumps per wheel notch (`mouse off`). After: 0 torn frames with `-T sync`, and 1 row per frame with `smoothScrollDuration` 100 ms. End position and direction were identical in all cases.
- **T7:** the colors are client-owned: Codex queries OSC 10/11 and paints its composer background for the theme it started in. With `minimumContrastRatio` 4.5 the before/after in xterm 6 went from `rgb(31,35,40)` to `rgb(150,151,155)` on the dark composer after switching to light. Real Codex through an OS switch stays an owner check.
- **Still open:** every M8 e2e scenario runs first at M7 T13. Owner check: the real Codex prompt through a dark-to-light switch.

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
- On touch devices, a vertical swipe directly scrolls xterm's browser-side scrollback as the finger moves. It must not enter tmux copy mode for this local history (which can be empty and show 0/0). Keep the Scroll history button available for tmux's older pane history.

**Tests:** U: T6 frontend coverage for wheel-event handling/render updates or the selected scroll-step behavior (Vitest), including direction, bounded movement and no duplicated/skipped scroll requests; also cover touch swipes scrolling the local xterm buffer without a copy-mode API call; I: n/a because this path uses xterm's browser-side buffer and makes no server or SSH request; E: T6 *Readable terminal scrolling* verifies distinct and repeated output while scrolling both directions, checks the expected text/position, and records visual comparison at the same wheel gestures; *Touch swipe scrolls history directly* verifies direct up/down gestures on a phone while tmux remains out of copy mode.

**E2E:** Add T6 *Readable terminal scrolling* (desktop): print deterministic numbered and repeated lines in the throwaway tmux target, wheel up/down at controlled and rapid intervals, assert movement/direction and visible text, and capture before/after views to compare flicker/readability. Add T6 *Touch swipe scrolls history directly* (phone): while numbered output streams through the attached terminal, swipe up/down, verify xterm's viewport moves in both directions and tmux stays out of copy mode. Do not use the real host's tmux.

## T7 — Contrast in long-lived terminal clients

- This is a follow-up to M6 T7 (Theme setting), prompted by a real Codex-in-tmux report; M6's completed theme-setting work remains recorded as done.
- Investigate the reported low-contrast Codex UI after a System theme switch: Codex starts in a dark hostbud/xterm theme at night, remains running for many hours, then the OS switches to light; the prompt area (reported as “Ask Codex to do anything”) can remain dark while its text is dark too.
- Separate hostbud-controlled xterm palette colors from colors explicitly rendered by Codex or another terminal client. Verify the theme change reaches an already-mounted terminal without detaching/restarting its tmux client. Improve palette or rendering behavior where hostbud controls the cause; when the client explicitly chooses colors hostbud cannot safely override, record that boundary and an actionable supported workaround instead of claiming the client updates live.
- Compare dark-to-light and light-to-dark changes with the same long-lived terminal, and inspect screenshots of the prompt surface and ordinary terminal text for readable contrast. Do not change user tmux configuration.

**Tests:** U: T7 tests palette updates and contrast for foreground/background pairs across Dark/Light/System changes, including a mounted terminal that stays attached (Vitest); I: n/a: color rendering and theme state are frontend-only and do not alter SSH/tmux state; E: T7 *Long-lived terminal contrast* changes the emulated OS theme with a running representative prompt-like TUI, asserts the terminal stays attached and hostbud palette changes, and captures the prompt/text colors in both themes. Real Codex-specific colors are recorded as a manual owner check if the e2e target cannot run Codex.

**E2E:** Add T7 *Long-lived terminal contrast* (desktop): run a deterministic prompt-like ANSI TUI on the throwaway target, leave the terminal mounted while switching System light/dark, assert palette and contrast tokens change without reconnecting, and capture both states. Never use the real host's tmux. Record the real Codex “Ask Codex to do anything” surface as a manual owner check if Codex itself is unavailable in the e2e target.

## T8 — Native touch scrolling, selection and dictation

- Let xterm's `.xterm-viewport` handle vertical touch scrolls with `touch-action: pan-y` and contained overscroll; prevent the document from becoming a competing scroll container. Touch up/down follows the finger and leaves tmux copy mode unchanged. Explicit Scroll history continues to use tmux.
- Make long-press word selection use the active buffer's absolute row and xterm cell columns, including when the viewport is scrolled into local history. Keep the Copy action tied to xterm's selection-change event.
- Prevent voice dictation/IME input corruption on xterm 6.0: after composition finalization sends its phrase, clear the hidden input on the next task so replacement-style dictation cannot replay or truncate stale text. Preserve the input when screen-reader mode is enabled.

**Tests:** U: Vitest verifies native-touch classes/no custom local scroll, buffer-row selection beyond row zero, and hidden textarea clearing after composition end. I: n/a (xterm touch, selection and input DOM are browser-side). E: T8 *Touch scroll stays inside the xterm viewport* · T8 *Touch long press selects terminal text for copying* · T8 *Dictation commits clean terminal input across successive phrases* (phone).

**E2E:** Write the three T8 phone scenarios above against the throwaway target. Dictation uses browser composition events to reproduce consecutive committed phrases; the owner checks actual iPhone dictation and copy. Don't run `make e2e` before M7 T13, and never use the real host's tmux.

## Done

- [ ] M8 acceptance criteria and their U/I/E coverage are complete.
- [ ] `make lint test` and `make gitleaks` are green; E2E scenarios pass under the post-M7 run policy.
- [ ] Desktop and phone screenshots were inspected during implementation; README/ARCHITECTURE updated only if user-facing behavior or design changes warrant it. T5's affected inputs were reproduced and checked in the running app; T6's scroll behavior was recorded and compared before/after; T7's long-lived terminal theme change and contrast were inspected and the palette-vs-client color ownership was recorded.
- [ ] Summary lists changes, any environment variables (expected none), host steps (expected none), and any open owner items.
