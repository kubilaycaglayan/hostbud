# M8 — Interface density and input behavior: acceptance

Every criterion has U (unit), I (integration) and E (end-to-end) coverage. Integration is n/a only where the behavior is strictly frontend presentation or browser markup.

## Header controls

- [ ] New session and Browse files follow the host name in the top app bar. Their visual inner padding is reduced by at least 50% from the current design; left-bar collapse/expand control also has compact inner padding. Names, tooltips, focus treatment, action wiring and phone hit targets remain usable.
  - U: T1 placement/order, dialog actions, accessible names, compact styling and hit-area classes (Vitest).
  - I: n/a: frontend presentation and local dialog actions; no server contract changes.
  - E: T1 *Header actions and compact controls* (desktop and phone).
  - Status (2026-09-27): U written and passing (`web/src/App.spec.ts`); E written and type-checked (`test/e2e/tests/header.actions.spec.ts`; M6 T13 phone/wide scenarios and helpers updated for the move), run pending (on demand). Not ticked until the e2e run passes.

- [ ] On compact screens, application dialogs remain above the project-tree gutter when both are open; the dialog content stays visible and usable.
  - U: T16 `App.spec.ts` verifies the compact drawer layer is below application dialog layers.
  - I: n/a: frontend stacking only; no server contract changes.
  - E: T16 *Dialogs stay above the reopened tree gutter* (iPhone 13 Pro), opening the Command palette while the drawer is open and checking the palette's search input is topmost and usable (and above the drawer's layer if the drawer stays open).
  - Status: U passes (`web/src/App.spec.ts`), E written and type-checked (`test/e2e/tests/mobile-layout.phone.spec.ts`); full browser run is pending on demand.

## Project and session tree

- [ ] Project drag/reorder and expand/collapse affordances sit close together; project/session row padding and margins are compact. Session names are the leading, prominent content. No session chevron is rendered where the session row has no expandable children; actual expandable content remains discoverable and operable. Reorder, collapse, keyboard access, focus and phone usability continue to work.
  - U: T2 row structure, affordance visibility, name-first layout, action wiring and compact styling (Vitest).
  - I: n/a: presentation-only; tree data and APIs are unchanged.
  - E: T2 *Compact tree* (desktop and phone), including actual reorder/collapse/open behavior.
  - Status (2026-09-27): U written and passing (`web/src/components/SessionList.spec.ts`, `SessionTree.spec.ts`; e2e `tests/tree.compact.spec.ts`); E written and type-checked, run pending (on demand). Not ticked until the e2e run passes.
- [ ] Session lists stay decluttered: no relative activity-age labels or green attachment dots on project/session rows; project session counts appear only while collapsed. Clicking any non-control area of a session row selects it, while row action buttons, drag handles and double-click rename keep their existing behavior. The selected row and active terminal tab have a distinctive, readable background in Dark, Light, Solarized and Dimmed themes.
  - U: T15 `SessionList.spec.ts` non-control row selection, interactive-control isolation, removed ages/dots and selected row; `SessionTree.spec.ts` collapsed-only count and removed project attachment dot; `TabBar.spec.ts` active-tab styling; `check-theme-contrast.test.mjs` selected foreground/background contrast for all themes.
  - I: n/a: frontend presentation and click handling only; no server contract changes.
  - E: T15 *Selected session and active tab stand out across themes* (desktop) and *Whole session row selects on touch* (iPhone 13 Pro), including button isolation, collapsed-only counts, no age/dot indicators, and selected styling in all four themes.

## File browser and browser autocomplete

- [ ] Browse files dialog is compact in outer size and inner padding, stays within the viewport, and file/folder rows are closer together while labels and actions remain usable. Existing navigation, folder creation, project and session actions continue to work.
  - U: T3 dialog/item density classes, bounds-related responsive classes and action wiring (Vitest).
  - I: n/a: layout is frontend-only; existing M4 file-browser API integration coverage remains applicable.
  - E: T3 *Compact file browser* (desktop and phone), screenshot/bounds plus navigation and item actions.
  - Status (2026-09-27): U written and passing (`web/src/components/FileBrowserDialog.spec.ts`, `web/src/lib/autocomplete.spec.ts`; e2e `tests/files.compact.spec.ts`); E written and type-checked, run pending (on demand). Not ticked until the e2e run passes.
- [ ] Browser autocomplete/autofill is disabled on all application inputs except the login-screen password input, whose current autocomplete behavior and markup remain unchanged.
  - U: T3 rendered form attribute audit, with explicit login-password exception regression (Vitest).
  - I: n/a: browser form markup; no server behavior changes.
  - E: T3 *No browser autocomplete outside login password* (desktop): inspect all application form controls and preserve login password behavior.
  - Status (2026-09-27): U written and passing (`web/src/components/FileBrowserDialog.spec.ts`, `web/src/lib/autocomplete.spec.ts`; e2e `tests/files.compact.spec.ts`); E written and type-checked, run pending (on demand). Not ticked until the e2e run passes.

## Open tab ordering

- [ ] Users can drag tabs directly to choose their order. Keep the existing tab appearance unchanged and add no drag handle. New tabs append to the end. The chosen order is saved in the existing per-account layout and survives reload and app restart; reordering does not activate another tab, remount terminals or detach tmux clients. The compact phone tab switcher reflects the same order.
  - U: T4 layout-store reorder/restore/append behavior and active/pane invariants (Vitest); drag/drop interaction updates order without changing the active tab or mounting terminals (component test).
  - I: n/a: this uses the existing `ui_state/layout` persistence route with no server changes; M3 covers its authenticated persistence integration.
  - E: T4 *Custom tab order* (desktop and phone), including persistence after reload and restart and unchanged appearance.
  - Status (2026-09-27): U written and passing (`web/src/lib/layout.spec.ts`, `stores/layout.spec.ts`, `components/TabBar.spec.ts`, `App.spec.ts`; e2e `tests/tabs.order.spec.ts`); E written and type-checked, run pending (on demand). Not ticked until the e2e run passes.

## Text input caret placement

- [ ] Option-click in editable text inputs places the caret at the clicked character and line in single-line and multiline inputs, without jumps to unrelated positions. Ordinary click, selection, typing, keyboard navigation and existing login password autocomplete behavior remain intact.
  - U: T5 pointer-to-caret regression coverage across positions/line boundaries and ordinary-click behavior (Vitest).
  - I: n/a: caret positioning is browser-side input behavior and makes no server request.
  - E: T5 *Option-click caret placement* (desktop), asserts caret coordinates and text insertion at the clicked location in representative single-line and multiline app inputs.
  - Status (2026-09-27): cause identified (xterm 6 `altClickMovesCursor` miscounts in the normal buffer; the app's own `<input>`s are unaffected and there are no `<textarea>`s). U written and passing (`web/src/lib/altClick.spec.ts`, including a reproduction of xterm's miscount on a bordered multiline prompt; `TerminalView.spec.ts`); E written and type-checked (`test/e2e/tests/caret.spec.ts`: bash single-line and soft-wrapped, a deterministic bordered multiline prompt on the throwaway target, and the New session Name input), run pending (on demand), which is also the first live check in a browser. Not ticked until the e2e run passes.

## Terminal scrolling readability

- [ ] Mouse-wheel scrolling remains correct in both directions and reaches the expected scrollback positions. Text is easier to track while moving through the terminal buffer, with reduced flicker or visual confusion for both distinct and repeated output. Terminal input and explicit Scroll history remain intact.
  - U: T6 wheel handling/render update or scroll-step behavior (Vitest), including direction and bounded movement.
  - I: T6 `TestIntegrationAttachDeclaresSynchronizedOutput` (`internal/term`, against `test/sshd`): the browser's tmux client declares `sync`, redraws arrive in DEC 2026 marks, and the session keeps working.
  - E: T6 *Readable terminal scrolling* (desktop), with deterministic distinct/repeated target output, both directions, controlled/rapid wheel input, visible text assertions and before/after captures for visual review.
  - Status (2026-09-27): wheel causes measured and fixed (tmux copy-mode redraws torn across network chunks → `-T sync`; xterm scrollback notches jumping 3 rows per frame → `smoothScrollDuration` 100 ms). U written and passing (`TerminalView.spec.ts`, `internal/tmux`, `internal/term`); I written and passing; E `test/e2e/tests/scroll.wheel.spec.ts` written and type-checked, run pending (on demand). Not ticked until the e2e run passes.

- [ ] On touch screens a vertical swipe over the terminal scrolls like a mouse wheel: a mouse-aware full-screen app (Claude Code, Codex) receives wheel reports and scrolls its own history; otherwise tmux copy mode scrolls tmux's full history, including output before the browser attached. xterm's partial local scrollbar is hidden on touch screens. Experimental momentum: flicks coast with friction, same-direction flicks stack up to a speed cap, and a touch stops it.
  - U: T9 `touchScroll.spec.ts`, `copyMode.spec.ts` wheel actions and `TerminalView.spec.ts` swipe request (Vitest); Go `TestWheelState`, `TestAppWheelArgs`, `TestCopyModeArgs`, `TestCopyModeWheelFollowsTmuxWheelRule`, `TestCopyModeAPIValidationAndAccess`.
  - I: T9 `TestIntegrationWheelScrollsAppOrTmuxHistory` (`internal/session`, against `test/sshd`).
  - E: T9 *Touch swipe scrolls the full tmux history* and *Touch swipe scrolls a mouse-aware full-screen app* (phone projects).
  - Status (2026-09-27): U and I written and passing; E written and type-checked, run pending (on demand). Deployed at the owner's request; the owner confirmed plain tmux and Claude Code scrolling on the phone.
  - **Manual (owner, open):** tune the momentum feel (friction, speed cap) in the iPhone 13 Pro PWA.

## Mobile terminal input

- [ ] Long-pressing a word selects it at the visible xterm buffer row and exposes Copy.
  - U: T2 selection uses the absolute active-buffer row (Vitest). I: n/a (browser-side xterm selection and input; no server behavior specific to these cases). E: T2 *Touch long press selects terminal text for copying* (phone projects). **Manual (owner):** verify selection and copy in the iPhone 13 Pro PWA.

## Long-lived terminal theme contrast

- [ ] With a terminal client left running across an OS System theme change, hostbud's xterm palette updates without detaching the session and hostbud-controlled text/background colors remain legible in both dark and light themes. Diagnose the reported Codex prompt contrast (dark prompt surface with dark text after a dark-to-light change): identify whether the colors are hostbud/xterm palette colors or explicit Codex colors. If Codex owns the colors and cannot adapt live, document that boundary and an actionable supported workaround; do not claim a hostbud palette change fixes client-owned colors.
  - U: T7 mounted-terminal palette update, contrast pairs and attached-session invariant across theme changes (Vitest).
  - I: n/a: theme and terminal color rendering are frontend-only; tmux/SSH attachment state is unchanged.
  - E: T7 *Long-lived terminal contrast* (desktop), representative prompt-like TUI on the throwaway target across System dark/light changes, attached-session and palette assertions, with screenshots. Real Codex-specific rendering is recorded as an owner check if Codex is unavailable in the throwaway target.
  - Status (2026-09-27): diagnosed as client-owned (Codex queries OSC 10/11 and paints an explicit composer background computed for the start-up theme; default-colored text on it measured 1.16:1 after dark → light). hostbud enforces `minimumContrastRatio` 4.5; before/after verified in headless Chromium with xterm 6 (`rgb(31,35,40)` → `rgb(150,151,155)` on the dark composer). U written and passing (`web/src/lib/theme.spec.ts`, `TerminalView.spec.ts`); E `test/e2e/tests/theme.contrast.spec.ts` written and type-checked, run pending (on demand). Real Codex remains the open owner check below. Not ticked until the e2e run passes.

## Solarized and Dimmed theme levels (T14)

- [ ] Solarized is tuned to 25% darkness and Dimmed to 70% on the Light=0 to Dark=100 scale. Both are available alongside Dark, Light and System in Account settings and the command palette, update open terminals and terminal snapshots, set matching status-bar metadata, persist per account, and apply before first paint. System and existing Light behavior remain unchanged.
  - U: T14 mode validation, first-paint mirrors, store persistence, both palettes, search/snapshot selection and contrast checks (Vitest).
  - I: n/a: uses the existing authenticated `ui_state/theme` path and changes no server behavior.
  - E: T14 *Solarized and Dimmed apply at their darkness levels and persist* (desktop; both page palettes, mounted terminal, saved account state and status-bar colors).
  - Status: U written and passing (`web/src/lib/theme.spec.ts`, `web/src/stores/theme.spec.ts`, `web/src/components/TerminalView.spec.ts`; `web/scripts/check-theme-contrast.test.mjs` checks both added UI palettes); E written and type-checked (`test/e2e/tests/theme.spec.ts`), run pending (on demand). The on-demand E2E run remains open.

## Dictation, focus return and terminal text view

- [ ] Dictation opens a native editable text box with Send and Cancel; Send delivers the finished text once through xterm paste, and Cancel sends nothing.
  - U: T8 dialog Send/Cancel and exactly-once xterm paste coverage (Vitest).
  - I: n/a: browser input handling only; no server state or integration contract changes.
  - E: T8 *Dictation editor sends reviewed text once* (desktop Chromium), verified in the throwaway shell.
  - Status (2026-09-27): U coverage is written and passing (`TerminalView.spec.ts`); E `test/e2e/tests/dictation.spec.ts` is written and type-checked, run pending (on demand). The iPhone system dictation check remains open owner backlog.
- [ ] Backgrounding blurs the active text field; returning and switching tabs or panes do not automatically focus the terminal or reopen the keyboard. An explicit Show keyboard action still focuses it.
  - U: T8 hidden-page blur, tab/pane switching focus guard, and Show keyboard coverage (Vitest).
  - I: n/a: browser visibility and focus behavior only.
  - E: T8 *Returning from background does not automatically refocus the terminal* (desktop Chromium).
  - Status (2026-09-27): U coverage is written and passing (`pageFocus.spec.ts`, `TerminalView.spec.ts`); E `test/e2e/tests/dictation.spec.ts` is written and type-checked, run pending (on demand).
- [ ] Per-terminal actions are consolidated in one three-dot menu, and View terminal text opens a frozen, full-screen, scrollable and selectable copyable snapshot of all history retained by the active tmux pane, including before browser attachment, with tmux colors and styles, wrapped long lines and native selection/copy/vertical scrolling, no visible title or text box frame, and a close button. Tab controls remain unchanged.
  - U: T8 capture command validation, service errors, authenticated uncached API, ANSI styling and safe text rendering, native reader and toolbar actions (Go/Vitest).
  - I: T8 `TestIntegrationOutputIncludesHistoryWithoutAttaching` captures styled historical output and joined rows without attaching or entering copy mode; `TestIntegrationOutputIncludesHistoryUnderFullScreenApp` captures shell history, the saved normal screen and the full-screen app's screen in order, with row padding trimmed (`test/sshd`).
  - E: T8 *Terminal text snapshot scrolls, selects, and closes* and *Terminal text includes shell history under a full-screen app* (desktop Chromium).
  - Status (2026-09-27): U coverage is written and passing (`terminalOutput.spec.ts`, `TerminalTextDialog.spec.ts`, `TerminalView.spec.ts`, Go capture/API tests); E `test/e2e/tests/dictation.spec.ts` is written and type-checked, run pending (on demand).
  - **Manual (owner, open):** verify two consecutive dictations with native iOS dictation in the iPhone 13 Pro PWA; Playwright cannot invoke iOS system dictation.

## Sending photos to a session repository

- [ ] From the terminal's three-dot menu, the user can select one or more iPhone photos and send them to the active session's repository directory; Cmd-V/Ctrl-Shift-V with an image clipboard also uploads it. The selected file bytes arrive unchanged, including HEIC/HEIF and DNG when provided by iOS. Filename conflicts use the next available `-1`, `-2`, etc. suffix without overwriting. After each successful upload, the relative path is pasted at the active terminal cursor. Size and transfer failures are actionable.
  - U: T10 API tests raw byte preservation, size/content/path validation and conflict errors; Vitest checks direct `File` body identity, numbered conflict retry, path insertion, menu action, destination display, photo selections and partial-upload retry, image clipboard extraction/upload, and text-only paste pass-through.
  - I: T10 `TestIntegrationSFTPUploadPreservesBytesWithoutOverwriting` (`test/sshd`) compares source and remote SHA-256 and verifies the existing target remains intact after raw API conflict; `TestIntegrationSFTPConcurrentUploadsDoNotOverwriteExistingName` verifies simultaneous raw uploads cannot overwrite the winner. Numbered retry is covered at the API client layer.
  - E: T10 *Send original photo bytes to the active session repo* (desktop and iPhone 13 Pro), verifies target size and SHA-256 against selected and clipboard-pasted image files, confirms numbered naming preserves the original and pastes the relative path into tmux, and retains existing text-paste coverage.
  - Status (2026-09-27): U passed (`client.spec.ts`, `PhotoUploadDialog.spec.ts`, `TerminalView.spec.ts`, API byte/content/size/path/origin/conflict checks); I passed (`test/sshd` SHA-256 and filename-collision checks). E is written and type-checked; full e2e run is on demand.

## Terminal split menu

- [ ] The terminal top-right three-dot menu stays compact when many sessions are active: the user chooses split right/down first, then sees and chooses a session or **New session…**. The existing session-row split actions continue to work.
  - U: T11 `TerminalView.spec.ts` verifies the staged menu and split event values (Vitest).
  - I: n/a: presentation-only client flow; the existing split API and layout integration remain unchanged.
  - E: T11 *Terminal menu chooses split position before listing sessions* (desktop), verifies the staged choices and the resulting pane position/session attachment.
  - Status (2026-09-27): U written; E written and type-check pending; full e2e run is on demand.

## Agent marks in the left session list

- [ ] Collapsed session rows show tiny colored Codex and Claude Code marks when any pane's foreground command is recognized. A session with both harnesses shows both marks in stable order; ordinary commands show none. Marks appear without opening window/pane details, stay in the left gutter only, and are exposed accessibly without changing the session name or row actions.
  - U: T12 command-to-agent parsing, unknown-command handling and inventory event changes (Go); T12 compact mark order, placement, accessible label and gutter-only behavior (Vitest).
  - I: T12 `TestIntegrationPollerReportsForegroundAgentCommand` against `test/sshd`, launching the Codex alias `coy` and clearing its mark when the fake foreground command exits; `TestIntegrationPollerFindsCodexProcessBehindNodeForeground` covers the Node-launched Codex process.
  - E: T12 *Agent logos appear on collapsed session rows* (desktop and iPhone 13 Pro), including the Codex child process behind foreground `node`, the `cly` alias and no mark for an ordinary shell.
  - Status: Go unit tests and focused `test/sshd` integration coverage for canonical and Node-launched Codex processes pass; Vitest component coverage passes. E2E covers the Node-launched Codex case and `cly`; the owner asked to skip E2E during this check, so its updated scenario is not type-checked or run yet.

## Provider hook status in the left session list

- [ ] Configured Codex and Claude Code hooks report work-in-progress, blocked/waiting and ended states for tmux panes. Collapsed session rows show 🟢, 🚧 or 🎯 at the beginning of the displayed name without changing the actual name; no status appears before a hook signal. In a session with multiple agent panes, blocked takes priority over working, and working takes priority over ended. A tracked agent that returns to a known interactive shell without sending a session-end hook is shown as ended.
  - U: T13 Python hook event mapping and safe tmux target validation; Go pane metadata parsing, forced-exit inference, aggregation and event diffing; Vitest emoji mapping, accessible label, collapsed-row placement, gutter-only rendering and unchanged session name; service-worker network-first shell refresh and offline fallback.
  - I: T13 `TestIntegrationPollerReportsProviderHookStatus` (`test/sshd`) verifies tmux pane-option working/blocked signals and ended inference after the foreground agent exits.
  - E: T13 *Provider hook status appears before a collapsed session name* (desktop and iPhone 13 Pro), verifies all three status marks and multi-pane priority without expanding the row, then confirms status survives an ordinary refresh despite a stale cached shell.
  - Manual (owner, open): install each user's Codex and Claude Code hooks following README instructions and confirm real `UserPromptSubmit`, wait/permission and `SessionEnd` signals on the target host; provider behavior cannot be exercised by fake foreground executables alone.

## Manual checks (owner)

Manual check (owner): if Codex cannot run in the throwaway E2E target, verify the reported “Ask Codex to do anything” prompt surface through a dark-to-light OS theme change with the long-running session still attached. This remains open owner backlog and does not block M8.

T8 output reader correction: switched from the browser screen snapshot to on-demand tmux history capture. No new environment variables or host configuration changes. E2E remains written/type-checked; run on demand. Manual (owner, open): confirm native selection and vertical scrolling in the installed iPhone PWA.

T8 full-screen apps: while a pane is on the alternate screen (vim, Claude Code, Codex), the view shows tmux's shell history, the saved normal screen and the app's current screen. An app's own internal scrollback isn't held by tmux and can't be shown. tmux keeps `history-limit` lines (default 2000); hostbud doesn't change it.

T10 (open): on an iPhone 13 Pro PWA, choose a known photo from Photos and compare its original library SHA-256/size with the uploaded target file. The app submits the selected `File` unchanged. WebKit fixed its earlier unconditional HEIC-to-JPEG conversion for file inputs (WebKit bug 267277), and Safari 17 added HEIC support, but this does not prove which file representation the iPhone Photos picker supplies for a specific library item or iCloud state. Verify the actual selected item against its original.
T10 (open): on macOS, copy an image from Preview or Finder and Cmd-V in the terminal; confirm it appears in the active repo and text-only Cmd-V still pastes into the terminal. Clipboard image exposure can vary by browser and source application.

T8 verification: native reader checked at 390px in Chromium and WebKit (358px content width and scroll width; full text selection; 6,220px vertical history). Capture integration passed. Full Go suite encountered the existing `TestIntegrationSlowTerminalClientDropped` timeout on two runs; tracked separately from the reader fix.
