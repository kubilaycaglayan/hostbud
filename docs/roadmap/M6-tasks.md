# M6 — Tree customization and polish: tasks

Goal: each account can shape its left-bar tree the way it works, and the app is comfortable from the keyboard. The tree becomes an accessible tree view with persistent collapse state and lazily loaded windows and panes under each session, and it gains inline rename, hide/unhide and pinned projects. A command palette (⌘K / Ctrl+Shift+K) jumps anywhere and runs actions. A small, conflict-free shortcut set has a help dialog. A Dark / Light / System theme covers the UI and the terminal, follows the OS live in System mode, and never flashes the wrong theme. Every customization survives reload and container restart. M4's manual order and "nothing auto-sorts" rule stay intact.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m6--tree-customization-and-polish) · checklist: [M6-acceptance.md](M6-acceptance.md).

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Windows and panes API | Done |
| T2 Tree state v2 and accessible tree | Done |
| T3 Windows and panes in the tree | Done |
| T4 Inline rename | Done |
| T5 Hide and unhide | Done |
| T6 Pinned projects | Done |
| T7 Theme setting | Not started |
| T8 Keyboard shortcuts | Not started |
| T9 Command palette | Not started |
| T10 Taken session names get a number | Not started |
| T11 Remove a project | Not started |
| T12 Add the current directory as a project | Not started |
| T13 Left bar toggle and icon toolbar | Not started |
| T14 Docs, audit and release | Not started |
| T15 Safe Docker cleanup | Not started |

T2 completed with the v2 state migration, guarded persistence, pruning rules, accessible tree behavior and task-tagged scenarios. The pruning regression/fix are in `00f3c29` and `fae320e`; CP1 passed with `make lint test`. E2E scenarios were type-checked only, as required.

T3 completed with lazy window/pane loading, event-driven refresh, window selection and persistent expansion. Frontend tests and lint passed; the desktop and phone E2E scenarios were type-checked only, as required.

T4 completed with inline project/session rename, validation and server errors in place, preserved session layout/tree state, menu and long-press entry points, and T4 E2E scenarios type-checked only. `make lint test` passed CP2; browser E2E remains paused until M7.

T5 completed with per-account hide/unhide controls, hidden-row presentation and focus handling, plus desktop and phone scenarios. `make web-test`, `make web-lint`, `make e2e-lint` and `make gitleaks` passed; browser E2E remains paused until M7.

T6 completed with per-account pinning, separate ordered sections, section-bounded drag and keyboard moves, and desktop/phone persistence and account-isolation scenarios. The Go handler rejects attempts to set the global `pinned` field. CP3 passed with `make lint test`; E2E scenarios were type-checked only, as required.

## Rules for this milestone

- Work top to bottom, one task at a time, and don't start a task until the previous one is done. Before starting M6, check that M5's acceptance checklist is complete, apart from open owner items (its *Manual checks (owner)* list): M6 adds controls to M5's drawer, sheets, long-press menu and account menu, and extends M5's touch-target scenarios. Don't build on an unfinished M5 layout.
- Before each task, read the U/I/E coverage lines that [M6-acceptance.md](M6-acceptance.md) assigns to it, plus its own **Tests:** and **E2E:** lines. Write all of them in the same commit(s) as the behavior. Never leave tests or scenarios for a later task.
- **Batched test checkpoints** (continuing M3–M5): each commit runs only fast checks: `go build`/`go vet` for the Go packages it touches, `vue-tsc` for the frontend, and `tsc` for the e2e suite when it changes. `make gitleaks` runs on every commit through the pre-commit hook. Full suites run at the checkpoints below. A checkpoint failure is fixed (with a regression test if it's a bug) before the next task starts, and the checkpoint re-runs until green.
- **E2E runs are paused until the end of M7.** Write every scenario in its task, but never run `make e2e`, `e2e-up` or `e2e-run` in M6, whether per commit, task, checkpoint or milestone. E items count as written and type-checked, not passed, until M7's full run.
- Every task has an **E2E:** line. A scenario is tagged with the task that writes it, both here and in the acceptance checklist.
- Tasks marked *(host)* need the real host, a desktop browser or the owner's iPhone. The agent does the host parts itself. Anything that needs the owner's device, account or decision is an open owner item: record it and continue, never wait (AGENTS.md, *Owner items never block agents*).
- **Per-account state only.** Every tree customization goes into the account's `tree` UI-state value and the theme into `theme` (ARCHITECTURE §8: `user:<id>:<key>`). Don't write the global `projects.pinned`, `projects.sort_order` or `machines.hidden` columns, and add **no migration**. Add a new env var only if one is really needed, and then to `.env.example` and `internal/config`. None is expected.
- **No automatic re-sorting** (M4 rule): new rows append, pin/unpin moves one row to the end of its target section, unhide and rename keep the saved position. Nothing orders by name, activity or recency, and that includes the palette's tie-break.
- **Keys belong to the terminal first.** A shortcut that fires while a terminal is focused uses ⌘ or Ctrl+Shift, never plain Ctrl/Alt+letter or function keys. Plain keys (`H`, `P`, F2, Delete, …) act only when a tree row has focus.
- **Bugs found on the way** follow the bug-fix workflow: a failing regression test in one commit, the fix in the next.
- When a task moves the design, it updates ARCHITECTURE (mostly §5.1, §8, §9, §11 and §13.1) in the same commit.
- Conventional commits, small and focused; commit only this task's files. Other agents may be working in the tree at the same time, so stage explicit paths, never use `git add -A`, and check `git status` before touching a shared file (`helpers/ui.ts`, `playwright.config.ts`, `App.vue`, `main.css`); coordinate rather than overwrite.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T1 + T2 (windows API, tree state and tree view) | `make lint test` | Passed |
| CP2 | T3 + T4 (windows in the tree, inline rename) | `make lint test` | Passed |
| CP3 | T5 + T6 (hide/unhide, pins) | `make lint test` | Passed |
| CP4 | T7 + T8 + T9 (theme, shortcuts, palette) | `make lint test`, plus `make build` so `check-dist` sees the real theme boot script | Not run |
| CP5 | T10 + T11 + T12 + T13 + T14 (taken names, remove project, add current directory, left bar toolbar, audit) | `make lint test`, `make gitleaks`, e2e `tsc` | Not run |

**What e2e can and can't reach.**
- Every scenario signs up its own account (`newAccount()`), because all M6 state is per account and must not leak between scenarios. The fixture that resets the saved layout is extended to leave `tree` and `theme` alone only for scenarios that seed them on purpose.
- **Saves are debounced**, so a scenario that reloads after a change first waits for the `PUT /api/ui-state/tree` (or `theme`) response. T2 adds `ui.waitForSave(key)` (a `page.waitForResponse` on that PUT). One scenario (T2 *Tree state survives an app restart*) deliberately reloads without waiting to prove the `pagehide` flush.
- **Restart** is the existing `ctl.restartApp()` (`docker restart hostbud-e2e-app`), followed by a reload once `/api/health` answers and the host banner is gone. No new ctl action is needed.
- **Ground truth for tmux** stays on the target: `list-windows -t '=<name>' -F …`, `display -p -t '=<name>' '#{window_index} #{pane_index}'`, `capture-pane -p`, and `list-clients -F '#{client_pid}'` (to prove a terminal did **not** re-attach when a row was collapsed, hidden, renamed or re-themed).
- **Theme:** `page.emulateMedia({ colorScheme })` drives `prefers-color-scheme`. T7 adds an e2e hook `window.__hostbud.termTheme(session?)` returning the terminal's current `{background, foreground}`. The first-paint check uses `page.addInitScript` to record `getComputedStyle(document.documentElement).backgroundColor` at the first `requestAnimationFrame`.
- **⌘ shortcuts:** Playwright on Linux can press `Meta+K`, but the app picks ⌘ vs Ctrl by platform (`navigator.platform`/`userAgentData`). Desktop scenarios use the Ctrl+Shift chords that work everywhere; the ⌘ variants are unit-tested with a faked platform and checked manually on a Mac (T14).
- Drag uses Playwright's `dragTo` as M4's scenarios do; keyboard reordering (Alt+↑/↓) is the more reliable e2e path and is used where the point is persistence rather than the drag itself.

---

## T1 — Windows and panes API

The server side for lazily loaded windows and panes (ARCHITECTURE §5.1: "Window/pane listing is fetched lazily when a session node is expanded"). Opening a session at a given window also needs a way to select it. Both are side-channel tmux commands, like M5's copy mode.

- `internal/tmux`, next to `RenameSessionArgs`/`KillSessionArgs`/`CopyModeArgs`:
  - `ListWindowsArgs(name string) ([]string, error)`: validates the session name with the existing rule (`^[A-Za-z0-9_-]{1,64}$`) and builds one exec: `list-windows -t '=<name>' -F '#{window_id}\t#{window_index}\t#{window_active}\t#{window_panes}\t#{window_name}'` `;` `list-panes -s -t '=<name>' -F '#{window_id}\t#{pane_id}\t#{pane_index}\t#{pane_active}\t#{pane_width}\t#{pane_height}\t#{pane_current_command}'` (tmux's `;` passed as its own shell-quoted argument). Free-form fields (window name, command) come **last** and are split with a field limit, so a tab in them can't shift columns. A line prefix (`W`/`P`) tells the two outputs apart. The pane's path is **not** listed: the tree doesn't need it, and it keeps paths out of the response.
  - `ParseWindows(out string) ([]Window, truncated bool, err error)`: windows in index order, each with its panes in index order. Control characters in names/commands become spaces; names are cut to 256 bytes. At most 256 windows and 64 panes per window, with `truncated` set.
  - `SelectArgs(name, windowID, paneID string) ([]string, error)`: `windowID` must match `^@[0-9]+$` and `paneID` (optional) `^%[0-9]+$`. It builds `select-window -t '=<name>:<windowID>'` and, with a pane, `select-pane -t '=<name>:<windowID>.<paneID>'`, followed by the same list commands, so the response carries the new state.
- **Membership check:** before selecting, the service lists the session's windows/panes (the same exec as the GET) and returns 404 if the window or pane isn't in that session. A `%id` from another session never reaches `select-pane`.
- `internal/api`:
  - `GET /api/machines/{machine}/sessions/{name}/windows` → 200 `{"windows": [{"id": "@1", "index": 0, "name": "…", "active": true, "panes": [{"id": "%1", "index": 0, "active": true, "command": "vim", "width": 80, "height": 24}]}], "truncated": false}`.
  - `POST /api/machines/{machine}/sessions/{name}/select` with body `{"window": "@n", "pane"?: "%n"}` → 200 with the same shape as the GET.
  - Both are authenticated, and the POST requires the Origin allowlist. Unknown machine → 404; unknown session (tmux "can't find session") → 404 with the standard `{error, hint}` shape; invalid name or ids → 400; tmux missing → M1's install hint. The default per-command timeout (10 s) through `sshx` applies, and cancellation kills the ssh process.
- **No event:** window and pane layout is tmux state that hostbud neither stores nor polls (the poller lists sessions only). Record this in ARCHITECTURE §9, as M5 did for copy mode, so it isn't mistaken for a missed event. Selecting a window changes what every attached client of that session shows; document this, as tmux does the same for `prefix n`.
- Info logs never include the session, window or pane names or the command. Debug logs may include the ids.
- ARCHITECTURE §5.1 (the list/select commands and caps) and §9 (the two routes and their response shape).

**Tests:** U (Go): `ListWindowsArgs`/`SelectArgs` table (exact argv, `=`-targets, invalid names, bad ids `@`, `@x`, `%`, `1`, `@1;x`); `ParseWindows` with a golden output (3 windows, a split window, a tab and a newline in a window name, an empty command, the 256/64 caps and `truncated`); membership check (pane from another window/session → 404, not an exec); handler auth (401), Origin on the POST (403 foreign, allowed local/domain), body validation (400), unknown machine/session (404), method table; log redaction (a window named after a secret-looking string never appears at info level). I (`test/sshd`): a session with 3 windows, window 1 split into 2 panes, `vim` in one pane: the GET matches `tmux list-windows`/`list-panes` run directly; select window 2, then window 1's second pane → `display -p '#{window_index} #{pane_index}'` agrees; a pane id from a second session → 404 and nothing changes; a window name with spaces and Unicode round-trips; a session killed between list and select → 404; the tmux-less target returns the install hint; a name with shell metacharacters is rejected before any ssh exec.

**E2E:** add API-level **(T1) Windows and panes API** in `windows.api.spec.ts` (desktop, [M6-acceptance.md](M6-acceptance.md#e2e-scenarios-make-e2e-simulated-user)): create the session layout on the target over SSH, list through Caddy and compare with the target, select a window and a pane and check `display -p`, a foreign pane id → 404, signed out → 401, foreign Origin on `select` → 403, unknown session → 404. Type-check only; don't run.

**Done:** both routes work against `test/sshd`, are authenticated (and Origin-checked for the POST), only build allowlisted tmux commands, never select outside the named session, and the scenario compiles.

## T2 — Tree state v2 and accessible tree

The foundation for every later customization: a versioned per-account state that holds more than order, pruning that can't lose data, and a real tree widget with keyboard support and persistent collapse.

**First, the M4 pruning bug (bug-fix workflow, before anything else in this task).** `stores/tree.ts` `sync()` rebuilds `order.sessions` from the current session list, and it's called on every live event (`stores/live.ts`) and once after sign-in before `live.start()` (`App.vue`). At those moments the list can be empty (no snapshot yet, or the empty first snapshot after an app restart) or stale (host unreachable), so the saved session order can be wiped and then saved. Commit a failing Vitest regression test first (sign-in `load()` → `sync()` with no sessions keeps `order.sessions`; a snapshot with machine status `unknown` or `unreachable` keeps it), then the fix: prune session-keyed entries only when the machine's status is `ok` (the same rule M3 uses for `closeEndedSessions`), and prune project-keyed entries only after `projects.load` succeeded. Record both commits in the Progress notes.

- **State format v2** (`lib/tree.ts`):
  ```ts
  interface TreeStateV2 {
    version: 2
    projects: string[]                    // project ids, manual order (M4)
    sessions: Record<string, string[]>    // group (project id | '__other__') → session names, manual order (M4)
    pinned: string[]                      // project ids (T6)
    hidden: { projects: string[]; sessions: string[] }   // session keys (T5)
    collapsed: string[]                   // project ids and '__other__' (default: expanded)
    expanded: string[]                    // session keys and window keys (default: collapsed) (T3)
    showHidden: boolean                   // (T5)
  }
  ```
  A session key is `<machineId>/<sessionName>`, and a window key `<machineId>/<sessionName>/<windowId>` (machine ids and session names can't contain `/`). `validateTreeState` accepts v1 (upgrades it: order kept, every new field empty) and v2, dedupes lists, rejects the M4 hostile cases (`__proto__`, oversize lists and strings) plus malformed keys, and returns `null` for anything else (→ empty state with a console warning, as today). Unknown future versions are also `null`. Later tasks fill `pinned`, `hidden`, `expanded` and `showHidden`; T2 defines, validates, persists and prunes them all, so no later task changes the format.
- **Pruning rules** (`sync()`): session names/keys are removed from `sessions`, `hidden.sessions` and `expanded` only for machines whose status is `ok` and whose current list lacks them. Project ids are removed from `projects`, `pinned`, `hidden.projects` and `collapsed` only once projects have loaded. Window keys are pruned by T3 when a window list for that session arrives.
- **Saving:** keep M4's 500 ms debounce, and add `flush()` on `pagehide` (a `fetch` with `keepalive: true`, like `layout.flush()`), so a reload right after a change keeps it. Before each PUT, measure the serialized size; if it's over 60 KiB after pruning, don't send it (console warning; the last saved value stays). `renameSession`, `setCollapsed`, `setExpanded` and the other mutators are store functions, so the tree component never edits the state directly.
- **Tree widget** (`SessionTree.vue`, reusing M4's `SessionList.vue` rows): the left bar becomes a WAI-ARIA tree: `role="tree"` labelled "Projects and sessions", with `treeitem`s for projects, Other sessions, sessions (and windows/panes in T3), `aria-level`, `aria-expanded` on items with children, `aria-selected` on the session of the focused pane, and `role="group"` for children. One roving `tabindex="0"` on the focused row. M4's drag handles and row actions (×, ⋯, pencil) stay, as buttons inside the row with `tabindex="-1"` (reachable by Tab from the row, per the tree pattern's "actions in a row" guidance), and M5's touch targets stay.
- **Keyboard** (only while a tree row has focus): ↑/↓ previous/next visible row; → expand, or move to the first child if expanded; ← collapse, or move to the parent; Home/End; Enter opens a session (M3 rules) or toggles a project/Other; **Alt+↑/↓** moves the focused project or session one place within its section and saves like a drag (a no-op at the section's edge). Shortcut keys added later (F2 in T4, `H` in T5, `P` in T6, Delete → the kill confirmation) are registered here as no-ops or wired to existing actions (Delete opens M1's confirmation dialog).
- **Collapse:** a chevron button (**Collapse <name>** / **Expand <name>**, `aria-expanded`) on projects and on Other sessions; clicking the project row's name also toggles it; state in `collapsed`. A collapsed group keeps its rows in the order, and collapsing never closes, detaches or resizes a terminal.
- **Row hierarchy styling:** project and Other rows become parent headers, distinct from session rows: a folder icon (Other: its own icon), the name in semibold, the project path in a smaller muted line (`~` for the home directory from the host's `$HOME` via the existing machine info, full path as `title`), and a tinted header background token (defined for both themes, so T7 needs no change). Children indent one step per `aria-level` (at least 12 px) with a thin vertical guide line on the group's left edge; drop M4's bordered box around each project and the uppercase "Other sessions" label. Selection highlight, drag handles, row actions and M5's touch targets are unchanged, and the phone drawer keeps session titles readable (at least 8 characters before truncation).
- **Hide an empty Other sessions group:** render the Other header only while it has at least one session (no "No tmux sessions yet." under it); it appears when a session lands there and disappears when the last one leaves or is saved as a project. Its saved collapse state and order are kept while it's hidden. When there are no projects and no sessions at all, the tree shows a single "No tmux sessions yet." empty state (M1 T15 *Empty list* keeps passing).
- **e2e helpers, same commit:** `test/e2e/helpers/ui.ts` locates rows through the new roles (`treeitem` with the name), keeps the existing method names so scenarios don't change, and adds `ui.treeItem(name)`, `ui.toggle(name)`, `ui.waitForSave(key)` and `api.putUIState(key, value)` / `api.getUIState(key)`. Every existing M1–M5 scenario that finds tree rows keeps its intent and only changes locators if the helper can't hide the difference. Check with `git status` whether another agent is editing a spec before touching it.
- ARCHITECTURE §11: the v2 format (replacing M4's v1 description), pruning rules, tree roles and keys, and the row hierarchy styling (project/Other headers, indent and guide line).

**Tests:** U (Vitest): the pruning regression tests (committed failing first); `validateTreeState` v1 upgrade, v2 round-trip, hostile/malformed input, unknown version; `sync()` prunes only for status `ok` and only after projects load; debounce, `pagehide` flush, the 60 KiB guard (500 projects × 20 sessions of 64-byte names stays under the limit after pruning; an artificially oversize state doesn't call `putUIState`); tree roles, levels, `aria-expanded`/`aria-selected`, roving tabindex (including after the focused row disappears); the key handler table, including Alt+↑/↓ at the edges; collapse toggle and persistence; collapsing doesn't touch the layout store; the project header renders icon, name, `~`-shortened path and a full-path `title`, session indent grows with `aria-level`, Other uses the header style without a path; the Other group isn't rendered with zero sessions and reappears with one, keeping its collapse state; with no projects and no sessions the tree shows one "No tmux sessions yet.". I (Go, PostgreSQL): extend the per-account UI-state integration test with a v2 `tree` value and with a v1 value read back byte-identical (the server stores it uninterpreted), plus the 64 KiB + 1 → 413 case for `tree`.

**E2E:** add in `tree.custom.spec.ts` (desktop): **(T2) Tree state upgrades from M4** (seed v1 through `api.putUIState`, reload, the order shows; reorder; `api.getUIState('tree')` is v2 with that order); **(T2) Tree state survives an app restart** (reorder sessions, reload at once without `waitForSave`, restart, the order is unchanged once the host is reachable); **(T2) Keyboard tree navigation** (arrows, Home/End, Enter opens, Alt+↓ moves, reload keeps it). Add **(T2) Collapse state persists** to `tree.custom.spec.ts` and `tree.custom.phone.spec.ts` (collapse a project and Other, reload, restart, still collapsed; a terminal open on a collapsed session keeps its `#{client_pid}`), and **(T2) Tree shows hierarchy** to both (header shows name and `~` path; session text starts right of the header text; header background differs from a session row; Other uses the header style), and **(T2) Empty Other sessions hidden** to `tree.custom.spec.ts` (with a project session only, no Other header and no "No tmux sessions yet."; start an unmatched session from the real terminal → Other appears within one poll interval; save it as a project → Other disappears). Update `helpers/ui.ts` as above. Type-check only.

**Done:** the pruning bug is fixed with its regression test; v2 state loads, upgrades, saves, flushes and prunes safely; the tree is keyboard-navigable with persistent collapse and shows its hierarchy visually; existing scenarios compile against the updated helpers.

## T3 — Windows and panes in the tree

Sessions expand to their windows, and split windows to their panes, loaded on demand from T1's route.

- **Store** (`stores/windows.ts`): a map from session key to `{status: 'idle' | 'loading' | 'ok' | 'error', windows, error}`. `ensure(key)` fetches once per expand; an in-flight request for a session that was collapsed, renamed or ended is dropped when it resolves. No request is ever made for a collapsed session.
- **Rows:** a session's chevron (**Expand <session>** / **Collapse <session>**) toggles it; its children are its windows (`<index>: <name>`, with tmux's current window marked from `window_active`; hostbud doesn't try to guess which window each browser terminal shows). A window with more than one pane has its own chevron and pane children (`Pane <index> — <command>`, active pane marked). While loading, one `Loading windows…` row with a spinner; on error, an actionable row (the API's message) with **Retry**. `truncated` shows a final "More windows not shown" row.
- **Freshness without polling:** while a session is expanded, re-fetch its windows (debounced 300 ms, one request in flight, one pending) when a `sessions.changed` event changes that session's `windows` count or `activity`, and whenever it's expanded again. Other sessions' changes don't trigger anything. A session leaving the list drops its entry. No timer runs while nothing is expanded.
- **Open at a window/pane:** clicking (or Enter on) a window row opens the session through `layout.open` (M3 rules: focus its tab or open one), then calls `POST …/select {window}`; a pane row sends `{window, pane}`. The response refreshes the rows. A 404 (window gone) shows a toast and refreshes. In compact layout (M5), this closes the drawer and shows the terminal. Selecting doesn't re-attach anything; tmux redraws the attached client.
- **Persistence:** expanded sessions and windows go in `expanded` (T2). On load, restored expanded sessions fetch their windows (and only those). Window keys that aren't in the refreshed list from a reachable host are pruned.
- Touch: chevrons and window/pane rows use M5's `touch-target` utility. The M5 long-press menu isn't extended here (T4–T6 do).
- ARCHITECTURE §11: window/pane rows, refresh rules, open-at-window.

**Tests:** U (Vitest): fetch once on first expand, none while collapsed; the loading and error rows, Retry; stale responses dropped (collapsed, renamed, ended); refresh triggers with fake timers (count change, activity change, unrelated session ignored, removal), the one-in-flight/one-pending limit; open-at-window calls `layout.open` then `select` with the row's ids, and the drawer closes in compact layout; 404 → toast and refresh; expand persistence, restore-on-load fetching only expanded sessions, stale window key pruning; touch-target classes on the new rows. I: T1's integration tests cover the server side; n/a beyond that (the client adds no server behavior).

**E2E:** add in `tree.windows.spec.ts` (desktop and, via `tree.windows.phone.spec.ts`, both phone projects where marked): **(T3) Windows load when a session is expanded** (record requests: none to `/windows` before expanding, one after; rows match the target's `list-windows`; the split window expands to its panes; reload and restart keep it expanded with rows shown) (desktop and `iphone-13-pro`); **(T3) Window rows follow the real terminal** (`tmux new-window -t '=<name>' -n extra` on the target → the row appears within one poll interval; `kill-window` removes it; no reload) (desktop); **(T3) Open at a window and pane** (print a marker in window 2 over SSH; click its row → `display -p '#{window_index}'` is 2 and the browser terminal shows the marker; click a pane row → `#{pane_index}` matches) (desktop and `iphone-13-pro`). Type-check only.

**Done:** sessions and split windows expand lazily, window rows follow the real terminal through events only, clicking a window or pane opens the session there, expansion persists, and the scenarios compile.

## T4 — Inline rename

Rename projects and sessions in place in the tree, replacing the rename dialog there.

- `InlineRename.vue`: replaces the row's name with an `input` (16 px font, `autocapitalize="off"`, `autocorrect="off"`, `spellcheck="false"`) with the current name selected. Enter or blur commits; Escape cancels. An unchanged or empty (after trim) value cancels without a request. A commit sends exactly one request even when Enter is followed by blur. Focus returns to the row afterwards (roving tabindex, T2).
- **Starting an edit:** the row's pencil (M4), F2 on a focused row, a double-click on the name with a fine pointer, the ⋯ menu's Rename and M5's long-press menu. From elsewhere (T9's palette), the tree reveals the row first: expand its project, un-collapse, scroll into view, and open the drawer in compact layout. A hidden row (T5) is shown while it's being edited.
- **Sessions:** validate with the server's rule (`^[A-Za-z0-9_-]{1,64}$`) before sending, with an inline message naming the allowed characters. Use the existing `PATCH /api/machines/:id/sessions/:name`. A server error (name taken, session gone, invalid) keeps the field open with the message below it (`aria-describedby`), and the old name stays shown. On success: M3 relabels every pane showing the session without re-attaching (its "renames in flight" rule), M4's link follows server-side, and `tree.renameSession(machine, old, new)` moves the name in `sessions`, `hidden.sessions` and `expanded` (including its window keys) in one change, so the row keeps its position, hidden and expanded state. Renaming in a real terminal still looks like an end and a new session (M3/M4 behavior; documented).
- **Projects:** trim, 1–255 bytes (the store's limit, measured with `TextEncoder`), using the existing `PATCH /api/projects/:id` `{name}`. Other browsers update from `projects.changed`. The path and placement never change.
- The Rename dialog (`RenameSessionDialog.vue`) is no longer opened from the tree. Keep it only if another entry point still needs it (e.g. the terminal header menu); otherwise remove it with its tests in this commit.
- Touch: the input and its (optional) ✓/✕ buttons meet M5's target size; the long-press menu gains **Rename**.
- ARCHITECTURE §11: inline rename and the re-keying rule.

**Tests:** U (Vitest): `InlineRename` start paths (pencil, F2, double-click fine pointer only, menu), Enter/blur/Escape, unchanged/empty cancel, one request for Enter-then-blur, focus return, error keeps editing with `aria-describedby`; session name validation messages; project name trim and byte length (multi-byte names); `tree.renameSession` re-keys order, hidden, expanded and window keys in one change; reveal-then-edit for a collapsed project and in compact layout (drawer opens); long-press menu Rename. I: n/a for new server behavior (none); M1's session rename and M4 T3's project rename and link integration tests stay authoritative.

**E2E:** add to `tree.custom.spec.ts` and `tree.custom.phone.spec.ts` (desktop and `iphone-13-pro`): **(T4) Inline rename a project** (pencil → type → Enter; a second page shows the new name without reload; start another edit and Escape cancels it; reload and restart keep the name; its sessions stay under it); **(T4) Inline rename a session** (F2 on desktop, ⋯ → Rename on the phone; the target has the new name; the open terminal's `#{client_pid}` is unchanged; the row keeps its position and its expanded windows; renaming to an existing session's name shows the inline error and keeps the old name). Type-check only.

**Done:** tree entry points rename projects and sessions in place; errors keep the edit open; renamed rows keep their position and state. T9 will connect the palette's reveal action to this editor.

## T5 — Hide and unhide

Let the user declutter the tree without touching tmux.

- **Hide:** the ⋯ menu (and M5's long-press menu) of a project or session gets **Hide**; `H` on a focused row does the same. A hidden project hides its whole group, including sessions that are placed in it later. Hidden sessions and projects go in `hidden` (T2). Hiding never kills, detaches or closes: open tabs of a hidden session stay open and attached, and the split picker (M3) and palette (T9) still list them.
- **Show hidden:** when anything is hidden, the tree header shows **Show hidden (n)** (a toggle button, `aria-pressed`). On, hidden rows render in their saved position, dimmed, with a "hidden" label in their accessible name and **Unhide** instead of Hide in their menu; `H` unhides. Off, they're gone again. The toggle is saved in `showHidden`.
- **Unhide** puts the row back in its saved place, since hiding never removed it from the order.
- **Focus:** hiding the focused row moves focus to the next visible row (or the previous one at the end), never to `body`.
- **Pruning:** a hidden session that ends leaves `hidden` under T2's rule (reachable-host lists only), so a later session with the same name isn't hidden by surprise. A hidden project that's deleted (not possible in v1 UI, but via the API later) is pruned after projects load.
- **Empty states:** when every row is hidden, the tree shows "Everything is hidden" with the Show hidden toggle, not the M1 "no sessions" message.
- ARCHITECTURE §11: hide semantics (per account, presentation only).

**Tests:** U (Vitest): `projectTree` with hidden projects and sessions (and a session placed into a hidden project later); the count; Show hidden rendering (dimmed class, accessible label, Unhide item); unhide restores position; focus moves to the neighbor; pruning of an ended hidden session only on a reachable-host list; hiding doesn't call the layout store or any session API; the empty state; long-press menu Hide/Unhide; touch-target classes. I: n/a (per-account presentation; T2's PostgreSQL round-trip covers storage, and nothing on the server changes).

**E2E:** add **(T5) Hide and unhide** to `tree.custom.spec.ts` and `tree.custom.phone.spec.ts` (desktop and `iphone-13-pro`): hide a session with an open terminal and a project; both leave the tree; the terminal keeps its `#{client_pid}` and the target still lists the session; Show hidden (n) shows them dimmed; Unhide returns them to their saved position; hide again, reload and restart → still hidden and the toggle's state kept; kill the hidden session on the target and create a new one with the same name → it's visible. Type-check only.

**Done:** projects and sessions hide and unhide per account without touching tmux or open terminals, the Show hidden toggle persists, and the scenario compiles.

## T6 — Pinned projects

Keep the projects the user works in most at the top, in their own manual order.

- **Pin/unpin:** the project's ⋯ menu (and M5's long-press menu) gets **Pin** / **Unpin**; `P` on a focused project row does the same. State in `pinned` (T2). The global `projects.pinned` column isn't written; ARCHITECTURE §8 notes it (with `sort_order` and `machines.hidden`) as reserved.
- **Sections:** pinned projects render first under a **Pinned** heading (a `group` in the tree, not a treeitem), in the manual order of `projects`; then the unpinned projects; then Other sessions. Pinning moves the project to the end of the Pinned section, unpinning to the end of the unpinned section (both by moving it within `projects`, so each section's relative order is still one list). Nothing else moves.
- **Reordering:** M4's drag and T2's Alt+↑/↓ work within a section only. A drag dropped across the boundary snaps back with no state change; Alt+↑/↓ stops at the section edge.
- A pin icon (**Pinned**, `aria-label`) shows on pinned rows; it's a button that unpins, with M5's touch target on coarse pointers.
- Hidden + pinned: a hidden pinned project is hidden (T5 wins); Show hidden shows it in the Pinned section.
- **The combined persistence scenario** lands here, since this is the last tree customization.
- ARCHITECTURE §11: sections and pin rules.

**Tests:** U (Vitest): `projectTree` sections and order; pin/unpin placement at the end of the target section with no other change; drag across the boundary rejected (`vue-draggable-plus` `move` callback returns false); Alt+↑/↓ stops at the edge; pin icon button unpins; hidden+pinned; stale pinned ids pruned only after projects load. U (Go): a store test that `PATCH /api/projects/:id` with a `pinned` field doesn't change `projects.pinned` (the field is ignored or the request rejected, whichever the current handler does, pinned down as a test). I: n/a (per-account presentation; T2's round-trip covers storage).

**E2E:** add to `tree.custom.spec.ts` and `tree.custom.phone.spec.ts`: **(T6) Pin projects** (desktop and `iphone-13-pro`: pin two projects → Pinned section in their manual order; unpin one → end of the unpinned section; a drag from Pinned into the unpinned section leaves the order unchanged); **(T6) Every customization survives reload and restart** (desktop and `iphone-13-pro`: on one account, drag to reorder, keyboard-move, rename a project and a session, hide a session, unhide another, pin a project, collapse Other, expand a session and a window; then reload **and** restart; every one of them is as the user left it); **(T6) Customizations are per account** (desktop: a second account signed in on another context sees default order, no pins, nothing hidden, nothing collapsed). Type-check only.

**Done:** pinned projects stay on top in their own manual order, sections can't be dragged across, every tree customization together survives reload and restart per account, and the scenarios compile.

## T7 — Theme setting

Dark / Light / System for the whole UI and the terminal, per account, live in System mode, and without a flash of the wrong theme (ARCHITECTURE §11).

- **Tokens** (`assets/main.css`): `:root` (dark) and `:root[data-theme='light']` define every `--hb-*` token. The `prefers-color-scheme` block goes away: the app always sets `data-theme` to the **resolved** theme (`dark` or `light`), so CSS has one source of truth. Audit components for hard-coded colors (drag ghost, splitpanes dividers, focus rings, scrollbars, toasts, dialogs and sheets, the M5 key bar and drawer backdrop) and move them to tokens. Add a lint check (a small node script in `make lint`) that fails on hex/rgb color literals in `.vue` files outside an allowlist (`lib/theme.ts`, `main.css`).
- **Terminal palettes** (`lib/theme.ts`): `darkTerminalTheme` (today's colors, completed with the 16 ANSI colors and selection) and `lightTerminalTheme`, each an xterm `ITheme`, plus the search-decoration colors for `TerminalSearch.vue` (its comment says the terminal is dark in every theme; that ends here). `TerminalView` applies the resolved palette at creation and sets `term.options.theme` on change for every mounted terminal (the WebGL renderer repaints; a full-screen program's screen is redrawn by xterm from its buffer, no re-attach). `color-scheme` on `<html>` follows too, so native scrollbars and form controls match.
- **Store** (`stores/theme.ts`): `mode: 'dark' | 'light' | 'system'` (default `system`), `resolved` from `mode` and `matchMedia('(prefers-color-scheme: dark)')`. A `change` listener applies only in System mode. Applying sets `document.documentElement.dataset.theme`, `color-scheme`, the `theme-color` meta's `content` (M5's two `media` variants become one meta the app updates) and the `localStorage` mirror.
- **Persistence:** `internal/api/uistate.go` adds `theme` to `uiStateKeys` (the comment already expects it). The value is `{version: 1, mode}`; the client validates it and treats anything else as System. It's loaded on sign-in next to `layout` and `tree`, before the app shell renders, and saved on change (no debounce needed; a single PUT per click, the last one wins). Sign-out keeps the mirror (it's per browser) and stops the listener.
- **No flash:** an inline, non-module `<script>` at the top of `web/index.html` `<head>`, before any stylesheet: it reads `localStorage['hostbud.theme']` (`dark`/`light`/`system`, in a `try`, since storage can throw), resolves `system` with `matchMedia`, and sets `data-theme` and `color-scheme` on `<html>`. It's under 1 KiB, holds no data, and makes no request. Its logic is also exported from `lib/themeBoot.ts` for unit tests; `check-dist` verifies the built `index.html` has the script before the first stylesheet link and that it's under 1 KiB. After sign-in the account's saved mode wins and updates the mirror. The sign-in screen and M5's unreachable screen therefore use the last mode this browser saw. ARCHITECTURE §11 notes that M7's CSP, if added, must allow this script by hash.
- **UI:** a **Theme** radio group (Dark, Light, System) in the account menu (M4's header menu; M5's compact **Account** menu), using Reka UI `DropdownMenuRadioGroup`. T9 adds palette actions for it.
- **e2e hook, same commit:** `window.__hostbud.termTheme(session?)` → `{background, foreground}` of the terminal's current theme (only in `VITE_E2E=1` builds, like the other hooks).
- README: the theme setting. ARCHITECTURE §8 (the `theme` key in the `ui_state` comment), §9 (allowlist) and §11.

**Tests:** U (Go): `uiStateKeys` has `theme`; PUT/GET `theme` 204/200; 401 signed out; 403 foreign Origin; 400 invalid JSON; 413 oversize; unknown keys still 404. U (Vitest/node): `resolve(mode, prefersDark)` table; the store applies `data-theme`, `color-scheme`, `theme-color` and the mirror; the media listener applies only in System mode and is removed on sign-out; the radio group; `TerminalView` sets `term.options.theme` on change and `TerminalSearch` switches decoration colors; `lib/theme.ts` contrast tests (WCAG relative luminance: foreground/background ≥ 7:1; ANSI colors other than black/white variants ≥ 3:1 on their background; selection keeps text ≥ 4.5:1) for both palettes; UI token contrast (text ≥ 4.5:1 on `bg` and `surface`, `muted` ≥ 4.5:1, borders/icons ≥ 3:1) for both themes; `themeBoot` with the mirror present, absent, invalid and with storage throwing; `check-dist` accepts the built boot script and rejects a missing/late/oversize one; the hex-color lint check passes on the tree and fails on a fixture. I (Go, PostgreSQL): per-account `theme` round-trip; two accounts are isolated.

**E2E:** add in `theme.spec.ts` (desktop) and `theme.phone.spec.ts` (both phone projects; `iphone-13-pro` named in the checklist): **(T7) Pick Dark and Light** (with a terminal open, pick Light then Dark in the account menu; each time `<html data-theme>`, the computed body background, the `theme-color` meta and `termTheme().background` change at once; `#{client_pid}` unchanged; a Light-mode trace screenshot of the tree, a dialog and the terminal is kept as an artifact) (desktop and `iphone-13-pro`); **(T7) Theme persists** (Light survives reload and restart; a second account signed in on another context still starts in System; a foreign-Origin `PUT /api/ui-state/theme` → 403) (desktop); **(T7) System follows the OS** (System selected; `emulateMedia({colorScheme: 'light'})` → light UI and terminal without reload; `'dark'` → dark; pick Dark, emulate light → stays dark) (desktop and `iphone-13-pro`); **(T7) No flash of the wrong theme** (save Light, emulate dark, `addInitScript` records the root background at the first animation frame, reload → it's the light token) (desktop). Type-check only.

**Done:** the three modes work for the UI and every terminal, persist per account, follow the OS live in System mode, never flash the wrong theme, both palettes pass contrast checks, and the scenarios compile.

## T8 — Keyboard shortcuts

One registry, a small conflict-free set of global chords, and a help dialog. The command palette (T9) uses the same registry.

- **Registry** (`lib/shortcuts.ts`): each entry has an `id`, a label, a group (General, Tabs, Tree), a scope (`global`: works even with the terminal focused; `outside-terminal`: not while typing in the terminal or a text field; `tree`: only on a focused tree row), and its chords per platform (`mac` / `other`). Formatting (`⌘⇧K` vs `Ctrl+Shift+K`) is a pure function. The platform comes from `navigator.userAgentData?.platform ?? navigator.platform`, faked in tests.
- **The set:**
  - General: Command palette — ⌘K (mac), Ctrl+Shift+K (all), Ctrl+K (`outside-terminal` only). Keyboard shortcuts — ⌘/ (mac), Ctrl+Shift+/ (all), `?` (`outside-terminal`). Focus tree ↔ terminal — ⌘⇧E (mac), Ctrl+Shift+E (all): from the terminal it focuses the tree's current row (opening the drawer in compact layout); from the tree it focuses the active terminal.
  - Tabs: Next tab — Ctrl+Shift+] ; Previous tab — Ctrl+Shift+[ (the same on macOS: ⌘⇧]/[ are Safari's and Chrome's own tab switches and can't be taken). Last tab — Ctrl+Shift+D (Ctrl on macOS too, never ⌘): switches to the previously selected tab, so pressing it again returns, like Alt+Tab between two windows. The layout store keeps a most-recently-selected list of tab ids (in memory, not persisted): every way of activating a tab (click, the chords, the palette, opening a session) moves it to the front, and closing a tab drops it, so the chord goes to the most recent tab that's still open. With one tab, or before a second tab has been selected since load, it does nothing. Handling it `preventDefault()`s Chrome's and Firefox's own Ctrl+Shift+D (bookmark all tabs).
  - Tree (from T2/T4/T5/T6): arrows, Home/End, Enter, Alt+↑/↓, F2 rename, Delete kill (confirmation), `H` hide/unhide, `P` pin/unpin, `N` new session in the focused project (or the default path for Other).
- **Rules, enforced by a unit test over the registry:** `global` chords use ⌘ or Ctrl+Shift only; no plain Ctrl+letter, Alt+letter, function key or bare key in `global`; no duplicates within a scope; no collision with M3's keys (Ctrl/⌘+Shift+C/V/F, ⌘C/⌘V, the Mac editing keys in `terminalKeys.ts`); none of the browser-reserved chords (Ctrl+T/W/N/Tab, Ctrl+Shift+T/W/N/Tab, Ctrl+PageUp/PageDown).
- **Dispatch:** one `keydown` listener on `window` in the capture phase for `global` chords (so xterm doesn't see them), and xterm's `attachCustomKeyEventHandler` returns `false` for them, as M3 does for copy/paste. `outside-terminal` and `tree` scopes are handled only when the event target isn't the terminal's textarea or an `input`/`textarea`/`contenteditable` (and for `tree`, only on a tree row). Handled events `preventDefault()`.
- **Help dialog** (`ShortcutsDialog.vue`, **Keyboard shortcuts**): Reka UI `Dialog` listing every registry entry by group with the current platform's keys (`<kbd>`), noting "Works in the terminal" for `global` ones; Escape closes and restores focus. It says shortcuts aren't customizable yet.
- Phones: nothing here needs a hardware keyboard; the help dialog is reachable from the palette (T9). With an iPad hardware keyboard, the same chords work.
- README: list the global chords and point to the in-app **Keyboard shortcuts** dialog for the rest (no generated table). ARCHITECTURE §11: the registry and scoping rules; §6 *Keys* gains a line that global chords are taken before xterm sees them.

**Tests:** U (Vitest): the registry rule test (fails on a fixture that adds Ctrl+K as `global`, a duplicate, Ctrl+Shift+C, or Ctrl+T); platform formatting; dispatch by scope and target (terminal textarea, an input, a tree row, the body); xterm's custom key handler returns `false` only for `global` chords (Ctrl+K and Alt+B pass through); Ctrl+Shift+]/[ cycle tabs with wrap-around and do nothing with one tab; the most-recently-selected list (every activation path moves a tab to the front, closing drops it) and Ctrl+Shift+D toggling between the two most recent tabs, falling back past a closed one, a no-op with one tab, `preventDefault()` called, and ⌘⇧D not bound; focus tree ↔ terminal including the compact drawer; the help dialog renders every entry, groups, platform keys and restores focus; `terminalKeys.spec.ts` unchanged and green. I: n/a (frontend only; no server behavior).

**E2E:** add in `palette.spec.ts` (desktop): **(T8) Keyboard shortcuts help** (Ctrl+Shift+/ opens it; so does `?` with a tree row focused; it lists the registry's labels; Escape closes and focus returns to where it was); **(T8) Switch tabs from the keyboard** (three tabs, last focused: Ctrl+Shift+] wraps to the first, Ctrl+Shift+[ wraps back; typing after each switch reaches that tab's shell, checked with `capture-pane`; with one tab the chords do nothing and send nothing); **(T8) Toggle to the last tab** (three tabs; select tab 1, then tab 3 by click: Ctrl+Shift+D focuses tab 1, again focuses tab 3, typing after each reaches that tab's shell (`capture-pane`); close tab 1, then Ctrl+Shift+D goes to the next most recent open tab; with one tab it does nothing and sends nothing to the shell); **(T8) Shortcuts don't reach the program** (vim in tab 1 in insert mode with some text, a shell in tab 2: Ctrl+Shift+] / [ switch tabs, and `capture-pane` shows vim's buffer and mode unchanged; Ctrl+Shift+E focuses the tree row of the active session, and again returns to the terminal where typing reaches the shell). Type-check only.

**Done:** the registry is the single source for shortcuts, global chords never take a key the terminal needs, the help dialog lists them, and the scenarios compile.

## T9 — Command palette

Jump to any session, window or project, and run any app action, from one box.

- `CommandPalette.vue`: Reka UI `Dialog` + `Combobox` (listbox pattern), labelled **Command palette**, with an input ("Type a session, project or command…"), results grouped (Sessions, Windows, Projects, Actions) and each action's shortcut from the T8 registry. Opened by T8's chords and a **Command palette** header button (visible on touch screens and in compact layout, with M5's touch target and a 16 px input). ↑/↓ move, Enter runs, Escape closes and restores the previous focus (the terminal, if it had it). Results are capped at 50.
- **Sources:**
  - Sessions (all live sessions, hidden ones marked "hidden"): open or focus (M3 rules), then focus its terminal.
  - Windows of sessions whose window list is loaded (T3's store; the palette doesn't fetch windows itself): open at that window.
  - Projects: reveal in the tree (un-collapse, expand, scroll into view, open the drawer in compact layout) and focus the row.
  - Actions: New session, New session in <project>, Browse files, Rename <row> (starts T4's inline edit), Hide/Unhide <row>, Pin/Unpin <project>, Collapse all, Expand all, Show hidden / Hide hidden, Split right, Split down, Close tab, Next/Previous tab, Theme: Dark / Light / System, Keyboard shortcuts, Sign out. **Kill <session>** is listed and opens M1's confirmation dialog; nothing destructive runs from the palette directly.
- **Matching** (`lib/fuzzy.ts`, pure): case-insensitive subsequence over the label (and a session's project name as a secondary field), scored with bonuses for word starts (`-`, `_`, `/`, space, camelCase) and contiguous runs; ties keep the list order (tree order for sessions and projects, registry order for actions). No recency ranking, in line with the no-auto-sort rule. An empty query lists sessions in tree order, then actions.
- Actions call the **same store functions** the tree and header use (no second code path): e.g. Hide calls `tree.hide`, Theme calls `theme.set`, New session opens the existing dialog/sheet.
- In compact layout, running an item that shows a terminal closes the palette and the drawer and doesn't re-attach other terminals (M5 rule).
- README: the palette. ARCHITECTURE §11: the palette's sources, matching and focus rules (replacing the one-line "Command palette (⌘/Ctrl-K)").

**Tests:** U (Vitest): `fuzzy` ranking table (word start beats mid-word, contiguous beats scattered, stable ties, no match excluded, Unicode case-folding), the 50 cap; sources (hidden marker, windows only when loaded, projects reveal); each action dispatches to the tree/theme/layout store function (spied), including Rename revealing the row and starting T4's inline editor, and Kill opens the confirmation instead of calling the API; opening from each chord and the header button; Escape restores the previous focus; Enter on a session focuses its terminal; compact layout closes the drawer; the header button's touch-target class and the 16 px input. I: n/a (frontend only; actions reuse existing API calls whose integration tests exist).

**E2E:** add in `palette.spec.ts` (desktop) and `palette.phone.spec.ts` (both phone projects): **(T9) Palette opens without stealing Ctrl+K** (desktop: type `echo abcdef` in a shell, move the cursor left 3, Ctrl+K → `capture-pane` shows `echo abc`; Ctrl+Shift+K opens the palette); **(T9) Palette jumps to a session** (desktop and `iphone-13-pro`: with two tabs open, type part of the other session's name, Enter → its tab is focused and typing reaches it; open again and Escape → focus back in the terminal); **(T9) Palette runs actions** (desktop: Theme: Light applies; Hide <session> then Unhide <session>; New session in <project> lands under that project; Kill <session> shows the confirmation and Cancel leaves it alive on the target); **(T9) Palette rename reveals and edits a row** (desktop and `iphone-13-pro`: a Rename action for a session in a collapsed project expands the project, opens the drawer on the phone and focuses the same inline editor); **(T9) Palette on the phone** (both phone projects: the header button opens it; picking a session closes it and the drawer and shows the terminal). Type-check only.

**Done:** the palette finds sessions, windows, projects and actions with stable fuzzy matching, runs everything through the existing store functions (kill still confirms), respects the terminal's Ctrl+K, works on phones, and the scenarios compile.

## T10 — Taken session names get a number

Creating a session with a name that's already taken no longer fails: the new session gets the first free `<name>-<n>`, the same numbering auto-derived names already use (`work` → `work-1` → `work-2`, …). The user sees which name it got.

- **Server** (`session.Service.Create`, the single creation function, so every entry point gets it): a typed name that the inventory already lists becomes `uniqueName(name, sessions)`; one that tmux still reports as `duplicate session` (created meanwhile) retries with `nextName`, up to the same 20 attempts auto names use. The base is trimmed so the result stays within the 64-character name limit (`abc…xyz-1`), and the result still passes `tmux.ValidateName`. A name that's invalid is still refused, not numbered. The response already returns the actual name. **Rename is unchanged:** renaming to a taken name still answers 409 "a session named … already exists", because silently renaming to something else would surprise.
- **UI:** every create entry point (New session dialog, New session here, the split picker's New session…, the palette's New session actions) already opens the returned name. When it differs from the typed name, an info toast says `Named "work-1": "work" was already taken.` (auto-derived names don't toast).
- **Supersedes M1's duplicate-name error on create** (M1 criterion *Duplicate name or a non-existent path*, M1 T11/T16): update the tests that expect it: `session_test.go` (the duplicate case in the error-mapping table becomes a numbering case; rename's duplicate test stays), `integration_test.go` (create `dup` twice on test sshd → the second is `dup-1`), `SessionDialogs.spec.ts` (the 409 fixture becomes a missing-path error), and the e2e *Invalid input* scenario in `actions.spec.ts` (the duplicate part now expects a tab named `<dup>-1`). A missing path still shows its actionable error.
- ARCHITECTURE: §9's session-create response notes the numbering and that rename still returns 409; README's session section mentions it.

**Tests:** U (Go, fake executor): a taken typed name → `-1`, `-1` taken too → `-2`; `work-1` taken → `work-1-1` (numbering appends to the typed name, it never reinterprets a suffix the user typed); tmux `duplicate session` on the first attempt → retries with the next suffix, gives up after 20 with the 409; a 64-character taken name is trimmed so the result is ≤ 64 and valid; invalid names still refused; rename to a taken name still `CodeDuplicate`. U (Vitest): the info toast appears only when the returned name differs from the typed one (not for an empty name), from the New session dialog and New session here. I (Go, test sshd): create `dup` twice → the second session exists as `dup-1` in real tmux; create in a project with a taken name → `dup-2`, linked to that project.

**E2E:** add in `sessions.spec.ts` (desktop): **(T10) Taken name gets a number** (a target session `<n>` exists; New session with name `<n>` → a tab `<n>-1` opens, the toast names it, and the target has both sessions; New session here in a project with the same name → `<n>-2` under that project); update M1's **(T16) Invalid input** in `actions.spec.ts` as above. API-level in `api.api.spec.ts`: **(T10) Create with a taken name** (`POST /api/machines/host/sessions` twice with the same name → 201 both times, the second response names `<n>-1`; renaming another session to `<n>` → 409). Type-check only.

**Done:** creating with a taken name always succeeds with a visible, numbered name from every entry point; rename keeps its duplicate error; the updated M1 tests and the new scenarios compile.

## T11 — Remove a project

Hide (T5) only hides a project for one account. This task deletes one: the project row goes for **every** account (projects are shared, not per account), with its session links and recent start commands. Files on disk and tmux sessions are never touched.

- **Server:** `DELETE /api/projects/:id` → 204, through `projects.Service.Delete` using the existing `store.DeleteProject`. The schema's `ON DELETE CASCADE` removes the project's `session_links` and `recent_commands`, so **no migration**. Unknown id → 404; signed out → 401; foreign Origin → 403. It publishes `projects.changed` with action `deleted`, and then re-places the machine's sessions, so the sessions that were in the project move right away (not at the next poll) to the project with the next-longest matching path, or to Other sessions.
- **UI:** **Remove project…** in the project row's ⋯ menu, the M5 long-press menu, and the palette (*Remove project <name>*); Delete on a focused project row opens the same dialog. The confirmation says what happens: "Remove project <name>? Its N sessions keep running and move to Other sessions (or <project>). Files in <~path> aren't touched. This removes it for every account." Cancel changes nothing. On success the tree drops the header, and the sessions appear in their new group, appended (no re-sort). Open tabs and splits stay attached. Project-keyed tree state (order, pinned, hidden, collapsed) is pruned by the existing rule after `projects.load`. If a New session here dialog or picker is open for that project, it closes with a toast.
- The file browser's add/open icon for that path turns back into *Add as project*. Adding the same path again later creates a fresh project (new id, no recent commands).
- ARCHITECTURE §5.3 (placement after a delete), §9 (the route) and §11 (the menu item and dialog); README's projects section: Hide vs Remove.

**Tests:** U (Go): service delete publishes `deleted` and re-places; 404 for an unknown id; handler 401/403/404/204 (Go). U (Vitest): the confirmation dialog's text (session count, target group, path), Cancel sends nothing, success drops the project and moves its sessions without re-attaching, an open New session here dialog for it closes, the menu item in the ⋯ menu, long-press menu and palette, Delete on a focused project row. I (Go, PostgreSQL + test sshd): deleting a project removes its links and recent commands but not other projects' rows; a linked session is re-placed under a parent project or Other sessions and still exists in tmux.

**E2E:** add in `projects.tree.spec.ts` (desktop) and `tree.custom.phone.spec.ts` (`iphone-13-pro`, via long-press): **(T11) Remove a project** (a project with a running session open in a tab; Remove → Cancel leaves it; Remove → confirm: the header is gone, the session is under Other sessions, the tab keeps its tmux client PID, the directory still exists on the target; reload and restart → still removed; add the same folder again from the browser → a fresh project with no recent commands). API-level in `projects.api.spec.ts`: **(T11) Delete project API** (signed out → 401, foreign Origin → 403, unknown id → 404, delete → 204 and a second delete → 404; `GET /api/projects` no longer lists it). Type-check only.

**Done:** a project can be removed from the tree after a confirmation, for every account; its sessions keep running and are re-placed at once; nothing on disk or in tmux changes; the scenarios compile.

## T12 — Add the current directory as a project

The file browser has add/open icons only on child-folder rows, so the folder you're in (your home folder, for example) can't be added without going to its parent.

- **UI:** a button in the browser's path bar for the directory being shown: **Add this directory as project** (FolderPlus) or, when it's already a project, **Open project** (FolderOpen), with matching accessible names and tooltips. It goes through the same `openProject` logic as the row icons: an existing project is selected instead of duplicated, the name defaults to the last path component (`/` stays `/`), and *New session here* then works for it. It updates when you navigate, and it's disabled while the listing is loading or failed.
- No server change: it uses `POST /api/projects` as it is.
- README's browsing section; ARCHITECTURE §7's browser UI line.

**Tests:** U (Vitest): the button shows Add or Open depending on whether the shown path is a project, and changes after navigating; clicking it creates the project with the shown path, or selects the existing one without a second POST; disabled while loading and on error; the touch-target class. I: n/a (frontend only; `POST /api/projects` has its integration tests from M4).

**E2E:** add in `projects.browser.spec.ts` (desktop and `iphone-13-pro`): **(T12) Add the current directory as project** (open the browser at home, click *Add this directory as project* → the home project appears in the tree with a `~` path, and the button now reads *Open project*; navigate into a folder and add it the same way; *New session here* starts a session in that folder, placed under it; clicking the button again doesn't create a duplicate). Type-check only.

**Done:** the directory being browsed can be added or opened as a project from the path bar, on desktop and phone, and the scenarios compile.

## T13 — Left bar toggle and icon toolbar

The left bar loses its text titles and gets one icon toolbar. Every icon is a semantic `lucide-vue-next` icon with an accessible name and a tooltip naming the action.

- **Remove the titles:** the header's text **Projects** button and the left bar's **Projects & sessions** heading go. The `aside` keeps `aria-label="Sessions"` so the region is still named for screen readers. The phone drawer's visible "Project tree" title becomes `sr-only` (the dialog still has its accessible title).
- **Left bar toggle:** one icon button in the app header, left of the `hostbud` name: `PanelLeftClose` ("Hide sidebar") while the left bar is open, `PanelLeftOpen` ("Show sidebar") while it's closed, with `aria-expanded` and `aria-controls` pointing at the left bar. On desktop it toggles the sidebar (the existing `app.sidebarOpen`, persisted as today). In compact layout it opens the drawer, and the drawer's close button uses `PanelLeftClose` too ("Hide sidebar"). Swipe-to-close and Escape still work. No shortcut is added here: T8 owns shortcuts.
- **Icon toolbar at the top of the left bar** (desktop sidebar, compact tree screen and drawer alike): **New session** as an icon button (`SquareTerminal`, accessible name "New session", opens the existing dialog) and **Add project** (`FolderPlus`, accessible name "Add project", opens the file browser dialog, which keeps its "Browse files" title). The header's separate Browse files button is removed, so there is one entry point. This supersedes M4's "FolderPlus header button, outside the left gutter" rule; the browser still opens in its modal dialog, never inside the sidebar.
- Icons use `currentColor` and the theme tokens (T7), are 18 px inside a 44×44 px target on coarse pointers (`touch-target`), show a visible focus ring, and have no text next to them. Tooltips use the `title` attribute like the existing icon buttons.
- Update the tests and e2e helpers that find these controls by their old names (`Projects`, `Show project tree`, `Browse files` in `helpers/ui.ts` and the M4/M5 specs) in the same commit.
- ARCHITECTURE §7 (where Browse files opens) and §11 (header and left bar layout); README's screenshots/wording if they name the old buttons.

**Tests:** U (Vitest): no "Projects" or "Projects & sessions" text renders; the toggle's icon, name and `aria-expanded` follow the open state and it toggles the sidebar on desktop and opens the drawer in compact layout; the drawer title is `sr-only`; New session and Add project render as icon-only buttons with accessible names and titles, and open the create dialog and the file browser; the header has no Browse files button; touch-target classes. I: n/a (frontend only).

**E2E:** add in `layout.wide.spec.ts` (desktop) and `mobile-layout.phone.spec.ts` (both phone projects): **(T13) Left bar toggle and toolbar** (desktop: no "Projects & sessions" or "Projects" text; *Hide sidebar* hides the left bar, the button becomes *Show sidebar*, reload keeps it closed, *Show sidebar* brings it back; *New session* icon opens the dialog and creates a session; *Add project* icon opens the file browser; phones: the toggle opens the drawer, the drawer's *Hide sidebar* closes it, and both icon buttons meet the touch-target size). Update the M4/M5 scenarios that used the old button names. Type-check only.

**Done:** the left bar has no text titles, one icon toggle opens and closes it on desktop and phone, New session and Add project are icon buttons with accessible names, and the updated tests and scenarios compile.

## T14 — Docs, audit and release

- README: *Customizing the tree* (drag and Alt+↑/↓ order, collapse, windows and panes, inline rename, hide and Show hidden, pins; all per account; renaming in a real terminal looks like a new session), *Command palette*, *Keyboard shortcuts* (the global chords and the `?` dialog; Ctrl+K stays the shell's inside the terminal), *Theme* (Dark / Light / System, per account, the per-browser mirror on the sign-in screen).
- ARCHITECTURE: reconcile §5.1 (window/pane listing and select), §8 (the `ui_state` comment for `tree` v2 and `theme`; `projects.pinned`, `projects.sort_order` and `machines.hidden` reserved and unused by the UI), §9 (the two routes, the `theme` key, the no-event notes), §11 (tree v2, tree roles and keys, windows, rename, hide, pins, theme and boot script, shortcuts registry, palette) and §13.1 (`termTheme()` hook, `waitForSave`, per-scenario accounts) with what was built. ROADMAP only if scope moved.
- **Audit:** every criterion in [M6-acceptance.md](M6-acceptance.md) has its U/I/E line with the right task, and every E item exists in the named spec file, is tagged, and type-checks. No new file in `internal/store/migrations/`. `.env.example` unchanged (or has any new variable with a placeholder). The security checklist items touched in M6: auth and Origin on the two new routes and on `PUT /api/ui-state/theme`; no session/window names, commands or paths in info logs; no external assets (the boot script is inline, and fonts stay bundled); destructive actions (kill) still confirm from every new entry point (tree Delete key, palette).
- **Extend M5's phone checks:** add the M6 controls (session/window chevrons, window and pane rows, the Pinned icon, Show hidden, inline rename input, the palette button and input, the theme radio items) to M5's *Touch targets* and *Usable without zoom* scenarios as **(T14) Touch targets and zoom for M6 controls** (both phone projects), and confirm the long-press menu has Rename, Hide/Unhide and Pin/Unpin.
- CP5: `make lint test`, `make gitleaks`, e2e `tsc`. Don't run e2e.
- *(host)* `make deploy`; `/api/health` is ok; through the loopback port, `GET /api/ui-state/theme` without a session answers 401 (the route is deployed and protected), and the windows route of a throwaway session answers 401 too. Don't use the owner's credentials or script a sign-in: the signed-in checks (theme 404 then 200 after a pick, expanding a session shows its windows) are owner manual checks, already covered by T7/T3's integration tests and e2e scenarios. Never kill, detach, rename or re-select windows in the owner's existing sessions: use only a throwaway session created for the check, and kill it only through the UI's confirmation dialog.
- *(host)* Owner's manual checks on a desktop browser (a Mac if available) and the iPhone, listed in [M6-acceptance.md](M6-acceptance.md#manual-checks-owner-t14); record the result of each there. If the owner hasn't done them yet, list them as open in the summary rather than ticking them, and don't wait for them: they don't block T14, T15 or M7.
- Summary to the owner: what changed, env vars (expected: none), manual steps (none on the host beyond `make deploy`; the theme and tree state start at their defaults for each account).

**Tests:** none new beyond regressions found by the audit, plus the extended phone touch-target scenario above.

**E2E:** add **(T14) Touch targets and zoom for M6 controls** (both phone projects) as above; audit that all T1–T13 scenarios are present, tagged and type-checked; none run (M7).

**Done:** docs match behavior, the deploy serves M6, the checklist is complete except the M7 e2e run and any open owner checks (backlog, not blockers), and the summary has been delivered.

## T15 — Safe Docker cleanup

Free the disk the milestone's builds used, **without touching the running deployment, its data, other projects, or work another agent may be doing at the same time.** This is the last step of the milestone, after T14's deploy and checks.

1. **Check that nothing is in use.** If any of these hold, skip the cleaning (steps 3–5), record "cleanup skipped: <reason>" in Progress and the summary, and treat the task as done. Cleanup can run again later:
   - a `make` test/lint/build or e2e run is in progress from this or another session (`pgrep -af 'scripts/tool.sh|docker exec hostbud-tools|test/e2e/run.sh|docker compose .*hostbud'`);
   - a `hostbud-tools-*` container is running a process other than its idle entrypoint (`docker top <container>` for each one listed by `docker ps --filter label=hostbud.tools=1`);
   - the `hostbud-e2e` stack is up (`docker compose -p hostbud-e2e ps -q` is non-empty). E2E is paused until M7, so it shouldn't be; if it is, someone is using it;
   - a `docker build` or `docker compose build` for hostbud is running (`pgrep -af 'docker (compose )?build'`).
2. **Record the before state:** `docker system df` and `docker compose ps` (the production stack: `hostbud`, `hostbud-caddy`, `hostbud-postgres` must be running and healthy before and after), plus `docker volume ls --filter name=hostbud`.
3. **Clean with the repo's own target:** `make docker-clean` **without** `CACHE=1`. It removes only hostbud's own disposable artifacts: the e2e stack and its images (`hostbud-e2e-*:local`), the toolbox containers labelled `hostbud.tools=1` (recreated on the next `make`, at a few seconds' cost), and dangling images labelled `hostbud.image=1`. Before running it, read the `docker-clean` recipe in the Makefile and confirm it still matches this list. If it has grown to remove anything else, don't run it: record the difference in Progress and the summary as an open owner item and finish the rest of the task.
4. **Never, in this task:**
   - `docker system prune`, `docker volume prune`, `docker image prune -a` without the `hostbud.image=1` label filter, `docker builder prune` (all projects' build cache; `CACHE=1` does this), or `docker network prune`;
   - removing the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data` or `hostbud-caddy-config`, or anything with another project's prefix;
   - stopping, recreating or removing the production containers, or removing the images they run from;
   - removing the toolbox *images* (`hostbud-toolbox-*`), the `test/sshd` integration targets (`make test-down` is not part of cleanup; the owner keeps them warm for speed), or anything under `.cache/`, `data/` or `backups/`.
   Build cache and the rest go only if the owner asks explicitly, as a separate step.
5. **Verify after:** `docker compose ps` shows the same three production containers still up and healthy; `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` answers `{"status":"ok"}`; `docker volume ls --filter name=hostbud` still lists the four production volumes; the `hostbud-test-sshd` containers are still there if they were before; `docker system df` again. Report the space reclaimed (before/after).
6. Commit only the Progress table update (this task changes no code).

**Tests:** n/a (operations only, no behavior change). The post-cleanup health check and `docker compose ps` above are the verification.

**E2E:** n/a: nothing reachable changes, and e2e runs are paused until M7.

**Done:** hostbud's disposable Docker artifacts are removed, the deployment and every volume are intact and healthy, nothing outside hostbud was touched, and reclaimed space is reported.
