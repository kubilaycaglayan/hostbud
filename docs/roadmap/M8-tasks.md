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
| T8 Dictation editor, focus return and terminal text view | Implemented; e2e written and type-checked, run pending (on demand) |
| T9 Touch scrolling through tmux | Implemented; e2e written, not run yet |
| T12 Agent marks on session rows | Implemented; focused unit/integration pass; e2e written and type-checked, run pending (on demand) |
| T13 Provider hook status on session rows | In progress |

**Progress note (T2–T4, 2026-09-27):** Vitest (447 tests), eslint, `vue-tsc` and the e2e `tsc` passed; deployed to the host from a clean checkout of `f664bb8` (a pre-deploy `pg_dump` is in `backups/`), and the stack came up healthy. Still open for these tasks:
- **E2E runs:** the scenarios *(T2) Compact tree*, *(T3) Compact file browser*, *(T3) No browser autocomplete outside login password* and *(T4) Custom tab order* type-check but haven't run: e2e runs only on demand. The T2 commit also updated two M6 scenarios that expanded single-window sessions (*(T4) Inline rename a session*, *(T3) Window rows follow the real terminal*).
- **Screenshot inspection:** done 2026-09-27 (with T1) against a scratch build of the SPA served with mocked API responses in headless Chromium, desktop 1280×800 and iPhone 13 Pro, dark and light. The tree is name-first and compact, and the Browse files dialog fits the viewport with tight rows (44 px row actions on the phone, list scrolling inside the sheet). Cosmetic note for a later pass: on the phone the *Show hidden files* checkbox itself is 44 px square. The e2e scenarios still save screenshots for a look at the first real run.
- **Decisions to review:** the login screen's email field is now `autocomplete="off"` (the literal policy: only the password is exempt), and a single-window session has no chevron until its windows are loaded (the inventory reports window counts, not pane counts).

**Progress note (T1, T5–T7, 2026-09-27):** `make lint test` green (Go unit and integration against `test/sshd`, Vitest, shell checks, eslint, `vue-tsc`, e2e `tsc`, docs check); not deployed (M7 forbids `make deploy` before T14). Evidence per task:
- **T1:** New session and Browse files sit right after the host name. Measured 28 px buttons on desktop, and a 44 px hit area around a 28 px visual box on the phone (was 44 px with about 12 px padding). The keyboard focus ring was checked in screenshots.
- **T5:** the affected input is the terminal's line editor, not the app's `<input>`s (all single-line, native caret handling; there are no `<textarea>`s). The cause is in xterm 6's `altClickMovesCursor` (source read, and its arrow count reproduced in `altClick.spec.ts` against a bordered multiline prompt). A live Option-click check in a browser is part of the pending e2e run (*(T5) Option-click caret placement*: bash, soft-wrapped bash, and a deterministic multiline prompt on the throwaway target).

**Progress note (T8, 2026-09-27):** replaced the ineffective xterm hidden-input composition cleanup with a native dictation text editor that sends through xterm paste once. Added PWA background blur/focus guards, a three-dot toolbar menu, and a full-screen frozen scrollback text view; updated existing search/copy/show-keyboard/close/split e2e helpers for the menu. `make lint test` passes (470 Vitest tests, Go unit/integration, ESLint/type checks, docs checks, and e2e `tsc`); `make build` and `make gitleaks` pass. Per the user's explicit request, deployed after a fresh mode-600 backup passed `make restore-check`; the stack is healthy, loopback `/api/health` returns ok, CSP is present, and container hardening is intact. `make doctor` still reports the four open host setup checks recorded under M7 T11. The e2e scenarios are type-checked but their full run is on demand. The real iPhone system dictation check remains open owner backlog. No new env vars are needed.
- **T6:** tmux's copy-mode output was recorded in a throwaway target container and replayed into xterm 6 in headless Chromium, with the rendered rows captured every frame. Before: 8–10 torn frames out of about 20 (tmux `mouse on`), and 3-row jumps per wheel notch (`mouse off`). After: 0 torn frames with `-T sync`, and 1 row per frame with `smoothScrollDuration` 100 ms. End position and direction were identical in all cases.
- **T7:** the colors are client-owned: Codex queries OSC 10/11 and paints its composer background for the theme it started in. With `minimumContrastRatio` 4.5 the before/after in xterm 6 went from `rgb(31,35,40)` to `rgb(150,151,155)` on the dark composer after switching to light. Real Codex through an OS switch stays an owner check.
- **Still open:** every M8 e2e scenario runs first at an on-demand run. Owner check: the real Codex prompt through a dark-to-light switch.

## Rules for this milestone

- Work top to bottom. Before each task, read its U/I/E coverage in [M8-acceptance.md](M8-acceptance.md) and write those tests/scenarios with the behavior.
- Add or update E2E scenarios in the same task as each UI behavior change. E2E runs only on demand (AGENTS.md): type-check the suite per commit and never run it unless the owner asks.
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
- Keep terminal input, copy-mode behavior and the existing scrollback content intact. Avoid adding a setting or control unless implementation demonstrates it is necessary.

**Tests:** U: T6 frontend coverage for wheel-event handling/render updates or the selected scroll-step behavior (Vitest), including direction, bounded movement and no duplicated/skipped scroll requests; I: T6 `TestIntegrationAttachDeclaresSynchronizedOutput`; touch behavior is n/a because it stays in the browser; E: T6 *Readable terminal scrolling* verifies distinct and repeated output while scrolling both directions, checks expected text/position and records visual comparison at the same wheel gestures.

**E2E:** Add T6 *Readable terminal scrolling* (desktop): print deterministic numbered and repeated lines in the throwaway tmux target, wheel up/down at controlled and rapid intervals, assert movement/direction and visible text, and capture before/after views to compare flicker/readability.

## T7 — Contrast in long-lived terminal clients

- This is a follow-up to M6 T7 (Theme setting), prompted by a real Codex-in-tmux report; M6's completed theme-setting work remains recorded as done.
- Investigate the reported low-contrast Codex UI after a System theme switch: Codex starts in a dark hostbud/xterm theme at night, remains running for many hours, then the OS switches to light; the prompt area (reported as “Ask Codex to do anything”) can remain dark while its text is dark too.
- Separate hostbud-controlled xterm palette colors from colors explicitly rendered by Codex or another terminal client. Verify the theme change reaches an already-mounted terminal without detaching/restarting its tmux client. Improve palette or rendering behavior where hostbud controls the cause; when the client explicitly chooses colors hostbud cannot safely override, record that boundary and an actionable supported workaround instead of claiming the client updates live.
- Compare dark-to-light and light-to-dark changes with the same long-lived terminal, and inspect screenshots of the prompt surface and ordinary terminal text for readable contrast. Do not change user tmux configuration.

**Tests:** U: T7 tests palette updates and contrast for foreground/background pairs across Dark/Light/System changes, including a mounted terminal that stays attached (Vitest); I: n/a: color rendering and theme state are frontend-only and do not alter SSH/tmux state; E: T7 *Long-lived terminal contrast* changes the emulated OS theme with a running representative prompt-like TUI, asserts the terminal stays attached and hostbud palette changes, and captures the prompt/text colors in both themes. Real Codex-specific colors are recorded as a manual owner check if the e2e target cannot run Codex.

**E2E:** Add T7 *Long-lived terminal contrast* (desktop): run a deterministic prompt-like ANSI TUI on the throwaway target, leave the terminal mounted while switching System light/dark, assert palette and contrast tokens change without reconnecting, and capture both states. Never use the real host's tmux. Record the real Codex “Ask Codex to do anything” surface as a manual owner check if Codex itself is unavailable in the e2e target.

## T8 — Dictation editor, focus return and terminal text view

- Replace the ineffective xterm composition cleanup with a native text editor that supports dictation, Send and Cancel. Send the reviewed text once through xterm's `paste` method, preserving bracketed-paste handling.
- Blur active text fields when the PWA backgrounds. Switching tabs or panes never opens the keyboard; an explicit terminal tap or Show keyboard action focuses it.
- Replace the per-terminal toolbar action buttons with one three-dot menu, retaining the existing actions and adding View terminal text. Keep tab controls unchanged.
- View terminal text reads the active pane’s full retained tmux history through an authenticated, uncached read-only endpoint, including output before browser attachment and, under a full-screen (alternate-screen) app, the shell history and saved screen before the app's screen. Trim row padding and trailing blank rows. Render SGR styles as native selectable text, join soft wraps, wrap long lines within the view, and support free vertical scrolling/copy without changing tmux. Show loading and retryable errors; discard output when closed.

**Tests:** U: T8 Vitest covers dictation Send/Cancel and exactly-once paste, hidden-page blur, no automatic focus on tab switch, toolbar action dispatch, and frozen wrapped scrollback text; I: T8 `TestIntegrationOutputIncludesHistoryWithoutAttaching` captures styled pre-attachment history and joined long rows against `test/sshd`, without attaching or entering copy mode; E: T8 *Dictation editor sends reviewed text once*, *Returning from background does not automatically refocus the terminal*, *Switching tabs does not automatically open the terminal keyboard*, *Terminal text snapshot scrolls, selects, and closes*, and *Terminal text includes shell history under a full-screen app*. Real iPhone system dictation remains an owner manual check.

**E2E:** Add the four T8 scenarios above, including explicit Show keyboard after the hidden/visible transition and tab switch, plus pre-attachment history, styling, no horizontal overflow at phone width, native scroll/selection/close,. Type-check only; runs on demand.

## T9 — Touch scrolling through tmux

- On touch screens, a vertical swipe over the terminal scrolls like a mouse wheel through the copy-mode API's `wheel-up`/`wheel-down` actions, following tmux's default wheel rule: a mouse-aware app outside a mode (Claude Code, Codex) receives wheel reports written into the pane (`send-keys -H`, tmux ≥ 3.1; one report per 3 lines, at most 20 per request); otherwise tmux copy mode scrolls its full history, including output before the browser attached. The user's tmux `mouse` option is neither needed nor changed.
- Hide xterm's local scrollbar on touch screens: xterm only holds what reached the browser since attaching, with gaps where tmux or a full-screen app redrew in place.
- Experimental momentum: a flick coasts with friction (325 ms time constant), same-direction flicks stack up to a speed cap, and a touch stops it. Slow drags stay 1:1.

**Tests:** U: T9 `touchScroll.spec.ts` (swipe to lines, direction, threshold, multi-finger, momentum, stacking, speed cap), `copyMode.spec.ts` wheel actions, `TerminalView.spec.ts` swipe sends `wheel-up`; Go `TestWheelState`, `TestAppWheelArgs`, `TestCopyModeArgs` wheel cases, `TestCopyModeWheelFollowsTmuxWheelRule`, `TestCopyModeAPIValidationAndAccess` wheel cases. I: T9 `TestIntegrationWheelScrollsAppOrTmuxHistory` (`test/sshd`): in a shell the wheel enters copy mode and scrolls; a mouse-reporting app receives SGR wheel reports and the pane stays out of copy mode. E: T9 *Touch swipe scrolls the full tmux history* and *Touch swipe scrolls a mouse-aware full-screen app* (phone projects).

**E2E:** Add the two T9 phone scenarios above on the throwaway target, never the real host's tmux. Type-check only; runs on demand. Momentum feel is a manual owner check on the iPhone PWA.

## T12 — Agent marks on session rows

- Show tiny colored Codex and Claude Code logos before the session name in the left gutter. A mark appears when any pane reports a recognized foreground command (`codex`, `claude` or `claude-code`); show both in stable order when a session has both. Keep logos out of terminal tabs and other session lists.
- Add only recognized agent names to session metadata by reading pane foreground commands during the regular inventory poll. Discard other command names and all arguments. Detection is best-effort and must not change tmux configuration.
- Include the metadata in session snapshots/events so the mark appears on collapsed session rows without fetching window/pane details. Re-evaluate it on each poll and publish changes when marks appear or disappear.

**Tests:** U: T12 Go parsing/inventory tests cover canonical command names, the owner's `coy`/`cly` aliases, recognized agent processes behind `node`, unknown commands and agent appearance/disappearance; Vitest covers small colored logos before the name, stable order, accessible labeling and left-gutter-only display. I: T12 `TestIntegrationPollerReportsForegroundAgentCommand` covers the `coy` alias and disappearance; `TestIntegrationPollerFindsCodexProcessBehindNodeForeground` covers the Codex process behind a `node` foreground command on `test/sshd`. E: T12 *Agent logos appear on collapsed session rows* (desktop and iPhone 13 Pro), using a fake Codex process behind `node` and fake `cly` on the throwaway target.

**E2E:** Add T12 *Agent logos appear on collapsed session rows* (desktop and iPhone 13 Pro): run a fake Codex process with `node` as the foreground command and a fake `cly` process in separate panes, verify both compact colored marks appear on the session row without expanding it, and verify an ordinary shell session has no mark. Type-check only; runs on demand.

## T13 — Provider hook status on session rows

- Ship one Python 3 hook handler for Codex and Claude Code. Document user-wide target-host installation by merging handlers into `~/.codex/hooks.json` and `~/.claude/settings.json`; do not overwrite existing hook settings. Hooks write a fixed status value to a pane-scoped tmux user option, and events outside tmux are ignored.
- Map prompt/tool activity to `working`, approval/turn-end/idle/interruption events to `blocked`, and provider session end to `ended`. Inventory reads pane status during the normal pane metadata poll and publishes aggregated status with session snapshots/events. If a known interactive shell resumes while an active status remains, report ended to cover common forced exits.
- Show agent logos first, then 🟢, 🚧 or 🎯, then the session name in the collapsed left gutter row; preserve actual names and accessible labels. Aggregate one status per session by priority: blocked, working, ended. Omit the marker until a hook reports a known state.

**Tests:** U: T13 Python hook mapping/target validation, Go pane metadata parsing and session status aggregation/change events, Vitest agent-logo/status order, accessible status text, unchanged session name and gutter-only rendering, and service-worker network-first shell refresh with offline fallback. I: T13 `TestIntegrationPollerReportsProviderHookStatus` sets pane options on `test/sshd`, checks working/blocked states and ended inference after the foreground process exits. E: T13 *Agent logos appear before provider hook status on collapsed session rows* (desktop and iPhone 13 Pro), covering all three marks, multi-pane aggregation, and status persistence across an ordinary refresh with a stale cached shell.

**E2E:** Add T12 *Agent logos appear on collapsed session rows* (desktop and iPhone 13 Pro): run fake `codex` and `claude` foreground commands in separate panes, verify both compact colored marks appear on the session row without expanding it, and verify an ordinary shell session has no mark. Type-check only; runs on demand.

## Done

- [ ] M8 acceptance criteria and their U/I/E coverage are complete.
- [ ] `make lint test` and `make gitleaks` are green; E2E scenarios pass under the post-M7 run policy.
- [ ] Desktop and phone screenshots were inspected during implementation; README/ARCHITECTURE updated only if user-facing behavior or design changes warrant it. T5's affected inputs were reproduced and checked in the running app; T6's scroll behavior was recorded and compared before/after; T7's long-lived terminal theme change and contrast were inspected and the palette-vs-client color ownership was recorded.
- [ ] Summary lists changes, any environment variables (expected none), host steps (expected none), and any open owner items.
