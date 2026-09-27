# M8 — Interface density and input behavior: acceptance

Every criterion has U (unit), I (integration) and E (end-to-end) coverage. Integration is n/a only where the behavior is strictly frontend presentation or browser markup.

## Header controls

- [ ] New session and Browse files follow the host name in the top app bar. Their visual inner padding is reduced by at least 50% from the current design; left-bar collapse/expand control also has compact inner padding. Names, tooltips, focus treatment, action wiring and phone hit targets remain usable.
  - U: T1 placement/order, dialog actions, accessible names, compact styling and hit-area classes (Vitest).
  - I: n/a: frontend presentation and local dialog actions; no server contract changes.
  - E: T1 *Header actions and compact controls* (desktop and phone).
  - Status (2026-09-27): U written and passing (`web/src/App.spec.ts`); E written and type-checked (`test/e2e/tests/header.actions.spec.ts`; M6 T13 phone/wide scenarios and helpers updated for the move), run pending (paused until M7 T13). Not ticked until the e2e run passes.

## Project and session tree

- [ ] Project drag/reorder and expand/collapse affordances sit close together; project/session row padding and margins are compact. Session names are the leading, prominent content. No session chevron is rendered where the session row has no expandable children; actual expandable content remains discoverable and operable. Reorder, collapse, keyboard access, focus and phone usability continue to work.
  - U: T2 row structure, affordance visibility, name-first layout, action wiring and compact styling (Vitest).
  - I: n/a: presentation-only; tree data and APIs are unchanged.
  - E: T2 *Compact tree* (desktop and phone), including actual reorder/collapse/open behavior.
  - Status (2026-09-27): U written and passing (`web/src/components/SessionList.spec.ts`, `SessionTree.spec.ts`; e2e `tests/tree.compact.spec.ts`); E written and type-checked, run pending (paused until M7 T13). Not ticked until the e2e run passes.

## File browser and browser autocomplete

- [ ] Browse files dialog is compact in outer size and inner padding, stays within the viewport, and file/folder rows are closer together while labels and actions remain usable. Existing navigation, folder creation, project and session actions continue to work.
  - U: T3 dialog/item density classes, bounds-related responsive classes and action wiring (Vitest).
  - I: n/a: layout is frontend-only; existing M4 file-browser API integration coverage remains applicable.
  - E: T3 *Compact file browser* (desktop and phone), screenshot/bounds plus navigation and item actions.
  - Status (2026-09-27): U written and passing (`web/src/components/FileBrowserDialog.spec.ts`, `web/src/lib/autocomplete.spec.ts`; e2e `tests/files.compact.spec.ts`); E written and type-checked, run pending (paused until M7 T13). Not ticked until the e2e run passes.
- [ ] Browser autocomplete/autofill is disabled on all application inputs except the login-screen password input, whose current autocomplete behavior and markup remain unchanged.
  - U: T3 rendered form attribute audit, with explicit login-password exception regression (Vitest).
  - I: n/a: browser form markup; no server behavior changes.
  - E: T3 *No browser autocomplete outside login password* (desktop): inspect all application form controls and preserve login password behavior.
  - Status (2026-09-27): U written and passing (`web/src/components/FileBrowserDialog.spec.ts`, `web/src/lib/autocomplete.spec.ts`; e2e `tests/files.compact.spec.ts`); E written and type-checked, run pending (paused until M7 T13). Not ticked until the e2e run passes.

## Open tab ordering

- [ ] Users can drag tabs directly to choose their order. Keep the existing tab appearance unchanged and add no drag handle. New tabs append to the end. The chosen order is saved in the existing per-account layout and survives reload and app restart; reordering does not activate another tab, remount terminals or detach tmux clients. The compact phone tab switcher reflects the same order.
  - U: T4 layout-store reorder/restore/append behavior and active/pane invariants (Vitest); drag/drop interaction updates order without changing the active tab or mounting terminals (component test).
  - I: n/a: this uses the existing `ui_state/layout` persistence route with no server changes; M3 covers its authenticated persistence integration.
  - E: T4 *Custom tab order* (desktop and phone), including persistence after reload and restart and unchanged appearance.
  - Status (2026-09-27): U written and passing (`web/src/lib/layout.spec.ts`, `stores/layout.spec.ts`, `components/TabBar.spec.ts`, `App.spec.ts`; e2e `tests/tabs.order.spec.ts`); E written and type-checked, run pending (paused until M7 T13). Not ticked until the e2e run passes.

## Text input caret placement

- [ ] Option-click in editable text inputs places the caret at the clicked character and line in single-line and multiline inputs, without jumps to unrelated positions. Ordinary click, selection, typing, keyboard navigation and existing login password autocomplete behavior remain intact.
  - U: T5 pointer-to-caret regression coverage across positions/line boundaries and ordinary-click behavior (Vitest).
  - I: n/a: caret positioning is browser-side input behavior and makes no server request.
  - E: T5 *Option-click caret placement* (desktop), asserts caret coordinates and text insertion at the clicked location in representative single-line and multiline app inputs.
  - Status (2026-09-27): cause identified (xterm 6 `altClickMovesCursor` miscounts in the normal buffer; the app's own `<input>`s are unaffected and there are no `<textarea>`s). U written and passing (`web/src/lib/altClick.spec.ts`, including a reproduction of xterm's miscount on a bordered multiline prompt; `TerminalView.spec.ts`); E written and type-checked (`test/e2e/tests/caret.spec.ts`: bash single-line and soft-wrapped, a deterministic bordered multiline prompt on the throwaway target, and the New session Name input), run pending (paused until M7 T13), which is also the first live check in a browser. Not ticked until the e2e run passes.

## Terminal scrolling readability

- [ ] Mouse-wheel scrolling remains correct in both directions and reaches the expected scrollback positions. Text is easier to track while moving through the terminal buffer, with reduced flicker or visual confusion for both distinct and repeated output. Terminal input and explicit Scroll history remain intact.
  - U: T6 wheel handling/render update or scroll-step behavior (Vitest), including direction and bounded movement.
  - I: T6 `TestIntegrationAttachDeclaresSynchronizedOutput` (`internal/term`, against `test/sshd`): the browser's tmux client declares `sync`, redraws arrive in DEC 2026 marks, and the session keeps working.
  - E: T6 *Readable terminal scrolling* (desktop), with deterministic distinct/repeated target output, both directions, controlled/rapid wheel input, visible text assertions and before/after captures for visual review.
  - Status (2026-09-27): wheel causes measured and fixed (tmux copy-mode redraws torn across network chunks → `-T sync`; xterm scrollback notches jumping 3 rows per frame → `smoothScrollDuration` 100 ms). U written and passing (`TerminalView.spec.ts`, `internal/tmux`, `internal/term`); I written and passing; E `test/e2e/tests/scroll.wheel.spec.ts` written and type-checked, run pending (paused until M7 T13). Not ticked until the e2e run passes.

- [ ] On touch screens a vertical swipe over the terminal scrolls like a mouse wheel: a mouse-aware full-screen app (Claude Code, Codex) receives wheel reports and scrolls its own history; otherwise tmux copy mode scrolls tmux's full history, including output before the browser attached. xterm's partial local scrollbar is hidden on touch screens. Experimental momentum: flicks coast with friction, same-direction flicks stack up to a speed cap, and a touch stops it.
  - U: T9 `touchScroll.spec.ts`, `copyMode.spec.ts` wheel actions and `TerminalView.spec.ts` swipe request (Vitest); Go `TestWheelState`, `TestAppWheelArgs`, `TestCopyModeArgs`, `TestCopyModeWheelFollowsTmuxWheelRule`, `TestCopyModeAPIValidationAndAccess`.
  - I: T9 `TestIntegrationWheelScrollsAppOrTmuxHistory` (`internal/session`, against `test/sshd`).
  - E: T9 *Touch swipe scrolls the full tmux history* and *Touch swipe scrolls a mouse-aware full-screen app* (phone projects).
  - Status (2026-09-27): U and I written and passing; E written and type-checked, run pending M7 T13. Deployed at the owner's request; the owner confirmed plain tmux and Claude Code scrolling on the phone.
  - **Manual (owner, open):** tune the momentum feel (friction, speed cap) in the iPhone 13 Pro PWA.

## Mobile terminal input

- [ ] Long-pressing a word selects it at the visible xterm buffer row and exposes Copy.
  - U: T2 selection uses the absolute active-buffer row (Vitest). I: n/a (browser-side xterm selection and input; no server behavior specific to these cases). E: T2 *Touch long press selects terminal text for copying* (phone projects). **Manual (owner):** verify selection and copy in the iPhone 13 Pro PWA.

## Long-lived terminal theme contrast

- [ ] With a terminal client left running across an OS System theme change, hostbud's xterm palette updates without detaching the session and hostbud-controlled text/background colors remain legible in both dark and light themes. Diagnose the reported Codex prompt contrast (dark prompt surface with dark text after a dark-to-light change): identify whether the colors are hostbud/xterm palette colors or explicit Codex colors. If Codex owns the colors and cannot adapt live, document that boundary and an actionable supported workaround; do not claim a hostbud palette change fixes client-owned colors.
  - U: T7 mounted-terminal palette update, contrast pairs and attached-session invariant across theme changes (Vitest).
  - I: n/a: theme and terminal color rendering are frontend-only; tmux/SSH attachment state is unchanged.
  - E: T7 *Long-lived terminal contrast* (desktop), representative prompt-like TUI on the throwaway target across System dark/light changes, attached-session and palette assertions, with screenshots. Real Codex-specific rendering is recorded as an owner check if Codex is unavailable in the throwaway target.
  - Status (2026-09-27): diagnosed as client-owned (Codex queries OSC 10/11 and paints an explicit composer background computed for the start-up theme; default-colored text on it measured 1.16:1 after dark → light). hostbud enforces `minimumContrastRatio` 4.5; before/after verified in headless Chromium with xterm 6 (`rgb(31,35,40)` → `rgb(150,151,155)` on the dark composer). U written and passing (`web/src/lib/theme.spec.ts`, `TerminalView.spec.ts`); E `test/e2e/tests/theme.contrast.spec.ts` written and type-checked, run pending (paused until M7 T13). Real Codex remains the open owner check below. Not ticked until the e2e run passes.

## Manual checks (owner)

Manual check (owner): if Codex cannot run in the throwaway E2E target, verify the reported “Ask Codex to do anything” prompt surface through a dark-to-light OS theme change with the long-running session still attached. This remains open owner backlog and does not block M8.
