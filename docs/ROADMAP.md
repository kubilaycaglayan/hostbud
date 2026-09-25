# hostbud — Roadmap

Each milestone is shippable and ends deployed on the host (`make deploy`). A milestone is **done** only when its acceptance criteria pass, tests are green (`make lint test` **and `make e2e`**), `gitleaks` is clean, and README/ARCHITECTURE are updated if behavior changed.

**E2E per milestone:** each milestone extends the e2e suite (ARCHITECTURE §13.1) with scenarios that simulate a real user doing everything that milestone added, in both the desktop and iPhone 13 Pro profiles, against the throwaway target. The *E2E* line under each milestone lists the minimum.

**E2E is never deferred.** The e2e harness is built early in M1, before any feature work. From then on:
- every **task** (not just every milestone) that adds or changes behavior e2e can reach adds its scenario in the same commit. "Reach" means through the UI or the HTTP/WebSocket API through Caddy;
- every task in a `roadmap/M*-tasks.md` breakdown has an **E2E:** line, either the scenarios it adds or why nothing is reachable;
- every E2E item in a `roadmap/M*-acceptance.md` checklist is tagged with the task that adds it.

When a milestone below is broken into tasks, spread its *E2E* line across those tasks. Don't collect it into a final "write e2e tests" task.

**Every acceptance criterion is tested at three layers.** Each criterion in a milestone's `roadmap/M*-acceptance.md` gets a coverage line: **U** (unit: Go with fakes, Vitest), **I** (integration: against the `test/sshd` container or the real deploy config) and **E** (an e2e scenario), each naming the task that writes it. A layer is **n/a** only with a one-line reason; "manual" is allowed only where no automated layer can observe the behavior. The *Accept* line below each milestone becomes such criteria when the milestone is broken down, and no criterion is ticked until its tests exist and pass.

Priority: **(1) a working tmux manager in the browser, (2) deployed on the domain**, then everything else.

v1 has a **single target: the host machine** (ARCHITECTURE §1). Multi-machine support is listed under *Later*.

---

## v1

### M1 — tmux manager in the browser (local access)
Tasks: [roadmap/M1-tasks.md](roadmap/M1-tasks.md) · Checklist: [roadmap/M1-acceptance.md](roadmap/M1-acceptance.md)

- Go module, `cmd/hostbud`, config from env, `log/slog`, `/api/health`.
- SQLite store + migration runner; first migration creates `machines` (seeded with the built-in host machine) and `ui_state`.
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

**E2E:** see the E2E section of [roadmap/M1-acceptance.md](roadmap/M1-acceptance.md) — list, real-terminal changes, create/rename/kill, attach and type, vim/htop, resize, restart/unreachable/tmux-missing recovery, Origin rejection.

**Accept:** from another machine, `ssh -L 9055:localhost:9055 <host>` then `http://localhost:9055` lists the host's tmux sessions; sessions created/killed in a real terminal appear/disappear within one poll interval; create/rename/kill work from the UI (kill asks first); attaching runs Claude Code, vim and htop correctly and resizing the browser resizes the tmux window; the app survives a container restart.

### M2 — Deploy on the domain
- Custom Caddy image (`caddy-dns/cloudflare`); TLS site for `${HOSTBUD_DOMAIN}` via DNS-01, published only on `${TAILSCALE_IP}:443` / `:80`.
- Origin allowlist covers both `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
- Basic phone usability: terminal fits the viewport, on-screen keyboard input works.
- README deployment guide (Cloudflare DNS-only record, token scope, stable ssh-agent socket, sshd on host, port-forward access).

**E2E:** in the iPhone 13 Pro profile — open the app, attach, type a command and see its output, switch sessions, rotate portrait↔landscape and the tmux window resizes. The domain/TLS path itself (real cert, tailnet-only reachability) stays a manual check.

**Accept:** from a phone on the tailnet, `https://${HOSTBUD_DOMAIN}` loads with a valid cert and can attach and type; unreachable from outside the tailnet; the `localhost` port-forward path still works.

### M3 — Terminal workspace
- Tabs; split view (horizontal/vertical); layout persisted in `ui_state`.
- Auto-reconnect with re-attach; copy/paste; link detection; search.

**E2E:** open several sessions in tabs, split horizontally/vertically and type in each pane; reload → same layout; cut the app's network (`docker network disconnect`) and restore it → terminals re-attach on their own; copy text out and paste a command in; click a printed URL; search finds text in scrollback.

**Accept:** killing the network and restoring it re-attaches without losing the session; tab/split layout survives reload.

### M4 — Projects and file browser
- SFTP-based browser: home, list, hidden toggle, breadcrumbs, path autocomplete, mkdir.
- "Open as project", "New session here"; projects persisted; recents.
- Longest-prefix mapping of sessions to projects + `session_links`; "Other sessions" node with "Save as project".
- Recent start commands per project (e.g. `claude`, `codex`).

**E2E:** browse home → into a folder → create a folder → "Open as project" → "New session here" → session appears under that project with the right path; a session started from a real terminal inside a project dir lands under it; one outside lands in "Other sessions" and "Save as project" moves it; hidden-files toggle and path autocomplete work; recent start command is offered next time.

**Accept:** tree renders Project → Session exactly as sessions are created.

### M5 — Mobile
- Responsive layout: tree drawer, single-terminal view, larger touch targets.
- On-screen key bar (Esc, Tab, Ctrl, Alt, arrows, common symbols) and Scroll button (copy-mode API).

**E2E (iPhone 13 Pro):** open the drawer, pick a session, use the key bar (Ctrl-C interrupts a running command, Esc leaves vim insert mode, arrows recall history); Scroll puts the pane in copy mode (`#{pane_in_mode}` = 1) and scrolling shows earlier output; touch targets are usable without zoom.

**Accept:** from a phone, attach to a session, type, scroll history, and switch sessions comfortably.

### M6 — Tree customization and polish
- Drag-to-sort projects; inline rename; hide/unhide; pin projects.
- Collapse state persisted; lazily loaded windows/panes under sessions.
- Command palette (Ctrl/⌘-K); keyboard shortcuts.
- Theme setting: **Dark / Light / System**. System follows the OS `prefers-color-scheme` and switches live when the OS changes. The setting applies to the whole UI and the xterm terminal palette, is persisted in `ui_state` (default: System), and is applied before first paint (no flash of the wrong theme).

**E2E:** drag to reorder, rename, hide/unhide, pin, collapse — then reload **and** restart `hostbud-e2e-app` → everything is as the user left it; windows/panes load when a session is expanded; the palette jumps to a session; theme: picking Dark or Light applies at once and survives reload and restart; with System, switching Playwright's emulated `colorScheme` flips the UI and terminal without a reload.

**Accept:** every tree customization survives reload and container restart; the Dark / Light / System theme setting works for the UI and terminal, persists, and in System mode follows the OS live.

### M7 — Hardening
- Timeouts/limits everywhere (exec, WS buffers, SFTP).
- Optional Tailscale identity allowlist via LocalAPI whois.
- `make backup` / restore docs.
- Integration test suite against `test/sshd`.

**E2E:** foreign-Origin requests and WebSockets rejected; a stalled terminal client is dropped and recovers by reconnecting; long-running exec/SFTP calls time out with a user-visible error instead of hanging; the full suite from all previous milestones still passes.

**Accept:** security checklist in AGENTS.md fully satisfied; fresh-host install from README works end to end.

---

## Later (not scheduled)
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
