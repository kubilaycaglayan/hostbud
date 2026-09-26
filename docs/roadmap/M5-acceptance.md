# M5 — Mobile: acceptance checklist

M5 is done when every box is ticked, except the *Manual checks (owner)* list, which is the owner's backlog and never blocks. Tasks: [M5-tasks.md](M5-tasks.md).

M5 makes hostbud comfortable on a phone and installable as an app. It adds one server route (copy mode, for the Scroll button) and static PWA files. It adds no new session lifecycle, no data model change and no migration. Authentication, the Origin allowlist, `sshx`, the single session service and event-driven UI rules still apply. The wide (desktop) layout and hardware-keyboard behavior stay as M3/M4 left them.

Setup for the manual checks: `make deploy` on the host; the owner's iPhone on the tailnet with Safari, `https://${HOSTBUD_DOMAIN}`; a tmux session on the host with some scrollback, vim and htop.

## Test coverage rule

Same as M1 ([M1-acceptance.md](M1-acceptance.md#test-coverage-rule)): every criterion names its **U** (unit: Go with fakes, Vitest, node build checks), **I** (integration: `test/sshd`, PostgreSQL or the rendered deploy config) and **E** (e2e) tests and the task that writes each. n/a needs a one-line reason, and "manual" is used only where automation can't observe the behavior (real iOS keyboard and gestures, Add to Home Screen, safe-area insets). E2E scenarios are written with the behavior and type-checked, but `make e2e` doesn't run until M7's final task. A ticked box means its U/I tests pass and its E scenario exists and type-checks; the e2e pass is recorded at M7.

## Responsive layout

- [x] On a phone (compact layout: narrower than 48rem, or a coarse pointer with a height under 32rem, so portrait **and** landscape), the project tree is the home screen when no terminal is open. Once a terminal is open, the tree opens as a drawer from ☰ (**Show project tree**) over the terminal.
  - U: T2 `COMPACT_QUERY` cases (portrait/landscape phone, tablet, desktop); `App` shows the tree without tabs and the drawer + terminal with tabs (Vitest).
  - I: n/a (frontend layout only; no server, tmux or database behavior).
  - E: T2 *Tree drawer* (both phone projects).
- [x] The drawer is a labelled modal ("Project tree") that traps focus. It closes on backdrop tap, Escape, **Close project tree**, a left swipe, or picking a session, and returns focus to ☰ (not the terminal, so iOS doesn't open the keyboard). Picking a session in it shows that session (focus its tab or open one).
  - U: T2 drawer open/close paths, focus trap and return, swipe threshold, session pick calls `layout.open` and closes (Vitest).
  - I: n/a (frontend only).
  - E: T2 *Tree drawer* (both phone projects). **Manual (T9):** the left swipe on a real iPhone.
- [x] Opening and closing the drawer, and rotating the phone, neither re-attach nor resize the terminal behind it: the same tmux client stays attached, and its size changes only for the rotation itself.
  - U: T2 the terminal component isn't unmounted when the drawer opens or on rotation (Vitest).
  - I: n/a (the attach/resize path is M1 T13's integration test; T2 only avoids re-mounting).
  - E: T2 *Drawer keeps the terminal attached* (`#{client_pid}` unchanged and `#{client_width}x#{client_height}` unchanged across open/close) · T2 *Single terminal view and rotation* (`#{client_pid}` unchanged across portrait → landscape → portrait while the size follows).
- [x] Compact layout shows exactly one terminal at a time (the focused pane of the active tab), edge to edge, with a one-row header: session name, compact tab switcher, pane cycler for split tabs, search, Show keyboard and the terminal menu. **Back to sessions** is gone.
  - U: T2 single-terminal rendering with several tabs and with a split tab (Vitest).
  - I: n/a (frontend only).
  - E: T2 *Single terminal view and rotation* (both phone projects; open two sessions and a split, and only one terminal is visible).
- [x] New session, Rename, Kill and the file browser open as full-width bottom sheets in compact layout, with their behavior unchanged. Kill still asks for confirmation.
  - U: T2 dialogs render as sheets in compact layout; Kill's confirmation is still required (Vitest).
  - I: n/a (presentation; M1/M4 integration tests cover the effects).
  - E: T2 *Tree drawer* creates a session from the drawer through the sheet · T3 *Touch targets* opens each sheet.
- [x] In compact layout the email and Sign out sit in an **Account** menu at the header's top right, reachable without opening the drawer. Sign out still revokes the session.
  - U: T2 account menu contents and sign-out wiring (Vitest).
  - I: n/a (placement; M1 integration covers revocation).
  - E: T2 *Account menu on the phone* (both phone projects).
- [x] The wide layout (desktop, tablet) is unchanged: sidebar, tab bar and splits as in M3/M4, with no drawer and no key bar on a fine pointer.
  - U: T2 wide layout snapshot of the regions rendered; T4 key bar hidden on fine pointer (Vitest).
  - I: n/a (frontend only).
  - E: T2 *Wide layout unchanged* (desktop); every existing desktop scenario (M1–M4) still type-checks.
- [x] Every M1–M4 phone scenario that used the list/terminal switch now navigates through the drawer, with its intent unchanged.
  - U: n/a (test-suite maintenance; no product logic).
  - I: n/a (no server behavior).
  - E: T2 migrates `helpers/ui.ts`, `phone.spec.ts` and the M4 phone scenarios; T9 audits that no scenario references **Back to sessions**.

## Touch and readability

- [x] Under a coarse pointer, every interactive control (tree rows and their actions, header buttons, tab switcher, pane cycler, search buttons, dialog/sheet controls, file browser rows and breadcrumbs, toast actions, key bar and Scroll bar keys) has a hit area of at least 44×44 CSS px, without overlapping its neighbors.
  - U: T3 the `touch-target` utility is applied in each interactive component under coarse pointer (Vitest); T4/T5 key bar and Scroll bar keys (Vitest).
  - I: n/a (presentation only).
  - E: T3 *Touch targets* (`touch.phone.spec.ts`, both phone projects; bounding boxes measured on the tree, drawer, terminal header, each sheet and the file browser, with any exception listed in the scenario with its reason) · T4 *Key bar keeps the keyboard* also measures the key bar.
- [x] The app is usable without zoom: the visual viewport scale stays 1, no screen scrolls horizontally in portrait or landscape, and every form control uses a font size of at least 16 px, so iOS doesn't zoom on focus. User zoom stays allowed.
  - U: T3 inputs carry the 16 px class; truncation classes on long names (Vitest).
  - I: n/a (presentation only).
  - E: T3 *Usable without zoom* (`touch.phone.spec.ts`, both phone projects). **Manual (T9):** focus each input on the iPhone and check that no zoom happens.
- [x] Long-pressing a session row in the tree opens that row's ⋯ menu; a short tap still opens the session; moving the finger cancels the long press.
  - U: T3 long-press timing and cancellation (Vitest, fake timers).
  - I: n/a (frontend only).
  - E: T3 *Long-press row menu* (`touch.phone.spec.ts`, `iphone-13-pro`).

## On-screen key bar

- [ ] On touch screens, a key bar under the terminal offers Esc, Tab, Ctrl, Alt, ←↑↓→, `|`, `~`, `/`, `-` and Scroll history, each with an accessible name. It can be collapsed and expanded, and it scrolls inside itself if it doesn't fit; the page never scrolls.
  - U: T4 `KeyBar` renders the keys, names and collapse state; hidden on fine pointer (Vitest).
  - I: n/a (frontend only).
  - E: T4 *Tab, Alt and symbols* (both phone projects).
- [ ] Keys send the same bytes a hardware keyboard would, through xterm's input path. Arrows follow the program's cursor-key mode (`ESC [ A` normally, `ESC O A` in application mode), so shell history, vim, less and htop all work.
  - U: T4 `lib/keyBar.ts` table in both cursor modes (Vitest).
  - I: n/a (byte passthrough through the M1 T13 PTY bridge; no new server code).
  - E: T4 *Arrows recall history* · T4 *Application cursor keys* · T4 *Esc leaves vim insert mode* (both phone projects).
- [ ] Ctrl and Alt are sticky one-shot modifiers that apply to the next key-bar key **or** the next character typed on the on-screen keyboard (a double tap locks them): Ctrl, then `c` interrupts a running command; Alt, then `b` moves back a word. An armed modifier shows as pressed.
  - U: T4 modifier state machine, control-code table, Alt prefix, Ctrl+Alt, applied once to the next soft-keyboard character, `aria-pressed` (Vitest).
  - I: n/a (byte passthrough).
  - E: T4 *Ctrl-C interrupts* · T4 *Tab, Alt and symbols* (both phone projects).
- [ ] Tapping key-bar keys never takes focus from the terminal, so the on-screen keyboard stays open. Arrow keys repeat while held.
  - U: T4 pointerdown + `preventDefault`, `document.activeElement` stays the terminal textarea; auto-repeat timing and stop on cancel (Vitest, fake timers).
  - I: n/a (browser focus only).
  - E: T4 *Key bar keeps the keyboard* (both phone projects). **Manual (T9):** on the iPhone, the keyboard stays up while tapping Esc, Ctrl and arrows, and holding ↑ repeats.
- [ ] Hardware-keyboard behavior on the desktop is unchanged (M3 Mac editing keys, copy/paste keys).
  - U: T4 `terminalKeys.ts` tests still pass unchanged; the key bar isn't mounted on fine pointer (Vitest).
  - I: n/a.
  - E: the M3 *Delete word and line* / *Move by word and line* scenarios still type-check unchanged.

## Scroll history (tmux copy mode)

- [x] `POST /api/machines/:id/sessions/:name/copy-mode` accepts only the actions `enter`, `scroll-up`, `scroll-down` (1–500 `lines`), `page-up`, `page-down`, `top`, `bottom` and `exit`. It runs them as side-channel tmux commands on the `=<name>:` target and returns `{inMode, scrollPosition, historySize}` as tmux reports them after the action. Scrolling or exiting when the pane already left copy mode succeeds with `inMode: false`.
  - U: T1 `CopyModeArgs` table, `lines` bounds, "not in a mode" mapping, `display` parsing, handler validation (Go).
  - I: T1 against `test/sshd`: enter/page-up/scroll-down-to-exit/exit/exit-again on a real session with history.
  - E: T1 *Copy-mode API* (API-level through Caddy).
- [x] The copy-mode route requires authentication and the Origin allowlist, validates the session name before any ssh exec, uses `sshx` with the default timeout, returns actionable errors (unknown session 404, tmux older than 2.4 → 409 "needs tmux 2.4 or newer", tmux missing → M1's install hint), and never logs the session name at info level. It publishes no event (copy mode isn't hostbud state), which ARCHITECTURE §9 records.
  - U: T1 401/403/400/404/409 handler tests, version gate, log redaction (Go).
  - I: T1 an invalid name is rejected before ssh runs; the tmux-less target returns the install hint.
  - E: T1 *Copy-mode API* (401 signed out, 403 foreign Origin, 404 unknown session).
- [ ] On a phone, **Scroll history** puts the session's pane in copy mode and shows earlier output (`#{pane_in_mode}` = 1, `#{scroll_position}` > 0). The Scroll bar (Top, Page up, Line up, Line down, Page down, Bottom, Done, "Line n of m") replaces the key bar while tmux is in copy mode, and vertical swipes on the terminal scroll tmux's history.
  - U: T5 Scroll store: enter/response handling, swipe → lines and direction, request coalescing, 500-line cap; `ScrollBar` actions and position label (Vitest).
  - I: T1's integration tests cover the tmux side; n/a beyond that (the client adds no server behavior).
  - E: T5 *Scroll into history* (both phone projects; buttons, since Playwright WebKit can't drive swipes). **Manual (T9):** swipe up/down through history on the iPhone.
- [ ] The UI never gets out of sync with tmux: Done, typing on the on-screen keyboard, or tmux leaving copy mode by itself (Bottom, or `-e` at the end of the history) bring back the key bar. An error shows the API's message and returns to the key bar. Scroll works while a full-screen program runs.
  - U: T5 `inMode:false` hides the bar; Done and typed input send `exit`; tab/pane switch resets the state; error → toast (Vitest).
  - I: T1 `scroll-down` past the bottom reports `inMode:false`; `exit` twice is fine.
  - E: T5 *Leave scroll mode* · T5 *Scroll works with a full-screen program* (both phone projects).

## Installable app (PWA)

- [ ] The page links a web app manifest (`application/manifest+json`) with `id`, `name`, `short_name`, `start_url` and `scope` `/`, `display: standalone`, theme and background colors, and bundled 192, 512 and maskable 512 PNG icons. `index.html` also links an opaque 180 px `apple-touch-icon`, an SVG favicon, `theme-color` (light/dark), and the Apple web-app capable, title and status-bar metas. Everything is same-origin; there are no external assets.
  - U: T7 `spaHandler` types and cache headers (Go); `check-dist` validates the manifest, icon files and their pixel sizes, and rejects off-origin URLs (node).
  - I: n/a (static files served by the Go handler; no target, database or deploy-config change).
  - E: T7 *Manifest and icons* (desktop-chromium) · T7 *Theme and status-bar meta* (both phone projects).
- [ ] A service worker at `/sw.js` (served `no-cache`, scope `/`) registers only in a secure context (the HTTPS domain or `localhost`) in production builds. It controls the page from the next load, and a blocked or failed registration is silent.
  - U: T8 `pwa.ts` registration conditions and rejection handling (Vitest); `/sw.js` headers (Go).
  - I: n/a (browser-side; the server only serves a static file).
  - E: T8 *Service worker registers and controls after reload* (desktop-chromium; service workers are only reliable in Chromium under Playwright).
- [ ] The worker precaches exactly the hashed app shell (`/`, `assets/*`, manifest, icons, favicon) and serves it cache-first. `/api/*` and `/ws/*` always go to the network and are never stored; nothing is cached at runtime.
  - U: T8 `routing.ts` classification table; fake-`caches` install/fetch tests show no runtime `put` (Vitest); `check-dist` says the precache list equals the shell files and contains no `/api` or `/ws` (node).
  - I: n/a (browser cache).
  - E: T8 *API never served from the cache* (desktop-chromium; `fromServiceWorker()` is false for every `/api` response, and the cache has no `/api` or `/ws` entries).
- [ ] Starting without a reachable server (installed app offline, or the app container down) renders the app shell and shows **Can't reach hostbud** with Try again and automatic retry. It never shows the sign-in form or stale sessions, and it recovers into the signed-in app once the server answers. A network error in the sign-in form is reported as a connection problem, not as bad credentials, and isn't counted as a login failure.
  - U: T6 `auth.check` outcome mapping, `UnreachableView` messages/backoff/`online`/visibility retries, sign-in network error (Vitest).
  - I: n/a (client-side decision; server 401/health behavior is covered by M1's integration tests).
  - E: T6 *Unreachable at start-up* (desktop and `iphone-13-pro`) · T6 *Offline sign-in error* (desktop) · T8 *Offline start shows the unreachable state* (desktop-chromium, `hostbud-e2e-app` stopped through `hostbud-e2e-ctl`). **Manual (T9):** launch the installed app in airplane mode.
- [ ] A new version takes over on the next launch: the worker doesn't call `skipWaiting()` or `clients.claim()`, and activation deletes only stale `hostbud-shell-*` caches.
  - U: T8 fake-`self` activate/install tests; `check-dist` rejects `skipWaiting` in `sw.js` (Vitest/node).
  - I: n/a (browser-side lifecycle).
  - E: T8 *Only hostbud caches are managed* (desktop-chromium). Taking over on the next launch needs two different builds in one run, which the e2e stack doesn't do. **Manual (T9):** after a `make deploy` with a change, the installed app shows the new version on its second launch.
- [ ] Safe-area insets are respected: header, key bar/terminal column, drawer and sheets pad with `env(safe-area-inset-*)`, so nothing sits under the notch, the status bar or the home indicator in standalone mode or in landscape Safari.
  - U: T7 the root, header, key bar and drawer carry the safe-area classes (Vitest).
  - I: n/a (CSS only).
  - E: n/a: Playwright always reports 0 insets, so it can't observe them. **Manual (T9):** portrait and landscape on the iPhone, installed and in Safari.
- [ ] hostbud installs to the iPhone's home screen from `https://${HOSTBUD_DOMAIN}` (Safari → Share → Add to Home Screen), shows its icon and name, and opens full-screen without Safari's UI; the installed app asks to sign in once and then works like the browser (attach, type, key bar, Scroll, drawer).
  - U: covered by the T7/T8 criteria above.
  - I: n/a (installation is an iOS feature).
  - E: n/a: Add to Home Screen and standalone launch don't exist in Playwright; T7/T8 e2e cover the manifest and worker that make it possible. **Manual (T9):** the owner's iPhone.

## Security and compatibility

- [ ] The new static files (`manifest.webmanifest`, `sw.js`, icons, favicon) are public like the rest of the SPA and hold no data. Every `/api/*` and `/ws/*` route keeps its authentication and Origin behavior, and the worker can't bypass it (it never answers those requests).
  - U: T7/T8 `spaHandler` serves the new files without a session; unknown `/icons/*` and root `*.js` → 404 (Go); T8 routing leaves `/api` and `/ws` to the network (Vitest).
  - I: n/a (no server-side auth change; M1 integration covers the auth middleware).
  - E: T8 *API never served from the cache* · T1 *Copy-mode API* (auth and Origin).
- [ ] M5 adds no env var, migration, published port or new remote command path: copy mode goes through `tmux` argument builders and `sshx` like every other tmux command.
  - U: T1 `CopyModeArgs` lives in `internal/tmux`, and the existing architecture tests (`internal/archtest`) still pass (no exec outside `sshx`, no SQL outside `store`) (Go).
  - I: the existing deploy-config check (published ports, mounts, `user`) still passes unchanged (T9 CP4).
  - E: n/a: nothing new is reachable beyond the routes and files covered above.

## E2E scenarios (`make e2e`, simulated user)

Profiles: `desktop-chromium`, `iphone-13-pro` (`http://localhost:9055`) and `iphone-13-pro-domain` (`https://hostbud.example.test`), against the throwaway `hostbud-e2e-target` only, never the real host. Phone-only scenarios live in `*.phone.spec.ts` (run in both phone projects), and PWA scenarios in `pwa.spec.ts` (desktop-chromium only; service workers blocked everywhere else). Each scenario is tagged with the task that writes it. During M5 the suite is only type-checked; it runs in M7's final task.

- [x] **(T1) Copy-mode API:** through Caddy: `enter` puts the target pane in copy mode with `scroll_position > 0`, `page-up` raises it, `exit` leaves (`pane_in_mode` 0) and a second `exit` still returns 200 `inMode:false`; signed out → 401; foreign Origin → 403; unknown session → 404 (desktop, API-level).
- [x] **(T2) Tree drawer:** with no terminal the tree is the home screen; open a session; ☰ opens the "Project tree" drawer; Escape, backdrop, close button and picking another session each close it; New session from the drawer (sheet) opens the new session (both phone projects).
- [x] **(T2) Drawer keeps the terminal attached:** the tmux client PID and client size are unchanged after opening and closing the drawer, and typed input still reaches the session (both phone projects).
- [x] **(T2) Single terminal view and rotation:** with two tabs and a split tab only one terminal is visible; portrait → landscape → portrait keeps the same client PID while `#{client_width}x#{client_height}` follows the orientation (both phone projects).
- [x] **(T2) Account menu on the phone:** the Account menu shows the email and Sign out without opening the drawer; Sign out returns to the sign-in form (both phone projects).
- [x] **(T2) Wide layout unchanged:** on desktop the sidebar, tab bar and splits are present, and there is no drawer dialog or key bar (desktop).
- [x] **(T3) Touch targets:** every visible interactive element on the tree, drawer, terminal header, each sheet and the file browser is at least 44×44 px (both phone projects).
- [x] **(T3) Usable without zoom:** viewport scale 1, no horizontal overflow in either orientation on each screen, form controls ≥ 16 px (both phone projects).
- [x] **(T3) Long-press row menu:** long-pressing a session row opens its ⋯ menu; a tap still opens the session (`iphone-13-pro`).
- [ ] **(T4) Ctrl-C interrupts:** `sleep 1000`, Ctrl then `c` → the prompt returns and `sleep` is no longer the pane's command (both phone projects).
- [ ] **(T4) Esc leaves vim insert mode:** in vim, insert text, Esc, then `dd` deletes the line (checked with `capture-pane`) (both phone projects).
- [ ] **(T4) Arrows recall history:** ↑ recalls the previous command and ← edits within the line (both phone projects).
- [ ] **(T4) Tab, Alt and symbols:** Tab completes, `|` `~` `/` `-` insert their characters, Alt then `b` moves back a word (both phone projects).
- [ ] **(T4) Key bar keeps the keyboard:** after several key taps, the focused element is still the terminal's textarea, and the keys meet the target size (both phone projects).
- [ ] **(T4) Application cursor keys:** ↓ moves by line in a program that enables application cursor mode (both phone projects).
- [ ] **(T5) Scroll into history:** after `seq 1 400`, Scroll history → `pane_in_mode` 1, `scroll_position` > 0, and earlier numbers are visible in the browser; Page up scrolls further (both phone projects).
- [ ] **(T5) Leave scroll mode:** Done exits copy mode and the key bar returns, and a typed command runs; Bottom makes tmux leave copy mode by itself and the UI follows (both phone projects).
- [ ] **(T5) Scroll works with a full-screen program:** with htop running, Scroll history enters copy mode and Done returns to htop (both phone projects).
- [ ] **(T6) Unreachable at start-up:** `/api/auth/me` aborted or 502 → **Can't reach hostbud**, never the sign-in form or a session name; after un-routing, Try again → signed-in app (desktop and `iphone-13-pro`).
- [ ] **(T6) Offline sign-in error:** a network-aborted login shows the connection message; the next correct login succeeds without throttling (desktop).
- [ ] **(T7) Manifest and icons:** manifest linked, served as `application/manifest+json`, with the required fields; every icon and the apple-touch-icon load as PNGs of their declared size through Caddy; no off-origin request (desktop-chromium).
- [ ] **(T7) Theme and status-bar meta:** `theme-color`, `apple-mobile-web-app-capable` and `viewport-fit=cover` present (both phone projects).
- [ ] **(T8) Service worker registers and controls after reload:** no controller on first load; after `ready` and a reload the page is controlled with scope `/` (desktop-chromium).
- [ ] **(T8) Offline start shows the unreachable state:** with the worker in control, stop `hostbud-e2e-app`, reload → the shell comes from the worker and shows **Can't reach hostbud** without session names; start the app, Try again → signed in with live sessions (desktop-chromium).
- [ ] **(T8) API never served from the cache:** no `/api` response has `fromServiceWorker()`, and the cache holds no `/api` or `/ws` URL (desktop-chromium).
- [ ] **(T8) Only hostbud caches are managed:** a cache with a different name survives the worker's activation (desktop-chromium).

## Manual checks (owner, T9)

On the owner's iPhone over `https://${HOSTBUD_DOMAIN}` on the tailnet, and a real tmux session on the host. These are the owner's backlog, not blockers: they don't hold back M5's done state or the next milestone, and no agent waits for them. Record the date and the result here when the owner does one; an unchecked item stays open in the summary.

- [ ] Safari: attach, type with the iOS keyboard, and switch sessions through the drawer comfortably, in portrait and landscape; no zoom on any input focus.
- [ ] Drawer: a left swipe closes it; the terminal doesn't flicker or re-attach.
- [ ] Key bar: the keyboard stays open while tapping Esc, Ctrl and arrows; Ctrl + `c` interrupts; holding ↑ repeats; Esc works in vim; Tab completes.
- [ ] Scroll history: swipe up and down through tmux history in a session with long output (and in Claude Code's session); Done and typing leave it.
- [ ] Install: Share → Add to Home Screen shows hostbud's icon and name; the app opens full-screen, with nothing under the notch, status bar or home indicator (portrait and landscape); sign in once, then attach and type.
- [ ] Offline launch: in airplane mode the installed app shows **Can't reach hostbud** (no sign-in form, no stale sessions) and recovers after the connection returns.
- [ ] Update: after a `make deploy` with a visible change, the installed app shows the new version on its next launch.

## Definition of done

- [ ] Every functional and security criterion above is satisfied: its U/I tests pass and its E scenario exists and type-checks.
- [ ] M5 scenarios are part of the M7 full e2e run; no e2e run happened during M5.
- [ ] `make lint test` and `make gitleaks` are green (CP1–CP4); no secrets, real hostnames, IPs or owner paths are tracked.
- [ ] README has the phone and install section; ARCHITECTURE §5.2, §6, §9, §11 and §13.1 match what was built; `.env.example` is unchanged (or updated if a variable was really needed).
- [ ] *(host)* `make deploy` done; the PWA files are served; the owner's manual checks are recorded above or listed as open.
- [ ] T10 safe Docker cleanup done: the production stack and all volumes intact and healthy, nothing outside hostbud touched, reclaimed space reported.
- [ ] Summary delivered: what changed, new env vars (expected none), manual steps on the host and phone.
