# M8 — Interface density and input behavior: acceptance

Every criterion has U (unit), I (integration) and E (end-to-end) coverage. Integration is n/a only where the behavior is strictly frontend presentation or browser markup.

## Header controls

- [ ] New session and Browse files follow the host name in the top app bar. Their visual inner padding is reduced by at least 50% from the current design; left-bar collapse/expand control also has compact inner padding. Names, tooltips, focus treatment, action wiring and phone hit targets remain usable.
  - U: T1 placement/order, dialog actions, accessible names, compact styling and hit-area classes (Vitest).
  - I: n/a: frontend presentation and local dialog actions; no server contract changes.
  - E: T1 *Header actions and compact controls* (desktop and phone).

## Project and session tree

- [ ] Project drag/reorder and expand/collapse affordances sit close together; project/session row padding and margins are compact. Session names are the leading, prominent content. No session chevron is rendered where the session row has no expandable children; actual expandable content remains discoverable and operable. Reorder, collapse, keyboard access, focus and phone usability continue to work.
  - U: T2 row structure, affordance visibility, name-first layout, action wiring and compact styling (Vitest).
  - I: n/a: presentation-only; tree data and APIs are unchanged.
  - E: T2 *Compact tree* (desktop and phone), including actual reorder/collapse/open behavior.

## File browser and browser autocomplete

- [ ] Browse files dialog is compact in outer size and inner padding, stays within the viewport, and file/folder rows are closer together while labels and actions remain usable. Existing navigation, folder creation, project and session actions continue to work.
  - U: T3 dialog/item density classes, bounds-related responsive classes and action wiring (Vitest).
  - I: n/a: layout is frontend-only; existing M4 file-browser API integration coverage remains applicable.
  - E: T3 *Compact file browser* (desktop and phone), screenshot/bounds plus navigation and item actions.
- [ ] Browser autocomplete/autofill is disabled on all application inputs except the login-screen password input, whose current autocomplete behavior and markup remain unchanged.
  - U: T3 rendered form attribute audit, with explicit login-password exception regression (Vitest).
  - I: n/a: browser form markup; no server behavior changes.
  - E: T3 *No browser autocomplete outside login password* (desktop): inspect all application form controls and preserve login password behavior.

## Open tab ordering

- [ ] Users can drag tabs directly to choose their order. Keep the existing tab appearance unchanged and add no drag handle. New tabs append to the end. The chosen order is saved in the existing per-account layout and survives reload and app restart; reordering does not activate another tab, remount terminals or detach tmux clients. The compact phone tab switcher reflects the same order.
  - U: T4 layout-store reorder/restore/append behavior and active/pane invariants (Vitest); drag/drop interaction updates order without changing the active tab or mounting terminals (component test).
  - I: n/a: this uses the existing `ui_state/layout` persistence route with no server changes; M3 covers its authenticated persistence integration.
  - E: T4 *Custom tab order* (desktop and phone), including persistence after reload and restart and unchanged appearance.

## Text input caret placement

- [ ] Option-click in editable text inputs places the caret at the clicked character and line in single-line and multiline inputs, without jumps to unrelated positions. Ordinary click, selection, typing, keyboard navigation and existing login password autocomplete behavior remain intact.
  - U: T5 pointer-to-caret regression coverage across positions/line boundaries and ordinary-click behavior (Vitest).
  - I: n/a: caret positioning is browser-side input behavior and makes no server request.
  - E: T5 *Option-click caret placement* (desktop), asserts caret coordinates and text insertion at the clicked location in representative single-line and multiline app inputs.

## Terminal scrolling readability

- [ ] Mouse-wheel scrolling remains correct in both directions and reaches the expected scrollback positions. Text is easier to track while moving through the terminal buffer, with reduced flicker or visual confusion for both distinct and repeated output. Terminal input, scrollback content, copy mode and touch scrolling remain intact.
  - U: T6 wheel handling/render update or scroll-step behavior (Vitest), including direction and bounded movement; if the fix changes rendering only, document why no separate state logic applies.
  - I: n/a when the fix is browser/xterm rendering only; no server or SSH behavior changes.
  - E: T6 *Readable terminal scrolling* (desktop), with deterministic distinct/repeated target output, both directions, controlled/rapid wheel input, visible text assertions and before/after captures for visual review.

## Long-lived terminal theme contrast

- [ ] With a terminal client left running across an OS System theme change, hostbud's xterm palette updates without detaching the session and hostbud-controlled text/background colors remain legible in both dark and light themes. Diagnose the reported Codex prompt contrast (dark prompt surface with dark text after a dark-to-light change): identify whether the colors are hostbud/xterm palette colors or explicit Codex colors. If Codex owns the colors and cannot adapt live, document that boundary and an actionable supported workaround; do not claim a hostbud palette change fixes client-owned colors.
  - U: T7 mounted-terminal palette update, contrast pairs and attached-session invariant across theme changes (Vitest).
  - I: n/a: theme and terminal color rendering are frontend-only; tmux/SSH attachment state is unchanged.
  - E: T7 *Long-lived terminal contrast* (desktop), representative prompt-like TUI on the throwaway target across System dark/light changes, attached-session and palette assertions, with screenshots. Real Codex-specific rendering is recorded as an owner check if Codex is unavailable in the throwaway target.

## Manual checks (owner)

Manual check (owner): if Codex cannot run in the throwaway E2E target, verify the reported “Ask Codex to do anything” prompt surface through a dark-to-light OS theme change with the long-running session still attached. This remains open owner backlog and does not block M8.
