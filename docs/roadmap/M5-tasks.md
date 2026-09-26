# M5 — Mobile: tasks

Goal: hostbud is comfortable on a phone. The tree lives in a drawer over a single full-screen terminal. An on-screen key bar supplies the keys a phone keyboard lacks (Esc, Tab, Ctrl, Alt, arrows, common symbols). A Scroll button reads tmux history through copy mode without the prefix key. Every control is large enough to tap. hostbud also installs to the home screen as an app (PWA) that opens full-screen, respects the notch and home indicator, and shows its own "can't reach hostbud" state when it starts without a connection.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m5--mobile) · checklist: [M5-acceptance.md](M5-acceptance.md).

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Copy-mode API | Done |
| T2 Compact layout and tree drawer | Done |
| T3 Touch targets and phone polish | Done |
| T4 On-screen key bar | Done |
| T5 Scroll mode | Done |
| T6 Unreachable state | Done |
| T7 Web app manifest, icons and safe areas | Done |
| T8 Service worker | Done |
| T9 Docs, audit and release | Done |
| T10 Safe Docker cleanup | Not started |

## Rules for this milestone

- Work top to bottom, one task at a time, and don't start a task until the previous one is done. Before starting M5, check that M4's acceptance checklist is complete, apart from open owner items (M4 T4/T7 were still in progress when this plan was written). Don't build on an unfinished M4 layout.
- Before each task, read the U/I/E coverage lines that [M5-acceptance.md](M5-acceptance.md) assigns to it, plus its own **Tests:** and **E2E:** lines. Write all of them in the same commit(s) as the behavior. Never leave tests or scenarios for a later task.
- **Batched test checkpoints** (continuing M3/M4): each commit runs only fast checks: `go build`/`go vet` for the Go packages it touches, `vue-tsc` for the frontend, and `tsc` for the e2e suite when it changes. `make gitleaks` runs on every commit through the pre-commit hook. Full suites run at the checkpoints below. A checkpoint failure is fixed (with a regression test if it's a bug) before the next task starts, and the checkpoint re-runs until green.
- **E2E runs are paused until the end of M7.** Write every scenario in its task, but never run `make e2e`, `e2e-up` or `e2e-run` in M5, whether per commit, task, checkpoint or milestone. E items count as written and type-checked, not passed, until M7's full run.
- Every task has an **E2E:** line. A scenario is tagged with the task that writes it, both here and in the acceptance checklist.
- Tasks marked *(host)* need the real host or the owner's iPhone. The agent does the host parts itself. Anything that needs the owner's device or decision is an open owner item: record it and continue, never wait (AGENTS.md, *Owner items never block agents*).
- When a task moves the design, it updates ARCHITECTURE (mostly §5.2, §9, §11 and §13.1) in the same commit. Add a new env var only if one is really needed, and then to `.env.example` and `internal/config`. None is expected in M5.
- Conventional commits, small and focused; commit only this task's files. Other agents may be working in the tree at the same time, so stage explicit paths and never use `git add -A`.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T1 + T2 + T3 (copy-mode API, compact layout, touch targets) | `make lint test` | Pass |
| CP2 | T4 + T5 (key bar, scroll mode) | `make lint test` | Pass |
| CP3 | T6 + T7 + T8 (unreachable state, manifest, service worker) | `make lint test`, plus `make build` so `check-dist` sees the real PWA output | Pass |
| CP4 | T9 (audit) | `make lint test`, `make gitleaks`, e2e `tsc` | Pass |

**What e2e can and can't reach.**
- Phones are Playwright's `iPhone 13 Pro` device (WebKit on Linux, 390×844, `hasTouch`, `isMobile`), on `http://localhost:9055` (`iphone-13-pro`) and on `https://hostbud.example.test` (`iphone-13-pro-domain`). The on-screen keyboard is simulated as M2 does it: text arrives through `page.keyboard.insertText` (text input without key events), and taps use `locator.tap()`. The real iOS keyboard, its autocorrect bar and real gestures stay manual checks (T9).
- Phone-only scenarios go in files named `*.phone.spec.ts`. The existing `playwright.config.ts` patterns already keep `phone.spec.ts` files out of `desktop-chromium` and run them in both phone projects.
- **Service workers:** Playwright supports them reliably only in Chromium. PWA scenarios live in `pwa.spec.ts` and run in `desktop-chromium` only (T8 adds `/pwa\.spec\.ts$/` to the phone projects' `testIgnore`). Every other scenario must not be affected by a registered worker, so T8 sets `serviceWorkers: 'block'` in the config's global `use`, and `pwa.spec.ts` opts back in with `test.use({ serviceWorkers: 'allow' })`. The app must treat a blocked registration as normal: no toast, no console error.
- Swipe gestures can't be driven reliably in Playwright WebKit. Scroll-mode scenarios use the Scroll bar's buttons. The swipe → scroll mapping is covered by unit tests and a manual check.
- Safe-area insets (`env(safe-area-inset-*)`) are always 0 in Playwright, and "Add to Home Screen" doesn't exist there. Both are manual checks on the owner's iPhone.
- Ground truth for tmux stays on the target: `display -p -t '=<name>:' '#{pane_in_mode} #{scroll_position}'`, `capture-pane -p`, and `list-clients -F '#{client_pid} #{client_width}x#{client_height}'` (to prove a terminal did **not** re-attach).
- Stopping the app: T8 adds two fixed actions to `hostbud-e2e-ctl` (`POST /app/stop`, `POST /app/start`: `docker stop`/`docker start hostbud-e2e-app`), next to the existing `restart-app`. Like the others, they take no arguments. `/app/start` waits until `/api/health` answers through Caddy before returning.

---

## T1 — Copy-mode API

The backend for the Scroll button (ARCHITECTURE §5.2 *Copy/scroll mode*, §9 `POST …/copy-mode`). Every copy-mode interaction goes through side-channel tmux commands, so it never depends on the user's prefix key, `mode-keys` (vi/emacs) or `mouse` setting, and no user text ever reaches tmux as keys.

- `internal/tmux`: `CopyModeArgs(name string, action CopyAction, lines int) ([]string, error)`, next to `RenameSessionArgs`/`KillSessionArgs`. It validates the session name with the existing rule (`^[A-Za-z0-9_-]{1,64}$`) and always targets `'=<name>:'` (exact session, current window, active pane). Actions, a closed allowlist:
  - `enter` → `copy-mode -e -u -t '=<name>:'`: enter copy mode and scroll up one page. `-e` makes scrolling back to the bottom leave copy mode by itself, as a phone user expects.
  - `scroll-up` / `scroll-down` → `send-keys -X -N <lines> -t … scroll-up|scroll-down`, with `lines` between 1 and 500 (default 1; anything else is rejected, not clamped).
  - `page-up` / `page-down` → `send-keys -X -t … page-up|page-down`.
  - `top` / `bottom` → `send-keys -X -t … history-top|history-bottom`.
  - `exit` → `send-keys -X -t … cancel`.
  - After every action, the same exec reads `display -p -t … '#{pane_in_mode}\t#{scroll_position}\t#{history_size}'` (tmux `;` separator passed as its own shell-quoted argument), so the response reports the state tmux actually ended in.
- **Not in copy mode:** `send-keys -X` fails with "not in a mode" when the pane already left copy mode (the user typed `q`, or scrolled back to the bottom). A scroll or `exit` then succeeds with `inMode: false` instead of returning an error, so the UI can't get stuck.
- **tmux version:** `copy-mode -e`, `send-keys -X` and `-N` need tmux ≥ 2.4. On an older tmux (the probe's `tmux_version`), return an actionable 409 ("Scrolling needs tmux 2.4 or newer on the host (found 2.1)").
- `internal/api`: `POST /api/machines/{machine}/sessions/{name}/copy-mode`, with body `{"action": "enter" | "scroll-up" | "scroll-down" | "page-up" | "page-down" | "top" | "bottom" | "exit", "lines"?: n}`. It returns 200 `{"inMode": bool, "scrollPosition": n, "historySize": n}`. It requires authentication and the Origin allowlist (it's a state-changing POST). An unknown machine returns 404, an unknown session 404 with the standard `{error, hint}` shape, and an invalid name, action or `lines` value 400. It uses the default per-command timeout (10 s) through `sshx`; cancellation kills the ssh process.
- **No event:** copy mode is transient tmux pane state that hostbud neither stores nor polls. Nothing in hostbud's state changes, so no bus event is published. Record this reasoning in ARCHITECTURE §9 so it isn't mistaken for a missed event.
- Info logs never include the session name or path. Debug logs may include the action.
- A pane with several attached clients: copy mode belongs to the tmux pane, so every client showing it scrolls together. Document this; don't work around it.
- ARCHITECTURE §5.2 and §9: the action list, the target form and the response shape.

**Tests:** U (Go): `CopyModeArgs` table (each action → exact argv, `=`-target, `lines` bounds 0/1/500/501, invalid names, unknown action); the "not in a mode" stderr maps to `inMode: false`; version gate below 2.4; handler auth (401), Origin (403 foreign, allowed local/domain), body validation (400), unknown machine/session (404), method table; response parsing of the `display` line (including an empty history). I (`test/sshd`): on a real session with 300 lines of history: `enter` → `pane_in_mode=1` and `scroll_position>0`; `page-up` raises `scroll_position`; `scroll-down` by more than the position → `inMode:false` (`-e` exits); `exit` → 0 and the shell still takes input; `exit` again → 200 with `inMode:false`; a session name containing shell metacharacters is rejected before any ssh exec; the tmux-less target returns the actionable tmux-missing error.

**E2E:** add API-level **(T1) Copy-mode API** in [M5-acceptance.md](M5-acceptance.md#e2e-scenarios-make-e2e-simulated-user): through Caddy, `enter`/`page-up`/`exit` change the target's `#{pane_in_mode}`/`#{scroll_position}` as reported, a foreign Origin gets 403, a signed-out call gets 401, and an unknown session gets 404. Type-check only; don't run.

**Done:** the route works against `test/sshd`, it's authenticated and Origin-checked, only allowlisted tmux commands are built, and the scenario compiles.

## T2 — Compact layout and tree drawer

Replace the narrow-screen "list **or** terminal" switch (M1/M2) with a drawer over a single terminal view, so opening the tree never hides, re-mounts or resizes the terminal.

- **Compact layout query.** `lib/media.ts` gains `COMPACT_QUERY = '(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)'`. It matches a phone in portrait *and* in landscape (an iPhone 13 Pro landscape is 844 px wide, which would otherwise count as wide). Tablets and desktops keep the wide layout. `App.vue` switches on "compact" instead of `!wide`. Rotating a phone therefore never crosses the layout boundary, so terminals aren't re-mounted (and don't re-attach) on rotation.
- **Home screen with no open terminal:** in compact layout, the project tree fills the main area (as today's list view does) with New session, Browse files and the host banner.
- **Drawer once a terminal is open:** the ☰ button in the app header (accessible name **Show project tree**, the same name on every layout) opens the tree as a modal drawer from the left: Reka UI `Dialog` styled as a sheet, `role="dialog"`, labelled "Project tree", width `min(85vw, 20rem)`, over a dimmed backdrop. The drawer holds the same `SessionTree`, New session and Browse files controls as the sidebar, reusing its components rather than copying them.
  - It closes on backdrop tap, Escape, a close button (**Close project tree**), a left swipe on the drawer (≥ 60 px horizontal travel, more horizontal than vertical), and after picking a session or finishing a create/browse action that opens one.
  - Picking a session closes the drawer and shows that session (focus its tab or open a new one, per M3 rules).
  - Focus is trapped while it's open. On close, focus goes back to ☰, not the terminal, so iOS doesn't pop the keyboard up uninvited.
  - The terminal behind it stays mounted and attached, and its size doesn't change: the drawer overlays it and doesn't push it.
  - No edge-swipe-to-open, because it collides with iOS Safari's back gesture.
- **Single terminal view:** in compact layout the main area shows exactly one terminal, the focused pane of the active tab, edge to edge. The terminal header becomes one row: session name (truncated), M3's compact tab switcher, M3's "Pane n of m" cycler for split tabs, 🔍, ⌨ (Show keyboard) and the terminal menu. The M1/M2 **Back to sessions** button goes away, replaced by ☰. Inactive tabs stay mounted and attached as in M3 (`v-show`).
- **Dialogs** (New session, Rename, Kill, the file browser) open as full-width bottom sheets in compact layout, and as centered dialogs otherwise, keeping their current content and behavior.
- **Account controls:** M4 T5 put the email and Sign out at the top right of the header. In compact layout the email collapses into an account menu button (**Account**) that holds the full email and Sign out, so the header fits 390 px with ☰, the title and the account button. Sign out stays reachable without opening the drawer.
- **Update the existing scenarios in this commit:** `test/e2e/helpers/ui.ts` (`Back to sessions` → open the drawer), `phone.spec.ts`, and every M1–M4 phone scenario that navigated with the list/terminal switch (currently `projects.tree.spec.ts`, among others). Keep scenario intent unchanged; only navigation changes. Check with `git status` whether a parallel agent is editing a spec before touching it, and coordinate rather than overwrite.
- ARCHITECTURE §11: replace the *Narrow screens (M1)* / *Phones (M2)* bullets' list-or-terminal description with the compact layout, and note that crossing the M3 split breakpoint no longer happens on rotation.

**Tests:** U (Vitest): `COMPACT_QUERY` against fake `matchMedia` (portrait phone, landscape phone, tablet, desktop); `App` in compact layout shows the tree with no tabs and the drawer + single terminal with tabs; the drawer opens from ☰, closes on Escape/backdrop/close button/swipe/session pick, traps focus and returns it to ☰; picking a session calls `layout.open` and closes the drawer; the terminal component isn't unmounted when the drawer opens or on a `COMPACT_QUERY` change caused only by rotation; dialogs render as sheets in compact layout; the account menu holds email and Sign out. I: n/a (frontend layout only; no server, tmux or database behavior changes, and the terminal/resize path stays covered by M1 T13's integration test).

**E2E:** add **(T2) Tree drawer**, **(T2) Drawer keeps the terminal attached**, **(T2) Single terminal view and rotation** and **(T2) Account menu on the phone** (all `*.phone.spec.ts`, both phone projects), plus **(T2) Wide layout unchanged** (desktop). Update the existing phone navigation as listed above. Type-check only.

**Done:** a phone shows the tree as the home screen and as a drawer over one terminal; opening the drawer, rotating and switching sessions keep terminals attached; existing phone scenarios compile against the new navigation.

## T3 — Touch targets and phone polish

Make every control comfortably tappable and the app usable without pinch-zoom.

- **Minimum target 44×44 CSS px** (Apple HIG) for every interactive element under `(pointer: coarse)`, in both compact and wide layouts: tree rows (the whole row is the target), session row actions (×, ⋯, pencil; M4 T5's compact cluster keeps its position right of the title, but each hit area grows to 44 px with visual size unchanged where needed), project rows and their actions, header buttons, tab switcher entries, pane cycler, search bar buttons, dialog buttons and inputs, file browser rows and breadcrumb segments, toasts' actions. Add a shared Tailwind utility (e.g. `touch-target`: `min-h-11 min-w-11` under `pointer-coarse:`) instead of per-component magic numbers. Adjacent targets keep ≥ 8 px between hit areas or don't overlap.
- **Readable without zoom:** body text ≥ 14 px and secondary text ≥ 12 px in compact layout. Every `input`, `textarea` and `select` (sign-in, dialogs, file browser path input, search) uses ≥ 16 px so iOS doesn't auto-zoom on focus (M2 did this for the terminal's input only). The viewport keeps user scaling allowed (accessibility), and nothing needs it.
- **No horizontal page scroll** in either orientation, on every compact screen: tree, terminal, drawer, sheets, file browser, sign-in. Long names truncate with an ellipsis, and the full name is available (title/tooltip or the rename dialog).
- **Tap feedback without a sticky hover:** hover styles apply only under `@media (hover: hover)`, active/pressed styles on touch. `touch-action: manipulation` on the app root (no double-tap zoom delay; M2 did it for the terminal only).
- **Long-press** on a tree session row opens its ⋯ menu (the same menu, not a new one). It doesn't conflict with M3's long-press terminal context menu, which is on the terminal element.

**Tests:** U (Vitest): the `touch-target` utility is applied to each interactive component under coarse pointer (component-level assertions on classes for SessionList, SessionTree, TabBar, dialogs, FileBrowser, TerminalView header, AuthView); inputs carry the 16 px class; the long-press → ⋯ menu handler (fake timers: 500 ms opens it, movement > 10 px cancels it, a short tap still selects). I: n/a (presentation only; no server behavior).

**E2E:** add **(T3) Touch targets** (both phone projects): with the drawer open, with a terminal shown, with each dialog and the file browser open, every visible `button`, `a[href]`, `[role=tab]`, `[role=menuitem]`, `[role=treeitem]`, `input` and `select` has a bounding box ≥ 44×44 (the list of deliberate exceptions, if any, lives in the scenario with a reason); **(T3) Usable without zoom** (both phone projects): `visualViewport.scale === 1`, `scrollWidth ≤ innerWidth` in portrait and landscape on each screen, and every form control's computed font size ≥ 16 px; **(T3) Long-press row menu** (`iphone-13-pro`). Type-check only.

**Done:** every control meets the target size on touch screens, nothing needs zoom or horizontal scrolling, and the scenarios compile.

## T4 — On-screen key bar

A row of keys above the on-screen keyboard for what phone keyboards lack (ARCHITECTURE §11 *Mobile*).

- `KeyBar.vue` under the terminal, shown when the primary pointer is coarse (the same rule as M2's ⌨ button) and a terminal is focused/visible. It's part of the terminal column, so it sits directly above the on-screen keyboard thanks to M2's visual-viewport `--app-height`. A chevron (**Hide key bar** / **Show key bar**) collapses it to a thin handle; that choice is per page load (not persisted).
- **Keys, in order:** Esc, Tab, Ctrl, Alt, ←, ↑, ↓, →, `|`, `~`, `/`, `-`, Scroll (T5). The row scrolls horizontally inside itself if it doesn't fit; the page never scrolls. Each key meets T3's 44 px target and has an accessible name ("Escape", "Tab", "Control", "Alt", "Left arrow", …, "Pipe", "Tilde", "Slash", "Hyphen", "Scroll history").
- **Bytes** come from a pure module, `lib/keyBar.ts`, the phone counterpart of `terminalKeys.ts`, and go in through xterm's input path (`term.input(data, true)`) so they reach the PTY exactly as typed keys would:
  - Esc `\x1b`; Tab `\t`; symbols as their characters.
  - Arrows respect the terminal's cursor-key mode: `ESC [ A`… normally, `ESC O A`… when the program set application cursor keys (DECCKM; `term.modes.applicationCursorKeysMode`), as vim, less and htop expect.
  - **Ctrl** and **Alt** are sticky one-shot modifiers. A tap arms the modifier (highlighted, `aria-pressed="true"`), and it applies to the next key from the key bar *or* the next character typed on the on-screen keyboard (intercepted through the terminal's data path before it's sent), then disarms. A double tap locks it until tapped again. Ctrl+letter → the control code (`c` → `\x03`, `[` → `\x1b`, `@`/space → `\x00`, `\\` → `\x1c`, `]` → `\x1d`, `^` → `\x1e`, `_` → `\x1f`, `?` → `\x7f`). Alt+x → `ESC x`. Ctrl/Alt+arrow → `ESC [ 1 ; 5 A` / `ESC [ 1 ; 3 A`. Ctrl+Tab/Ctrl+symbols without a control code send the plain key and disarm. Both armed → Alt's ESC prefix plus Ctrl's code.
  - Arrows auto-repeat while held (first repeat after 400 ms, then every 80 ms, stopping on pointer up/cancel/leave).
- **Focus stays in the terminal:** keys act on `pointerdown` and call `preventDefault()` (and `mousedown.prevent`), with `tabindex="-1"`, so tapping them never blurs xterm's hidden textarea. That's what keeps iOS's keyboard open. If the keyboard was closed, a key tap sends its bytes without opening it (use ⌨ for that).
- The bar belongs to the focused pane. With M3 splits, it sends to the focused pane only.
- Nothing changes for desktop keyboards: hardware keys still go through `terminalKeys.ts` and xterm, and the bar is hidden on fine pointers.
- ARCHITECTURE §11: the key list, sticky modifier rules and the DECCKM behavior.

**Tests:** U (Vitest): `lib/keyBar.ts` table: every key in both cursor modes, Ctrl on every mapped character and on unmapped ones, Alt prefixing, Ctrl+Alt, modifier arm/disarm/lock state machine, a modifier applied to the next soft-keyboard character only once; `KeyBar` sends on pointerdown, prevents default, keeps `document.activeElement` on the terminal textarea, auto-repeat timing with fake timers (and stopping on cancel), `aria-pressed` for armed modifiers, hidden on fine pointer, collapse chevron. I: n/a (the bytes pass straight through the M1 T13 PTY bridge; nothing new server side).

**E2E:** add, in `keybar.phone.spec.ts` (both phone projects): **(T4) Ctrl-C interrupts** (run `sleep 1000`, tap Ctrl, insert `c` → the prompt returns, and `capture-pane` shows `^C` with no `sleep` in `#{pane_current_command}`); **(T4) Esc leaves vim insert mode** (`vim`, `i`, insert `abc`, tap Escape, insert `dd` → the buffer line is gone, checked with `capture-pane`; `:q!` exits); **(T4) Arrows recall history** (run `echo one`, tap ↑, Enter → `one` printed twice; ← moves inside the line so an inserted character lands before the last one); **(T4) Tab, Alt and symbols** (`ech` + Tab completes `echo`; tap `|`, `~`, `/`, `-` insert those characters; Alt then `b` moves back a word); **(T4) Key bar keeps the keyboard** (after tapping several keys, the focused element is still the terminal's textarea); **(T4) Application cursor keys** (in `less` on a long file or vim, ↓ moves by line, proving `ESC O B` handling). Type-check only.

**Done:** the key bar sends correct bytes in both cursor modes, modifiers work with the on-screen keyboard, focus never leaves the terminal, and the scenarios compile.

## T5 — Scroll mode

The key bar's **Scroll** button puts the pane in tmux copy mode through T1's API, so a phone user can read history the browser never received (ARCHITECTURE §6 *Scrollback under tmux*: xterm only holds what arrived since attaching).

- Tapping **Scroll history** calls `POST …/copy-mode {action: "enter"}`. While the response says `inMode: true`, the key bar is replaced by the **Scroll bar**: ⤒ Top, ⇞ Page up, ↑ Line up, ↓ Line down, ⇟ Page down, ⤓ Bottom, and **Done**, with a "Line n of m" position read from `scrollPosition`/`historySize`. Each button calls the matching action.
- **Swipes in scroll mode:** a vertical swipe on the terminal sends `scroll-up`/`scroll-down` with `lines` = swipe distance ÷ cell height, coalesced so at most one request is in flight and one pending per animation frame. Up to ~20 requests/s; the pending count accumulates up to the API's 500 line limit. Outside scroll mode, touch scrolling stays xterm's own (the browser-side scrollback, M3).
- **State follows tmux, not the UI:** every response updates `inMode`. When tmux leaves copy mode on its own (`-e` at the bottom, or the user types `q`), the bar returns to the key bar. **Done** sends `exit`, and so does typing on the on-screen keyboard while in scroll mode, which then passes the typed text through. Switching tab/pane or detaching drops the UI state without a request; the tmux pane may stay in copy mode, which is what tmux does for desktop users too, and the next Scroll tap works either way.
- Errors (409 old tmux, 404 session gone, network) show the API's actionable message as a toast and return to the key bar. Requests are bounded by a client timeout (10 s) and never retried automatically.
- Desktop: no Scroll button (desktop users have the wheel and `prefix [`). The route stays available.
- ARCHITECTURE §11: the Scroll bar and swipe behavior.

**Tests:** U (Vitest): Scroll store/composable: `enter` → bar shown, response `inMode:false` → bar hidden, request coalescing and one-in-flight limit (fake timers), swipe distance → line count and direction, 500-line cap, Done and typed input send `exit`, pane/tab switch resets the state, error → toast + key bar; `ScrollBar` buttons map to actions and show "Line n of m". I: T1's integration tests cover the tmux side; n/a beyond that (the client adds no server behavior).

**E2E:** add, in `scroll.phone.spec.ts` (both phone projects): **(T5) Scroll into history** (print `seq 1 400` in the session, tap Scroll history → target `#{pane_in_mode}` = 1 and `#{scroll_position}` > 0, and the browser terminal shows a number below 360 that wasn't on screen before; Page up raises `scroll_position`); **(T5) Leave scroll mode** (Done → `#{pane_in_mode}` = 0 and the key bar is back; typed `echo ok` runs; a second round leaves via Bottom and the bar returns because tmux exited copy mode by itself); **(T5) Scroll works with a full-screen program** (`htop` running → Scroll history still enters copy mode and Done returns to htop's screen). Type-check only.

**Done:** Scroll reads tmux history on a phone and never leaves the UI out of sync with tmux; the scenarios compile.

## T6 — Unreachable state

**Status:** Done.

A prerequisite for the PWA: when hostbud can't be reached at start-up, the app must say so rather than showing the sign-in form or anything stale. It's also useful without a service worker, e.g. when a phone opens the page as the Wi-Fi drops.

- `stores/auth`: `check()` distinguishes three outcomes. 200 → authenticated; 401 → anonymous (sign-in form); a network error, timeout (8 s), or 502/503/504 → the new **`unreachable`** status. Today every failure falls through to `sessionEnded()`; stop that.
- `UnreachableView.vue`: a **Can't reach hostbud** heading. The message says "You're offline" when `navigator.onLine` is false, and "hostbud isn't responding" otherwise, with a short hint (check the tailnet/VPN or the port forward). A **Try again** button. It retries by itself with backoff (1, 2, 4, 8, then every 15 s), immediately on the `online` event and when the page becomes visible, and stops once the app is reachable. It never shows session names, projects or layout: nothing is loaded, and nothing is cached (the service worker in T8 never caches `/api`).
- Sign-in and registration submits that hit a network error show "Can't reach hostbud, check your connection" in the form instead of the generic credentials error. They are **not** counted as login failures (they never reached the server).
- Once signed in, losing the server keeps M1/M3 behavior (the live store's "Reconnecting…" and terminal auto-reconnect). T6 only changes start-up and the auth forms.
- ARCHITECTURE §11: the unreachable state.

**Tests:** U (Vitest): `auth.check` mapping (200, 401, network error, timeout, 502, 503, 504, 500 → server error message rather than the sign-in form); `UnreachableView` text for offline vs. not responding, Try again, the backoff schedule, `online`/`visibilitychange` retries, stopping after success, and a transition to the sign-in form on a 401 retry; the sign-in form's network-error message. I: n/a (client-side decision; the server's 401 and health behavior is covered by M1 integration tests).

**E2E:** `unreachable.spec.ts`: **(T6) Unreachable at start-up** (desktop and `iphone-13-pro`): with `page.route('**/api/auth/me')` aborting, the app shows **Can't reach hostbud** and never the sign-in form or a session name; after un-routing, **Try again** leads into the signed-in app; the same with a 502 response. **(T6) Offline sign-in error** (desktop): a network-aborted login shows the connection message, and a following correct login succeeds without a throttle delay. Type-check only.

**Done:** a start without a server shows the unreachable state with retry, and never the sign-in form or stale data; the scenarios compile.

## T7 — Web app manifest, icons and safe areas

Make hostbud installable and full-screen (ARCHITECTURE §11 *Installable app (PWA, M5)*). The service worker is T8.

- **Icons:** an original, simple hostbud glyph as `web/icons/hostbud.svg` (no third-party marks). A dependency-free Node generator, `web/scripts/icons.mjs`, run by a new `make icons` target in the node toolbox container (nothing installed on the host), writes the PNGs into `web/public/icons/`: `icon-192.png`, `icon-512.png`, `icon-maskable-512.png` (artwork inside the central 80 % safe zone, on a solid background), `apple-touch-icon-180.png` (opaque, no transparency, since iOS blacks it out otherwise), plus `favicon.svg`. The PNGs are committed, so a normal build doesn't need the generator.
- **Manifest:** `web/public/manifest.webmanifest` with `id: "/"`, `name: "hostbud"`, `short_name: "hostbud"`, `description`, `start_url: "/"`, `scope: "/"`, `display: "standalone"`, `orientation: "any"`, `background_color` and `theme_color` from the dark theme tokens, and `icons` (192, 512 and maskable 512 with `purpose: "maskable"`). Only relative, same-origin URLs, no external assets.
- `web/index.html`: `<link rel="manifest">`, `<link rel="icon" type="image/svg+xml">`, `<link rel="apple-touch-icon" href="/icons/apple-touch-icon-180.png">`, `<meta name="theme-color">` with light and dark `media` variants (M6's theme setting later updates it at runtime), `<meta name="mobile-web-app-capable" content="yes">`, `<meta name="apple-mobile-web-app-capable" content="yes">`, `<meta name="apple-mobile-web-app-title" content="hostbud">` and `<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">`. The existing `viewport-fit=cover` stays.
- **Safe areas:** the app root pads with `env(safe-area-inset-top/right/bottom/left)` (fallback 0). The header takes the top inset (the translucent status bar sits over it), the key bar/terminal column the bottom inset (home indicator), and the drawer and sheets their own edges. This applies in the browser too (landscape Safari with `viewport-fit=cover` puts content under the notch), and the insets are 0 where irrelevant. `@media (display-mode: standalone)` only removes browser-only affordances, if any.
- **Serving** (`internal/api/spa.go`): `manifest.webmanifest` as `application/manifest+json` with `Cache-Control: no-cache`; files under `icons/` and `favicon.svg` with `Cache-Control: no-cache` (they aren't content-hashed). `assets/` keeps its immutable caching. These paths are public like the rest of the static SPA (they hold no data).
- `web/scripts/check-dist.mjs` extended: the build output has the manifest, every icon it references, the apple-touch icon and the favicon. The manifest parses, and every icon's PNG header matches its `sizes`. `index.html` links them. No URL in `index.html` or the manifest is absolute or off-origin.
- Update the e2e build if needed so the manifest is present (it's static, and the e2e image builds the same SPA).
- ARCHITECTURE §11 and README: install steps (iPhone: Safari → Share → Add to Home Screen; Android/desktop Chromium: the install prompt) and a note that the installed iOS app has its own cookie store, so it asks to sign in once.

**Tests:** U (Go): `spaHandler` content types and cache headers for the manifest, icons, favicon and `assets/`; the manifest is served without authentication; unknown `/icons/*` → 404, not the shell. U (node): `check-dist` accepts a correct fixture dist and rejects a missing icon, a size mismatch, an off-origin URL and a missing manifest link; the icon script's output sizes (run on a fixture SVG); safe-area offsets exist on the shell, drawer and compact sheets. I: n/a (static files served by the Go handler; no target, database or deploy-config change. The Caddy path is exercised by e2e).

**E2E:** add, in `pwa.spec.ts` (desktop-chromium): **(T7) Manifest and icons** (the page links a manifest served as `application/manifest+json`; it has `display: standalone`, `start_url`/`scope` `/`, a name and theme/background colors; every icon URL loads as `image/png` through Caddy with the declared pixel size; the apple-touch-icon link loads; no request leaves the origin while loading the app). Add **(T7) Theme and status-bar meta** in `pwa.phone.spec.ts` (both phone projects): `theme-color`, `apple-mobile-web-app-capable` and `viewport-fit=cover` are present on the phone. Type-check only.

**Done:** a valid manifest with icons is served, iOS metadata and safe-area padding are in place, and the scenarios compile.

## T8 — Service worker

**Status:** Done.

A minimal, hand-written worker that caches only the app shell, so an installed hostbud starts without a connection into its own unreachable state (T6) and updates on the next launch.

- **Source and build:** `web/src/sw/sw.ts` (worker entry) and `web/src/sw/routing.ts` (pure request classification, unit-testable). A small Vite plugin builds the worker as its own unhashed output `dist/sw.js` and, in `closeBundle`, injects the precache list plus a version derived from a hash of that list. The list is `/`, every file under `dist/assets/`, the manifest, the icons and the favicon; nothing under `/api` or `/ws`. No Workbox or runtime dependency; the worker is plain TypeScript compiled by Vite.
- **Behavior:**
  - `install`: open cache `hostbud-shell-<version>` and `addAll` the precache list. No `skipWaiting()`: a new worker waits and takes over when every hostbud window is closed, i.e. on the next launch.
  - `activate`: delete other caches named `hostbud-shell-*` (never caches with another prefix). It does **not** call `clients.claim()`, so a page is controlled from its next load. This matches "controls the page after a reload".
  - `fetch`: only same-origin `GET`s are considered. `/api/*` and `/ws/*` return without `respondWith`, so they go straight to the network and are never cached. A navigation request (`mode: "navigate"`) for any other path gets the cached `/` (cache-first; network if the cache is missing it). A precached asset is served cache-first. Everything else goes to the network, untouched, and isn't stored. No runtime caching of any response.
  - No push, background sync or periodic sync.
- **Registration** (`web/src/lib/pwa.ts`, called from `main.ts` after mount): only in production builds (including the `VITE_E2E=1` e2e image), only when `window.isSecureContext` and `'serviceWorker' in navigator` (the HTTPS domain or `localhost`), scope `/`. A rejection (blocked by Playwright, private mode, a plain-HTTP LAN address) is logged at `console.debug` only.
- **Serving** (`spa.go`): `/sw.js` with `Content-Type: text/javascript`, `Cache-Control: no-cache`, served from the root so its scope covers the app, and public like the rest of the static SPA. The existing 404 for missing extensioned files applies to other `*.js` at the root.
- **e2e harness:** `hostbud-e2e-ctl` gets `POST /app/stop` and `POST /app/start` (`docker stop --time 5` / `docker start hostbud-e2e-app`; start polls `/api/health` through Caddy, up to 60 s). `playwright.config.ts`: `serviceWorkers: 'block'` in the global `use`; `/pwa\.spec\.ts$/` added to both phone projects' `testIgnore`. `pwa.spec.ts` opts in with `test.use({ serviceWorkers: 'allow' })`. An `afterEach` in that file always calls `/app/start`, so a failure can't leave the app down for later scenarios.
- `check-dist` extended: `dist/sw.js` exists, its precache list equals the set of shell files in `dist` (nothing missing, nothing extra, nothing matching `/api` or `/ws`), and it contains no `skipWaiting`.
- ARCHITECTURE §11 (worker behavior, update-on-next-launch) and §13.1 (the two new ctl actions, and service workers blocked by default in e2e).

**Tests:** U (Vitest): `routing.ts` classification table (navigation → shell; a precached asset → cache; `/api/auth/me`, `/api/machines/host/sessions`, `/ws/events`, `/ws/term?…` → network/never cached; cross-origin and non-GET → network; an unknown path with an extension → network); worker install/activate/fetch against a fake `self`/`caches` (install caches exactly the list; activate deletes only stale `hostbud-shell-*`; no `skipWaiting` or `clients.claim`; fetch never `put`s at runtime); `pwa.ts` registers only in production + secure context and swallows a rejection. U (Go): `/sw.js` headers and public access. U (node): `check-dist` precache checks with good and bad fixtures. I: n/a (browser-side cache; the server only serves a static file, covered by Go unit tests and by e2e through Caddy).

**E2E:** add to `pwa.spec.ts` (desktop-chromium): **(T8) Service worker registers and controls after reload** (the first load has no controller; after `navigator.serviceWorker.ready` and a reload, `navigator.serviceWorker.controller` is set, and the registration's scope is `/`); **(T8) Offline start shows the unreachable state** (with the worker controlling, `ctl.appStop()`, reload → the app shell renders from the service worker (`response.fromServiceWorker()` on the document) and shows **Can't reach hostbud**, with no session names; `ctl.appStart()` then Try again → signed-in app with live sessions); **(T8) API never served from the cache** (throughout both scenarios, every `/api/*` response has `fromServiceWorker() === false`, and `caches.keys()`/`cache.keys()` contain no `/api` or `/ws` URL); **(T8) Only hostbud caches are managed** (a pre-existing cache named differently survives activation). Type-check only.

**Done:** the worker precaches only the shell, never touches `/api` or `/ws`, takes over on the next launch, and a start with the app stopped shows the unreachable state; the scenarios compile.

## T9 — Docs, audit and release

- README: a *Using hostbud on a phone* section: the drawer, the key bar (sticky Ctrl/Alt, double tap to lock, arrows follow the program's cursor mode), Scroll history (tmux ≥ 2.4, shared by all clients on that pane, Done/Bottom/typing leave it), installing to the home screen (iPhone Safari → Share → Add to Home Screen; sign in once in the installed app; updates apply on the next launch; the "Can't reach hostbud" screen), and that the PWA needs the HTTPS domain or `localhost` (not a plain-HTTP LAN address).
- ARCHITECTURE: reconcile §5.2, §6, §9, §11 and §13.1 with what was built (copy-mode actions/response, compact query, drawer, key bar bytes, Scroll bar, unreachable state, manifest, worker behavior, ctl actions, `serviceWorkers: 'block'`). ROADMAP only if scope moved.
- Audit: every criterion in [M5-acceptance.md](M5-acceptance.md) has its U/I/E line with the right task, and every E item exists in the named spec file, is tagged, and type-checks. Check `.env.example` (no new variable expected; if one was added, it's there with a placeholder). Confirm the M1–M4 phone scenarios were migrated to the drawer navigation and nothing references **Back to sessions**. Also check the security checklist items touched in M5: Origin on the copy-mode POST, no secrets or paths in info logs, no external assets.
- CP4: `make lint test`, `make gitleaks`, e2e `tsc`. Don't run e2e.
- *(host)* `make deploy`; `/api/health` is ok; `/manifest.webmanifest`, `/sw.js` and the icons are served with the right headers through the loopback port (`curl -I`); existing sessions are untouched (never kill or detach the owner's sessions; use only a throwaway session created for the check, and kill it only through the UI's confirmation dialog).
- *(host)* Owner's manual checks on a real iPhone, listed in [M5-acceptance.md](M5-acceptance.md#manual-checks-owner-t9); record the result of each in that file. If the owner hasn't done them yet, list them as open in the summary rather than ticking them, and don't wait for them: they don't block T9, T10 or M6.
- Summary to the owner: what changed, env vars (expected: none), manual steps (install on the phone, sign in once in the installed app).

**Tests:** none new beyond regressions found by the audit.

**E2E:** audit only: all T1–T8 scenarios present, tagged and type-checked; none run (M7).

**Done:** docs match behavior, the deploy serves the PWA files, the checklist is complete except the M7 e2e run and any open owner checks (backlog, not blockers), and the summary has been delivered.

## T10 — Safe Docker cleanup

Free the disk the milestone's builds used, **without touching the running deployment, its data, other projects, or work another agent may be doing at the same time.** This is the last step of the milestone, after T9's deploy and checks.

1. **Check that nothing is in use.** If any of these hold, skip the cleaning (steps 3–5), record "cleanup skipped: <reason>" in Progress and the summary, and treat the task as done. Cleanup can run again later:
   - a `make` test/lint/build or e2e run is in progress from this or another session (`pgrep -af 'scripts/tool.sh|docker exec hostbud-tools|test/e2e/run.sh|docker compose .*hostbud'`);
   - a `hostbud-tools-*` container is running a process other than its idle entrypoint (`docker top <container>`);
   - the `hostbud-e2e` stack is up (e2e is paused until M7, so it shouldn't be; if it is, someone is using it).
2. **Record the before state:** `docker system df` and `docker compose ps` (the production stack: `hostbud`, `hostbud-caddy`, `hostbud-postgres` must be running and healthy before and after).
3. **Clean with the repo's own target:** `make docker-clean` **without** `CACHE=1`. It removes only hostbud's own disposable artifacts: the e2e stack and its images (`hostbud-e2e-*:local`), stopped/idle toolbox containers (recreated on the next `make`, at a few seconds' cost), and dangling images labelled `hostbud.image=1`. Before running it, read the `docker-clean` recipe in the Makefile and confirm it still matches this list. If it has grown to remove anything else, don't run it: record the difference in Progress and the summary as an open owner item and finish the rest of the task.
4. **Never, in this task:**
   - `docker system prune`, `docker volume prune`, `docker image prune -a` without the `hostbud.image=1` label filter, or `docker builder prune` (all projects' build cache; `CACHE=1` does this); `docker network prune`;
   - removing the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data` or `hostbud-caddy-config`, or anything with another project's prefix;
   - stopping, recreating or removing the production containers, or removing the images they run from;
   - removing the toolbox *images* (`hostbud-toolbox-*`), the `test/sshd` integration targets (`make test-down` is not part of cleanup; the owner keeps them warm for speed), or anything under `.cache/`, `data/` or `backups/`.
   Build cache and the rest go only if the owner asks explicitly, as a separate step.
5. **Verify after:** `docker compose ps` shows the same three production containers still up; `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` answers `{"status":"ok"}`; `docker volume ls` still lists the four production volumes; `docker system df` again. Report the space reclaimed (before/after).
6. Commit only the Progress table update (this task changes no code).

**Tests:** n/a (operations only, no behavior change). The post-cleanup health check and `docker compose ps` above are the verification.

**E2E:** n/a: nothing reachable changes, and e2e runs are paused until M7.

**Done:** hostbud's disposable Docker artifacts are removed, the deployment and every volume are intact and healthy, nothing outside hostbud was touched, and reclaimed space is reported.
