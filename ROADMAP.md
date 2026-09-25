# hostbud — Roadmap

Each milestone is shippable and ends deployed on the host (`make deploy`). A milestone is **done** only when its acceptance criteria pass, tests are green, `gitleaks` is clean, and README/ARCHITECTURE are updated if behavior changed.

---

## v1

### M0 — Skeleton and deploy pipeline
- Go module, `cmd/hostbud`, config loading from env, structured logging (`log/slog`), `/api/health`.
- Vue 3 + Vite + TS + Tailwind + Reka UI scaffold; built into `web/dist` and embedded with `go:embed`.
- Multi-stage `Dockerfile` (linux/amd64), `docker-compose.yml` (hostbud + caddy), custom Caddy image with `caddy-dns/cloudflare`, `Caddyfile`.
- `Makefile`: `build`, `test`, `lint`, `deploy`, `logs`, `backup`.
- `.env.example`, `.gitignore`, pre-commit config with `gitleaks`; a GitHub Action that runs lint + tests + gitleaks (no image publishing).
- SQLite store + migration runner + first empty migration.

**Accept:** `make deploy` on the host → `https://${HOSTBUD_DOMAIN}` shows the empty app shell with a valid cert from a tailnet device; unreachable from outside the tailnet.

### M1 — Machines and SSH
- Generated `/data/ssh/config` (ordering per ARCHITECTURE §4.1), ControlMaster.
- Discover hosts from `~/.ssh/config`; built-in "Host machine" via `host.docker.internal`.
- Add/edit/delete custom connections in the UI.
- Activate/deactivate (persisted); capability probe (OS, tmux version, tmux-missing warning with install hint).
- Host-key trust flow (keyscan → confirm fingerprint → app known_hosts).
- Events WebSocket; machine status dots in tree.

**Accept:** host + ≥1 remote machine can be activated from the UI; state survives container restart; unknown host requires fingerprint confirmation; a machine without tmux shows the warning.

### M2 — tmux sessions
- Per-machine poller, diffing, `sessions.changed` events; backoff + unreachable state.
- Tree shows sessions under each machine (grouped under "Other sessions" until M4).
- Create (name, path, optional start command), rename, kill (confirmation dialog).

**Accept:** sessions created/killed from a real terminal on a target appear/disappear in the UI within one poll interval; creating a session from the UI works on host and remote.

### M3 — Terminal
- `/ws/term` PTY bridge (binary frames, resize, exit), xterm.js with WebGL + addons.
- Tabs; split view (horizontal/vertical); layout persisted.
- Auto-reconnect with re-attach; copy/paste; link detection; search.

**Accept:** Claude Code, Codex, vim, htop render and behave correctly; resizing the browser resizes the tmux window; killing the network and restoring it re-attaches without losing the session.

### M4 — Projects and file browser
- SFTP-based browser: home, list, hidden toggle, breadcrumbs, path autocomplete, mkdir.
- "Open as project", "New session here"; projects persisted per machine; recents.
- Longest-prefix mapping of sessions to projects + `session_links`; "Save as project" for unmatched sessions.
- Recent start commands per project (e.g. `claude`, `codex`).

**Accept:** tree renders Machine → Project → Session exactly as sessions are created; browsing works on both Linux and macOS targets.

### M5 — Tree customization and polish
- Drag-to-sort machines and projects; inline rename (labels); hide/unhide; pin projects.
- Collapse state persisted; lazily loaded windows/panes under sessions.
- Command palette (Ctrl/⌘-K); keyboard shortcuts; light/dark theme.

**Accept:** every tree customization survives reload and container restart.

### M6 — Mobile
- Responsive layout: tree drawer, single-terminal view, larger touch targets.
- On-screen key bar (Esc, Tab, Ctrl, Alt, arrows, common symbols) and Scroll button (copy-mode API).

**Accept:** from a phone on the tailnet, attach to a session, type, scroll history, and switch sessions comfortably.

### M7 — Hardening
- Origin checks on all WebSockets and state-changing requests; optional Tailscale identity allowlist via LocalAPI whois.
- Timeouts/limits everywhere (exec, WS buffers, SFTP).
- `make backup` / restore docs; README deployment guide (Cloudflare DNS-only record, token scope, stable ssh-agent socket, sshd on host).
- Integration test suite against `test/sshd` in CI.

**Accept:** security checklist in AGENTS.md fully satisfied; fresh-host install from README works end to end.

---

## v2 — Orchestration (design in ARCHITECTURE §10)

- **V2.1 Tasks:** task CRUD + queue UI (board/list), priorities, agent kind, project/machine constraints.
- **V2.2 Runs and hooks:** create sessions for tasks with injected `HOSTBUD_*` env; `/api/hooks/:run_id` with per-run tokens; shipped hook scripts for Claude Code and Codex; run status in tree.
- **V2.3 Capacity and dispatcher:** per-machine slots; the dispatcher starts the next task when a slot frees up; pause/resume queue.
- **V2.4 LLM supervisor (fallback):** pluggable provider interface, OpenAI first; classifies stale runs from `capture-pane`; results recorded as `run_events` with `source='llm'`.
- **V2.5 Notifications:** browser/push notifications on blocked/completed runs.
- **Later:** Postgres option; tmux control-mode push instead of polling; git status per project.
