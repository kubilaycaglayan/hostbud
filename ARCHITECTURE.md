# hostbud — Architecture

hostbud is a self-hosted web app for managing **tmux sessions across many machines** (the host itself plus any SSH target), built for terminal-first and agentic-coding workflows. It runs in Docker on a single host, is reachable only inside a Tailscale tailnet, and is served on a custom subdomain.

> The important state (tmux sessions and whatever runs in them) lives on the target machines. hostbud only remembers *metadata*: connections, projects (directories), UI layout — and in v2, tasks.

---

## 1. Goals and non-goals

**v1 goals**
- Auto-discover SSH hosts from the host's `~/.ssh/config`; let the user add more connections in the UI.
- Activate/deactivate machines; while active, continuously read their tmux sessions.
- Left-gutter tree: **Machine → Project → Session**, expandable/collapsible, sortable, renameable, persisted.
- Full-fidelity terminal in the browser (attach to tmux as if in a local terminal). Multiple tabs, split view, mobile-friendly.
- File-system browser on any machine to pick a directory, create folders, and start a session there. Chosen directories become **projects**.
- Deployed via Docker Compose, behind Caddy with TLS, reachable only on the tailnet.

**v1 non-goals**
- Storing terminal output, scrollback, or agent state.
- Installing software on targets (missing tmux → warning only).
- File editing / previews, git integration.
- Multi-user. (Single user; tailnet + optional Tailscale identity allowlist is the auth boundary.)

**v2 (design for it now, build later)** — see §10: task queue, per-machine capacity, hook-based session status, LLM supervisor (OpenAI first, pluggable), dispatcher that spawns sessions for queued tasks.

---

## 2. High-level design

```
 Browser (Vue SPA + xterm.js)
   │  HTTPS / WSS  (tailnet only)
   ▼
 Caddy  (bound to host's Tailscale IP :443, TLS via Cloudflare DNS-01)
   │  http://hostbud:8080
   ▼
 hostbud  (single Go binary, SPA embedded)
   ├─ api        REST + WebSocket endpoints
   ├─ events     in-process pub/sub bus  (v2: orchestrator subscribes here)
   ├─ inventory  machines, activation, per-machine pollers, state cache
   ├─ sshx       runs system `ssh` with an app-generated config + ControlMaster
   ├─ tmux       tmux command builder/parser (list/new/kill/rename/copy-mode)
   ├─ fsbrowse   SFTP over `ssh -s sftp` (list, mkdir, stat, home dir)
   ├─ term       PTY ⇄ WebSocket bridge for interactive attach
   └─ store      SQLite (migrations, repository layer; Postgres-ready)
   │
   │  ssh (multiplexed via ControlMaster sockets in /data/ssh/cm)
   ▼
 host machine (via host.docker.internal)   server-a   laptop   vps …
```

**Why this stack**
- **Go backend, single binary.** Latency is dominated by network and PTY I/O, not compute, so WebAssembly or exotic runtimes buy nothing. Go gives first-class PTYs (`creack/pty`), WebSockets, SFTP (`pkg/sftp`), static builds, and a tiny container.
- **System `ssh` binary, not a Go SSH library.** Gets `~/.ssh/config` semantics (ProxyJump, Include, Match, IdentityAgent, …), ssh-agent, and known_hosts for free and exactly as the user's terminal behaves. **ControlMaster** makes every non-interactive call (poll, list dir) reuse one TCP/SSH connection → millisecond-level round trips.
- **xterm.js (WebGL renderer).** The same terminal engine as VS Code; handles full-screen TUIs (Claude Code, Codex, vim) correctly.
- **SQLite behind a repository layer.** Zero-ops for a single-host app; migrations + a thin data-access layer keep a later Postgres move to a driver/dialect swap.

---

## 3. Deployment topology (Docker)

Services in `docker-compose.yml`:

| Service | Image | Notes |
|---|---|---|
| `caddy` | custom build (`caddy:builder` + `xcaddy` + `caddy-dns/cloudflare`) | Publishes `${TAILSCALE_IP}:443:443` and `${TAILSCALE_IP}:80:80` **only** (never `0.0.0.0`). Obtains cert for `${HOSTBUD_DOMAIN}` via DNS-01 using `CLOUDFLARE_API_TOKEN`. Reverse-proxies to `hostbud:8080` (WebSocket upgrade supported by default). |
| `hostbud` | built from repo `Dockerfile` | No published ports. Runs as `${HOST_UID}:${HOST_GID}`. |

**DNS:** Cloudflare `A` record `${HOSTBUD_DOMAIN}` → host's Tailscale IP (100.x.y.z), **DNS only (grey cloud)**. Never proxied (orange) and never a Cloudflare Tunnel — both would expose the app publicly. The name resolves publicly but the address is only routable inside the tailnet.

**hostbud container mounts / settings**
- `hostbud-data:/data` — SQLite DB, generated SSH config, ControlMaster sockets, app known_hosts.
- ssh-agent socket: `${HOST_SSH_AUTH_SOCK}:/run/ssh-agent.sock` and `SSH_AUTH_SOCK=/run/ssh-agent.sock`. Private keys never enter the container. The host should use a **stable** agent socket path (e.g. systemd user `ssh-agent.socket`, or `ssh-agent -a ~/.ssh/agent.sock`) — document this in README.
- User SSH config, **read-only, at the same absolute path as on the host**, with `HOME=${HOST_HOME}` inside the container so `~` and absolute `Include` paths resolve identically:
  - `${HOST_HOME}/.ssh/config` (ro)
  - `${HOST_HOME}/.ssh/known_hosts` (ro)
  - optional `${HOST_HOME}/.ssh/config.d` (ro) — use long-syntax bind with `create_host_path: false`.
  - Do **not** mount the whole `~/.ssh` (keeps private key files out).
- `extra_hosts: ["host.docker.internal:host-gateway"]` — the host machine is reached over SSH like any other target (requires `sshd` on the host and the user's own key in `authorized_keys`).
- Tailnet reachability from the container: traffic to 100.x routes through the host. Verify MagicDNS names resolve inside the container; if not, set `dns: [100.100.100.100]` on the service. Fallback (documented, not default): `network_mode: host`.

**Runtime image:** multi-stage — `node` (build SPA) → `golang` (build with SPA embedded via `go:embed`, `CGO_ENABLED=0`) → `debian:stable-slim` with `openssh-client`, `ca-certificates`, `tini`. Target arch: **linux/amd64**.

**Build & deploy:** on the host, `git pull && docker compose up -d --build` (wrapped in `make deploy`). No registry.

---

## 4. SSH layer (`sshx`)

### 4.1 Generated config
hostbud writes `/data/ssh/config` on startup and whenever custom connections change, and always invokes `ssh -F /data/ssh/config …`. Because OpenSSH uses the **first** value obtained for each option, order matters:

```sshconfig
# 1. App-managed custom connections (from DB)
Host hb-custom-<id>            # internal alias; UI shows the user's label
  HostName …
  User …
  Port …
  ProxyJump …

# 2. The user's real config (read-only mount)
Include ~/.ssh/config

# 3. App defaults — last, so user settings win
Host *
  ControlMaster auto
  ControlPath /data/ssh/cm/%C
  ControlPersist 10m
  ServerAliveInterval 15
  ServerAliveCountMax 3
  UserKnownHostsFile /data/ssh/known_hosts ~/.ssh/known_hosts
  StrictHostKeyChecking yes
  BatchMode yes                # never prompt inside non-interactive calls
```

- `ControlPath` uses `%C` (hash) to stay under the Unix socket path length limit.
- `BatchMode yes` for all non-interactive calls; interactive attach also relies on agent auth (no password prompts in v1).

### 4.2 Host discovery
- Parse the user's config with `github.com/kevinburke/ssh_config` **for display only** (list concrete `Host` aliases; skip wildcard/negated patterns). Resolve effective settings for display via `ssh -G <alias>`.
- Machine sources: `host` (built-in, `host.docker.internal`, user `${HOST_SSH_USER}`, label `${HOSTBUD_HOST_LABEL}`), `sshconfig` (discovered; re-scanned on startup and via "Refresh"), `custom` (DB).
- Discovered hosts are shown but **inactive by default**; activation state persists in DB.

### 4.3 Host-key trust
First connection to an unknown host must not silently accept. Flow: `ssh-keyscan` → show fingerprints in UI → on user confirmation append to `/data/ssh/known_hosts`. Mismatch → hard error surfaced in UI.

### 4.4 Command execution rules
- Every remote command goes through one function that builds `ssh -F cfg <alias> -- <cmd>`; `<cmd>` is assembled only from **shell-quoted** arguments (single-quote escaping helper). Never interpolate user input unquoted.
- Validate tmux session names: `^[A-Za-z0-9_-]{1,64}$` (tmux forbids `.` and `:`; we are stricter).
- Per-command timeouts (default 10s); context cancellation kills the process.
- Machine capability probe on activation: `uname -s; command -v tmux; tmux -V` → store `os`, `tmux_version`, `tmux_missing`. If tmux is missing, mark the machine and show the install command (`apt install tmux` / `brew install tmux`); never auto-install.

---

## 5. tmux integration (`tmux` + `inventory`)

### 5.1 Reading state
Per **active** machine, one poller goroutine runs every `HOSTBUD_POLL_INTERVAL` (default 3s):

```
tmux list-sessions -F '#{session_id}\t#{session_name}\t#{session_path}\t#{session_attached}\t#{session_windows}\t#{session_created}\t#{session_activity}'
```
(“no server running” ⇒ empty list, not an error.) Window/pane listing (`list-windows -a`) is fetched lazily when a session node is expanded.

The poller diffs against the in-memory cache and publishes `sessions.changed` / `machine.status` events on the bus. Browsers receive them over the events WebSocket. Pollers back off exponentially on failure and mark the machine `unreachable`.

*Later optimization:* tmux control mode (`tmux -C attach`) for push-based `%sessions-changed` notifications. The poller interface should allow swapping the implementation.

### 5.2 Mutations
- **Create:** `tmux new-session -d -s <name> -c <path> [-e KEY=VAL …] [<start-cmd>]` then attach. Default name `<project-basename>` or `<project-basename>-<n>`. `-e` requires tmux ≥ 3.2 (needed in v2 for hook env vars; degrade gracefully).
- **Rename:** `tmux rename-session -t '=<old>' <new>`.
- **Kill:** `tmux kill-session -t '=<name>'` — **always behind a confirmation dialog**.
- **Copy/scroll mode** (for mobile): `tmux copy-mode -t '=<name>'` issued as a side-channel exec, so we never depend on the user's prefix key or `mouse` setting.
- Always use `=`-prefixed exact targets.
- **Never modify the user's tmux config or global options.**

### 5.3 Project ↔ session mapping
A session belongs to the project whose `path` is the **longest prefix** of the session's `session_path` on the same machine. Unmatched sessions appear under an "Other sessions" node with a "Save as project" action. When a session is created from a project, record `(machine_id, session_name) → project_id` in `session_links` as a hint that takes precedence.

---

## 6. Interactive terminal (`term`)

- Browser opens `WSS /ws/term?machine=<id>&session=<name>&cols=&rows=`.
- Server spawns, inside a PTY (`creack/pty`):
  `ssh -F cfg -tt <alias> -- tmux attach-session -t '=<name>'` (with `TERM=xterm-256color`).
- Protocol: **binary frames** for terminal I/O in both directions; small **JSON text frames** for control (`{"type":"resize","cols":..,"rows":..}`, `{"type":"ping"}`; server → `{"type":"exit","code":..}`).
- Resize → `pty.Setsize` → ssh forwards window-change → tmux resizes.
- WebSocket close ⇒ kill the ssh process (tmux session survives). Client auto-reconnects with backoff and re-attaches; the tmux redraw restores the screen. The server holds **no terminal state**.
- Each tab/split pane = its own WebSocket + ssh process, all multiplexed over the machine's ControlMaster.
- Same session open in two views: tmux sizes per its `window-size` option; document this, don't override it.
- Backpressure: bounded write buffer per connection; drop the connection if the client stalls beyond a limit (it will reconnect and redraw).

**Frontend terminal:** `@xterm/xterm` + addons `fit`, `webgl` (fallback to canvas/DOM), `web-links`, `unicode11`, `search`, `clipboard`. Font: a bundled Nerd-Font-compatible monospace (self-hosted, no external CDN).

---

## 7. File browser (`fsbrowse`)

- SFTP via `github.com/pkg/sftp` over a pipe to `ssh -F cfg <alias> -s sftp` (reuses ControlMaster). Portable across Linux and macOS (no reliance on GNU `find -printf`) and safe with odd filenames.
- Operations: `home` (Getwd), `list(path)` (dirs first, hidden toggle, symlinks resolved lazily), `stat`, `mkdir`. No delete/rename in v1.
- UI: breadcrumb + path input with autocomplete, keyboard navigation, favorites = projects, recents per machine. Actions: **Create folder**, **Open as project**, **New session here**.
- Keep one SFTP client per active machine (lazy, idle-timeout close).

---

## 8. Persistence (`store`)

- SQLite via `modernc.org/sqlite` (pure Go, no CGO), WAL mode, `busy_timeout`. File at `/data/hostbud.db`.
- Migrations: embedded SQL files run at startup (`pressly/goose` or equivalent). Queries via `sqlc` or a hand-written repository interface — **no SQLite-specific SQL outside the store package**, so Postgres is a later swap.
- IDs: ULIDs (text). Timestamps: UTC.

**v1 schema (sketch)**
```sql
machines(id, source TEXT CHECK(source IN ('host','sshconfig','custom')),
         ssh_alias, label, active BOOL, sort_order, hidden BOOL,
         os, tmux_version, tmux_missing BOOL, last_seen_at, created_at, updated_at)
custom_connections(machine_id PK/FK, hostname, user, port, proxy_jump, identity_agent, extra_options_json)
projects(id, machine_id FK, path, name, sort_order, pinned BOOL,
         last_used_at, created_at, UNIQUE(machine_id, path))
session_links(machine_id, session_name, project_id, created_at, PRIMARY KEY(machine_id, session_name))
recent_commands(id, project_id, command, last_used_at)    -- start commands like `claude`, `codex`
ui_state(key PK, value_json, updated_at)                    -- tree collapse state, tab/split layout, theme
```

**Backups:** `make backup` copies the DB with `sqlite3 .backup` (or `VACUUM INTO`) to `./backups/` (gitignored).

---

## 9. API surface (v1)

REST (JSON), all under `/api`:
```
GET    /api/machines                      list (with status)
POST   /api/machines                      add custom connection
PATCH  /api/machines/:id                  label, sort, hidden, connection fields
DELETE /api/machines/:id                  custom only
POST   /api/machines/:id/activate | /deactivate
POST   /api/machines/refresh              re-scan ~/.ssh/config
GET    /api/machines/:id/hostkey          keyscan fingerprints
POST   /api/machines/:id/hostkey/trust
GET    /api/machines/:id/sessions
POST   /api/machines/:id/sessions         {name, path, startCommand?}
PATCH  /api/machines/:id/sessions/:name   rename
DELETE /api/machines/:id/sessions/:name   kill (UI confirms)
POST   /api/machines/:id/sessions/:name/copy-mode
GET    /api/machines/:id/fs?path=         list dir
POST   /api/machines/:id/fs/mkdir
GET    /api/machines/:id/fs/home
GET|POST|PATCH|DELETE /api/projects[/:id]
GET|PUT /api/ui-state/:key
GET    /api/health
```
WebSockets: `/ws/events` (server → client state events), `/ws/term` (interactive).

**Security middleware (all routes):**
- Reject WebSocket upgrades whose `Origin` ≠ `https://${HOSTBUD_DOMAIN}`.
- Optional Tailscale identity allowlist: if `HOSTBUD_ALLOWED_TS_USERS` is set and the tailscaled socket is mounted, resolve the client IP (from Caddy's `X-Forwarded-For`, trusted only from the Caddy container) via Tailscale LocalAPI `whois` and reject unknown users.
- CSRF: JSON-only API + SameSite cookies + Origin check on state-changing requests.
- Never log command strings containing user paths at info level.

---

## 10. v2 readiness — orchestration

Not built in v1, but v1 must not block it.

**Concepts**
- **Task:** a unit of work (prompt/instructions, target project or machine constraints, agent kind e.g. `claude`/`codex`, priority).
- **Slot:** per-machine (and optionally per-project) capacity for concurrent agent sessions.
- **Run:** a task bound to a tmux session; has a status lifecycle.
- **Status signals:** `running | waiting_input | blocked | completed | failed | unknown`.

**Detection (decided):** **hooks are the default.** When hostbud creates a session for a run, it injects `HOSTBUD_URL`, `HOSTBUD_RUN_ID`, `HOSTBUD_RUN_TOKEN` via `tmux new-session -e`. Agent hooks (e.g. Claude Code `Stop` / `Notification` hooks; equivalent for Codex) call `POST /api/hooks/:run_id` with the per-run bearer token. The **LLM supervisor is a fallback**: for runs with no hook signal within a window, it reads `tmux capture-pane -p -t '=<name>' -S -200` and classifies.
  - Implication: **target machines must be on the tailnet** to reach `HOSTBUD_URL`.
  - hostbud will ship hook scripts + install instructions per agent kind.

**Supervisor LLM:** provider interface `Classifier` (and later `Planner`) in `internal/llm`; **OpenAI first**, Anthropic/Ollama later. Config via `HOSTBUD_LLM_PROVIDER`, `HOSTBUD_LLM_MODEL`, `OPENAI_API_KEY`. Runs in-process (a separate container is only needed for a local model).

**Dispatcher:** subscribes to the event bus; when a run completes and a slot frees up, it takes the next eligible task, creates a session with the task's start command/prompt, and records a run.

**v1 obligations to keep this cheap later**
1. All state changes flow through the `events` bus (typed events), not direct UI pushes.
2. Session creation goes through one service function that already accepts env vars and a start command.
3. Store layer is dialect-agnostic; migrations are append-only.
4. The API has room for token-authenticated machine-to-server endpoints (`/api/hooks/*`) that bypass the browser-origin check but require per-run tokens.

**v2 schema (sketch)**
```sql
tasks(id, title, body, agent_kind, project_id NULL, machine_constraint NULL, priority, status, created_at, updated_at)
runs(id, task_id, machine_id, session_name, status, token_hash, started_at, ended_at, last_signal_at)
run_events(id, run_id, source CHECK(source IN ('hook','llm','poller','user')), status, payload_json, created_at)
machine_capacity(machine_id PK, max_concurrent_runs)
```

---

## 11. Frontend

- **Vue 3 + Vite + TypeScript**, Pinia stores, Vue Router (minimal).
- **Tailwind CSS + Reka UI** (headless, accessible primitives) for an IDE-like dense dark UI; light theme too via CSS variables.
- Layout: resizable left gutter (tree) | main area with **tabs**, each tab may be **split** (horizontal/vertical, via `splitpanes`). Layout persisted in `ui_state`.
- Tree: machines → projects → sessions (→ windows, lazily). Status dots (● attached/active, ○ detached, grey = machine inactive, red = unreachable, amber = tmux missing). Drag-to-sort (`vue-draggable-plus`), inline rename, collapse state persisted, context menus (attach, attach in split, new session, rename, kill, open folder, save as project).
- Command palette (⌘/Ctrl-K): jump to session/project/machine.
- **Mobile:** tree becomes a drawer; single terminal view; an on-screen key bar (Esc, Tab, Ctrl, Alt, arrows, `|`, `~`, `/`, Scroll-mode button → copy-mode API); larger touch targets.
- No external CDNs at runtime (fonts and assets bundled).

---

## 12. Configuration (env)

All config comes from environment (`.env`, gitignored). See `.env.example` for the full list. The app must start with sensible defaults and fail loudly (clear error) only for truly required values.

---

## 13. Testing strategy

- **Unit:** quoting/escaping, name validation, tmux output parsing, SSH config generation ordering, project-path matching.
- **Integration:** `test/sshd/` — a disposable container with `openssh-server` + `tmux` and a generated throwaway key; the test suite runs hostbud's sshx/tmux/fsbrowse packages against it (activate, list, create, attach via PTY, mkdir, kill).
- **Frontend:** Vitest for stores/utilities; Playwright smoke test (load app, tree renders, open terminal against the test sshd).
- No committed fixtures containing real hostnames, usernames, or paths — use `example.com`, `server-a`, `/home/dev`.

---

## 14. Repo layout

```
hostbud/
├─ cmd/hostbud/main.go
├─ internal/{api,events,inventory,sshx,tmux,fsbrowse,term,store,config}/
├─ internal/{llm,orchestrator}/          # v2
├─ migrations/                           # embedded SQL
├─ web/                                  # Vue app (built into web/dist, embedded)
├─ deploy/caddy/{Dockerfile,Caddyfile}
├─ test/sshd/                            # integration-test target container
├─ Dockerfile
├─ docker-compose.yml
├─ Makefile
├─ .env.example
├─ ARCHITECTURE.md  ROADMAP.md  AGENTS.md  CLAUDE.md  README.md  LICENSE
```
