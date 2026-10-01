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
| T4 Drag to reorder open tabs | Superseded by T22 after tab strips were removed |
| T5 Reliable Option-click caret placement | Implemented; e2e written, not run yet |
| T6 Readable terminal wheel scrolling | Implemented; e2e written, not run yet |
| T7 Contrast in long-lived terminal clients | Implemented; e2e written, not run yet |
| T8 Dictation editor, focus return and terminal text view | Implemented; e2e written and type-checked, run pending (on demand) |
| T9 Touch scrolling through tmux | Implemented; e2e written, not run yet |
| T12 Agent marks on session rows | Implemented; focused unit/integration pass; e2e written and type-checked, run pending (on demand) |
| T13 Provider hook status on session rows | Implemented; focused Go unit/integration pass, E2E scenario written and type-checked, browser run pending on demand |
| T14 Solarized and Dimmed theme levels | Implemented; E2E written and type-checked, run pending (on demand) |
| T15 Selected-session clarity and list decluttering | Implemented; U/contrast pass, E written and type-checked, screenshots inspected; browser run pending (on demand) |
| T16 Dialog and compact gutter stacking | Implemented; U passes, E written and type-checked, browser run pending (on demand) |
| T17 Colored project sections | Implemented; U passes; E written and type-checked, browser run pending on demand |
| T18 Reorderable project sections and anchored gutter action | Implemented; U passes; E written and type-checked, browser run pending on demand |
| T19 Selected session highlights its collapsed parent project | Implemented; U passes; E written and type-checked, browser run pending on demand |
| T20 Collapsible sections and selected-content marker | Implemented; U passes; E written and type-checked, browser run pending on demand |
| T21 Background terminal resize isolation | Implemented; U passes; E written and type-checked, browser run pending on demand |
| T22 Session switching without tab strips | Implemented; U written; E written and type-checked, browser run pending on demand |
| T23 Semantic command palette groups | Implemented; U written; E written and type-checked, browser run pending on demand |
| T24 Full-width session names with hover actions | Implemented; U written; E written and type-checked, browser run pending on demand |
| T25 Full-width project names and terminal session context | Implemented; U written; E written and type-checked, browser run pending on demand |
| T26 Remove project counts and navigate a three-column command palette | Implemented; U written; E written and type-checked, browser run pending on demand |
| T27 Restore terminal cursor focus on browser-tab return and page load | Implemented; U passes; E updated and type-checked, browser run pending on demand |

**Progress note (T2–T4, 2026-09-27):** Vitest (447 tests), eslint, `vue-tsc` and the e2e `tsc` passed; deployed to the host from a clean checkout of `f664bb8` (a pre-deploy `pg_dump` is in `backups/`), and the stack came up healthy. Still open for these tasks:
- **E2E runs:** the scenarios *(T2) Compact tree*, *(T3) Compact file browser* and *(T3) No browser autocomplete outside login password* type-check but haven't run: e2e runs only on demand. The T2 commit also updated two M6 scenarios that expanded single-window sessions (*(T4) Inline rename a session*, *(T3) Window rows follow the real terminal*). T22 replaces the former M8 *(T4) Custom tab order* scenario.
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
- Blur active text fields when the PWA backgrounds. Returning to the browser tab restores focus to the active terminal cursor; switching hostbud's internal tabs or panes does not open the keyboard. An explicit terminal tap or Show keyboard action focuses it.
- Replace the per-terminal toolbar action buttons with one three-dot menu, retaining the existing actions and adding View terminal text.
- View terminal text reads the active pane’s full retained tmux history through an authenticated, uncached read-only endpoint, including output before browser attachment and, under a full-screen (alternate-screen) app, the shell history and saved screen before the app's screen. Trim row padding and trailing blank rows. Render SGR styles as native selectable text, join soft wraps, wrap long lines within the view, and support free vertical scrolling/copy without changing tmux. Show loading and retryable errors; discard output when closed.

**Tests:** U: T8 Vitest covers dictation Send/Cancel and exactly-once paste, hidden-page blur, internal tab/pane no-focus behavior, toolbar action dispatch, and frozen wrapped scrollback text; I: T8 `TestIntegrationOutputIncludesHistoryWithoutAttaching` captures styled pre-attachment history and joined long rows against `test/sshd`, without attaching or entering copy mode; E: T8 *Dictation editor sends reviewed text once*, *Switching tabs does not automatically open the terminal keyboard*, *Terminal text snapshot scrolls, selects, and closes*, and *Terminal text includes shell history under a full-screen app*; T27 owns browser visibility-return focus coverage. Real iPhone system dictation remains an owner manual check.

**E2E:** Add the four T8 scenarios above, including explicit Show keyboard after the hidden/visible transition and tab switch, plus pre-attachment history, styling, no horizontal overflow at phone width, native scroll/selection/close,. Type-check only; runs on demand.

## T9 — Touch scrolling through tmux

- On touch screens, a vertical swipe over the terminal scrolls like a mouse wheel through the copy-mode API's `wheel-up`/`wheel-down` actions, following tmux's default wheel rule: a mouse-aware app outside a mode (Claude Code, Codex) receives wheel reports written into the pane (`send-keys -H`, tmux ≥ 3.1; one report per 3 lines, at most 20 per request); otherwise tmux copy mode scrolls its full history, including output before the browser attached. The user's tmux `mouse` option is neither needed nor changed.
- Hide xterm's local scrollbar on touch screens: xterm only holds what reached the browser since attaching, with gaps where tmux or a full-screen app redrew in place.
- Experimental momentum: a flick coasts with friction (325 ms time constant), same-direction flicks stack up to a speed cap, and a touch stops it. Slow drags stay 1:1.

**Tests:** U: T9 `touchScroll.spec.ts` (swipe to lines, direction, threshold, multi-finger, momentum, stacking, speed cap), `copyMode.spec.ts` wheel actions, `TerminalView.spec.ts` swipe sends `wheel-up`; Go `TestWheelState`, `TestAppWheelArgs`, `TestCopyModeArgs` wheel cases, `TestCopyModeWheelFollowsTmuxWheelRule`, `TestCopyModeAPIValidationAndAccess` wheel cases. I: T9 `TestIntegrationWheelScrollsAppOrTmuxHistory` (`test/sshd`): in a shell the wheel enters copy mode and scrolls; a mouse-reporting app receives SGR wheel reports and the pane stays out of copy mode. E: T9 *Touch swipe scrolls the full tmux history* and *Touch swipe scrolls a mouse-aware full-screen app* (phone projects).

**E2E:** Add the two T9 phone scenarios above on the throwaway target, never the real host's tmux. Type-check only; runs on demand. Momentum feel is a manual owner check on the iPhone PWA.

## T10 — Send original photos to the active session repo

- Add **Send photos to this repo** to the terminal's existing three-dot menu. The iPhone PWA can select one or more Photos-library items, including HEIC/HEIF and DNG when exposed by iOS, and send them to the active session's recorded repository path.
- When Cmd-V or Ctrl-Shift-V receives image file data from the clipboard, intercept the browser paste event and upload those original image files directly to the active session repo. Plain text paste must continue through xterm unchanged; clipboard files that are not images are ignored.
- Pass each selected browser `File` as a raw `application/octet-stream` request body. Do not decode, resize, convert, recompress or otherwise alter its bytes. The server writes through the existing SFTP subsystem to a unique temporary sibling, checks the exact byte count, and renames it into place only when complete. On a filename conflict, retry as `name-1.ext`, `name-2.ext`, etc.; never overwrite an existing file.
- Cap a file at 100 MiB and bound transfers with `HOSTBUD_UPLOAD_TIMEOUT` (5 min default, 30 s–10 min), distinct from the short metadata/listing timeout. Show actionable size, timeout and transfer errors in the dialog; report the actual destination and byte count on success. After upload, paste the file path relative to the session's recorded directory at the active terminal cursor.
- **E2E policy:** write the desktop and phone scenarios now and type-check only; `make e2e` runs only when the owner asks.

**Tests:** U: T10 tests the terminal menu dispatch/destination, raw `File` identity and bytes, numbered conflict retry, path insertion after upload, photo selection and partial-upload retry, clipboard image extraction and upload routing, ordinary text paste behavior, and API destination/content/collision/size handling (Vitest/Go); I: T10 `TestIntegrationSFTPUploadPreservesBytesWithoutOverwriting` transfers an opaque binary fixture through `test/sshd` and verifies its SHA-256 and collision behavior, while `TestIntegrationSFTPConcurrentUploadsDoNotOverwriteExistingName` verifies simultaneous same-name uploads cannot replace the winning file; E: T10 *Send original photo bytes to the active session repo* on desktop and iPhone 13 Pro, including Cmd-V image paste, actual relative path insertion, and ordinary text paste.

**E2E:** Add T10 *Send original photo bytes to the active session repo* (desktop and phone): create a linked project with the active session inside a nested folder, select a file named and typed as a HEIC photo with non-UTF8 bytes through the three-dot menu, verify the dialog targets the project root, then compare the target file size and SHA-256 with the exact selected fixture. Paste an image file with Cmd-V/Ctrl-Shift-V and verify its SHA-256 in the same repo. Upload a same-name photo and verify it is saved under the next numbered filename, the original remains unchanged, and its correct path relative to the nested session directory is pasted into tmux. Existing text-paste E2E coverage continues to verify terminal input.

## T11 — Choose split position before session

- Keep the terminal's top-right three-dot menu a fixed size as the number of live sessions grows. Replace the per-session split entries with a **Split pane…** action, then ask whether to split right or down, and only then show the available sessions plus **New session…**.
- Preserve the selected split direction and session in the existing split event/layout flow. Keep the session list menu's **Open in split right/down** actions unchanged.

**Tests:** U: T11 `TerminalView.spec.ts` covers the fixed-size first step, direction selection, session list and new-session path; I: n/a because this changes only frontend menu flow and uses the existing split API/layout behavior; E: T11 *Terminal menu chooses split position before listing sessions* on desktop, verifies no session fan-out before direction choice, then performs a real split with the selected direction/session.

**E2E:** Add T11 *Terminal menu chooses split position before listing sessions* to `test/e2e/tests/splits.spec.ts`: create several throwaway sessions, open the pane menu, verify session names are absent until a split position is chosen, select down and a specific session, then verify pane geometry and target attachment. Type-check only; runs on demand.

## T12 — Agent marks on session rows

- Show tiny colored Codex and Claude Code logos before the session name in the left gutter. A mark appears when any pane reports a recognized foreground command (`codex`, `coy`, `claude`, `cly` or `claude-code`); show both in stable order when a session has both. Show the correct mark for the owner's `coy` and `cly` launch aliases. Keep logos out of terminal tabs and other session lists.
- Add only recognized agent names to session metadata by reading pane foreground commands and recognized agent process names on the same pane TTY during the regular inventory poll. This covers Codex launchers whose foreground command is `node`. Discard other process names and all arguments. Detection is best-effort and must not change tmux configuration.
- Include the metadata in session snapshots/events so the mark appears on collapsed session rows without fetching window/pane details. Re-evaluate it on each poll and publish changes when marks appear or disappear.

**Tests:** U: T12 Go parsing/inventory tests cover canonical command names, the owner's `coy`/`cly` aliases, recognized agent processes behind `node`, unknown commands and agent appearance/disappearance; Vitest covers small colored logos before the name, stable order, accessible labeling and left-gutter-only display. I: T12 `TestIntegrationPollerReportsForegroundAgentCommand` covers the `coy` alias and disappearance; `TestIntegrationPollerFindsCodexProcessBehindNodeForeground` covers the Codex process behind a `node` foreground command on `test/sshd`. E: T12 *Agent logos appear on collapsed session rows* (desktop and iPhone 13 Pro), using a fake Codex process behind `node` and fake `cly` on the throwaway target.

**E2E:** Add T12 *Agent logos appear on collapsed session rows* (desktop and iPhone 13 Pro): run a fake Codex process with `node` as the foreground command and a fake `cly` process in separate panes, verify both compact colored marks appear on the session row without expanding it, and verify an ordinary shell session has no mark. Type-check only; runs on demand.

## T13 — Provider hook status on session rows

- Ship one Python 3 hook handler for Codex and Claude Code. Document user-wide target-host installation by merging handlers into `~/.codex/hooks.json` and `~/.claude/settings.json`; do not overwrite existing hook settings. Hooks write a fixed status value to a pane-scoped tmux user option, and events outside tmux are ignored.
- Map prompt/tool activity to `working`, approval/turn-end/idle/interruption events to `blocked`, and provider session end to `ended`. Inventory reads pane status during the normal pane metadata poll and publishes aggregated status with session snapshots/events. If a known interactive shell resumes while an active status remains, report ended to cover common forced exits. Unit and integration tests cover stale Codex `ended` recovery while its process is still live.
- Clear a cached `ended` marker when the same pane still has a recognized live Codex process. Codex can keep its terminal process open while stopping, starting or resuming a conversation, so an old end event must not keep 🎯 visible over the live client; fresh hook events provide the current working or waiting state.
- Show agent logos first, then 🟢, 🚧 or 🎯, then the session name in the collapsed left gutter row; preserve actual names and accessible labels. Aggregate one status per session by priority: blocked, working, ended. Omit the marker until a hook reports a known state.

**Tests:** U: T13 Python hook mapping/target validation, Go pane metadata parsing and session status aggregation/change events, Vitest agent-logo/status order, accessible status text, unchanged session name and gutter-only rendering, and service-worker network-first shell refresh with offline fallback. I: T13 `TestIntegrationPollerReportsProviderHookStatus` sets pane options on `test/sshd`, checks working/blocked states and ended inference after the foreground process exits. E: T13 *Agent logos appear before provider hook status on collapsed session rows* (desktop and iPhone 13 Pro), covering all three marks, multi-pane aggregation, and status persistence across an ordinary refresh with a stale cached shell.

**E2E:** Add T13 *Agent logos appear before provider hook status on collapsed session rows* to `test/e2e/tests/agent.status.spec.ts`; drive fixed pane options on the throwaway target, assert agent logos precede status, status changes and priority without expanding the row, verify the session name remains unchanged, and ensure an ordinary refresh replaces a stale cached app shell while retaining the status. Type-check only; runs on demand.

## T14 — Solarized and Dimmed theme levels

- Place **Solarized** at 25% darkness and add **Dimmed** at 70% on the requested 0 (Light) to 100 (Dark) scale. Both use Solarized-inspired colors, readable interface and xterm palettes, and independent per-account selections applied before first paint; preserve Dark, Light and System behavior.
- Expose both choices in Account controls and the command palette. Update status-bar color, search decorations and terminal text snapshots for both palettes.

**Tests:** U: T14 validates/persists both modes, first-paint mirrors, app and terminal palettes, snapshot palette, search decoration and contrast (Vitest); I: n/a because selection and rendering use existing UI-state persistence with no server behavior change; E: T14 *Solarized and Dimmed apply at their darkness levels and persist* (desktop, including open terminal and status-bar color).

**E2E:** Add T14 *Solarized and Dimmed apply at their darkness levels and persist* in `test/e2e/tests/theme.spec.ts`; assert both page surfaces, matching terminal backgrounds, status-bar metadata, saved account state, and persistence after reload and app restart. Type-check only; runs on demand.

## T15 — Selected-session clarity and list decluttering

- Make the selected session row and active terminal tab clearly distinct from unselected entries in Dark, Light, Solarized and Dimmed themes, with accessible foreground/background contrast.
- Let clicks on any non-control area of a session row select that session; preserve button actions, tab close behavior, drag handles and double-click rename.
- Remove session activity-age labels and green attachment dots from project headers and session lists.
- Keep project rows free of session counts.

**Tests:** U: T15 `SessionList.spec.ts` covers non-control row selection, button isolation, selected-row styling, and absent age/attachment indicators; `SessionTree.spec.ts` covers the absence of project session counts; `check-theme-contrast.test.mjs` checks selected foreground/background contrast for every theme. I: n/a; presentation and browser click handling only, no server contract changes. E: T15 *Selected session stands out across themes* (desktop) and *Whole session row selects on touch* (iPhone 13 Pro), including action-button isolation, no project session counts, and removed activity/dot indicators.

**E2E:** Add T15 *Selected session stands out across themes* to `test/e2e/tests/theme.spec.ts` and *Whole session row selects on touch* to `test/e2e/tests/tree.custom.phone.spec.ts`. Type-check only; runs on demand.

## T16 — Dialog and compact gutter stacking

- Keep the compact project-tree drawer below application dialogs in the shared portal stacking order, so opening a dialog while the gutter is expanded leaves the dialog visible and usable.

**Tests:** U: `App.spec.ts` asserts the compact drawer backdrop and panel layers remain below application dialogs; I: n/a (frontend stacking only); E: T16 *Dialogs stay above the reopened tree gutter* opens the Command palette while the drawer is present on iPhone 13 Pro and verifies the dialog's computed layer and search input usability.

**E2E:** Add T16 *Dialogs stay above the reopened tree gutter* (iPhone 13 Pro): open the drawer, invoke the Command palette through its keyboard shortcut, and assert the dialog stays above the drawer and its search input is usable. Type-check only; run on demand.

## T17 — Colored project sections

- Add per-account named project sections, stored with the existing tree UI state. Each section is created empty and has one of six lightly accented colors: red, green, blue, yellow, orange or purple.
- Add a compact create-section button at the bottom of the left gutter. The editor can name, recolor, rename and delete a section. Deleting a section returns its projects to the regular project list without deleting projects or sessions.
- Let users assign or move a project through its existing more-actions menu. Draw a thin tinted border around each section's projects with no added tree indentation or gutter width. Keep project/session interactions, pinning, ordering, hidden rows and touch targets usable.
- Persist section definitions and project membership in the per-account `tree` UI state, upgrading versions 1 and 2 without changing the saved project/session order. No backend, migration or environment change.

**Tests:** U: T17 `tree.spec.ts` validates/migrates section state and colors; `SessionTree.spec.ts` covers empty creation, color/name edits, assignment, deletion/unassignment, and the unchanged project-row indentation. I: n/a because the feature uses the existing authenticated `ui_state/tree` endpoint; M3 UI-state integration coverage verifies that persistence contract. E: T17 *Project sections* on desktop and iPhone 13 Pro verifies creation, color/name edits, assignment, gutter bounds/indentation, and persistence after reload/restart.

**E2E:** Add T17 *Project sections* to `test/e2e/tests/tree.sections.spec.ts`; type-check only and leave browser execution on demand.

## T18 — Reorderable project sections and anchored gutter action

- Let users drag sections into a custom order with a dedicated, accessible touch-sized handle; save their order with the existing per-account tree state.
- Keep the create-section action at the bottom of the left gutter while the project tree scrolls. Keep a 4px vertical gap between sections, a 1px horizontal gap to the enclosing tree, and no inner right padding; project rows stay aligned with the existing tree level.
- Close the project actions menu immediately after moving a project into or out of a section.

**Tests:** U: T18 `tree.spec.ts` and `stores/tree.spec.ts` verify section ordering and retained membership; `SessionTree.spec.ts` verifies the sortable list, 4px section gap, the far-right drag handle after the three-dot menu, footer anchoring classes, and menu dismissal after assignment. I: n/a because this uses the existing per-account tree UI-state endpoint and browser layout. E: T18 updates *Project sections and ordering* in `test/e2e/tests/tree.sections.spec.ts` to check menu/handle order, drag sections, verify the 4px gap and persisted order, and check the create action is outside the scrolling tree at the gutter bottom.

**E2E:** Add T18 ordering and anchored-footer assertions to `test/e2e/tests/tree.sections.spec.ts`; type-check only and leave browser execution on demand.

## T19 — Selected session highlights its collapsed parent project

- When the selected session belongs to a collapsed project, show the selected treatment on that parent project row and expose the proxy selection accessibly. Keep the session selected in the layout; do not expand or otherwise change the project tree just to show the marker.

**Tests:** U: T19 `SessionTree.spec.ts` verifies the parent project marker follows the selected session when collapsed, and clears/moves when selection changes; I: n/a because this derives from the existing selected-session prop and tree state; E: T19 *Selected content stays marked when its project or section collapses* (desktop and iPhone 13 Pro) selects sessions across projects, collapses their parent, and checks visible and accessible selection.

**E2E:** Add T19 *Selected content stays marked when its project or section collapses* to `test/e2e/tests/tree.sections.spec.ts`; type-check only and leave browser execution on demand.

## T20 — Collapsible sections and selected-content marker

- Let users click a section's color dot to collapse or expand that section's projects. Persist the collapsed state per account through reload/restart, including a safe tree-state version upgrade that preserves earlier saved order and section membership.
- If a selected session is inside a collapsed section, show the selection treatment on the section shell and expose it accessibly, without changing the selected session or automatically expanding the section.

**Tests:** U: T20 `tree.spec.ts` covers v3-to-v4 migration and `SessionTree.spec.ts` covers color-dot toggle and the selected marker; I: n/a because collapse and selection derive from per-account tree UI state and the selected-session prop; E: T20 *Selected content stays marked when its project or section collapses* (desktop and iPhone 13 Pro) toggles a section, verifies hidden rows and selected marker, then verifies collapse state after reload/restart.

**E2E:** Add T20 assertions to *Selected content stays marked when its project or section collapses* in `test/e2e/tests/tree.sections.spec.ts`; type-check only and leave browser execution on demand.

## T21 — Background terminal resize isolation

- Detach a terminal while its hostbud tab is inactive or its browser page is hidden, so stale tmux clients cannot constrain the shared session size. Refit before reattaching when active/visible; preserve the tmux session and running process.

**Tests:** U: T21 `TerminalView.spec.ts` verifies hidden-page and inactive-tab clients detach, remain detached if selected while the page stays hidden, then refit and reconnect at the current dimensions without focusing xterm. I: n/a because this is frontend attachment lifecycle; existing M1 PTY-to-tmux resize integration coverage remains applicable. E: T21 two-page same-session scenario verifies the background client detaches, cannot constrain the visible client, and reattaches at its current size on return.

**E2E:** Add T21 *Background window detaches its terminal until it is visible* to `test/e2e/tests/terminal.spec.ts`; type-check only and leave browser execution on demand.

## T27 — Restore active terminal focus on browser-tab return and page load

- When the user returns to hostbud from another browser tab or loads/refreshes the page with a saved terminal layout, refit and reconnect the active terminal, then focus its xterm input so typing resumes at the active session cursor. Inactive panes stay unfocused. Explicit backgrounding still blurs the field.

**Tests:** U: T27 `TerminalView.spec.ts` verifies initial-load focus only reaches the active pane and focus is restored when the page becomes visible, while active/inactive changes during a hidden page do not focus. I: n/a because this is browser focus behavior. E: T27 extends *Background window detaches its terminal until it is visible* to assert focus after browser-tab return and after reloading the saved terminal layout.

**E2E:** Update T21 *Background window detaches its terminal until it is visible* in `test/e2e/tests/terminal.spec.ts` with T27's active-input focus assertion and saved-layout reload check; remove the obsolete T8 no-refocus scenario from `test/e2e/tests/dictation.spec.ts`. Type-check only.

## T22 — Session switching without tab strips

- Remove desktop and compact-phone tab bars while preserving the saved multi-layout and split-pane model. Keep inactive views mounted and detached per T21.
- Make Ctrl+Shift+] / Ctrl+Shift+[ cycle only open sessions with currently visible tree rows, in rendered tree order. Skip hidden sessions and descendants of collapsed projects or sections; wrap around and focus the matching pane. When fewer than two open sessions are visible, do nothing.
- Rename the command-palette action to **Close terminal view**. It removes the active layout while the tmux session keeps running. Keep Ctrl+Shift+D as switch to the most recently selected session.

**Tests:** U: T22 `tree.spec.ts` covers rendered grouping order, open-only filtering, hidden sessions and collapsed projects/sections; palette and shortcut specs cover the session-oriented labels. I: n/a because no server behavior or contract changes. E: T22 desktop and phone scenarios verify no tab strip and keyboard switching; desktop also checks collapsed rows, unopened sessions and palette close preserving the tmux session.

**E2E:** Replace the obsolete T4 *Custom tab order* scenario in `test/e2e/tests/tabs.order.spec.ts` with T22 *Visible open-session shortcuts replace tab strips on desktop* and *Phone terminal has no tab strip and session cycling still works*. Type-check only; browser runs remain on demand.

## T23 — Semantic command palette groups

- Split palette results into Sessions, Windows, Projects, Create, Open, Organize, Terminal, Appearance, Account and Destructive groups.
- Assign each command to the group that reflects its purpose. Keep destructive session/project actions together and visually marked as destructive.
- Color-code headings and result rows with theme-aware semantic colors while preserving readable text and the existing focus indicator.

**Tests:** U: T23 `palette.spec.ts` covers command-to-group assignments and `CommandPalette.spec.ts` covers rendered group membership and destructive styling; I: n/a because grouping and styling are frontend-only; E: T23 *Command palette groups commands by purpose and color* checks representative commands and semantic group colors with a live session.

**E2E:** Add T23 *Command palette groups commands and navigates across columns with counts* to `test/e2e/tests/palette.spec.ts`; type-check only and leave browser execution on demand. T26 updates the same scenario.

## T24 — Full-width session names with hover actions

- Let session names use the full available row width on desktop; place row actions over the trailing edge instead of reserving their width.
- Reveal session actions on mouse hover (and keyboard focus/open state), retaining visible inline actions and the current touch layout on phones.
- Keep action hit targets, accessible names, selection, expansion and drag behavior intact.

**Tests:** U: T24 `SessionList.spec.ts` covers full-width row/action grouping and existing accessible action controls; I: n/a (presentation-only); E: T24 *Session names use the row width and actions reveal on desktop hover* checks desktop hover and phone inline visibility.

**E2E:** Add T24 *Session names use the row width and actions reveal on desktop hover* to `test/e2e/tests/tree.custom.spec.ts`; type-check only and leave browser execution on demand.

## T25 — Full-width project names and terminal session context

- Give project names the full row width on desktop, with project actions floating at the trailing edge and appearing on hover. Keep the current inline controls on phones.
- Place a directory icon first in the terminal header, followed by the focused session name and that session's directory on the same row. When its project belongs to a colored section, fill the focused header with that section color. Keep the name and directory in the same foreground color, and keep the full directory available as a tooltip and accessible label.
- Keep pin state, keyboard focus, menus and touch targets usable.

**Tests:** U: T25 `SessionTree.spec.ts` covers project action grouping and full-width row structure; `TerminalView.spec.ts` covers icon/name/directory order, focused header section fill, matching name/directory foreground color and full-path access; I: n/a (presentation-only); E: T25 *Project names use the row width and terminal header shows session context* checks desktop hover, phone inline controls, and the focused terminal's icon/session/directory context, section-colored header and matching name/directory foreground color.

**E2E:** Add T25 *Project names use the row width and terminal header shows session context* to `test/e2e/tests/tree.custom.spec.ts`; type-check only and leave browser execution on demand.

## T26 — Remove project counts and navigate a three-column command palette

- Remove the session total shown on project rows, whether expanded or collapsed.
- Lay out command palette result groups in three columns on desktop, responsively reducing columns on narrower screens; retain semantic group colors and readable focus treatment.
- Show each visible group's result count beside its heading. Left/right arrows move the highlighted result to the adjacent group while up/down continue moving through results; Enter still activates the highlighted result.

**Tests:** U: T26 `SessionTree.spec.ts` covers no project counts in either state; `CommandPalette.spec.ts` covers per-group counts, responsive grid markers, horizontal group navigation and Enter activation; I: n/a (presentation and client keyboard behavior only); E: T26 *Command palette groups commands and navigates across columns with counts* checks responsive columns, session count and left/right movement.

**E2E:** Update `test/e2e/tests/palette.spec.ts` with T26 *Command palette groups commands and navigates across columns with counts*; update tree hierarchy E2E assertions in desktop and phone specs to confirm project counts stay absent. Type-check only; browser execution remains on demand.

## Done

- [ ] M8 acceptance criteria and their U/I/E coverage are complete.
- [ ] `make lint test` and `make gitleaks` are green; E2E scenarios pass under the post-M7 run policy.
- [ ] Desktop and phone screenshots were inspected during implementation; README/ARCHITECTURE updated only if user-facing behavior or design changes warrant it. T5's affected inputs were reproduced and checked in the running app; T6's scroll behavior was recorded and compared before/after; T7's long-lived terminal theme change and contrast were inspected and the palette-vs-client color ownership was recorded.
- [ ] Summary lists changes, any environment variables (`HOSTBUD_UPLOAD_TIMEOUT`, default 5m), host steps, and any open owner items.
