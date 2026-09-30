# M6 — Tree customization and polish: acceptance checklist

M6 is done when every box is ticked, except the *Manual checks (owner)* list, which is the owner's backlog and never blocks. Tasks: [M6-tasks.md](M6-tasks.md).

M6 lets each account shape its left-bar tree and adds keyboard-first polish. The tree becomes an accessible tree view with persistent collapse state, lazily loaded windows and panes under sessions, inline rename, hide/unhide and pinned projects. It adds a command palette, a keyboard shortcut set with a help dialog, and a Dark / Light / System theme setting for the UI and the terminal.

It adds three server routes: listing a session's windows and panes, selecting a window or pane, and deleting a project (the one shared, not per-account, change: T11). It also allows one new `ui_state` key (`theme`). It adds no migration, no env var and no published port. Every customization is **per account** and lives in `ui_state` (`tree` and `theme`), like M4's order. The global `projects.pinned`, `projects.sort_order` and `machines.hidden` columns stay unused by the UI. M4's drag-to-sort and "new rows append, nothing auto-sorts" rule still hold. Authentication, the Origin allowlist, `sshx`, the single session service and event-driven UI rules still apply. M5's compact layout (drawer, sheets, key bar, touch targets) must keep working with every new control.

Setup for the manual checks: `make deploy` on the host; a desktop browser on the port-forward path and the owner's iPhone on `https://${HOSTBUD_DOMAIN}`; a few projects and tmux sessions on the host, one with several windows and a split window.

## Test coverage rule

Same as M1 ([M1-acceptance.md](M1-acceptance.md#test-coverage-rule)): every criterion names its **U** (unit: Go with fakes, Vitest, node build checks), **I** (integration: `test/sshd`, PostgreSQL or the rendered deploy config) and **E** (e2e) tests and the task that writes each. n/a needs a one-line reason, and "manual" is used only where automation can't observe the behavior (a real macOS keyboard's ⌘ shortcuts inside Safari, the real OS appearance switch, real iOS). E2E scenarios are written with the behavior and type-checked, but `make e2e` doesn't run until M7's final task. A ticked box means its U/I tests pass and its E scenario exists and type-checks; the e2e pass is recorded at M7.

"Survives reload and restart" in the criteria below always means both: a page reload, **and** `ctl.restartApp()` (`docker restart hostbud-e2e-app`) followed by a reload, with the same account signed in.

## Tree state and persistence

- [x] The `tree` UI-state value moves to version 2: M4's `projects` and `sessions` order, plus `pinned` (project ids), `hidden` (project ids and session keys), `collapsed` (project ids and `__other__`), `expanded` (session keys and window keys) and `showHidden`. A session key is `<machineId>/<sessionName>` and a window key `<machineId>/<sessionName>/<windowId>`, so multi-machine can return without a format change. A saved version 1 value loads and is upgraded with its order unchanged. An invalid value falls back to the empty state with a console warning, as in M4.
  - U: T2 `lib/tree.ts` validator: v1 → v2 upgrade keeps order; v2 round-trip; rejects wrong types, bad keys (`__proto__`, missing `/`), oversize lists; unknown version → empty (Vitest).
  - I: T2 per-account `tree` round-trip through PostgreSQL with a v2 value, and a v1 value stored by M4 still reads back byte-identical (the server stores it uninterpreted) (Go, PostgreSQL).
  - E: T2 *Tree state upgrades from M4* (desktop; seed a v1 value through the API, reload, the M4 order is shown, and the next save is v2).
- [x] Session-keyed state (order, hidden, expanded) is pruned only from a list reported by a **reachable** host. The empty first snapshot after an app restart, an unreachable host's stale list and the pre-live empty list at sign-in never drop saved entries. Project-keyed state is pruned only after `projects.load` succeeds.
  - U: T2 `sync()` with machine status `unknown`/`unreachable` keeps every entry; `ok` with the session gone prunes it; the sign-in path (`tree.load` → `sync` before `live.start`) keeps entries (Vitest). This includes the regression test for the M4 pruning bug, committed failing before the fix.
  - I: n/a (client-side decision; the server's snapshot and status behavior is M1's integration coverage).
  - E: T2 *Tree state survives an app restart* (desktop; custom session order, `ctl.restartApp()`, the order is unchanged once the host is reachable again).
- [x] Changes are saved with `PUT /api/ui-state/tree`, debounced 500 ms, and a pending save is flushed on `pagehide` (a keep-alive request), so a reload right after a change keeps it. The serialized value never exceeds the server's 64 KiB limit: stale entries are pruned first, and a value that is still too large isn't sent (console warning, the previous saved value stays).
  - U: T2 debounce and flush (fake timers, `pagehide`); the size guard with 500 projects × 20 sessions of 64-byte names; the oversize path doesn't call `putUIState` (Vitest).
  - I: T2 a 64 KiB + 1 body still gets 413 (existing M1/M3 handler test extended to `tree`) (Go).
  - E: T2 *Tree state survives an app restart* reloads immediately after a change.
- [ ] Only the user's edits save the tree; rows appended or pruned by a live event never do, since every client derives them. So an open tab or device that loaded earlier never overwrites a manual order, and it adopts the saved order when it becomes visible again (unless it has an unsaved edit).
  - U: T2 a live event that appends and prunes rows sends no `PUT`; a reorder does; `refresh()` adopts the saved order and skips it while a local save is pending (Vitest).
  - I: n/a (client-side save policy; the server stores values uninterpreted).
  - E: T2 *Another open tab never overwrites a manual session order* (desktop).
- [x] Customization is per account: pins, hidden rows, collapse state, order and theme saved by one account never show for another account on the same host.
  - U: T2 the store resets on sign-out (Vitest).
  - I: T2 two accounts' `tree` and T7 two accounts' `theme` values are isolated (Go, PostgreSQL; extends the M3/M4 per-user UI-state test).
  - E: T6 *Customizations are per account* (desktop; account B sees none of account A's pins, hidden rows, collapse state or order) · T7 *Theme persists* (a second account still starts in System after account A picked Light).
- [x] No automatic re-sorting is introduced: newly observed projects and sessions append to the end of their section, pinning keeps the manual order inside each section, unhiding puts a row back at its saved position, and renaming keeps the row's position.
  - U: T2 `projectTree` with pins/hidden keeps relative order; T4 rename re-keys in place; T5 unhide restores position; T6 pin/unpin keeps relative order (Vitest).
  - I: n/a (presentation state; persistence is covered above).
  - E: T6 *Every customization survives reload and restart* (desktop; checks the full order after each step).

## Accessible tree and collapse

- [x] The left bar is a WAI-ARIA tree (`role="tree"`, labelled "Projects and sessions"): projects, the Other sessions group, sessions, windows and panes are `treeitem`s with `aria-level`, `aria-expanded` where they have children, `aria-selected` for the focused session, and one roving `tabindex`. M4's drag handles, row actions (×, ⋯, pencil) and M5's touch targets stay.
  - U: T2 roles, levels, `aria-expanded`, roving tabindex after re-render and after the focused row disappears (Vitest).
  - I: n/a (frontend only).
  - E: T2 *Keyboard tree navigation* (desktop); every M4/M5 scenario that located tree rows still type-checks against the updated `helpers/ui.ts`.
- [x] Rows show the hierarchy visually, not only through ARIA. A project row is a parent header: a chevron, a folder icon, the name in semibold, a dot when one of its sessions is attached and a session-count badge; its drag handle, more menu and new-session button show on hover with a mouse (always on touch). An expanded project's path heads its group in smaller muted text (the home directory shown as `~`, the full path in a tooltip). Its sessions (and later windows and panes) are indented one step per `aria-level` and hang off a thin vertical guide line from the parent. Each session row leads with an attached/detached dot, then the name in semibold and its last activity as a compact age (`now`, `4m`, `3h`, `2d`); a second muted line shows the active pane's title (the agent's current task, leading spinner glyphs dropped; hidden when tmux's default hostname title is set). The selected session is tinted with a left bar. Other sessions is a small uppercase header with a folder icon, its rows at root level. Indentation stays readable in the phone drawer (at least 12 px per level, the row title never shrinks below 8 characters before truncating), and M5's touch targets are unchanged. (Restyled 2026-09-28 from the owner's mockup; the tinted header background was replaced by the tinted selection.)
  - U: T2 `SessionTree` renders the project header with chevron, icon, name and count, the path with its full `title` at the head of the group, hover-only actions with the drag handle last; a session row's indent grows with `aria-level`; Other uses the uppercase label (Vitest). `SessionList` dot-first rows with age, subtitle and the selected tint; `lib/relativeTime` ages and subtitle cleanup (Vitest). `tmux.ParsePaneMetadata` reads the focused pane's title; `inventory.sameSession` publishes title changes and activity by the minute (Go).
  - I: `TestIntegrationPollerReportsActivePaneTitle` (active pane's title, default hostname title blanked; against `test/sshd`).
  - E: T2 *Tree shows hierarchy* (desktop and `iphone-13-pro` in the drawer); *Session rows show the pane title and last activity* (desktop).
- [x] The Other sessions group is shown only while it has sessions: with none, neither its header nor "No tmux sessions yet." is rendered. It appears as soon as an unmatched session exists and disappears when the last one ends or is saved as a project, keeping its collapse state and order. With no projects and no sessions at all, the tree shows a single "No tmux sessions yet." empty state.
  - U: T2 `SessionTree` omits Other with zero sessions, renders it with one, keeps its collapse state across hide/show; the all-empty tree shows one empty-state line (Vitest).
  - I: n/a (frontend presentation only).
  - E: T2 *Empty Other sessions hidden* (desktop); M1 T15 *Empty list* still covers the all-empty tree.
- [x] Keyboard navigation in the tree: ↑/↓ move, → expands or moves to the first child, ← collapses or moves to the parent, Home/End, Enter opens a session (M3 rules: focus its tab or open one), Enter on a project or Other toggles it, and Alt+↑/↓ moves the focused project or session one place within its section (the keyboard equivalent of drag, saved like a drag).
  - U: T2 key handler table on a fixture tree, including Alt+↑/↓ at section edges (no-op) and across a pinned boundary (no-op) (Vitest).
  - I: n/a (frontend only).
  - E: T2 *Keyboard tree navigation* (desktop; walk the tree, open a session with Enter, reorder with Alt+↓, reload, the order persists).
- [x] Projects and the Other sessions group collapse and expand from a chevron (**Collapse**/**Expand** `<name>`), a click on the row, or ←/→. The collapse state survives reload and restart. Collapsing a group never closes or detaches a terminal showing one of its sessions.
  - U: T2 toggle, persistence into `collapsed`, default expanded, collapsed group hides its rows but keeps them in the order (Vitest).
  - I: n/a (per-account presentation stored through the existing UI-state route; T2's I test covers the round-trip).
  - E: T2 *Collapse state persists* (desktop and `iphone-13-pro` in the drawer; collapse a project and Other, reload, restart, still collapsed; an open terminal of a collapsed session keeps its tmux client PID).

## Windows and panes

- [x] `GET /api/machines/:id/sessions/:name/windows` returns the session's windows in index order, each with `id` (`@n`), `index`, `name`, `active` and its panes in index order (`id` `%n`, `index`, `active`, `command`, `width`, `height`). It reads them in one side-channel exec (`list-windows` and `list-panes -s` on the `=<name>` target), parses free-form fields (window name, command) safely, and caps the reply at 256 windows and 64 panes per window with `truncated: true`.
  - U: T1 `ListWindowsArgs` argv, parser (tabs and newlines in names are replaced by spaces, empty session, caps), handler validation (Go).
  - I: T1 against `test/sshd`: a session with 3 windows, one split into 2 panes, running `vim` in one pane; names with spaces and Unicode; a session that ends mid-request returns 404 (Go).
  - E: T1 *Windows and panes API* (API-level through Caddy).
- [x] `POST /api/machines/:id/sessions/:name/select` with `{window: "@n", pane?: "%n"}` makes that window current in the session (`select-window`) and, with a pane, makes it the active pane (`select-pane`). Ids must match `^@[0-9]+$` / `^%[0-9]+$`, and the server checks that they belong to the named session before selecting, so a pane of another session is a 404. It returns the refreshed window list.
  - U: T1 `SelectArgs` argv, id validation, membership check, handler 400/404 (Go).
  - I: T1 against `test/sshd`: select window 2 then pane 1 → `display -p '#{window_index} #{pane_index}'` reports them; a pane id from another session is rejected and changes nothing (Go).
  - E: T1 *Windows and panes API* (select through Caddy, checked with `display -p` on the target).
- [x] Both routes require authentication; the POST also requires an allowed Origin. Session names are validated before any ssh exec, commands go through `tmux` builders and `sshx` with the default timeout, errors are actionable (unknown session 404, tmux missing → M1's install hint), and info logs never include the session, window or pane names or the pane's command. They publish no event (ARCHITECTURE §9 records why: windows aren't hostbud state).
  - U: T1 401/403/400/404 handler tests, log redaction (Go); `internal/archtest` still passes.
  - I: T1 an invalid name is rejected before ssh runs; the tmux-less target returns the install hint (Go).
  - E: T1 *Windows and panes API* (401 signed out, 403 foreign Origin, 404 unknown session).
- [x] Expanding a session in the tree loads its windows lazily (one request on first expand, a spinner row meanwhile, an actionable error row with **Retry** on failure). A window with more than one pane expands to its panes. Nothing is fetched for collapsed sessions.
  - U: T3 windows store: fetch on first expand only, no fetch while collapsed, error row and retry, a response for a collapsed or renamed session is dropped (Vitest).
  - I: T1's integration tests cover the server side; n/a beyond that (the client adds no server behavior).
  - E: T3 *Windows load when a session is expanded* (desktop and `iphone-13-pro`; no `/windows` request before expanding, one after, the rows match `tmux list-windows` on the target).
- [x] Window rows stay current without polling: while a session is expanded, its window list is re-fetched (debounced 300 ms, one request in flight) when a `sessions.changed` event changes that session's `windows` count or `activity`, and when it is expanded again. A `sessions.changed` that removes the session drops its window rows.
  - U: T3 refresh triggers (count change, activity change, unrelated session change ignored, removal) with fake timers; no timers run while nothing is expanded (Vitest).
  - I: n/a (client reacts to existing events; M1 integration covers the events).
  - E: T3 *Window rows follow the real terminal* (desktop; `tmux new-window` on the target makes a new row appear within one poll interval, `kill-window` removes it).
- [x] Clicking (or Enter on) a window row opens the session (M3 rules) and selects that window through the select route; a pane row also selects the pane. The attached terminal shows the selected window.
  - U: T3 row actions call `layout.open` then `select` with the right ids; an error toast on 404 refreshes the list (Vitest).
  - I: T1 select integration.
  - E: T3 *Open at a window and pane* (desktop and `iphone-13-pro`; target `#{window_index}`/`#{pane_index}` match the clicked rows, and the browser terminal shows a marker printed in that window).
- [x] Session and window expand state is saved in `expanded` and restored after reload and restart; window keys whose window no longer exists are pruned when the refreshed list arrives from a reachable host.
  - U: T3 expand persistence, restore on load (fetches only the restored expanded sessions), pruning of stale window keys (Vitest).
  - I: n/a (per-account presentation; T2's I test covers the round-trip).
  - E: T3 *Windows load when a session is expanded* reloads and restarts with the session still expanded and its windows shown.

## Inline rename

- [x] Projects and sessions rename inline in the tree: the pencil, F2 on a focused row, or a double-click on the name (fine pointer) turns the name into a text field with the current name selected. Enter or blur saves, Escape cancels, and an unchanged or empty value cancels without a request. The row keeps its position and focus returns to it.
  - U: T4 `InlineRename` component: start paths, Enter/blur/Escape, unchanged/empty cancel, focus return, one request per commit even when Enter is followed by blur (Vitest).
  - I: n/a for the component (frontend only); the endpoints' integration tests are M1 (session rename) and M4 T3 (project rename).
  - E: T4 *Inline rename a project* and *Inline rename a session* (desktop and `iphone-13-pro` in the drawer).
- [x] Session renames use the existing `PATCH /api/machines/:id/sessions/:name` and validate the name client-side with the server's rule (`^[A-Za-z0-9_-]{1,64}$`) before sending. A server error (taken name, invalid name, session gone) keeps the field open with the message inline, and the old name stays. On success, every open pane relabels without re-attaching (M3), the session link follows (M4), and its tree order, hidden and expanded keys move to the new name.
  - U: T4 validation messages; error keeps editing; `tree.renameSession` re-keys order/hidden/expanded (including window keys) in one change (Vitest).
  - I: M1's rename integration and M4 T3's link rename stay authoritative; n/a for new server behavior (none).
  - E: T4 *Inline rename a session* (the new name appears immediately on Enter, focus returns to the open terminal cursor without reattaching, the tmux client PID is unchanged, the row stays in place and stays hidden/expanded as before; a taken name shows the inline error).
- [x] Project renames use the existing `PATCH /api/projects/:id` (`{name}`, trimmed, 1–255 bytes); other signed-in browsers update from the `projects.changed` event. Renaming a project never changes its path or its sessions' placement.
  - U: T4 trimming, byte-length validation, error display (Vitest).
  - I: M4 T3's project rename integration; n/a beyond it.
  - E: T4 *Inline rename a project* (a second page sees the new name without reload; the project's sessions stay under it; reload and restart keep the name).
- [x] The old Rename dialog is removed. The tree's ⋯ and long-press menus start the same inline edit.
  - U: T4 the menu and long-press items start inline edit on the right row (Vitest).
  - I: n/a (frontend only).
  - E: T4 *Inline rename a session* starts from the ⋯ menu on the phone.
- [x] The command palette's Rename action reveals the selected row (expand its project, un-collapse, scroll into view, and open the drawer on compact screens) before starting the same inline edit.
  - U: T9 the palette's Rename action reveals the row and starts inline edit (Vitest).
  - I: n/a (frontend only).
  - E: T9 *Palette rename reveals and edits a row* (desktop and `iphone-13-pro`).

## Hide and unhide

- [x] Projects and sessions can be hidden from their ⋯ menu (and with `H` on the focused row). A hidden project hides its whole group, including sessions that start in it later. Hiding never kills, detaches or closes anything: open tabs of a hidden session stay open and attached.
  - U: T5 `SessionTree` hides group/session rows and newly observed sessions in a hidden project; H moves focus to the next row; layout state and session API calls stay unchanged (Vitest).
  - I: n/a (per-account presentation; T2's I test covers the round-trip).
  - E: T5 *Hide and unhide* (desktop and `iphone-13-pro`; the target session still exists and its open terminal keeps its tmux client PID).
- [x] The tree header shows **Show hidden (n)** when anything is hidden. Turning it on shows hidden rows dimmed, labelled "hidden", with **Unhide** in place of Hide; turning it off hides them again. The toggle is saved (`showHidden`). Unhiding returns the row to its saved position.
  - U: T5 `hiddenCount`, toggle, dimmed rendering and accessible label, unhide position (Vitest); T2 persistence round-trip.
  - I: n/a (frontend only).
  - E: T5 *Hide and unhide* (reload and restart keep hidden rows hidden and the toggle state).
- [x] Hidden sessions still count everywhere else: they appear in the command palette marked "hidden", in the split session picker, and in `/api/machines/:id/sessions`. A hidden session that ends is pruned from `hidden` (only from a reachable-host list, per T2).
  - U: T5 pruning; T9 palette includes hidden rows with the marker (Vitest).
  - I: n/a (no server change).
  - E: T5 *Hide and unhide* ends the hidden session from the target and checks that a new session with the same name is **not** hidden.

## Pinned projects

- [x] Projects can be pinned and unpinned from their ⋯ menu (and with `P` on the focused project). Pinned projects show first in a **Pinned** section in their manual order, then the other projects in theirs, then Other sessions. Pinning and unpinning move a project to the end of the target section and change nothing else.
  - U: T6 `projectTree` sections; pin/unpin placement; stale pinned ids pruned only after projects load (Vitest); `PATCH /api/projects/:id` with a `pinned` field doesn't change `projects.pinned` (Go).
  - I: n/a (per-account presentation stored through the existing UI-state route; T2's I test covers the round-trip).
  - E: T6 *Pin projects* (desktop and `iphone-13-pro`).
- [x] Drag-to-sort (M4) and Alt+↑/↓ (T2) reorder within a section only; dropping across the Pinned boundary snaps back without changing order. The pin icon (**Pinned**) is visible on pinned rows and meets M5's touch target on coarse pointers.
  - U: T6 drag between sections is rejected; keyboard move stops at the boundary; icon label (Vitest).
  - I: n/a (frontend only).
  - E: T6 *Pin projects* (a drag across the boundary leaves the order unchanged).
- [x] Every tree customization survives reload and container restart together: order, collapse, expanded sessions/windows, renames, hidden rows, the Show hidden toggle and pins.
  - U: covered by the T2–T6 criteria above.
  - I: T2 PostgreSQL round-trip.
  - E: T6 *Every customization survives reload and restart* (desktop and `iphone-13-pro`): the ROADMAP's combined scenario.

## Theme

- [x] A **Theme** setting with **Dark**, **Light** and **System** (default System) sits in the account menu (M4 header, M5 compact **Account** menu) as a radio group, and in the command palette. Choosing one applies at once, without a reload, to the whole UI (every surface, dialog, sheet, menu, toast, drag ghost, split divider, focus ring) and to every mounted terminal.
  - U: T7 `stores/theme`: resolve mode + OS preference → `dark`/`light`; `data-theme` on `<html>`; radio group state (Vitest).
  - I: n/a (frontend presentation; the stored value's round-trip is below).
  - E: T7 *Pick Dark and Light* (desktop and `iphone-13-pro`; `<html data-theme>`, the body background token and the terminal's theme background change at once).
- [x] The terminal follows the theme: each theme has its own xterm palette (background, foreground, cursor, selection, 16 ANSI colors) and search-highlight colors. Both palettes meet contrast checks: foreground on background ≥ 7:1, every ANSI color except black/white variants ≥ 3:1 against its background, selection keeps the text ≥ 4.5:1. A running full-screen program (vim, htop) repaints in the new palette without re-attaching.
  - U: T7 `lib/theme.ts` WCAG contrast tests on both palettes; `TerminalView` sets `term.options.theme` on change; `TerminalSearch` decorations switch (Vitest).
  - I: n/a (frontend only).
  - E: T7 *Pick Dark and Light* (`window.__hostbud.termTheme()` reports the new background; the tmux client PID is unchanged).
- [x] The choice is saved per account in `PUT /api/ui-state/theme` as `{version: 1, mode}` and restored after reload and restart. The server allowlist gains `theme` (ARCHITECTURE §9), with the same JSON/size rules; any other value read back is ignored (System).
  - U: T7 `uiStateKeys` includes `theme`; unknown keys still 404 (Go); the client validator (Vitest).
  - I: T7 per-account `theme` round-trip through PostgreSQL (Go).
  - E: T7 *Theme persists* (desktop; Light survives reload and `ctl.restartApp()`).
- [x] System follows the OS live: with System selected, a `prefers-color-scheme` change flips the UI and every terminal without a reload. With Dark or Light selected, an OS change does nothing.
  - U: T7 `matchMedia` change listener applies only in System mode, and is removed on sign-out (Vitest).
  - I: n/a (browser media query).
  - E: T7 *System follows the OS* (desktop and `iphone-13-pro`; `page.emulateMedia({colorScheme})` flips it). **Manual (T14):** switch macOS and iOS appearance with hostbud open.
- [x] No flash of the wrong theme: a tiny inline script in `index.html` applies the last resolved mode (a per-browser `localStorage` mirror, `hostbud.theme`) before the stylesheet paints, including on the sign-in screen and the M5 unreachable screen. After sign-in the account's saved setting wins and updates the mirror. A browser without storage (blocked, private mode) falls back to System without errors.
  - U: T7 the boot script's logic as a pure function (mirror present/absent/invalid, storage throwing); `check-dist` asserts the inline script is present, under 1 KiB, and runs before the stylesheet link (Vitest/node).
  - I: n/a (browser-side).
  - E: T7 *No flash of the wrong theme* (desktop; saved Light with an OS in dark: an init script records the root background at the first animation frame, and it's already light).
- [x] The `theme-color` meta follows the resolved theme at runtime (M5's light/dark `media` variants are replaced by one value the app updates), so the phone's status bar and the installed app's chrome match.
  - U: T7 meta updated on each change (Vitest).
  - I: n/a (browser-side).
  - E: T7 *Pick Dark and Light* checks the meta's `content` in both phone projects. **Manual (T14):** the installed app's status bar on the iPhone in both themes.
- [x] The light theme is complete: every UI token has a light value that meets WCAG AA (text ≥ 4.5:1 on its surface, UI borders and icons ≥ 3:1), and no component uses a hard-coded dark color (a lint check fails on hex colors in `.vue` files outside `lib/theme.ts` and `main.css`).
  - U: T7 token contrast tests; the hex-color check runs in `make lint` (Vitest/node).
  - I: n/a (presentation only).
  - E: T7 *Pick Dark and Light* screenshots the tree, a dialog and the terminal in Light as a trace artifact (no pixel diff; inspected at M7).

## Command palette

- [x] ⌘K (macOS) or Ctrl+Shift+K (everywhere) opens the palette from anywhere, including a focused terminal. Plain Ctrl+K opens it only when focus is outside the terminal; inside the terminal it stays the program's (readline kill-line). A **Command palette** header button opens it on touch screens and in compact layout.
  - U: T9 chord handling by focus target and platform; the terminal's key handler doesn't swallow ⌘K/Ctrl+Shift+K and passes plain Ctrl+K through (Vitest).
  - I: n/a (frontend only).
  - E: T9 *Palette opens without stealing Ctrl+K* (desktop; Ctrl+K in a shell deletes to the end of the line, checked with `capture-pane`, and Ctrl+Shift+K opens the palette) · T9 *Palette on the phone* (both phone projects, header button).
- [x] The palette is a modal combobox (Reka UI `Dialog` + `Combobox`, labelled "Command palette") that lists sessions (open or focus), windows of expanded sessions, projects (reveal and expand in the tree), and actions: New session, New session in <project>, Browse files, Rename, Hide/Unhide, Pin/Unpin, Collapse all, Expand all, Show hidden, Split right/down, Close tab, Theme: Dark/Light/System, Keyboard shortcuts, Sign out. Kill session is listed but still goes through the confirmation dialog.
  - U: T9 item sources, action dispatch to the same store functions the tree uses, Kill opens the confirmation (Vitest).
  - I: n/a (frontend only; actions reuse existing API calls).
  - E: T9 *Palette jumps to a session* (desktop and `iphone-13-pro`) · T9 *Palette runs actions* (desktop; theme change, hide/unhide, new session in a project, kill asks for confirmation).
- [x] Fuzzy matching is a pure function (`lib/fuzzy.ts`): case-insensitive subsequence with word-start and contiguity bonuses and a stable tie-break by list order (no recency sort). ↑/↓ move, Enter runs, Escape closes and restores the previous focus, results are capped at 50, and hidden rows show a "hidden" marker. Each action shows its shortcut from the T8 registry.
  - U: T9 `fuzzy` ranking table, stability, cap; keyboard handling and focus restore (Vitest).
  - I: n/a (frontend only).
  - E: T9 *Palette jumps to a session* (typing part of a session name and Enter focuses its tab; Escape returns focus to the terminal).

## Keyboard shortcuts

- [x] One registry (`lib/shortcuts.ts`) defines every app shortcut, its label and platform variants; the global handler, the help dialog and the palette hints all read it. The set: command palette (⌘K / Ctrl+Shift+K), keyboard shortcuts help (⌘/ / Ctrl+Shift+/, and `?` outside text fields and the terminal), focus tree ↔ terminal (⌘⇧E / Ctrl+Shift+E), next/previous tab (Ctrl+Shift+] / Ctrl+Shift+[ on every platform), plus the tree keys from T2, T4, T5 and T6.
  - U: T8 registry is the only source (the help dialog renders every entry; the palette hint matches); platform formatting (⌘ vs Ctrl) (Vitest).
  - I: n/a (frontend only).
  - E: T8 *Keyboard shortcuts help* (desktop).
- [x] Ctrl+Shift+] moves to the next tab and Ctrl+Shift+[ to the previous one, on every platform and even from a focused terminal. They wrap around at either end, do nothing with a single tab, and focus the new tab's focused pane so typing reaches it. The keys never reach the program.
  - U: T8 next/previous with wrap-around, the single-tab no-op, and focus moving to the new tab's focused pane; xterm's custom key handler returns `false` for both chords (Vitest).
  - I: n/a (frontend only).
  - E: T8 *Switch tabs from the keyboard* (desktop) · T8 *Shortcuts don't reach the program* (desktop).
- [x] Ctrl+Shift+D switches to the previously selected tab, and pressing it again switches back, even from a focused terminal. On Mac, Ctrl+⌘+D is an additional binding. If that tab was closed, it goes to the most recently selected tab that's still open; with one tab it does nothing. The new tab's focused pane gets focus, and the key never reaches the program or the browser's own Ctrl+Shift+D.
  - U: T8 the most-recently-selected list (click, chords, palette and opening a session all count; closing drops the tab), the toggle, the closed-tab fallback, the single-tab no-op, `preventDefault()`, xterm's custom key handler returns `false`, ⌘⇧D unbound, Ctrl+⌘+D bound only on Mac (Vitest).
  - I: n/a (frontend only; the list is in memory, not persisted).
  - E: T8 *Toggle to the last tab* (desktop).
- [x] No global shortcut takes a key a terminal program needs: registry entries that fire while the terminal is focused use ⌘ or Ctrl+Shift only, never plain Ctrl+letter, Alt+letter or function keys, and don't collide with M3's keys (Ctrl/⌘+Shift+C/V/F, Mac editing keys). Browser-reserved chords (Ctrl+T, Ctrl+W, Ctrl+N, Ctrl+Tab) aren't used.
  - U: T8 a registry test fails on any terminal-scope chord that is plain Ctrl/Alt+key, a duplicate, or one of M3's keys; `terminalKeys.ts` tests pass unchanged (Vitest).
  - I: n/a (frontend only).
  - E: T8 *Shortcuts don't reach the program* (desktop; Ctrl+Shift+] switches tabs while vim runs, and vim's buffer is unchanged, checked with `capture-pane`).
- [x] The help dialog (**Keyboard shortcuts**) lists every shortcut grouped (General, Tabs, Tree), shows the current platform's keys, and closes on Escape with focus restored. Shortcuts are not customizable in M6 (documented).
  - U: T8 grouping, platform keys and Escape close event (Vitest); focus restoration is checked by the task-tagged browser scenario.
  - I: n/a (frontend only).
  - E: T8 *Keyboard shortcuts help* (opened with the chord and with `?` from the tree; Escape closes).

## Taken session names

- [x] Creating a session with a name that's already taken succeeds with the first free `<name>-<n>` (the auto-name numbering), from every entry point (New session, New session here, split picker, palette); the result stays within 64 characters; the tab opens with the actual name and an info toast names it when it differs from what was typed. Invalid names are still refused, and renaming to a taken name still answers 409.
  - U: T10 numbering, the tmux-duplicate retry and its limit, 64-character trimming, typed suffixes kept, rename unchanged (Go, fake executor) · T10 the toast only when the name differs (Vitest).
  - I: T10 create `dup` twice on test sshd → `dup-1` in real tmux; project creation with a taken name is numbered and linked (Go).
  - E: T10 *Taken name gets a number* (desktop) · T10 *Create with a taken name* (API) · T10 updates M1's T16 *Invalid input*.

## Remove a project

- [x] **Remove project…** (⋯ menu, long-press menu, palette, Delete on a focused project row) asks for confirmation that names the session count, where they move and the path, and says files aren't touched and it applies to every account. Cancel changes nothing. Confirm deletes the project with its session links and recent commands (schema cascade, no migration); its sessions keep running, open tabs stay attached, and the sessions move at once to the next-longest matching project or Other sessions, appended. Removal survives reload and restart; adding the same folder again creates a fresh project.
  - U: T11 service delete publishes `projects.changed` `deleted` and re-places; handler 204/404 (Go) · T11 dialog text, Cancel, success moves sessions without re-attaching, open New session here dialog closes, all entry points (Vitest).
  - I: T11 delete removes the project's links and recent commands only; a linked session is re-placed and still exists in tmux on test sshd (Go, PostgreSQL + test sshd).
  - E: T11 *Remove a project* (desktop and `iphone-13-pro`).
- [x] `DELETE /api/projects/:id` requires a session (401) and an allowed Origin (403); an unknown id is 404.
  - U: T11 handler tests (Go).
  - I: T11 route through the real router and PostgreSQL (Go).
  - E: T11 *Delete project API* (API-level).

## Add the current directory as a project

- [x] The file browser's path bar has a button for the directory being shown: *Add this directory as project* (FolderPlus), or *Open project* (FolderOpen) when it already is one. It uses the same logic as the row icons (no duplicates; the name defaults to the last path component), follows navigation, is disabled while loading or on error, and *New session here* works for it afterwards.
  - U: T12 Add/Open state, create vs select without a second POST, disabled states, touch target (Vitest).
  - I: n/a: frontend only; `POST /api/projects` keeps its M4 integration tests.
  - E: T12 *Add the current directory as project* (desktop and `iphone-13-pro`).

## Left bar toggle and icon toolbar

- [x] The header's **Projects** text button and the left bar's **Projects & sessions** heading are gone; the left bar stays named "Sessions" for assistive technology, and the phone drawer's title is visually hidden but still labels the dialog.
  - U: T13 neither text renders; the `aside` label and the drawer's `sr-only` title (Vitest).
  - I: n/a: frontend only.
  - E: T13 *Left bar toggle and toolbar* (desktop and both phone projects).
- [x] One icon button in the header opens and closes the left bar: `PanelLeftClose` "Hide sidebar" when open, `PanelLeftOpen` "Show sidebar" when closed, with `aria-expanded`/`aria-controls`. Desktop: toggles the sidebar and the state survives reload. Compact: opens the drawer, whose close button is the same *Hide sidebar* icon; swipe and Escape still close it.
  - U: T13 icon, name and `aria-expanded` follow the state; desktop toggle vs compact drawer (Vitest).
  - I: n/a: frontend only.
  - E: T13 *Left bar toggle and toolbar*.
- [x] The top of the left bar (sidebar, compact tree screen and drawer) has icon-only **New session** (`SquareTerminal`) and **Add project** (`FolderPlus`, opens the file browser dialog) buttons with accessible names and tooltips, 44×44 px targets on coarse pointers and visible focus. The header's separate Browse files button is removed.
  - U: T13 icon-only buttons with names and titles open the create dialog and the file browser; no header Browse files button; touch-target classes (Vitest).
  - I: n/a: frontend only.
  - E: T13 *Left bar toggle and toolbar*; the M4/M5 scenarios that used the old names are updated in T13.

## Phone and compact layout (M5 compatibility)

- [x] Every new control works in M5's compact layout: tree chevrons, window/pane rows, the Pinned icon, Show hidden, inline rename fields (16 px font, no zoom), the palette button, the theme radio group. Each meets the 44×44 px target on coarse pointers. The M5 long-press row menu gains Rename, Hide/Unhide and (projects) Pin/Unpin.
  - U: T3–T9 the `touch-target` utility on each new control; inline rename input carries the 16 px class; long-press menu items (Vitest).
  - I: n/a (presentation only).
  - E: T14 extends M5's *Touch targets* and *Usable without zoom* scenarios to the new controls (both phone projects).
- [x] Opening a window row or a palette result in compact layout closes the drawer or palette and shows the terminal, without re-attaching other terminals (M5 rule).
  - U: T3/T9 drawer close on open (Vitest).
  - I: n/a (frontend only).
  - E: T3 *Open at a window and pane* and T9 *Palette on the phone* (both phone projects).

## Security and compatibility

- [x] M6 adds no migration, env var, published port or remote command path outside `internal/tmux` + `sshx`. The global `projects.pinned`, `projects.sort_order` and `machines.hidden` columns aren't written by M6 (ARCHITECTURE §8 notes them as reserved for a later shared/multi-machine use).
  - U: T1 the new builders live in `internal/tmux`, and `internal/archtest` still passes (no exec outside `sshx`, no SQL outside `store`) (Go).
  - I: the existing deploy-config check still passes unchanged (T14 CP5); no new file in `internal/store/migrations/` (T14 audit).
  - E: n/a: nothing new is reachable beyond the routes covered above.
- [x] The `ui_state` route keeps its rules for the new key: authentication, Origin on PUT, JSON only, 64 KiB, per-account namespacing, no event.
  - U: T7 handler tests for `theme` (401, 403, 400, 413) (Go).
  - I: T7 per-account round-trip (above).
  - E: T7 *Theme persists* also checks a foreign-Origin PUT gets 403.
- [x] The inline theme boot script reads only `localStorage` and sets one attribute; it holds no data and needs no network. ARCHITECTURE §11 notes that a future CSP (M7) must allow it by hash.
  - U: T7 `check-dist` content check (node).
  - I: n/a (static file).
  - E: n/a: covered by *No flash of the wrong theme*.

## E2E scenarios (`make e2e`, simulated user)

Profiles: `desktop-chromium`, `iphone-13-pro` (`http://localhost:9055`) and `iphone-13-pro-domain`, against the throwaway `hostbud-e2e-target` only, never the real host. Tree customization scenarios live in `tree.custom.spec.ts` (desktop) and `tree.custom.phone.spec.ts` (phones), windows in `tree.windows.spec.ts` / `tree.windows.phone.spec.ts`, the API in `windows.api.spec.ts`, theme in `theme.spec.ts` / `theme.phone.spec.ts`, and palette/shortcuts in `palette.spec.ts` / `palette.phone.spec.ts`. "Restart" means `ctl.restartApp()` then reload. Each scenario uses its own account (`newAccount()`), so per-account state never leaks between scenarios. Each scenario is tagged with the task that writes it. During M6 the suite is only type-checked; it runs in M7's final task.

- [x] **(T1) Windows and panes API:** through Caddy, a session with 3 windows (one split) lists them in order with ids, names, active flags and panes; `select` a window and a pane, and `display -p` on the target agrees; a pane id from another session → 404; signed out → 401; foreign Origin on `select` → 403; unknown session → 404 (desktop, API-level).
- [x] **(T2) Tree state upgrades from M4:** a v1 `tree` value seeded through the API loads with its order; after a reorder, `GET /api/ui-state/tree` returns version 2 with that order (desktop).
- [x] **(T2) Tree state survives an app restart:** reorder sessions, reload immediately (flush on `pagehide`), then restart; the order is unchanged after the host is reachable again, and no saved session entry was pruned by the first empty snapshot (desktop).
- [ ] **(T2) Another open tab never overwrites a manual session order:** a second tab loaded before a reorder gets a new session by live event and saves nothing (`GET /api/ui-state/tree` still has the manual order); `visibilitychange` makes it show the saved order; reload keeps it (desktop).
- [x] **(T2) Keyboard tree navigation:** focus the tree, walk it with the arrow keys, Home/End, open a session with Enter, move a session with Alt+↓, reload → the order persists (desktop).
- [x] **(T2) Collapse state persists:** collapse a project and Other sessions, reload and restart → still collapsed; a terminal open on a collapsed session keeps its tmux client PID (desktop and `iphone-13-pro` in the drawer).
- [x] **(T2) Tree shows hierarchy:** with a project holding two sessions and one Other session, the project header shows its name and `~`-shortened path; each session row's text starts to the right of its project header's text (bounding boxes); the header shows the session count and no tint; Other sessions has an uppercase label (computed style) (desktop and `iphone-13-pro` in the drawer).
- [x] **(T2) Empty Other sessions hidden:** with only a project session, there's no Other sessions header and no "No tmux sessions yet."; an unmatched session started from the real terminal makes Other appear within one poll interval; saving it as a project makes Other disappear (desktop).
- [x] **(T3) Windows load when a session is expanded:** no `/windows` request before expanding; expanding shows the target's windows and a split window's panes; reload and restart → still expanded with rows loaded (desktop and `iphone-13-pro`).
- [x] **(T3) Window rows follow the real terminal:** `tmux new-window` / `kill-window` on the target add/remove the row within one poll interval, without a reload (desktop).
- [x] **(T3) Open at a window and pane:** clicking window 2's row opens the session at window 2, and a pane row makes that pane active (`display -p` on the target, and a marker printed there shows in the browser terminal) (desktop and `iphone-13-pro`).
- [x] **(T4) Inline rename a project:** pencil → type → Enter; a second page sees the name without reload; Escape cancels another edit; reload and restart keep the name; its sessions stay under it (desktop and `iphone-13-pro`).
- [x] **(T4) Inline rename a session:** F2 (desktop) / ⋯ → Rename (phone); the target has the new name, the open terminal's client PID is unchanged, the row keeps its position and its expanded state; a taken name shows the inline error and keeps the old name (desktop and `iphone-13-pro`).
- [x] **(T5) Hide and unhide:** hide a session and a project; they leave the tree, their terminal stays attached and the target still has them; Show hidden shows them dimmed; Unhide restores the saved position; reload and restart keep all of it; a hidden session that ends and is recreated with the same name is visible (desktop and `iphone-13-pro`).
- [x] **(T6) Pin projects:** pin two projects → a Pinned section in their manual order; unpin → back at the end of the unpinned section; a drag across the boundary changes nothing (desktop and `iphone-13-pro`).
- [x] **(T6) Every customization survives reload and restart:** drag to reorder, rename, hide/unhide, pin, collapse and expand, then reload **and** restart → everything is as the user left it (desktop and `iphone-13-pro`).
- [x] **(T6) Customizations are per account:** account B, signed in on the same stack, sees none of account A's pins, hidden rows, collapse state or order (desktop).
- [x] **(T7) Pick Dark and Light:** each choice applies at once to `<html data-theme>`, the background token, `theme-color` and the terminal's theme (`__hostbud.termTheme()`), with the terminal still attached (desktop and `iphone-13-pro`).
- [x] **(T7) Theme persists:** Light survives reload and restart; a second account still starts in System; a foreign-Origin `PUT /api/ui-state/theme` → 403 (desktop).
- [x] **(T7) System follows the OS:** in System mode, `page.emulateMedia({colorScheme: 'light'})` then `'dark'` flips the UI and terminal without a reload; in Dark mode it doesn't (desktop and `iphone-13-pro`).
- [x] **(T7) No flash of the wrong theme:** with Light saved and the emulated OS dark, the root background at the first animation frame of a reload is already the light token (desktop).
- [x] **(T8) Keyboard shortcuts help:** Ctrl+Shift+/ and `?` from the tree open it; it lists the registry's entries; Escape closes and restores focus (desktop; authored and type-checked, browser run at M7).
- [x] **(T8) Switch tabs from the keyboard:** with three tabs open and the last one focused, Ctrl+Shift+] wraps to the first tab and Ctrl+Shift+[ wraps back to the last; after each switch, typing in the terminal reaches that tab's shell (`capture-pane`); with one tab, the chords leave it focused and send nothing to the shell (desktop; authored and type-checked, browser run at M7).
- [x] **(T8) Toggle to the last tab:** with three tabs, select tab 1 then tab 3: Ctrl+Shift+D focuses tab 1, again focuses tab 3, and typing after each reaches that tab's shell (`capture-pane`); after closing tab 1 it goes to the next most recent open tab; with one tab it does nothing and sends nothing to the shell (desktop; authored and type-checked, browser run at M7).
- [x] **(T8) Shortcuts don't reach the program:** with vim in one tab and a shell in another, Ctrl+Shift+] / [ switch tabs, and vim's buffer and mode are unchanged; Ctrl+Shift+E moves focus to the tree and back (desktop; authored and type-checked, browser run at M7).
- [x] **(T9) Palette opens without stealing Ctrl+K:** in a shell, Ctrl+K deletes to the end of the line (`capture-pane`); Ctrl+Shift+K opens the palette (desktop; authored and type-checked, browser run at M7).
- [x] **(T9) Palette jumps to a session:** type part of a name, Enter → its tab is focused (or opened) and the terminal is focused; Escape without choosing returns focus (desktop and `iphone-13-pro`; authored and type-checked, browser run at M7).
- [x] **(T9) Palette runs actions:** Theme: Light applies; Hide then Unhide a session; New session in <project> lands under the project; Kill asks for confirmation and Cancel leaves the session alive (desktop; authored and type-checked, browser run at M7).
- [x] **(T9) Palette rename reveals and edits a row:** select Rename for a session in a collapsed project, including on the phone; the tree reveals it and opens the inline editor (desktop and `iphone-13-pro`; authored and type-checked, browser run at M7).
- [x] **(T9) Palette on the phone:** the header button opens it; picking a session closes it and shows the terminal (both phone projects; authored and type-checked, browser run at M7).
- [x] **(T10) Taken name gets a number:** with a target session `<n>`, New session named `<n>` opens a tab `<n>-1`, the toast names it, and both exist on the target; New session here in a project with the same name gives `<n>-2` under that project (desktop; authored and type-checked, browser run at M7).
- [x] **(T10) Create with a taken name:** `POST` twice with the same name → 201 both times, the second named `<n>-1`; renaming another session to `<n>` → 409 (API; authored and type-checked, browser run at M7).
- [x] **(T11) Remove a project:** a project with a session open in a tab; Remove → Cancel leaves it; Remove → confirm: the header is gone, the session is under Other sessions, the tab keeps its tmux client PID, the folder still exists on the target; reload and restart → still removed; adding the folder again gives a fresh project with no recent commands (desktop and `iphone-13-pro` via long-press; authored and type-checked, browser run at M7).
- [x] **(T11) Delete project API:** signed out → 401, foreign Origin → 403, unknown id → 404, delete → 204 then 404; the list no longer has it (API; authored and type-checked, browser run at M7).
- [x] **(T12) Add the current directory as project:** at home, *Add this directory as project* → the home project appears with a `~` path and the button reads *Open project*; a subfolder is added the same way; *New session here* lands under it; a second click makes no duplicate (desktop and `iphone-13-pro`; authored and type-checked, browser run at M7).
- [x] **(T13) Left bar toggle and toolbar:** no "Projects & sessions" or "Projects" text; *Hide sidebar* hides the left bar and becomes *Show sidebar*, which survives reload and brings it back; the *New session* icon directly creates a default-named session and focuses its terminal; the *Add project* icon opens the file browser (desktop). On phones the toggle opens the drawer, its *Hide sidebar* closes it, and both icon buttons meet the touch-target size (both phone projects; authored and type-checked, browser run at M7).
- [x] **(T14) Touch targets and zoom for M6 controls:** M5's *Touch targets* and *Usable without zoom* checks extended to session/window chevrons, window and pane rows, the Pinned icon, Show hidden, inline rename, the palette button and input, and the theme radio items; the long-press menu exposes Rename, Hide/Unhide and Pin/Unpin (both phone projects; authored and type-checked, browser run at M7).

## Manual checks (owner, T14)

On a desktop browser (port forward, a Mac if available) and the owner's iPhone over `https://${HOSTBUD_DOMAIN}`. These are the owner's backlog, not blockers: they don't hold back M6's done state or the next milestone, and no agent waits for them. Record the date and the result here when the owner does one; an unchecked item stays open in the summary.

- [ ] macOS: ⌘K opens the palette from a terminal in Chrome and Safari; ⌘/ opens the help; ⌘⇧E moves focus; Ctrl+Shift+] / [ switch to the next/previous hostbud tab from a focused terminal (while ⌘⇧]/[ still switch browser tabs); Ctrl+Shift+D and Ctrl+⌘+D toggle between the last two hostbud tabs in Chrome and Safari (⌘⇧D keeps the browser's behavior), and on Linux/Windows Chrome Ctrl+Shift+D doesn't open the bookmark-all-tabs dialog; Ctrl+K in a shell still kills to the end of the line.
- [ ] Theme: switch macOS appearance with hostbud in System mode, and the UI and a running vim/htop repaint without a reload; Dark and Light ignore the OS switch.
- [ ] No flash: hard-reload in Light with the OS in dark (and the reverse), and nothing dark (or light) flashes.
- [ ] iPhone: Light and Dark look right in Safari and in the installed app (status bar color, sheets, drawer, key bar); System follows iOS appearance.
- [ ] iPhone: inline rename in the drawer doesn't zoom; hide, pin and collapse by long-press menu; the palette button jumps to a session.
- [ ] Real host: expanding a session with Claude Code running shows its windows; clicking a window switches the attached terminal to it.
- [ ] Real host, signed in: `GET /api/ui-state/theme` answers 404 for an account that never picked a theme, then 200 after a pick (T14 checks only the unauthenticated 401).

## Definition of done

- [x] Every functional and security criterion above is satisfied: its U/I tests pass and its E scenario exists and type-checks.
- [x] M6 scenarios are part of the M7 full e2e run; no e2e run happened during M6.
- [x] `make lint test` and `make gitleaks` are green (CP1–CP5); no secrets, real hostnames, IPs or owner paths are tracked.
- [x] README has the tree customization, windows, palette, shortcuts and theme sections; ARCHITECTURE §5.1, §8, §9, §11 and §13.1 match what was built; `.env.example` is unchanged (or updated if a variable was really needed); no new migration.
- [x] *(host)* `make deploy` done; the owner's manual checks are recorded above or listed as open.
- [x] T15 safe Docker cleanup done: the production stack and all volumes intact and healthy, nothing outside hostbud touched, reclaimed space reported.
- [x] Summary delivered: what changed, new env vars (expected none), manual steps on the host, desktop and phone.
