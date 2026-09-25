# hostbud — Roadmap

Each milestone is shippable and ends deployed on the host (`make deploy`). A milestone is **done** only when its acceptance criteria pass, tests are green, `gitleaks` is clean, and README/ARCHITECTURE are updated if behavior changed.

Priority: **(1) a working tmux manager in the browser, (2) deployed on the domain**, then everything else.

v1 has a **single target: the host machine** (ARCHITECTURE §1). Multi-machine support is listed under *Later*.

---

## v1

### M1 — tmux manager in the browser (local access)
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

**Accept:** from another machine, `ssh -L 9055:localhost:9055 <host>` then `http://localhost:9055` lists the host's tmux sessions; sessions created/killed in a real terminal appear/disappear within one poll interval; create/rename/kill work from the UI (kill asks first); attaching runs Claude Code, vim and htop correctly and resizing the browser resizes the tmux window; the app survives a container restart.

### M2 — Deploy on the domain
- Custom Caddy image (`caddy-dns/cloudflare`); TLS site for `${HOSTBUD_DOMAIN}` via DNS-01, published only on `${TAILSCALE_IP}:443` / `:80`.
- Origin allowlist covers both `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
- Basic phone usability: terminal fits the viewport, on-screen keyboard input works.
- README deployment guide (Cloudflare DNS-only record, token scope, stable ssh-agent socket, sshd on host, port-forward access).

**Accept:** from a phone on the tailnet, `https://${HOSTBUD_DOMAIN}` loads with a valid cert and can attach and type; unreachable from outside the tailnet; the `localhost` port-forward path still works.

### M3 — Terminal workspace
- Tabs; split view (horizontal/vertical); layout persisted in `ui_state`.
- Auto-reconnect with re-attach; copy/paste; link detection; search.

**Accept:** killing the network and restoring it re-attaches without losing the session; tab/split layout survives reload.

### M4 — Projects and file browser
- SFTP-based browser: home, list, hidden toggle, breadcrumbs, path autocomplete, mkdir.
- "Open as project", "New session here"; projects persisted; recents.
- Longest-prefix mapping of sessions to projects + `session_links`; "Other sessions" node with "Save as project".
- Recent start commands per project (e.g. `claude`, `codex`).

**Accept:** tree renders Project → Session exactly as sessions are created.

### M5 — Mobile
- Responsive layout: tree drawer, single-terminal view, larger touch targets.
- On-screen key bar (Esc, Tab, Ctrl, Alt, arrows, common symbols) and Scroll button (copy-mode API).

**Accept:** from a phone, attach to a session, type, scroll history, and switch sessions comfortably.

### M6 — Tree customization and polish
- Drag-to-sort projects; inline rename; hide/unhide; pin projects.
- Collapse state persisted; lazily loaded windows/panes under sessions.
- Command palette (Ctrl/⌘-K); keyboard shortcuts; light/dark theme.

**Accept:** every tree customization survives reload and container restart.

### M7 — Hardening
- Timeouts/limits everywhere (exec, WS buffers, SFTP).
- Optional Tailscale identity allowlist via LocalAPI whois.
- `make backup` / restore docs.
- Integration test suite against `test/sshd`.

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
