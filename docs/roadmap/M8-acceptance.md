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

- [ ] Project drag/reorder and expand/collapse affordances sit close together; project/session row padding and margins are compact. Session names are the leading, prominent content. Clickable project names and session rows show a pointer cursor; other sidebar areas show the arrow cursor and editable fields show the text cursor. No session chevron is rendered where the session row has no expandable children; actual expandable content remains discoverable and operable. Reorder, collapse, keyboard access, focus and phone usability continue to work.
  - U: T2 `SessionList.spec.ts` and `SessionTree.spec.ts` cover pointer cursor classes on session rows/names and project names, row structure, affordance visibility, name-first layout, action wiring and compact styling.
  - I: n/a: presentation-only; tree data and APIs are unchanged.
  - E: T2 *Compact tree* (desktop and phone), including pointer cursors over clickable project/session names and actual reorder/collapse/open behavior.
  - Status (2026-09-27): U written and passing (`web/src/components/SessionList.spec.ts`, `SessionTree.spec.ts`; e2e `tests/tree.compact.spec.ts`); E written and type-checked, run pending (on demand). Not ticked until the e2e run passes.
- [ ] Session lists stay decluttered: no relative activity-age labels, green attachment dots or project session counts. Clicking any non-control area of a session row selects it, with a pointer cursor over the selectable row/name, while row action buttons, drag handles and double-click rename keep their existing behavior. The selected row has a distinctive, readable background in Dark, Light, Solarized and Dimmed themes.
  - U: T15 `SessionList.spec.ts` non-control row selection, interactive-control isolation, removed ages/dots and selected row; T26 `SessionTree.spec.ts` confirms per-project session totals remain absent in both states; `check-theme-contrast.test.mjs` selected foreground/background contrast for all themes.
  - I: n/a: frontend presentation and click handling only; no server contract changes.
  - E: T15 *Selected session stands out across themes* (desktop) and *Whole session row selects on touch* (iPhone 13 Pro), including pointer cursor over the session row/name, button isolation, no per-project session totals, no age/dot indicators, and selected styling in all four themes; T26 hierarchy scenarios keep per-project session totals absent in either state.

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

## Session switching without tab strips (T22)

- [ ] Desktop and phone terminal layouts show no tab strip. Open terminal views remain in the saved layout and can be selected from the left tree. Ctrl+Shift+] / Ctrl+Shift+[ cycle only open sessions whose rows are visible, in tree order, wrapping at either end; collapsed or hidden rows and unopened sessions are skipped. Closing the active terminal view from the command palette removes its view while leaving the tmux session running. Inactive views detach and reconnect on activation.
  - U: T22 tree-order helper covers pinned, sectioned, unsectioned and Other sessions, open-only filtering, hidden rows, and collapsed projects/sections; palette action labels cover close view and session cycling.
  - I: n/a: this is frontend layout and keyboard behavior; saved layout and terminal attach APIs do not change.
  - E: T22 *Visible open-session shortcuts replace tab strips on desktop* and *Phone terminal has no tab strip and session cycling still works*; desktop scenario also checks collapsed rows, unopened sessions and palette close preserving tmux.
  - Status: U written; E written and type-checked, full run pending on demand.

## Command palette command groups (T23)

- [ ] Command palette results are separated by purpose: Sessions, Windows, Projects, Create, Open, Organize, Terminal, Appearance, Account and Destructive. Group headings and their result rows carry theme-aware color markers; destructive commands use the danger color, while keyboard focus and text remain readable in every theme.
  - U: T23 palette builder tests assert command-to-group assignment; `CommandPalette.spec.ts` checks rendered group membership and destructive marker; theme-aware colors use existing interface tokens.
  - I: n/a: group assignment and rendering are frontend-only and make no API or persistence changes.
  - E: T23 *Command palette groups commands and navigates across columns with counts* (desktop): inspect the groups, representative commands, and distinct semantic colors with a live session present.
  - Status: U written; E written and type-checked, browser run pending on demand.

## Three-column command palette (T26)

- [ ] On desktop, visible command groups appear in three columns; narrower screens use fewer columns. Each group heading shows its current result count. ArrowLeft/ArrowRight move the highlighted result to the adjacent group, ArrowUp/ArrowDown continue moving through results, and Enter activates the highlighted result.
  - U: T26 `CommandPalette.spec.ts` covers group counts, grid columns and horizontal navigation; existing Enter and vertical navigation coverage remains in the same component spec.
  - I: n/a: palette rendering and keyboard navigation are frontend-only.
  - E: T26 *Command palette groups commands and navigates across columns with counts* in `palette.spec.ts` verifies responsive column count, session total and horizontal navigation on desktop and iPhone 13 Pro.
  - Status: E written and type-checked; browser execution pending on demand.

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
- [ ] Backgrounding blurs the active text field; returning to the browser tab or loading/refreshing hostbud focuses the active terminal cursor. Switching hostbud's internal tabs or panes does not automatically focus a terminal. An explicit Show keyboard action still focuses it.
  - U: T8 hidden-page blur and internal tab/pane switching focus guard plus Show keyboard coverage (Vitest); T27 browser-page return and initial-load focus coverage.
  - I: n/a: browser visibility and focus behavior only.
  - E: T8 *Switching tabs does not automatically open the terminal keyboard* (`dictation.spec.ts`); T27 *Background window detaches its terminal until it is visible* (`terminal.spec.ts`) verifies browser-tab return focuses the active terminal after refitting/reconnecting and reload restores the saved layout with the active terminal focused.
  - Status (2026-10-01): U coverage passes (`pageFocus.spec.ts`, `TerminalView.spec.ts`); E is written and type-checked, browser execution pending on demand.
- [ ] Per-terminal actions are consolidated in one three-dot menu, and View terminal text opens a frozen, full-screen, scrollable and selectable copyable snapshot of all history retained by the active tmux pane, including before browser attachment, with tmux colors and styles, wrapped long lines and native selection/copy/vertical scrolling, no visible title or text box frame, and a close button.
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
  - U: T13 Python hook event mapping and safe tmux target validation; Go pane metadata parsing, stale Codex `ended` cleanup, forced-exit inference, aggregation and event diffing; Vitest emoji mapping, accessible label, collapsed-row placement, gutter-only rendering and unchanged session name; service-worker network-first shell refresh and offline fallback.
  - I: T13 `TestIntegrationPollerReportsProviderHookStatus` (`test/sshd`) verifies tmux pane-option working/blocked signals, clears stale ended status while the recognized Codex process is still live, and infers ended after the foreground agent exits.
  - E: T13 *Provider hook status appears before a collapsed session name* (desktop and iPhone 13 Pro), verifies all three status marks, stale ended cleanup for a live Codex process and multi-pane priority without expanding the row, then confirms status survives an ordinary refresh despite a stale cached shell.
  - Manual (owner, open): install each user's Codex and Claude Code hooks following README instructions and confirm real `UserPromptSubmit`, wait/permission and `SessionEnd` signals on the target host; provider behavior cannot be exercised by fake foreground executables alone.
  - Status (2026-09-29): Focused Go unit/integration coverage passes, including clearing a stale `ended` marker while a live Codex process remains in the pane. E2E covers that case and shell-resume inference and type-checks; browser execution remains on demand. Real provider hook behavior remains an open owner check.

## Manual checks (owner)

Manual check (owner): if Codex cannot run in the throwaway E2E target, verify the reported “Ask Codex to do anything” prompt surface through a dark-to-light OS theme change with the long-running session still attached. This remains open owner backlog and does not block M8.

T8 output reader correction: switched from the browser screen snapshot to on-demand tmux history capture. No new environment variables or host configuration changes. E2E remains written/type-checked; run on demand. Manual (owner, open): confirm native selection and vertical scrolling in the installed iPhone PWA.

T8 full-screen apps: while a pane is on the alternate screen (vim, Claude Code, Codex), the view shows tmux's shell history, the saved normal screen and the app's current screen. An app's own internal scrollback isn't held by tmux and can't be shown. tmux keeps `history-limit` lines (default 2000); hostbud doesn't change it.

T10 (open): on an iPhone 13 Pro PWA, choose a known photo from Photos and compare its original library SHA-256/size with the uploaded target file. The app submits the selected `File` unchanged. WebKit fixed its earlier unconditional HEIC-to-JPEG conversion for file inputs (WebKit bug 267277), and Safari 17 added HEIC support, but this does not prove which file representation the iPhone Photos picker supplies for a specific library item or iCloud state. Verify the actual selected item against its original.
T10 (open): on macOS, copy an image from Preview or Finder and Cmd-V in the terminal; confirm it appears in the active repo and text-only Cmd-V still pastes into the terminal. Clipboard image exposure can vary by browser and source application.

T28 (open): verify live counts on a real Codex conversation after deployment. The optional hooks are already configured on this host with this checkout’s handler; no user config changes are needed. Browser suite run remains open/on demand.

T28 verification (2026-10-02): hook, Go unit/integration and focused terminal component tests pass; E2E type-check passes; rendered header inspected at 320px, 390px and desktop width with no horizontal overflow. `make lint test` passes after correcting the stale initial-highlight and reorder-handle assertions in `CommandPalette.spec.ts` and `SessionList.spec.ts`.

T8 verification: native reader checked at 390px in Chromium and WebKit (358px content width and scroll width; full text selection; 6,220px vertical history). Capture integration passed. Full Go suite encountered the existing `TestIntegrationSlowTerminalClientDropped` timeout on two runs; tracked separately from the reader fix.

## Colored project sections

- [ ] Users can create empty, named project sections, choose among six lightly accented colors, rename/recolor/delete them, and assign or move projects from project row actions. Membership and section settings persist per account through reload and restart; deleting a section unassigns projects without removing them. Thin colored borders enclose each section's projects without adding a tree level, extra project indentation, or gutter width. Existing project ordering, pinning, actions, keyboard operation and phone targets continue to work.
  - U: T17 `tree.spec.ts` covers version 1/2 migration, section validation and six color values; `SessionTree.spec.ts` covers create/edit/assign/delete flows, empty section rendering and indentation/width constraints.
  - I: n/a: sections use the existing authenticated `ui_state/tree` persistence endpoint and introduce no new server behavior; its persistence integration is covered by M3.
  - E: T17 *Project sections* (desktop and iPhone 13 Pro), including section creation, color/name changes, project assignment, gutter geometry, and state after reload and app restart.
  - Status: U passes (`tree.spec.ts`, `SessionTree.spec.ts`); E written and type-checked (`tree.sections.spec.ts`), browser run pending on demand.

- [ ] Users can drag sections into a custom saved order with a dedicated touch-sized handle. The create-section action stays at the bottom of the gutter while its tree scrolls. Section gaps are 4px vertically and 1px horizontally to the enclosing tree, with no inner right padding and no additional project indentation.
  - U: T18 `tree.spec.ts` and `stores/tree.spec.ts` cover section ordering and membership retention; `SessionTree.spec.ts` covers the drag list, 4px gap, handle positioned after the three-dot menu at the row's right edge, section color dot and title alignment with project expanders/folder icons, collapsed section project count beside the title without a second line, a bounded flex scroll region with the footer outside it, and action-menu dismissal after project assignment.
  - I: n/a: sorting and gutter placement use existing tree UI-state persistence and browser layout; no server contract changes.
  - E: T18 *Project sections and ordering* in `tree.sections.spec.ts` (desktop and iPhone 13 Pro) checks the three-dot menu precedes the far-right drag handle, aligns the section color dot/title with project expanders/folder icons, checks collapsed counts beside the title without a second line, drags sections, checks the 4px gap and persisted order, gutter-bottom placement and that the create action remains outside the scrolling tree.
  - Status: U passes; E written and type-checked, browser run pending on demand.

## Selected session in a collapsed project

- [ ] When the selected session belongs to a collapsed project, its parent project row visibly and accessibly shows the selection; changing selected sessions moves that marker without expanding the project or changing the selected session.
  - U: T19 `SessionTree.spec.ts` covers selection following across collapsed parent projects and accessible selected state.
  - I: n/a: this is derived from the existing selected-session prop and saved collapsed state; no server contract changes.
  - E: T19 *Selected content stays marked when its project or section collapses* in `tree.sections.spec.ts` (desktop and iPhone 13 Pro) checks the visible and accessible project marker as selection changes and projects collapse.
  - Status: U passes; E written and type-checked (`tree.sections.spec.ts`); browser run pending on demand.

## Collapsible project sections

- [ ] Clicking a section's color dot collapses or expands its projects. Collapse state persists per account through reload/restart with a safe tree-state migration. If the selected session belongs to a collapsed section, the section shell visibly and accessibly shows the selection without expanding or changing the selected session.
  - U: T20 `tree.spec.ts` covers state migration; `SessionTree.spec.ts` covers color-dot toggle, hidden project rows and the collapsed-section selection marker.
  - I: n/a: this uses the existing per-account tree UI-state endpoint and selected-session prop; no server behavior changes.
  - E: T20 *Selected content stays marked when its project or section collapses* in `tree.sections.spec.ts` (desktop and iPhone 13 Pro) verifies section toggle behavior, selected marking and persistence after reload/restart.
  - Status: U passes; E written and type-checked (`tree.sections.spec.ts`); browser run pending on demand.

## Native choice control sizing

- [ ] On coarse-pointer devices, radio and checkbox visuals remain compact across the app, including controls added in future features. Their surrounding labels or wrappers retain at least a 44px touch target, and authored 16px/20px control sizes remain intact.
  - U: T32 `control-sizing.test.mjs` covers the shared CSS sizing contract; T32 component assertions cover touch targets on terminal search and queue parallel toggle labels.
  - I: n/a: shared CSS and label markup only; no server behavior.
  - E: T32 *Native choice controls stay compact on touch screens* in `notifications.phone.spec.ts` checks actual WebKit checkbox boxes and label targets.
  - Status: U passes (`control-sizing.test.mjs`, `TerminalSearch.spec.ts`, `QueuePanel.spec.ts`); E is written and type-checks (`notifications.phone.spec.ts`); browser run remains on demand.

## Terminal resizing across browser windows

- [ ] A terminal detaches while its hostbud tab is inactive or its browser page is hidden, so its stale tmux client size cannot constrain a visible terminal. On return to the browser tab, and on initial page load/refresh, it refits and reconnects at the current size and focuses the active terminal cursor; inactive panes remain unfocused.
  - U: T21 `TerminalView.spec.ts` covers detach on hidden/inactive state, remaining detached when selected while hidden, and refit/reattach at current dimensions; T27 covers active-terminal focus restoration on page return.
  - I: n/a: resize eligibility is frontend-only; M1 T13 covers PTY-to-tmux resize integration.
  - E: T21 *Background window detaches its terminal until it is visible* (`terminal.spec.ts`, desktop Chromium) attaches two pages to the same throwaway tmux session, verifies the hidden client detaches and cannot affect the visible terminal, then checks it reattaches at the new size on return; T27 checks the active input is focused after return.
  - Status (2026-10-01): T21 detach/reconnect U and E coverage passes/type-checks; T27 browser-return and initial-load focus U coverage passes, and E coverage for return/reload type-checks. Browser execution is pending on demand. The full web suite has unrelated existing failures in `CommandPalette.spec.ts` and `SessionList.spec.ts` (683/685 tests passed); the focused `TerminalView.spec.ts` suite now has 42 tests.

## Session row name width and hover actions

- [ ] Desktop session names use the available row width, with row actions positioned over the trailing edge and revealed by mouse hover. Phone layouts retain inline visible actions and current touch behavior; keyboard focus and an open menu keep actions available.
  - U: T24 `SessionList.spec.ts` covers action grouping, full-width row structure and accessible controls.
  - I: n/a: row layout and hover visibility are frontend-only.
  - E: T24 *Session names use the row width and actions reveal on desktop hover* (`tree.custom.spec.ts`) checks desktop name width, hidden/revealed action state, and phone inline action visibility.
  - Status: E written; type-check and browser execution pending on demand.

## Project row width and terminal session context

- [ ] Project names use the available row width on desktop, with actions at the trailing edge revealed on hover; phones retain visible inline controls. The focused terminal header starts with a directory icon followed by the emphasized session name, without repeated directory or project labels; the full path is available accessibly from the icon, and the header uses its project's section color when assigned. Sessions on other servers show a bold nickname chip.
  - U: T25 `SessionTree.spec.ts` covers project action grouping and row structure; `TerminalView.spec.ts` covers icon/name order, focused header section fill, full-path tooltip and absence of repeated directory/project labels.
  - I: n/a: these are frontend presentation changes.
  - E: T25 *Project names use the row width and terminal header shows session context* (`tree.custom.spec.ts`, desktop and iPhone 13 Pro) checks project action reveal/phone visibility and terminal icon/session order, absence of repeated labels, and section-colored header. V2-M13 T4 (`servers.spec.ts`) checks the bold server nickname chip.
  - Status: E written; type-check and browser execution pending on demand.

## Codex token usage in the terminal header

- [x] The active Codex pane shows its last reported context tokens and cumulative tokens consumed in the session window header. Exact counts and context limit are available accessibly. Values update through session events, survive browser reload, hide for an active shell or unknown usage, and distinguish zero from unknown. Only validated numeric metadata leaves the hook; transcript content is not stored or exposed.
  - U: T28 `test-agent-status-hook.py`, `tmux_test.go`, `inventory_test.go`, `TerminalView.spec.ts`: bounded parsing, count semantics, malformed/partial data, compaction, new sessions, zero/unknown, events and rendering.
  - I: T28 `TestIntegrationCodexUsageFromHookInActivePane`: actual hook → tmux → SSH inventory, including active-pane isolation.
  - E: T28 *Codex header shows context and consumed tokens and follows the active pane* (`codex-usage.spec.ts`), desktop and iPhone 13 Pro; written, execution pending on demand.

## Web focus mode (T29)

- [ ] A signed-in user can turn on the Focus mode toggle in the top bar. While on, the mouse leaving the viewport for 2 s shows an opaque overlay covering the entire browser viewport with only “focus” centered; returning sooner cancels it. The mouse re-entering hides the overlay immediately, as does clicking, tapping or Escape on it, and focus returns to where it was; the toggle stays on until the user turns it off. The mode is local to the page and leaves sessions running. Phones, touch (coarse-pointer) devices and the installed PWA don't show the toggle, and the mode stays off there (switching to such a platform turns it off).
  - U: T29 `App.spec.ts` checks the toggle state, no overlay while off, the 2 s delay and its cancellation, full-viewport overlay classes, visible word, mouse-return/click/Escape dismissal that keeps the toggle on, disarming only via the toggle, focus restoration, and that the PWA, phone and touch layouts hide the toggle and turn the mode off.
  - I: n/a: browser-only presentation state; no HTTP, WebSocket or host action.
  - E: T29 *Focus mode toggle shows the overlay after the mouse leaves and hides it on return while staying on* (`header.actions.spec.ts`, desktop and iPhone 13 Pro) (desktop) checks the toggle, the delayed overlay after a dispatched viewport leave, viewport bounds, centered word, dismissal on return and on click with the toggle still on, turning it off, and focus; T29 *Focus mode is not offered on phones or in the installed PWA and stays off there* (iPhone 13 Pro, and desktop with `navigator.standalone`) checks the missing toggle and no overlay.
  - Status: U passes; E written and type-checked, browser run pending on demand.

## Claude Code token usage in the terminal header (T30)

- [x] The active Claude Code pane shows its last context tokens and cumulative tokens consumed (main conversation, each message counted once, cached input included) in the session window header, labelled as Claude Code in the accessible text. A new conversation clears the counts; only numeric metadata leaves the hook.
  - U: T30 `test-agent-status-hook.py` (`ClaudeUsageTests`), `tmux_test.go` (`TestAgentUsageMetadata`), `TerminalView.spec.ts`: dedupe, incremental reads, invalid/sidechain records, rewritten transcripts, path restrictions, agent-tagged records and the label.
  - I: T30 `TestIntegrationClaudeUsageFromHookInActivePane`: actual hook → tmux → SSH inventory.
  - E: T30 *Claude Code header shows context and consumed tokens* (`codex-usage.spec.ts`), desktop and iPhone 13 Pro; written, execution pending on demand.
  - Status: U and I pass; E type-checked, browser run pending on demand. Owner (open): confirm live counts on a real Claude Code session after deployment.

## Active session header alignment (T31)

- [ ] The active session header keeps the session name on the left, centers available agent token usage, and shows the session's project name on the right. Directory context and its full-path tooltip remain available. Missing token usage stays hidden, and the project label has an accessible name.
  - U: T31 `TerminalView.spec.ts` covers project matching, content ordering/alignment classes, accessible project label, and hidden unknown usage.
  - I: n/a: presentation-only header change with no server behavior.
  - E: T31 *Active session header aligns project and token usage* (`codex-usage.spec.ts`, desktop and iPhone 13 Pro) checks left/center/right placement and project context with live usage.
  - Status: E written and type-checked; browser run pending on demand.

## Installed PWA terminal keyboard space (T33)

- [ ] In the installed PWA in portrait, the app header has a keyboard toggle that focuses the active terminal pane to show the native keyboard and blurs it to hide it. Only the active focused pane responds. The app header stays on one compact row while retaining the iOS safe-area inset and usable touch targets. The in-app key bar remains visible, including shortcut buttons such as `/`. Terminal input keeps autocorrect, autocapitalization, autocomplete and spellcheck disabled. The colored terminal title bar has no vertical padding to leave more space for terminal text, and its three-dot actions mark has a heavier visual weight. The terminal uses the available viewport through the bottom edge when the keyboard is closed and fills the remaining area above it when open. Regular mobile browsers keep their existing key bar and title bar sizing.
  - U: T33 `App.spec.ts` covers the PWA-only header action and one-row app header; existing `TerminalView.spec.ts` covers input attributes; title-bar sizing/color and bold dots coverage checks PWA and regular browsers.
  - I: n/a: this is browser presentation and focus behavior; server/SSH APIs do not change.
  - E: T33 *Installed PWA offers a header keyboard toggle* (`pwa.spec.ts`, desktop Chromium standalone simulation) checks the compact one-row app header, title-bar padding, bold dots and active terminal focus/blur; *Installed PWA keeps the in-app terminal shortcut buttons* (`pwa.phone.spec.ts`) checks the shortcut bar and `/` button on a touch profile. The physical iOS suggestion/accessory rows and viewport bottom edge require the owner check below.
  - Status: E updated; type-check pending after the latest layout assertions; browser run pending on demand.
  - **Manual (owner, open):** verify suggestion/accessory rows are absent and the terminal reaches the usable bottom edge in the installed iPhone PWA with the keyboard both open and closed.

## Installed iOS PWA Backspace repeat (T34)

- [ ] Holding the iOS software keyboard's Backspace key in the installed PWA repeatedly deletes terminal input after the initial press and stops when released or when the terminal loses focus. Regular browsers and other platforms retain their existing keyboard behavior.
  - U: T34 `TerminalView.spec.ts` covers repeat timing and keyup stop behavior in the standalone iOS environment.
  - I: n/a: this is client-side keyboard handling; no backend or SSH behavior changes.
  - E: T34 *Installed iPhone PWA repeats Backspace while it is held* (`pwa.phone.spec.ts`) verifies repeated deletion against a real shell line.
  - Status: E written; type-check and browser run pending (browser run on demand).

## Chrome-free landscape view in the installed PWA (T35)

- [ ] With a terminal open in the installed PWA in landscape, the tmux terminal fills the available viewport; the app header, host banner, terminal title/actions, key bar and visible buttons are hidden. The terminal input is blurred so the native keyboard stays dismissed, including when a saved layout loads in landscape. The client-rendered content remains visible as part of the tmux terminal. Portrait PWA and regular browser layouts retain their controls.
  - U: T35 `App.spec.ts` checks the standalone-landscape state and its orientation transition with an open terminal.
  - I: n/a: layout and focus are browser presentation behavior; server and SSH contracts do not change.
  - E: T35 *Installed PWA landscape shows only the tmux terminal* (`pwa.phone.spec.ts`) verifies visible controls are absent, the terminal fills the 844×390 viewport, live tmux output remains visible, rotation blurs the terminal input, and an open dialog closes. **Manual (owner):** confirm native iOS keyboard dismissal on rotation and a real Codex/Claude client display.
  - Status: U passes; E is type-checked and the focused `iphone-13-pro` WebKit scenario passes. The full suite is on demand.

## Extra terminal rows for landscape PWA clients (T36)

- [ ] In installed-PWA landscape view, only the focused tmux pane receives an extra 9rem of terminal height. xterm fits and reports the added rows to tmux while the page clips content at the physical screen edge, giving terminal TUIs more transcript space and moving their bottom composer offscreen. Portrait PWAs, regular browsers and unfocused panes retain their normal dimensions.
  - U: n/a: the added height is a browser CSS layout rule; its actual xterm row count and viewport clipping require a rendered browser.
  - I: n/a: the existing terminal resize protocol is unchanged; the e2e scenario verifies the additional rows reach xterm.
  - E: T36 *Installed PWA reports extra landscape rows below the viewport* (`pwa.phone.spec.ts`) checks xterm and tmux receive extra rows, then simulates a bottom-row composer and verifies it falls below the visible viewport while the terminal box remains full-screen. **Manual (owner):** verify the real Codex composer is below the screen and the transcript is usable on an iPhone PWA.
  - Status: focused WebKit E2E passed in both iPhone profiles; real-device check remains open.

## Web terminal font size (T37)

- [ ] The active terminal's colored title bar has accessible decrease/increase controls on the right, immediately before the three-dot actions button. They change only that machine/session's terminal text, synchronize across its split panes, leave other sessions unchanged, and use a bounded range. The setting exists only in page memory and resets to the default after reload.
  - U: T37 `TerminalView.spec.ts` checks the initial size, both controls, split-pane synchronization, session isolation and inactive-pane visibility.
  - I: n/a: the font size is browser-local and makes no API or persistence request.
  - E: T37 *Terminal font size controls affect only the active session and reset on reload* (`terminal-font.spec.ts`) checks title-bar button order, rendered font size for two sessions and the reset after reload; written and type-checked, browser execution pending on demand.

## Adaptive terminal title bar and branch context (T38)

- [ ] The colored title bar stays on one line whenever its content fits. Only when content overflows does the regular browser header offer expansion; clicking expands to at most three compact rows, and clicking/focusing outside or pressing Escape collapses it. In the installed PWA, the default single row shows the title icon, session title, server badge when applicable, font controls and three-dot actions; other details appear on demand, with token usage on row two at phone width. The current Git branch for the active session directory is shown when details are expanded and available; non-Git directories hide it.
  - U: T38 `TerminalView.spec.ts` covers fit/overflow affordance, temporary expansion, dismissal, branch visibility, the compact PWA default and expanded phone usage row; `tmux_test.go` covers branch command quoting.
  - I: T38 `TestIntegrationGitBranchInSessionInventory` verifies branch reporting and non-repository handling over `test/sshd`.
  - E: T38 *Adaptive title bar expands only on overflow and shows branch context* (`codex-usage.spec.ts`) verifies live branch context and outside dismissal; *Installed phone PWA keeps a compact title bar and expands details on demand* (`pwa.phone.spec.ts`) verifies the minimal default, second-row token usage, expansion and outside dismissal; written and type-checked, execution pending on demand.
  - Status: U and I pass; E written and type-checked, browser execution pending on demand.
