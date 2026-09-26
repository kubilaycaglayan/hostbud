# M3 — Terminal workspace: tasks

Goal: the terminal feels like a desktop terminal app. Mac editing keys, copy and paste (with OSC 52), terminals that re-attach on their own after a network drop, clickable links, search, and several sessions open at once in tabs and splits, with a layout that survives reload.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m3--terminal-workspace) · checklist: [M3-acceptance.md](M3-acceptance.md).

## Progress
Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Mac editing keys | ✅ done |
| T2 Copy and paste | ✅ done (owner checks in T9) |
| T3 Auto-reconnect | ✅ done (owner check in T9) |
| T4 Links | next |
| T5 Search | |
| T6 UI state API | |
| T7 Tabs | |
| T8 Split view | |
| T9 Docs, audit and release | |

The owner asked for T1 and T2 first (after M2). The per-terminal features (T2–T5) come before the multi-terminal layout (T6–T8), so each is built and tested on the single terminal view before the view is multiplied.

Same rules as M1/M2: work top to bottom. Each task ends with a green `make lint test` **and `make e2e`**, a clean `make gitleaks`, and its own conventional commit(s). Every task has a **Tests:** line (unit and integration tests it owes) and an **E2E:** line (scenarios it adds, tagged with the task in [M3-acceptance.md](M3-acceptance.md#e2e-make-e2e-simulated-user)), all landing in the same commit as the behavior. Tasks marked *(host)* need the real host to verify. When a task moves the design, it updates ARCHITECTURE in the same commit.

**What e2e can and can't reach.**
- Clipboard: Playwright grants `clipboard-read`/`clipboard-write` only in Chromium, so clipboard scenarios run in `desktop-chromium`. The phone and a real macOS keyboard stay manual checks.
- The browser in e2e has no internet. Link scenarios intercept the opened page with `context.route` and assert its URL.
- Network cuts: `hostbud-e2e-ctl` gets two new fixed actions (T3) that disconnect and reconnect `hostbud-e2e-app` from the `hostbud-e2e` network (keeping its `hostbud` alias). TCP connections then hang rather than close, which is the case liveness detection exists for.
- Terminal setup on the throwaway target (tmux `mouse on`, `set-clipboard on`) is done per scenario with `tmux set` over SSH. It happens only on `hostbud-e2e-target` and never on the real host.

---

### T1 — Mac editing keys
- `web/src/lib/terminalKeys.ts`: a pure mapping from a key event to the bytes a macOS terminal sends with "natural text editing" (iTerm2's preset):
  - Option+Backspace → `ESC DEL` (readline/zsh: delete the previous word);
  - Cmd+Backspace → `Ctrl-U` (delete to the start of the line);
  - Option+← / → → `ESC b` / `ESC f` (word back / forward);
  - Cmd+← / → → `Ctrl-A` / `Ctrl-E` (start / end of line).
  Only these exact modifier combinations match (Shift, Ctrl or a second modifier ⇒ unmapped, left to xterm or the browser). The mapping is the same on every OS (Alt is Option; `metaKey` is Cmd, or the Windows/Super key elsewhere).
- `TerminalView`: `attachCustomKeyEventHandler` sends a mapped sequence through xterm's input path on `keydown`, prevents the browser default (e.g. Cmd+← as "Back"), and swallows the matching `keypress`/`keyup`.

**Tests:** U (Vitest): the mapping table (each key, unmapped plain keys and extra modifiers); `TerminalView` sends the sequence once on keydown, prevents the default and ignores keyup. I: n/a (bytes pass straight through to the PTY; M1 T13 covers the bridge).

**E2E (desktop-chromium; a hardware-keyboard feature):**
- **Delete word and line (T1):** at a bash prompt, `echo alpha beta gamma` then Option+Backspace leaves `echo alpha beta `; Cmd+Backspace clears the line (checked with `capture-pane`), and a new command still runs.
- **Move by word and line (T1):** `echo one three`, Option+← then typing `two ` and Enter prints `one two three`; `cho X`, Cmd+←, `e`, Cmd+→, Enter prints `X`; Option+→ moves forward a word.

### T2 — Copy and paste
Scope: the *Copy and paste* bullet of [ROADMAP M3](../ROADMAP.md#m3--terminal-workspace).

- **Keys** (`web/src/lib/terminalKeys.ts`, a second pure function `clipboardKey(ev) → 'copy' | 'paste' | undefined`):
  - Ctrl+Shift+C and Cmd+Shift+C → copy. Cmd+C → copy when there is a selection (macOS habit); with no selection it does nothing, since Cmd isn't Ctrl and never sent an interrupt.
  - Ctrl+Shift+V, Cmd+Shift+V and Cmd+V → paste.
  - Ctrl+C (no Shift) stays unmapped, so xterm sends `^C`. Ctrl+V stays xterm's default.
- **Copy:** `navigator.clipboard.writeText(term.getSelection())`. The selection stays in place afterwards (like a desktop terminal). If the write fails (no permission, or the page isn't a secure context), a toast says so: "Couldn't copy: the browser blocked clipboard access."
- **Paste:** the key handler returns `false` without `preventDefault`, so the browser runs its own paste into xterm's textarea. xterm's paste listener then calls `term.paste()`, which wraps the text in `ESC[200~ … ESC[201~` when the program enabled bracketed paste (mode 2004: bash ≥ 5.1, zsh, vim, Claude Code) and normalizes newlines to CR. The native path needs no clipboard-read permission prompt. If Chromium doesn't fire `paste` for Ctrl+Shift+V in e2e, fall back to `navigator.clipboard.readText()` + `term.paste()` for that combination only, and note that here. *(Built: Chromium fires `paste` for Ctrl+Shift+V, so no fallback was needed.)*
- **Context menu** (`TerminalMenu.vue`, Reka UI `ContextMenu` on the terminal element): **Copy** (disabled with no selection), **Paste** (`readText()` + `term.paste()`, since a menu click is a user gesture) and **Select all**. Right-click opens it when the program doesn't capture the mouse. Shift+right-click (Option+right-click on macOS) opens it even when the program does capture the mouse (tmux `mouse on`), because right-click then belongs to tmux's own menu. A long press on touch screens opens the menu too.
- **Forced selection:** xterm option `macOptionClickForcesSelection: true`. Shift+drag already forces selection on other platforms. Document both in the README.
- **OSC 52** (`@xterm/addon-clipboard`), with a custom `IClipboardProvider`:
  - writes: decoded base64 → `navigator.clipboard.writeText`. The browser may reject a write that doesn't come from a user gesture. The provider tries, and a failure is silent (logged at debug in the console). In practice a yank follows a keypress.
  - reads (`OSC 52 ; c ; ?`): **refused** (answered with nothing). Otherwise any program on the host could read the browser's clipboard. *(Built: the addon always answers a query, so a second OSC 52 handler registered after it swallows queries before the addon sees them.)*
  - size cap: payloads over 1 MiB decoded are ignored.
- **tmux:** tmux's default `set-clipboard external` already forwards copy-mode yanks to the outer terminal as OSC 52 (`xterm*` has the `clipboard` feature by default). Programs *inside* tmux (vim, Claude Code) need `set -g set-clipboard on` in the user's `~/.tmux.conf`. README documents both, and hostbud never changes the user's tmux config.
- **Secure context:** the clipboard API works on `http://localhost:${HOSTBUD_LOCAL_PORT}` and `https://${HOSTBUD_DOMAIN}`. README says a plain-HTTP LAN address won't work.
- ARCHITECTURE §6 *Keys* and §11: copy/paste keys, OSC 52 write-only.

**Tests:**
- U (Vitest):
  - `clipboardKey` table: each combination; Ctrl+C, Ctrl+V and plain keys unmapped; Cmd+C only with a selection.
  - `TerminalView`: copy writes the selection and doesn't send bytes; a paste combination sends no bytes and doesn't `preventDefault`; a failed write shows the toast.
  - `TerminalMenu`: Copy disabled without a selection; Paste calls `term.paste` with the clipboard text; Select all.
  - The OSC 52 provider: writes decoded text; refuses reads; ignores oversized payloads.
- I (`test/sshd`): the M1 term bridge attaches to a tmux session and runs `copy-mode` + `send-keys -X select-line` + `copy-selection`. The PTY output then contains `ESC]52;c;<base64 of the line>`, which pins the tmux side of the OSC 52 path.

**E2E (desktop-chromium; clipboard permissions granted):**
- **Copy selection (T2):** `echo copy-<marker>`; drag-select the output line; Ctrl+Shift+C → `navigator.clipboard.readText()` holds `copy-<marker>`. Repeat with the context menu's **Copy**.
- **Ctrl+C still interrupts (T2):** with a selection present, `sleep 100` then Ctrl+C returns to the prompt (`capture-pane` shows `^C` and a new prompt), and the clipboard is unchanged.
- **Bracketed paste (T2):** clipboard = `echo one-<m>\necho two-<m>`; Ctrl+Shift+V at a bash prompt → both lines sit in the command line and nothing runs until Enter. After Enter both outputs appear once. The same text pasted with the menu's **Paste** behaves the same.
- **Forced selection (T2):** with `tmux set -t <s> mouse on` on the target, a plain drag does *not* produce a browser selection (tmux gets it), and Shift+drag does, and copying it works.
- **OSC 52 yank (T2):** in tmux copy mode on the target (`send-keys -X` from the runner, or keys typed in the browser), select a line holding a marker and copy it → the browser clipboard holds the marker.
- **OSC 52 read refused (T2):** `printf '\e]52;c;?\a'` then reading stdin for 1 s gets no reply (the command prints `no-reply`).

**Done:** Vitest, `make test` and `make e2e` green; *(host)* owner check in Claude Code (multi-line paste, `/copy` or a yank) and on a phone over the domain (context menu Copy).

### T3 — Auto-reconnect
A terminal whose socket drops re-attaches by itself. The tmux session never notices beyond a detach and re-attach.

- **Liveness** (`web/src/api/term.ts`): the client sends `{"type":"ping"}` every 10 s (the server already answers `pong`). If no frame of any kind arrives for 25 s, the connection counts as dead: close the socket and report `disconnected`. This catches the hung-TCP case that a network cut produces, where `onclose` never fires.
- **Reconnect loop** (a `TermSession` wrapper around `TermConnection`, or an option on it; one owner of the retry state):
  - on `disconnected`, retry with backoff 0.5 s, 1, 2, 4, 8, then every 10 s (±20 % jitter), with no attempt cap;
  - retry immediately on `window` `online` and when the page becomes visible;
  - on success, attach at the terminal's current size. Keep xterm's buffer (no `reset()`): tmux's redraw replaces the screen and the scrollback stays searchable (T5);
  - **no** auto-reconnect after an `exit` frame (detach, or the session ended), after the component unmounts, or when the session is gone from the live session list. The existing banner with **Reconnect** stays for those cases;
  - *(Built:)* an `exit` with code 255 is ssh's own failure (host unreachable, or the app's ControlMaster hung by a cut), not tmux ending, so it is retried like a drop. The backoff starts over only once an attach has stayed up for 5 s, so an attach that opens and then fails in ssh doesn't retry every 0.5 s;
  - if an attempt fails before the socket opens, call `/api/auth/me`. On `401` stop and hand over to `auth.sessionEnded()` (the sign-in form), reusing `LiveConnection`'s `stillAuthorized` pattern.
- **UI:** while retrying, a non-blocking status strip over the terminal says "Reconnecting… (attempt n)" and offers **Retry now**. Keystrokes typed while disconnected are dropped (not queued), because sending them late into a different screen state is worse than losing them.
- **`/ws/events` liveness:** the server sends `{"type":"heartbeat"}` every 15 s (text frame; `events.go` ticker). `LiveConnection` treats 40 s of silence as a dead socket and reconnects with its existing backoff. The snapshot then resyncs the list.
- **Server:** unchanged in behavior. Its WebSocket ping (25 s interval, 10 s timeout) already ends the bridge and kills the ssh process when the client is gone, and the tmux session survives. T3 adds the integration test that pins this.
- **e2e target:** *(built)* the throwaway sshd sets `ClientAliveInterval 5` / `ClientAliveCountMax 3`. After a cut the app comes back with a new IP, so its old ssh connection is dead for good; the target drops it (and the stale tmux client with it) instead of keeping it until TCP gives up. The scenario also keeps the cut for ≥ 50 s: the app's hung ControlMaster needs `ServerAlive` (3 × 15 s) to give up before new attaches can work, as in a real cut of that length.
- **e2e ctl:** `POST /network/cut` → `docker network disconnect hostbud-e2e hostbud-e2e-app`; `POST /network/restore` → `docker network connect --alias hostbud hostbud-e2e hostbud-e2e-app`. Both are fixed commands, as the other routes are. The harness always restores in `afterEach`.
- ARCHITECTURE §6 (liveness, backoff, when not to retry) and §9 (`heartbeat` event).

**Tests:**
- U (Vitest, fake timers): the ping cadence and the 25 s silence ⇒ `disconnected`; the backoff schedule and jitter bounds; the immediate retry on `online`/`visible`; no retry after `exit`, after `close()`, or when the session isn't listed; a failed pre-open attempt with `401` stops and signs out; input is dropped while disconnected; `LiveConnection` 40 s silence ⇒ reconnect.
- U (Go): the events handler sends `heartbeat` on its ticker (injected interval).
- I (`test/sshd`):
  - a term client that stops reading and answering pings is dropped within ping interval + timeout (short test intervals), its ssh process exits, and the tmux session still exists;
  - a client `ping` gets a `pong` through the real bridge.

**E2E:**
- **Network cut re-attach (T3, desktop-chromium and iphone-13-pro):** attach, `echo before-<m>`. Then `/network/cut`, and meanwhile the runner creates a session on the target over SSH. The UI shows "Reconnecting…". Then `/network/restore`: within 30 s the terminal re-attaches with no click, `before-<m>` is still on screen, typing `echo after-<m>` reaches `capture-pane`, the same tmux session (unchanged `#{session_id}`) has one client attached, and the list shows the session created during the cut.
- **App restart re-attach (T3, desktop-chromium):** `/restart-app` → the terminal re-attaches by itself and input works (M1's restart scenario covered the list only).
- **Detach doesn't loop (T3, desktop-chromium):** `tmux detach-client -s <s>` from the runner → the "Session detached or ended." banner, and `#{session_attached}` stays 0 for 5 s (no automatic re-attach). **Reconnect** then attaches.

**Done:** Vitest, `make test` and `make e2e` green (run the network-cut scenario 5× locally for flakiness).

### T4 — Links
- `WebLinksAddon` (already loaded in M1) gets a custom handler in `web/src/lib/links.ts`:
  - only `http:` and `https:` URLs open, in a new tab with `noopener,noreferrer`, and anything else is ignored;
  - activation: a plain click (xterm shows the underline on hover). A drag that starts on a link still selects text (xterm's default).
- **OSC 8 hyperlinks** (`ls --hyperlink`, `gcc`, Claude Code): xterm's `linkHandler` option uses the same allowlist and handler. Because an OSC 8 link's text can differ from its target, the hover tooltip (`linkHandler.hover`) shows the real URL.
- Phones: a tap on a link opens it (xterm treats a tap as a click).
- ARCHITECTURE §6: link rules.

**Tests:** U (Vitest): the scheme allowlist (`http`, `https` open; `javascript:`, `file:`, `data:`, `ssh:` ignored); `window.open` gets `_blank` and `noopener,noreferrer`; the OSC 8 hover shows the target. I: n/a (the browser handles clicks; nothing reaches the host).

**E2E:**
- **Click a URL (T4, desktop-chromium and iphone-13-pro):** `echo https://example.com/hostbud-<m>`; click (tap) the URL → a new page opens on exactly that URL (answered by `context.route`), and the terminal page is unchanged.
- **OSC 8 link (T4, desktop-chromium):** `printf '\e]8;;https://example.com/osc8-<m>\e\\label-<m>\e]8;;\e\\\n'`; hovering `label-<m>` shows the target, and clicking opens it. A `javascript:` OSC 8 target opens nothing.

**Done:** Vitest and `make e2e` green.

### T5 — Search
- `@xterm/addon-search` (new dependency, bundled). A `TerminalSearch.vue` bar over the terminal's top-right corner has a text field, **Previous**/**Next**, a match count ("3 of 12", from `onDidChangeResults`), and toggles for **Match case** and **Regex**. Every match is highlighted and the current one is emphasized (decorations, in theme colors).
- **Open it** with Ctrl+Shift+F / Cmd+F / Cmd+Shift+F (a new `clipboardKey`-style mapping; plain Ctrl+F stays readline's forward-char) or a 🔍 button in the terminal header, which is also the way in on phones. Opening pre-fills the field with the current selection.
- **Keys in the field:** Enter = next, Shift+Enter = previous, Escape closes the bar, clears the highlights and refocuses the terminal. An invalid regex shows "Invalid pattern" instead of throwing.
- **Scope:** xterm's buffer, meaning the screen plus the scrollback the browser received since attaching (5000 lines). tmux history from before the attach isn't in it. That's tmux copy mode's job (`prefix [`, then `?`, and M5's Scroll button). The bar's empty state says so briefly, and README documents it.
- ARCHITECTURE §6 and §11: search scope.

**Tests:** U (Vitest): the search key mapping (Ctrl+F unmapped); `TerminalSearch` calls `findNext`/`findPrevious` with the case/regex options, shows the count, handles an invalid regex, and closes on Escape with the highlights cleared and focus back on the terminal; opening pre-fills the selection. I: n/a (client-side buffer only).

**E2E:**
- **Search scrollback (T5, desktop-chromium):** `echo needle-<m>; seq 1 300` (the marker scrolls off screen); Ctrl+Shift+F, type `needle-<m>` → "1 of 1", and the viewport scrolls so the marker line is visible (`window.__hostbud` reports the viewport contains it). Escape → bar gone, typing goes to the shell.
- **Search options (T5, desktop-chromium):** `echo Foo foo FOO` → `foo` finds 3, with **Match case** 1; regex `f[o]{2}` with **Regex** + case finds 1; `(` with **Regex** shows "Invalid pattern".
- **Search on the phone (T5, iphone-13-pro):** the 🔍 button opens the bar, and a search finds a printed marker.

**Done:** Vitest and `make e2e` green.

### T6 — UI state API
The backend for persisted layout (T7, T8), later also tree state and theme (M6).

- `GET /api/ui-state/{key}` → `200` with the stored JSON, or `404`. `PUT /api/ui-state/{key}` with a JSON body → `204`.
- **Per account:** the server namespaces keys as `user:<user-id>:<key>` in the existing `ui_state` table, so no migration is needed. Accounts don't see each other's layout.
- **Key allowlist:** `layout` for now (M6 adds `tree` and `theme`). An unknown key → `404`.
- **Limits:** the body must be valid JSON (`400` otherwise) and ≤ 64 KiB (`413`). The server doesn't interpret the value. The client validates what it reads back (T7).
- Authenticated and Origin-checked like every state-changing route. No event on the bus: the value is per-account UI preference, and the other tabs of the same browser don't need live sync in v1.
- `store`: add `UIStateForUser`/`PutUIStateForUser`, or build the namespaced key in `api`, keeping SQL only in `store`.
- `web/src/api/client.ts`: `getUIState<T>(key)`, `putUIState(key, value)`.
- ARCHITECTURE §8 (namespacing) and §9 (limits, allowlist).

**Tests:** U (Go, fakes): the handler's key allowlist, `400` on invalid JSON, `413` over 64 KiB, `404` before the first PUT, namespacing by the session's user, `401` signed out, `403` foreign Origin on PUT. U (Vitest): client helpers (`404` → `null`). I (PostgreSQL via `make test`): store round-trip for two users under the same key; overwrite updates `updated_at`.

**E2E:**
- **UI state API (T6, desktop-chromium, API level through Caddy):** PUT then GET returns the value; a second account gets `404` for the same key; PUT with a foreign `Origin` → `403`; an unknown key → `404`; 65 KiB → `413`.

**Done:** `make test` and `make e2e` green.

### T7 — Tabs
- **Layout model** (`web/src/stores/layout.ts`, Pinia):
  ```ts
  interface Layout { version: 1; tabs: Tab[]; activeTab: string | null }
  interface Tab    { id: string; root: LayoutNode; focusedPane: string }
  type LayoutNode = Pane | Split          // Split lands in T8
  interface Pane   { type: 'pane'; id: string; machine: string; session: string }
  ```
  `app.selected` is replaced by the layout (the focused pane of the active tab is "the selected session"). IDs are client-generated (`crypto.randomUUID`).
- **Behavior:**
  - Picking a session in the list focuses the tab that already shows it, or opens a new tab at the end and activates it. **New session** opens its session in a new tab.
  - The tab bar sits above the terminal area, with one tab per open terminal labeled with the session name. Click activates; the × button or a middle click closes. It scrolls horizontally when full. `role="tablist"` with arrow-key navigation (Reka UI `Tabs`).
  - **Closing a tab only detaches the view.** The tmux session keeps running and no confirmation is needed (not destructive).
  - Inactive tabs stay mounted (`v-show`) and connected, so switching is instant and their scrollback keeps filling. Showing a tab refits it (the ResizeObserver does it), which resizes tmux. The limit is 16 open terminals in total: opening a 17th shows a toast asking to close one.
  - **Rename** from the UI updates every pane showing that session. **A session that disappears** from the live list (killed in the UI or from a real terminal) closes its panes with a toast "Session <name> ended". A rename done in a real terminal looks like an end, so the pane closes; the renamed session is in the list.
  - **Persistence:** the layout is saved with `PUT /api/ui-state/layout`, debounced 500 ms, on every change. On sign-in it's loaded before any terminal mounts. It's validated (shape, `version`, the pane limit), and invalid data falls back to an empty layout with a console warning. Once the first `/ws/events` snapshot arrives, panes whose session doesn't exist are dropped (one toast listing them).
  - **Narrow screens** (below `md`, phones): M2's list-or-terminal switch stays. The terminal side shows the tab bar (compact) and the active tab. **Back to sessions** returns to the list without closing tabs.
- **Per-terminal pieces:** T1–T5 behaviors stay inside `TerminalView`. Keyboard input goes only to the active tab's focused terminal.
- **e2e hooks:** `window.__hostbud` becomes a registry keyed by pane. `termText(session?)` and `termSize(session?)` default to the focused pane, and `panes()` lists `{session, active, focused}`. Existing specs keep working through the defaults. `check-dist.mjs` is unchanged.
- ARCHITECTURE §11: layout model, persistence and limits; §6 notes that every open tab holds a WebSocket and an ssh process.

**Tests:** U (Vitest):
- the layout store: open (new vs existing), activate, close (focus moves to the neighbor), rename, drop-missing, the 16-terminal limit;
- serialize/validate: a round-trip, bad shape or version → empty, over-limit trimmed;
- saves are debounced (fake timers), and the load happens before mount;
- the tab bar: roles, close button, middle click, arrow keys;
- `App` wires list clicks to the store.

I: n/a (each pane is M1 T13's attach; persistence is T6's integration test).

**E2E:**
- **Tabs (T7, desktop-chromium):** create sessions `a-<m>`, `b-<m>` and `c-<m>` from a real terminal; open all three from the list → three tabs, the third active. Type a marker in each tab → each lands in its own session (`capture-pane`), and all three sessions have `#{session_attached}` = 1. Clicking `a-<m>` in the list activates its existing tab (no fourth tab).
- **Close tab detaches (T7, desktop-chromium):** close `b-<m>` → two tabs remain, `b-<m>` is still listed and its `#{session_attached}` = 0, and no confirmation dialog appeared.
- **Tabs follow rename and kill (T7, desktop-chromium):** rename `a-<m>` from the UI → the tab label follows and typing still reaches it. `tmux kill-session -t c-<m>` from the runner → its tab closes and the "ended" toast shows.
- **Tabs survive reload (T7, desktop-chromium):** reload → the same tabs in the same order, the same active tab, all attached. Restart `hostbud-e2e-app` and reload → still the same. Kill one session between saving and reload → that tab is dropped with a toast.
- **Tabs on the phone (T7, iphone-13-pro):** open two sessions (via Back to sessions), switch tabs from the compact tab bar, and type in each. The M2 phone scenarios stay green.

**Done:** Vitest and `make e2e` green in all projects.

### T8 — Split view
- **Model:** `Split { type: 'split'; id; dir: 'row' | 'column'; sizes: number[]; children: LayoutNode[] }` (row = side by side). At most 4 panes per tab.
- **Actions**, from the terminal header of the focused pane (**Split right**, **Split down**) and from each session row's menu in the list (**Open in split right/down**, splitting the active tab's focused pane):
  - the header buttons open a small session picker (Reka UI `Popover`: sessions list + **New session…**). The chosen session opens in the new pane, which gets focus;
  - splitting inside a split of the same direction adds a sibling, and the space is shared equally. The other direction nests a new split;
  - **Close pane** (× in the pane header) detaches that view and hands its space back to the siblings. A split left with one child collapses to that child, and closing the last pane closes the tab.
  - **Focus:** clicking a pane focuses it (a visible outline in the accent color). Keyboard input goes to the focused pane only.
- **Rendering:** `splitpanes` (new dependency) renders the tree recursively (`LayoutNodeView.vue`). Dragging a divider resizes the panes live. Each pane's ResizeObserver refits it and resizes its tmux window. New sizes are saved when the drag ends (`resized` event). The minimum pane size is 10 %.
- **Rename/kill/drop-missing** (T7 rules) apply to panes inside splits; removing a pane rebalances as **Close pane** does.
- **Narrow screens:** a split tab shows only its focused pane at full size, with a "Pane n of m" button in the header that cycles focus. The layout itself isn't changed, so a desktop reload shows the split again.
- ARCHITECTURE §11: the split model and the narrow-screen rule.

**Tests:** U (Vitest):
- tree operations: split same/other direction, the 4-pane limit, close with collapse, remove-missing with rebalance, sizes kept summing to 100;
- validation of stored splits (bad sizes → equal shares);
- `LayoutNodeView` renders nested splits and marks the focused pane;
- the narrow-screen pane cycler.

I: n/a (resize → tmux is M1 T13's integration test).

**E2E:**
- **Split and type (T8, desktop-chromium):** open `a-<m>`, **Split right** → pick `b-<m>`, then in `b` **Split down** → pick `c-<m>`. Three panes are visible and arranged as expected (bounding boxes). Clicking each pane and typing a marker lands it in the right session.
- **Resize split (T8, desktop-chromium):** dragging the vertical divider left makes `a-<m>`'s `#{window_width}` smaller and `b-<m>`'s larger on the target.
- **Close pane (T8, desktop-chromium):** closing `c-<m>`'s pane leaves two side by side with `c-<m>` detached (still listed). Closing another leaves one full-size pane.
- **Splits survive reload (T8, desktop-chromium):** reload and restart the app → the same tree, the same divider position (±2 %), the same focused pane.
- **Split on the phone (T8, iphone-13-pro):** a split tab created in the phone profile (via the list's row menu) shows one full-size pane, and the "Pane n of m" button switches which session is shown and typed into.

**Done:** Vitest and `make e2e` green in all projects.

### T9 — Docs, audit and release
- README: a *Terminal* section covering the keys (Mac editing, copy/paste, search), selection with the mouse captured (Shift/Option+drag), OSC 52 and tmux `set-clipboard` (what works by default and what needs `on`), the secure-context requirement, search scope vs tmux copy mode, tabs/splits and what closing does, auto-reconnect.
- ARCHITECTURE: check that §6, §8, §9 and §11 match what was built.
- *(host)* `make deploy`. The owner does the manual checks in [M3-acceptance.md](M3-acceptance.md): Mac keys in a real Mac browser and in Claude Code; multi-line paste into Claude Code; OSC 52 from vim/Claude Code with `set-clipboard on`; the phone over the domain (tabs, reconnect after toggling Wi-Fi, link tap, context-menu Copy).
- E2E and coverage audit as in M1 T18: every E2E item exists and passes in its projects, twice in a row from a clean checkout; every criterion's U/I/E tests exist.
- Summary to the owner (what changed, new env vars: none expected, manual steps).

**Tests:** none new beyond the audit (docs task).

**E2E:** audit only (see above).
