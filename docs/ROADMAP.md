# hostbud — Roadmap

Each milestone is shippable and ends deployed on the host (`make deploy`). A milestone is **done** only when its acceptance criteria pass, tests are green (`make lint test`; `make e2e` only at M7, see below), `gitleaks` is clean, and README/ARCHITECTURE are updated if behavior changed.

**Owner items never block.** Manual checks on the owner's devices, approvals and decisions are the owner's backlog. Agents record them as open (in the milestone's *Manual checks (owner)* list and the summary), take the safe default, and keep going. Open owner items don't hold back a task, a milestone's done state or the next milestone (AGENTS.md, *Owner items never block agents*).

**E2E per milestone:** each milestone extends the e2e suite (ARCHITECTURE §13.1) with scenarios that simulate a real user doing everything that milestone added, in both the desktop and iPhone 13 Pro profiles, against the throwaway target. The *E2E* line under each milestone lists the minimum.

**E2E is never deferred.** The e2e harness is built early in M1, before any feature work. From then on:
- every **task** (not just every milestone) that adds or changes behavior e2e can reach adds its scenario in the same commit. "Reach" means through the UI or the HTTP/WebSocket API through Caddy;
- every task in a `roadmap/M*-tasks.md` breakdown has an **E2E:** line, either the scenarios it adds or why nothing is reachable;
- every E2E item in a `roadmap/M*-acceptance.md` checklist is tagged with the task that adds it.

When a milestone below is broken into tasks, spread its *E2E* line across those tasks. Don't collect it into a final "write e2e tests" task.

**E2E runs are paused until the end of M7** (owner's decision, 2026-09-26). From M4 on, scenarios are still written task by task as above, but `make e2e` is not run while building milestones: not per commit, task, checkpoint or milestone. Each commit only type-checks the e2e suite. M7 starts with one diagnostic full-suite pass of every milestone's scenarios; save its detailed output, counts, duration and full failure inventory in `roadmap/M7-e2e-triage.md`. Batch-fix the complete inventory using focused checks before starting another full pass. If a verification pass finds failures, record and fix all of them before the next pass. M7 closes with two consecutive green full runs from a clean checkout, including M3's open stability check. Until then, E items in the acceptance checklists count as written but not yet passed.

**Every acceptance criterion is tested at three layers.** Each criterion in a milestone's `roadmap/M*-acceptance.md` gets a coverage line: **U** (unit: Go with fakes, Vitest), **I** (integration: against the `test/sshd` container or the real deploy config) and **E** (an e2e scenario), each naming the task that writes it. A layer is **n/a** only with a one-line reason; "manual" is allowed only where no automated layer can observe the behavior. The *Accept* line below each milestone becomes such criteria when the milestone is broken down, and no criterion is ticked until its tests exist and pass.

Priority: **(1) a working tmux manager in the browser, (2) deployed on the domain**, then everything else.

v1 has a **single target: the host machine** (ARCHITECTURE §1). Multi-machine support is listed under *Later*.

---

## v1

### M1 — tmux manager in the browser (local access)
Tasks: [roadmap/M1-tasks.md](roadmap/M1-tasks.md) · Checklist: [roadmap/M1-acceptance.md](roadmap/M1-acceptance.md)

- Go module, `cmd/hostbud`, config from env, `log/slog`, `/api/health`.
- PostgreSQL store + append-only migration runner; initial schema creates `machines` (seeded with the built-in host machine), `ui_state`, and authentication tables.
- Email/password account creation and sign-in, both gated by an owner-managed PostgreSQL email whitelist. No password reset, email delivery or email verification in M1.
- Server-side sessions in secure HttpOnly cookies and authentication required for the application API/UI.
- Escalating login rate limits with email+IP and IP-wide buckets, `429`/`Retry-After`, and PostgreSQL-backed shared state.
- PostgreSQL owner access documented through the untracked `.env`, `docker compose exec`, and an optional uncommon loopback-only maintenance port; no database port is exposed publicly or on the tailnet.
- Dockerized toolchain: `Makefile` targets `build`, `test`, `lint`, `gitleaks`, `deploy`, `logs`, `backup` all run in containers (no local Go needed). Pre-commit hook runs gitleaks via Docker.
- Multi-stage `Dockerfile` (linux/amd64), `docker-compose.yml` (hostbud + Caddy). Caddy serves plain HTTP on `127.0.0.1:${HOSTBUD_LOCAL_PORT}` only.
- `sshx`: generated `/data/ssh/config` for the host, ControlMaster, host key pinned from the mounted `/etc/ssh/ssh_host_*_key.pub` (ARCHITECTURE §4.3).
- Capability probe (tmux present/version; actionable error if missing).
- Poller + diffing → `sessions.changed` events over `/ws/events`; backoff + unreachable state.
- Session list; create (name, path defaulting to `~`, optional start command), rename, kill (confirmation dialog).
- `/ws/term` PTY bridge (binary frames, resize, exit) + xterm.js (WebGL, fit) — one terminal view.
- Vue 3 + Vite + TS + Tailwind + Reka UI app, embedded via `go:embed`.
- Origin check on WebSockets and state-changing requests.
- `.env.example` updated; `.gitignore`.
- E2E environment (`make e2e`, Compose project `hostbud-e2e`, Playwright desktop + iPhone 13 Pro): built **right after the container (T5), before any feature**. Each later task adds its own scenarios (API-level first, then UI).

**E2E:** see the E2E section of [roadmap/M1-acceptance.md](roadmap/M1-acceptance.md) — account creation/sign-in/logout, whitelist gating and escalating login throttling, then list, real-terminal changes, create/rename/kill, attach and type, vim/htop, resize, restart/unreachable/tmux-missing recovery, and Origin rejection.

**Accept:** from another machine, `ssh -L 9055:localhost:9055 <host>` then `http://localhost:9055` requires a whitelisted account; registration, sign-in and logout work; repeated failed logins receive progressively longer throttling; authenticated users can list the host's tmux sessions; sessions created/killed in a real terminal appear/disappear within one poll interval; create/rename/kill work from the UI (kill asks first); attaching runs Claude Code, vim and htop correctly and resizing the browser resizes the tmux window; the app survives a container restart.

### M2 — Deploy on the domain
Tasks: [roadmap/M2-tasks.md](roadmap/M2-tasks.md) · Checklist: [roadmap/M2-acceptance.md](roadmap/M2-acceptance.md)

- Custom Caddy image (`caddy-dns/cloudflare`); TLS site for `${HOSTBUD_DOMAIN}` via DNS-01, published only on `${TAILSCALE_IP}:443` / `:80`.
- Origin allowlist covers both `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
- Basic phone usability: terminal fits the viewport, on-screen keyboard input works.
- README deployment guide (Cloudflare DNS-only record, token scope, stable ssh-agent socket, sshd on host, port-forward access).

**E2E:** in the iPhone 13 Pro profile — open the app, attach, type a command and see its output, switch sessions, rotate portrait↔landscape and the tmux window resizes. The domain/TLS path itself (real cert, tailnet-only reachability) stays a manual check.

**Accept:** from a phone on the tailnet, `https://${HOSTBUD_DOMAIN}` loads with a valid cert and can attach and type; unreachable from outside the tailnet; the `localhost` port-forward path still works.

### M3 — Terminal workspace
Tasks: [roadmap/M3-tasks.md](roadmap/M3-tasks.md) · Checklist: [roadmap/M3-acceptance.md](roadmap/M3-acceptance.md)

- Tabs; split view (horizontal/vertical); layout persisted in `ui_state`.
- Auto-reconnect with re-attach; link detection; search.
- **Copy and paste** (owner request after M1):
  - Select text with the mouse and copy it: Ctrl/Cmd+Shift+C and a "Copy" context-menu item; Ctrl+C stays an interrupt for the program. Shift+drag (Option+drag on macOS) selects even when the program (or tmux `mouse on`) captures the mouse.
  - Paste with Ctrl/Cmd+Shift+V (and Cmd+V on macOS) as bracketed paste, so a multi-line paste into a shell or Claude Code doesn't run line by line.
  - OSC 52 (`@xterm/addon-clipboard`): text yanked in tmux copy mode, vim or Claude Code lands in the browser clipboard. It needs tmux `set-clipboard on`; hostbud documents that and never changes the user's tmux config.
  - The clipboard API needs a secure context: `http://localhost` (port forward) and the HTTPS domain (M2) both qualify. Copying from tmux scrollback on phones comes with M5's copy-mode button.
- **Mac editing keys** (owner request after M2), like a macOS terminal's "natural text editing": Option+Backspace deletes the previous word, Cmd+Backspace deletes to the start of the line, Option+←/→ move by word, Cmd+←/→ jump to the start/end of the line.

**E2E:** Option/Cmd+Backspace and Option/Cmd+←/→ edit a shell command line as described; open several sessions in tabs, split horizontally/vertically and type in each pane; reload → same layout; cut the app's network (`docker network disconnect`) and restore it → terminals re-attach on their own; select and copy text out (clipboard holds it), paste a multi-line command in (bracketed paste, runs once), an OSC 52 yank from tmux copy mode reaches the clipboard; click a printed URL; search finds text in scrollback.

**Accept:** killing the network and restoring it re-attaches without losing the session; tab/split layout survives reload.

### M4 — Projects and file browser
Tasks: [roadmap/M4-tasks.md](roadmap/M4-tasks.md) · Checklist: [roadmap/M4-acceptance.md](roadmap/M4-acceptance.md)

- SFTP-based browser: home, list, hidden toggle, breadcrumbs, path autocomplete, mkdir. Open it in a modal dialog from an icon-only FolderPlus button outside the left gutter; directory project actions use related icons with accessible labels.
- "Open as project", "New session here"; projects persisted; recents.
- Longest-prefix mapping of sessions to projects + `session_links`; "Other sessions" node with "Save as project".
- Recent start commands per project (e.g. `claude`, `codex`).
- Left bar has explicit user-controlled order for projects and sessions; new rows do not auto-sort existing entries. Session row actions sit to the right of the title; account email/sign-out move to the app header's top-right. Session rows do not show window counts.

**E2E:** browse home → into a folder → create a folder → "Open as project" → "New session here" → session appears under that project with the right path; a session started from a real terminal inside a project dir lands under it; one outside lands in "Other sessions" and "Save as project" moves it; hidden-files toggle and path autocomplete work; recent start command is offered next time.

**Accept:** tree renders Project → Session exactly as sessions are created.

### M5 — Mobile
Tasks: [roadmap/M5-tasks.md](roadmap/M5-tasks.md) · Checklist: [roadmap/M5-acceptance.md](roadmap/M5-acceptance.md)

- Responsive layout: tree drawer, single-terminal view, larger touch targets.
- On-screen key bar (Esc, Tab, Ctrl, Alt, arrows, common symbols) and Scroll button (copy-mode API).
- **Installable app (PWA):** a web app manifest (name, `display: standalone`, theme colors, bundled icons incl. `apple-touch-icon`) and a minimal service worker, so hostbud can be added to the home screen and opens full-screen like an app. The worker caches only the hashed app shell (never `/api/*` or `/ws/*`), so a start without a connection shows the app's own "can't reach hostbud" state, never stale sessions. A new version takes over on the next launch. Safe-area insets (notch, home indicator) are respected in standalone mode. Installable over the HTTPS domain (and `localhost`); see ARCHITECTURE §11.

**E2E (iPhone 13 Pro):** open the drawer, pick a session, use the key bar (Ctrl-C interrupts a running command, Esc leaves vim insert mode, arrows recall history); Scroll puts the pane in copy mode (`#{pane_in_mode}` = 1) and scrolling shows earlier output; touch targets are usable without zoom. PWA (desktop-chromium, where Playwright supports service workers): the page links a valid manifest whose icons all load; the service worker registers and controls the page after a reload; with `hostbud-e2e-app` stopped, a reload still renders the shell from the cache and shows the unreachable state, and no `/api` response is ever served from the cache.

**Accept:** from a phone, attach to a session, type, scroll history, and switch sessions comfortably; hostbud installs to the phone's home screen from the domain and opens full-screen (manual check on the owner's iPhone: Safari → Share → Add to Home Screen; an owner backlog item that doesn't block M5).

### M6 — Tree customization and polish
Tasks: [roadmap/M6-tasks.md](roadmap/M6-tasks.md) · Checklist: [roadmap/M6-acceptance.md](roadmap/M6-acceptance.md)

- Inline rename; hide/unhide; pin projects. M4 provides drag-to-sort and persistent manual ordering for projects and sessions; M6 must preserve it and must not introduce automatic re-sorting.
- Collapse state persisted; lazily loaded windows/panes under sessions.
- Visual tree hierarchy: projects (and Other sessions) render as parent headers with icon, name and `~`-shortened path; child rows are indented with a guide line. The Other sessions group is hidden while it has no sessions.
- Command palette (Ctrl/⌘-K); keyboard shortcuts, including next/previous tab with Ctrl+Shift+] / Ctrl+Shift+[ (on macOS too, since ⌘⇧]/[ belong to the browser), and Ctrl+Shift+D to switch between the two most recently selected tabs (Ctrl on macOS too, never ⌘).
- Remove a project (for every account, after a confirmation): its sessions keep running and move to the next matching project or Other sessions; nothing on disk or in tmux changes. Hide stays the per-account alternative.
- File browser: add (or open) the directory being shown as a project from the path bar, not only its child folders.
- Left bar: no "Projects & sessions" or "Projects" titles; one semantic icon button in the header opens and closes the left bar (drawer on phones); New session and Add project (opens the file browser) are icon buttons at the top of the left bar.
- Creating a session with a taken name gets the next free number (`work` → `work-1`) instead of an error, from every entry point, and the user is told the name; renaming to a taken name still errors.
- Theme setting: **Dark / Light / System**. System follows the OS `prefers-color-scheme` and switches live when the OS changes. The setting applies to the whole UI and the xterm terminal palette, is persisted in `ui_state` (default: System), and is applied before first paint (no flash of the wrong theme).

**E2E:** drag to reorder, rename, hide/unhide, pin, collapse — then reload **and** restart `hostbud-e2e-app` → everything is as the user left it; windows/panes load when a session is expanded; project headers are visually distinct and sessions indented beneath them; remove a project (its session keeps running, now under Other sessions) and add the browsed directory as a project; the icon toggle hides and shows the left bar (survives reload) and its New session / Add project icons work; the palette jumps to a session; theme: picking Dark or Light applies at once and survives reload and restart; with System, switching Playwright's emulated `colorScheme` flips the UI and terminal without a reload.

**Accept:** every tree customization survives reload and container restart; the Dark / Light / System theme setting works for the UI and terminal, persists, and in System mode follows the OS live.

### M7 — Hardening
Tasks: [roadmap/M7-tasks.md](roadmap/M7-tasks.md) · Checklist: [roadmap/M7-acceptance.md](roadmap/M7-acceptance.md)

- Timeouts/limits everywhere (exec, WS buffers, SFTP), plus HTTP requests and database queries: one documented inventory (ARCHITECTURE §15), a few operator-tunable values (`HOSTBUD_EXEC_TIMEOUT`, `HOSTBUD_SFTP_TIMEOUT`, terminal caps), ControlMaster self-healing, and actionable errors instead of hangs.
- Security headers and a strict CSP (allowing only M6's theme boot script by hash); HSTS on the domain site.
- Container hardening: read-only root filesystem, dropped capabilities, `no-new-privileges`, a healthcheck, bounded logs.
- Optional Tailscale identity allowlist via LocalAPI whois (domain path only, opt-in Compose override).
- `make backup` fixed for PostgreSQL, plus `make restore-check` (non-destructive) and a guarded `make restore`, with docs.
- Integration test suite against `test/sshd`: a coverage matrix with its gaps filled (failure modes, host-key mismatch) and a log-hygiene test.
- Fresh-host install: `make doctor` preflight checks, a docs consistency check, and a step-by-step README.

**E2E:** foreign-Origin requests and WebSockets rejected (every route, from one shared list); a stalled terminal client is dropped and recovers by reconnecting; long-running exec/SFTP calls time out with a user-visible error instead of hanging; security headers present with no CSP violations anywhere; the Tailscale allowlist (against a fake LocalAPI); the full suite from all previous milestones still passes.

**Full e2e run (last code task of M7, before release and Docker cleanup):** after every full `make e2e` pass, create a per-run report under `docs/e2e-triage/`, link it from `roadmap/M7-e2e-triage.md`, and commit it before another full run. Record the command/commit, counts, elapsed time, and every failed test title/profile, error summary and classification. The runner clears its result directory at the next run, so preserve and commit the report first. Batch-fix all listed failures using focused checks; only then start the next full run. If that run fails, repeat the report and batch-fix sequence. Finish with two consecutive green runs from a clean checkout to check stability.

**Accept:** security checklist in AGENTS.md fully satisfied; fresh-host install from README works end to end; `make e2e` green twice in a row.

### M8 — Interface density and input behavior
Tasks: [roadmap/M8-tasks.md](roadmap/M8-tasks.md) · Checklist: [roadmap/M8-acceptance.md](roadmap/M8-acceptance.md)

- Compact the New session, Browse files, and left-bar collapse/expand controls by reducing excess inner padding while preserving clear focus and usable targets.
- Move New session and Browse files into the top app bar immediately after the host name.
- Tighten project and session rows in the left tree: bring drag/reorder and project expand controls closer together, reduce row padding and margins, remove session chevrons where sessions have no expandable children, and prioritize session names.
- Make the Browse files dialog compact: reduce its outer dimensions and inner whitespace, and bring file/folder items closer together without making the individual items hard to use.
- Disable browser autocomplete/autofill on application inputs, except for the existing login-screen password behavior, which stays as it is.
- Let users drag open tabs to set a custom order. Keep the existing tab design unchanged: tabs themselves are draggable, with no added drag handle or other visual redesign. Newly opened tabs continue to append after the current last tab unless the user reorders them.
- Fix Option-click caret placement in editable text inputs: clicking a character/line should place the caret there, without it jumping to unrelated positions.
- Improve the terminal's visual readability while mouse-wheel scrolling. Preserve the currently correct up/down direction and position behavior, while making changing text easier to follow and reducing flicker, especially with repeated output.
- Provide a native dictation text editor with Send/Cancel that sends reviewed text through xterm's paste path once.
- Prevent the PWA from reopening the keyboard after returning from another app or switching tabs/panes; terminal taps and Show keyboard remain explicit focus actions.
- Consolidate per-terminal toolbar actions into a three-dot menu and provide a full-screen selectable snapshot of all history retained by tmux, including before browser attachment, with native text selection, wrapping and tmux colors/styles. In the installed PWA, terminal gestures no longer scroll tmux; use the dedicated text view for scrollback.
- Investigate contrast in long-lived terminal clients when the OS theme changes. Preserve the report: a Codex session opened while dark can still show a dark prompt surface with dark text after switching to a light theme the next morning. Determine whether hostbud's xterm palette or Codex's own color choices cause it, and improve what hostbud controls.

**E2E:** tree controls and row affordances remain operable and correctly named after relocation/density changes; Browse files opens in its compact layout and navigation/item actions still work; non-login inputs declare browser autocomplete off while login password behavior remains unchanged; open tabs can be dragged into a custom order, which survives reload and restart without changing tab styling; Option-click positions the caret exactly in single- and multiline text inputs; terminal wheel scrolling remains correctly directed and repeated output remains readable while moving through the buffer; a long-lived terminal remains legible after a system theme change, including a prompt-like surface; dictation text is sent once, foreground return and tab switching do not automatically focus the terminal, PWA terminal gestures do not scroll tmux, and the frozen styled text snapshot can be opened, scrolled, selected and closed. Add scenarios task by task in M8; follow the normal E2E run policy after M7's scheduled full run has completed.

**Accept:** the controls and tree present a denser layout with session names easy to scan; the file browser dialog uses the viewport efficiently with compact item spacing; all controls remain keyboard accessible and usable on desktop and phone; browser autocomplete is disabled everywhere except the unchanged login password behavior; users can drag tabs to reorder them and the custom order persists while the current visual design remains unchanged; Option-click consistently places the caret at the clicked location in editable text inputs; terminal text is easier to follow while scrolling, including repeated lines, with the existing scroll direction and position behavior preserved; hostbud-controlled terminal colors remain legible through a System theme change while a process stays attached, and any client-owned color limitation is diagnosed and documented; dictation uses an explicit editor and sends once, switching tabs does not reopen the keyboard, the installed PWA does not scroll tmux from terminal gestures, and retained terminal scrollback is available in a frozen styled snapshot.

---

## Later (not scheduled)
- **Session start command creation failure (reported):** creating a session with a start command fails to create the session. Investigate and fix; M4's documented behavior expects the command to pass through the normal session creation path and run in the selected directory. Keep this item open until reproduced and verified.
- **Multi-machine:** discover hosts from `~/.ssh/config`, custom connections, activate/deactivate, host-key trust UI (keyscan → confirm fingerprint), Machine level in the tree.
- **Public GitHub repo + CI** (lint, tests, gitleaks).

---

## v2 — Orchestration (design in ARCHITECTURE §10; not implemented)

- **V2.1 Tasks:** task CRUD + queue UI (board/list), priorities, agent kind, project/machine constraints.
- **V2.2 Runs and hooks:** create sessions for tasks with injected `HOSTBUD_*` env; `/api/hooks/:run_id` with per-run tokens; shipped hook scripts for Claude Code and Codex; run status in tree.
- **V2.3 Capacity and dispatcher:** per-machine slots; the dispatcher starts the next task when a slot frees up; pause/resume queue.
- **V2.4 LLM supervisor (fallback):** pluggable provider interface, OpenAI first; classifies stale runs from `capture-pane`; results recorded as `run_events` with `source='llm'`.
- **V2.5 Notifications:** browser/push notifications on blocked/completed runs.
- **Later:** Postgres option; tmux control-mode push instead of polling; git status per project.
