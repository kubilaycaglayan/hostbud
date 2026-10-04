# hostbud — Architecture

hostbud is a self-hosted web app for managing **tmux sessions** from the browser, built for terminal-first and agentic-coding workflows. It runs in Docker on a single host. **v1 manages tmux on that host only**; the design keeps a `machine` abstraction so more SSH targets can be added later.

It is reachable two ways:
1. **Port forward:** Caddy serves plain HTTP on `127.0.0.1:${HOSTBUD_LOCAL_PORT}` (default `9055`); from another machine run `ssh -L 9055:localhost:9055 <host>` and open `http://localhost:9055`.
2. **Domain:** `https://${HOSTBUD_DOMAIN}`, bound to the host's Tailscale IP, so it is only reachable inside the tailnet (e.g. from a phone).

> The important state (tmux sessions and whatever runs in them) lives on the target machines. hostbud only remembers *metadata*: connections, projects (directories), UI layout — and in v2, tasks.

---

## 1. Goals and non-goals

**v1 goals**
- Single target: the **host machine**, reached over SSH from the container. It is always active; hostbud continuously reads its tmux sessions. (V2-M13 adds **servers**: other SSH targets added in the UI; see §4.1–4.3.)
- Left-gutter tree: **Project → Session**, expandable/collapsible, sortable, renameable, persisted. (The data model is Machine → Project → Session; the machine level is hidden while there is only one.)
- Full-fidelity terminal in the browser (attach to tmux as if in a local terminal). Multiple open terminal layouts, split view, mobile-friendly; select open sessions from the left tree.
- File-system browser to pick a directory, create folders, and start a session there. Chosen directories become **projects**.
- Deployed via Docker Compose behind Caddy: plain HTTP on loopback for port-forward access, TLS on the Tailscale IP for the domain.

**v1 non-goals**
- `~/.ssh/config` discovery and activation (deferred; see ROADMAP *Later*). Custom connections with a host-key trust UI arrived in V2-M13 (*servers*, §4.1–4.3).
- Storing terminal output, scrollback, or agent state.
- Installing software on targets (missing tmux → warning only).
- File editing / previews, git integration.
- Multi-user authorization beyond the single-user v1 account model. v1 still has one trusted host machine, but the web app requires an account; reachability is not sufficient by itself.

**v2** — see §10 and [roadmap-v2/ARCHITECTURE.md](roadmap-v2/ARCHITECTURE.md): task queue, per-machine capacity, hook-based session status, an optional LLM supervisor, and durable metadata-only queue history. Later browser suites run on demand. The LLM supervisor is off unless explicitly configured.

---

## 2. High-level design

```
 Browser (Vue SPA + xterm.js)
   │  HTTPS/WSS on the domain (tailnet only)   or   HTTP/WS via ssh -L to 127.0.0.1:9055
   ▼
 Caddy  (Tailscale IP :443 with TLS via Cloudflare DNS-01;  127.0.0.1:${HOSTBUD_LOCAL_PORT} plain HTTP)
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
   ├─ auth       account registration, sign-in, sessions, whitelist and login throttling
   └─ store      PostgreSQL (migrations and repository layer)
   │
   │  ssh (multiplexed via ControlMaster sockets in /data/ssh/cm)
   ▼
 host machine (via host.docker.internal)          [later: other SSH targets]
```

**Why this stack**
- **Go backend, single binary.** Latency is dominated by network and PTY I/O, not compute, so WebAssembly or exotic runtimes buy nothing. Go gives first-class PTYs (`creack/pty`), WebSockets, SFTP (`pkg/sftp`), static builds, and a tiny container.
- **System `ssh` binary, not a Go SSH library.** Gets `~/.ssh/config` semantics (ProxyJump, Include, Match, IdentityAgent, …), ssh-agent, and known_hosts for free and exactly as the user's terminal behaves. **ControlMaster** makes every non-interactive call (poll, list dir) reuse one TCP/SSH connection → millisecond-level round trips. Channels that stay open (terminal attaches, SFTP, streams) use separate long-lived masters (`/data/ssh/cml/%C-<n>`, at most 8 channels each, under sshd's default `MaxSessions 10`): on the shared master they would fill its session slots, and every short command would fall back to a fresh ~0.4 s handshake (creating and opening a session took seconds).
- **xterm.js (WebGL renderer).** The same terminal engine as VS Code; handles full-screen TUIs (Claude Code, Codex, vim) correctly.
- **PostgreSQL behind a repository layer.** Authentication needs durable, concurrent relational state and owner-managed SQL access to the email whitelist. The database is a Compose service with credentials supplied only through the untracked `.env`; the repository remains the only place with SQL.

---

## 3. Deployment topology (Docker)

Services in `docker-compose.yml` (Compose project `hostbud`; everything named with the `hostbud` prefix):

| Service | Image | Notes |
|---|---|---|
| `hostbud-caddy` | custom build (`caddy:builder` + `xcaddy` + `caddy-dns/cloudflare`) | Publishes `${TAILSCALE_IP}:443:443`, `${TAILSCALE_IP}:80:80` and `127.0.0.1:${HOSTBUD_LOCAL_PORT}:${HOSTBUD_LOCAL_PORT}` **only** (never `0.0.0.0`). Certificates and the ACME account live in the `hostbud-caddy-data` / `hostbud-caddy-config` volumes, so restarts don't re-issue. Two sites: `${HOSTBUD_DOMAIN}` with a cert via DNS-01 using `CLOUDFLARE_API_TOKEN` (read from the environment at runtime; `ACME_EMAIL` optional), and `http://:${HOSTBUD_LOCAL_PORT}` (plain HTTP, loopback only, for SSH port forwarding). Both reverse-proxy to `hostbud:8080` (WebSocket upgrade supported by default). Only the TLS domain site sends `Strict-Transport-Security: max-age=31536000`; it does not pin subdomains. Caddy waits for hostbud's healthcheck; it drops all capabilities except `NET_BIND_SERVICE`, uses `no-new-privileges`, and has a read-only root with `/tmp` writable. Config: `deploy/caddy/hostbud.caddy` holds the proxy snippet and the loopback site (shared with e2e); `deploy/caddy/Caddyfile` adds the global options (admin off; HTTP/1.1 and HTTP/2 only, since UDP isn't published) and the domain site. |
| `hostbud` | built from repo `Dockerfile` | No published ports. Runs as `${HOST_UID}:${HOST_GID}` with a read-only root filesystem, `/tmp` tmpfs, all capabilities dropped, `no-new-privileges`, and `pids_limit: 1024`. Compose starts Caddy only after `hostbud healthcheck` sees PostgreSQL-backed `/api/health` return 200. |
| `hostbud-postgres` | pinned official PostgreSQL image | No public or tailnet exposure. Owner maintenance access is optional and loopback-only at `127.0.0.1:${HOSTBUD_DB_LOCAL_PORT}:5432`; credentials come from the untracked `.env`. It uses `no-new-privileges`, drops all capabilities except the empirically verified `DAC_OVERRIDE`, `SETGID`, and `SETUID`, and reserves 128 MiB of shared memory. |

All three production services rotate `json-file` logs at 10 MiB with five files. The app image's `hostbud healthcheck` command calls `http://127.0.0.1:8080/api/health` with a two-second bound and exits non-zero on connection failure, timeout, or a non-200 status.

The optional `deploy/compose.tailscale.yml` override passes `HOSTBUD_ALLOWED_TS_USERS` and bind-mounts `${TAILSCALED_SOCKET}` read-only at `/run/tailscale/tailscaled.sock` (`create_host_path: false`). It is disabled by default for hosts without Tailscale. Enable it through `COMPOSE_FILE` in `.env`; if the allowlist is set but the socket is unavailable, startup fails.

**DNS:** Cloudflare `A` record `${HOSTBUD_DOMAIN}` → host's Tailscale IP (100.x.y.z), **DNS only (grey cloud)**. Never proxied (orange) and never a Cloudflare Tunnel — both would expose the app publicly. The name resolves publicly but the address is only routable inside the tailnet.

**hostbud container mounts / settings**
- `hostbud-data:/data` — generated SSH config, ControlMaster sockets and app known_hosts. PostgreSQL data lives in the separate `hostbud-postgres-data` volume.
- ssh-agent socket: `${HOST_SSH_AUTH_SOCK}:/run/ssh-agent.sock` and `SSH_AUTH_SOCK=/run/ssh-agent.sock`. Private keys never enter the container. The host uses a **dedicated key** (`~/.ssh/hostbud_ed25519`) loaded into the agent at boot by a systemd user unit; its `authorized_keys` entry is restricted with `from="172.16.0.0/12"` (Docker networks) and `no-agent-forwarding,no-port-forwarding,no-X11-forwarding`, so the compose network uses a fixed subnet in that range. The host should use a **stable** agent socket path (e.g. systemd user `ssh-agent.socket`, or `ssh-agent -a ~/.ssh/agent.sock`) — document this in README.
- The host's **public** host keys, read-only: `/etc/ssh/ssh_host_ed25519_key.pub`, `…_ecdsa_key.pub`, `…_rsa_key.pub` → `/run/host-keys/` (used to pin the host key, §4.3). Each file is bound individually with `create_host_path: false` (so a missing key fails loudly instead of Docker creating a directory).
- *Later (multi-machine):* user SSH config, **read-only, at the same absolute path as on the host**, with `HOME=${HOST_HOME}` inside the container so `~` and absolute `Include` paths resolve identically (`~/.ssh/config`, `~/.ssh/known_hosts`, optional `~/.ssh/config.d` via long-syntax bind with `create_host_path: false`). Never mount the whole `~/.ssh` (keeps private key files out).
- `extra_hosts: ["host.docker.internal:host-gateway"]` — the host machine is reached over SSH like any other target (requires `sshd` on the host and the user's own key in `authorized_keys`).
- *Later (multi-machine):* tailnet reachability from the container — traffic to 100.x routes through the host. Verify MagicDNS names resolve inside the container; if not, set `dns: [100.100.100.100]` on the service. Fallback (documented, not default): `network_mode: host`.

**Runtime image:** multi-stage — `node` (build SPA) → `golang` (build with SPA embedded via `go:embed`, `CGO_ENABLED=0`) → `debian:bookworm-slim` with `openssh-client`, `postgresql-client-15`, `ca-certificates`, `tini`. Target arch: **linux/amd64**. The image creates a `hostbud` user with `${HOST_UID}:${HOST_GID}` (build args) because `ssh` refuses to run for a uid without a passwd entry, and pre-creates `/data` owned by it so the named volume is writable. The Compose network's subnet is `${HOSTBUD_SUBNET}` (default `172.29.55.0/24`).

The runtime container's root filesystem is read-only; `/tmp` is a tmpfs and the only persistent app write path is `/data`. `hostbud healthcheck` is a lightweight command used by Compose's healthcheck and does not load the app configuration or connect to PostgreSQL except through the local health endpoint.

**Build & deploy:** the dev machine is the host; `docker compose up -d --build` (wrapped in `make deploy`). No registry.

**Dockerized toolchain:** everything that can run in Docker does. `make build`, `test`, `lint` (golangci-lint, eslint/vue-tsc) and `gitleaks` run in containers, so the host needs only Docker. The gitleaks pre-commit hook also runs via Docker. Tools run in long-lived toolbox containers (`hostbud-tools-<tool>`, `scripts/tool.sh`) that `make` reaches with `docker exec`: creating a container costs seconds per call on a busy daemon, while an exec is near-instant. No CI for now.

`make doctor` is a read-only host-side prerequisite check. It verifies Docker/Compose access, private and complete `.env` settings, the current UID/GID, the SSH agent and host SSH/tmux prerequisites, pinned public keys, Tailscale address/DNS, published ports, subnet conflicts and the optional LocalAPI socket. It prints a check and a fix without printing configured values.

---

## 4. SSH layer (`sshx`)

### 4.1 Generated config
hostbud writes `/data/ssh/config` on startup and whenever custom connections change, and always invokes `ssh -F /data/ssh/config …`. Because OpenSSH uses the **first** value obtained for each option, order matters:

```sshconfig
# 1. Built-in host machine
Host hostbud-host
  HostName ${HOSTBUD_HOST_ADDR}   # default host.docker.internal; e2e points it at the target container
  User ${HOST_SSH_USER}
  HostKeyAlias hostbud-host          # pinned key is stored under this name

# 2. Servers added in the UI (V2-M13, from DB), one block each:
Host hostbud-custom-<id>
  HostName <host>  Port <port>  User <user>
  HostKeyAlias hostbud-custom-<id>   # the keys the owner confirmed are pinned here
# (later) 3. The user's real config (read-only mount): Include ~/.ssh/config

# 4. App defaults — last, so user settings win
Host *
  ControlMaster auto
  ControlPath /data/ssh/cm/%C
  ControlPersist 10m
  ConnectTimeout 10
  ServerAliveInterval 15
  ServerAliveCountMax 3
  UserKnownHostsFile /data/ssh/known_hosts
  StrictHostKeyChecking yes
  BatchMode yes                # never prompt inside non-interactive calls
```

- `ControlPath` uses `%C` (hash) to stay under the Unix socket path length limit.
- `BatchMode yes` for all non-interactive calls; interactive attach also relies on agent auth (no password prompts in v1).

### 4.2 Servers (V2-M13) and host discovery (later)
- **Servers** are `machines` rows with `source = 'custom'`: id `s-<10 hex>`, alias `hostbud-custom-<id>`, a nickname (`label`, 1–40 characters, unique case-insensitively), `host_name`, `port`, `ssh_user` and the confirmed `host_keys`. They persist in PostgreSQL; on startup and on every add/remove, sshx rewrites the generated config and `known_hosts` atomically from them. Host names, users and ports are validated before they reach any argv or config line.
- A runtime registry (`internal/machines`) owns one inventory poller and one SFTP browser per machine. Adding a server starts its poller; removing one stops it, ends its ControlMasters and publishes `machine.removed`. Removal is refused while projects use the server, and never touches its tmux sessions.
- Authentication is the agent key only: the server's `authorized_keys` must accept it. Status messages name *the server* instead of *the host* and point at the Servers dialog.
- **Queues on servers:** a server's project takes queues like a host project. Its runs start on that server (the queue, its items and runs carry the server's `machine_id`), and their hooks call `HOSTBUD_SERVER_HOOK_BASE_URL`, or `https://${HOSTBUD_DOMAIN}` when it is empty, so the server must reach hostbud there (usually over the tailnet; hook routes skip the Origin, cookie and Tailscale identity checks and use the per-run token). With neither set, a server run fails at start with that hint. The dispatcher hands out slots per machine: the host uses the cap from Settings, a server the default cap (`DefaultConcurrentRuns`). The parallel-queues switch applies to every machine. Each machine's inventory snapshot ends only that machine's runs and releases only its session-linked queues. Verify commands run in the project directory on the queue's machine.
- In the UI, sessions are identified by a **session ref**: the bare name on the host, `machine/name` on a server, so saved tree state from before V2-M13 stays valid. Projects place only sessions on their own machine. A server's project rows and its unplaced sessions show a chip with the server's nickname. New session and Browse files show a *Server* select once a server exists.

**Host discovery (later — multi-machine)**
- Parse the user's config with `github.com/kevinburke/ssh_config` **for display only** (list concrete `Host` aliases; skip wildcard/negated patterns). Resolve effective settings for display via `ssh -G <alias>`.
- Machine sources: `host` (built-in, `host.docker.internal`, user `${HOST_SSH_USER}`, label `${HOSTBUD_HOST_LABEL}`), `sshconfig` (discovered; re-scanned on startup and via "Refresh"), `custom` (DB).
- Discovered hosts are shown but **inactive by default**; activation state persists in DB.

### 4.3 Host-key trust
Never trust on first use.
- **v1 (host machine):** on startup hostbud reads the read-only mounted `/run/host-keys/ssh_host_*_key.pub` and writes them to `/data/ssh/known_hosts` as `hostbud-host <key>`. The key comes from the host's filesystem, not from the network, so no UI confirmation is needed. Missing key files → startup error with instructions.
- **Servers (V2-M13):** `ssh-keyscan` (argv only, validated host and port, 10 s) → the UI shows the SHA256 fingerprints → only the keys the owner confirms are stored with the server and pinned under its `HostKeyAlias`. A key that later differs is a hard error asking to remove and re-add the server.
- Mismatch → hard error surfaced in UI.

### 4.4 Command execution rules
- Every remote command goes through one function that builds `ssh -F cfg <alias> -- <cmd>`; `<cmd>` is assembled only from **shell-quoted** arguments (single-quote escaping helper). Never interpolate user input unquoted.
- Validate tmux session names: `^[A-Za-z0-9_-]{1,64}$` (tmux forbids `.` and `:`; we are stricter). Whitespace in a typed name (create, rename, project session) becomes `-` without asking ("new session" → "new-session"), in the UI and at the API before validation.
- Per-command timeouts (default 10s); context cancellation kills the process.
- Machine capability probe on startup (and on activation, later): `uname -s; command -v tmux; tmux -V` → store `os`, `tmux_version`, `tmux_missing`. If tmux is missing, mark the machine and show the install command (`apt install tmux` / `brew install tmux`); never auto-install.

---

## 5. tmux integration (`tmux` + `inventory`)

### 5.1 Reading state
Per **active** machine (v1: the host, always active), one poller goroutine runs every `HOSTBUD_POLL_INTERVAL` (default 3s):

```
tmux list-sessions -F '#{session_id}\t#{session_name}\t#{session_path}\t#{session_attached}\t#{session_windows}\t#{session_created}\t#{session_activity}'
```
(“no server running” ⇒ empty list, not an error.) Window and pane listing is fetched lazily when a session node is expanded, in one side-channel exec using `list-windows -t '=<name>'` followed by `list-panes -s -t '=<name>'`, under `LC_ALL=C.UTF-8` so tmux preserves tab separators and Unicode for the non-interactive SSH client. The response is sorted by window/pane index and capped at 256 windows and 64 panes per window; free-form names and commands are sanitized and capped at 256 bytes. This layout is not polled or published as an event. An authenticated `POST /api/machines/:id/sessions/:name/select` validates window/pane ids against that session before issuing `select-window` / `select-pane`; selecting a window is visible to every client attached to the session, as with tmux's `prefix n`.

The poller diffs against the in-memory cache and publishes `sessions.changed` / `machine.status` events on the bus (one poller per machine; removing a server publishes `machine.removed`). Browsers receive them over the events WebSocket. Pollers back off exponentially on failure and mark the machine `unreachable`.

**Foreground agent marks and status (M8 T12–T13):** each inventory poll reads `list-panes -a` and checks recognized process names on each pane's TTY (one `ps -A` per poll joined to the panes in `awk`, not one `ps` per pane), so launchers whose foreground command is a generic runtime such as `node` can still be identified. Only recognized agent names are returned to hostbud; other process names and all arguments stay on the host. Inventory also reads the hostbud pane user option `@hostbud_agent_status`. Optional user-wide Codex/Claude Code hooks write fixed `working`, `blocked`, or `ended` values to the client's pane (`scripts/agent-status-hook.py`). Codex runs hooks in its shared app-server daemon without `TMUX_PANE`, so the script matches the `codex` process with a terminal in the event's `cwd` to a pane by tty, remembering each session's pane/pid claim in the user's state directory; ambiguous matches are skipped; hostbud reads but never writes tmux options. A stale `ended` marker is omitted while a recognized Codex process remains in that pane because the live process alone cannot establish whether Codex is still working or has stopped; a tracked active status becomes ended when a known interactive shell resumes, covering common forced exits. Per-session status precedence is blocked, working, ended. Hook configuration stays user-managed and is documented in the README. Status and agent marks travel in collapsed session events, so the UI needs no window/pane expansion. Process-name detection remains best effort; background or differently named agents may not receive a logo.

**Codex token usage (M8 T28):** the optional status hook reads the last 1 MiB of its hook-supplied rollout under `$CODEX_HOME` (default `~/.codex`), in `sessions` or `archived_sessions`, accepting only regular files and structured `event_msg/token_count` records. It copies only `last_token_usage.total_tokens`, `total_token_usage.total_tokens` and `model_context_window` into `@hostbud_codex_usage`; cache/reasoning subtotals are not added twice. Missing or invalid counts clear the option. Inventory reports usage only for a recognized Codex in the active pane of the active window, via `agentUsage` (formerly `codexUsage`) on `sessions.changed` and session snapshots. The header labels these last-reported context and consumed counts, with exact values in accessible text; counts are neither billing nor quota estimates. No transcript content or path leaves the hook, no database changes, and hostbud does not install hooks or change user config.

**Claude Code token usage (M8 T30):** for Claude Code events the same hook reads the hook-supplied transcript (only regular `.jsonl` files under `$CLAUDE_CONFIG_DIR/projects`, default `~/.claude/projects`) and writes `context,total,0` to `@hostbud_claude_usage`. Context is the last non-sidechain assistant message's input + cache creation + cache read + output tokens; total sums the same once per message id. Because transcripts are unbounded, the hook reads incrementally from a per-transcript offset cached (with counts only) in `~/.local/state/hostbud/claude-usage.json`; a replaced or shrunk file restarts. Inventory emits agent-tagged `U` records and publishes `agentUsage` (`agent`: `codex` or `claude`) for the active pane's recognized agent; T28's `codexUsage` field was renamed accordingly.

*Later optimization:* tmux control mode (`tmux -C attach`) for push-based `%sessions-changed` notifications. The poller interface should allow swapping the implementation.

### 5.2 Mutations
- **Create:** `tmux new-session -d -s <name> -c <path> [-e KEY=VAL …] [<start-cmd>]` then attach. The user may give a custom name; otherwise the default is the directory's last path segment (`/home/dev/docs` → `docs`; characters a name can't hold become `-`). If either an auto-derived or typed name is already in use, creation picks the first free `<name>-<n>` (`work` → `work-1`; a typed `work-1` → `work-1-1`) and returns that actual name; a colliding 64-character name is trimmed to fit the suffix. A tmux race retries up to 20 times. Rename still returns 409 for a taken name. `-e` requires tmux ≥ 3.2 (needed in v2 for hook env vars; degrade gracefully).
- **Rename:** `tmux rename-session -t '=<old>' <new>`.
- **Kill:** `tmux kill-session -t '=<name>'` — **always behind a confirmation dialog**.
- **Copy/scroll mode** (for mobile): authenticated `POST /api/machines/:id/sessions/:name/copy-mode` accepts `enter`, `scroll-up`/`scroll-down` (1–500 lines), `wheel-up`/`wheel-down` (1–500 lines), `page-up`/`page-down`, `top`, `bottom` or `exit`. The wheel actions follow tmux's default wheel binding: if the pane's app enabled mouse reporting and the pane isn't in a mode (Claude Code, Codex), hostbud writes wheel reports into the pane (`send-keys -H`, tmux ≥ 3.1, one report per 3 lines, at most 20) so the app scrolls its own history; otherwise they scroll tmux copy mode, entering it (`copy-mode -e`) on the way up. On touch screens a vertical swipe over the terminal sends wheel actions (`web/src/lib/touchScroll.ts`; experimental momentum: a flick coasts with a 325 ms friction time constant, same-direction flicks stack up to 12 px/ms, a touch stops it), and xterm's local scrollbar is hidden, since xterm only holds what reached the browser since attaching. It uses the exact `=<name>:` pane target and side-channel `tmux copy-mode -e -u` / `send-keys -X` commands, so it never depends on the user's prefix key, `mode-keys` or `mouse` setting. Each command reads back `{inMode, scrollPosition, historySize}` from tmux. If tmux has already left copy mode, a repeated scroll/exit reports its current state instead of an error. Requires tmux ≥ 2.4. Copy mode is transient pane state: it is neither stored nor polled and publishes no hostbud event. Since copy mode belongs to the pane, all clients attached to that pane see its scrolling together.
- Always use `=`-prefixed exact targets.
- **Never modify the user's tmux config or global options.**

### 5.3 Project ↔ session mapping
A session belongs to its `session_links` project when a link exists on the same machine; otherwise it belongs to the project whose `path` is the **longest path-component prefix** of `session_path` on that machine. Unmatched sessions appear under an "Other sessions" node with a "Save as project" action. Project-created sessions record `(machine_id, session_name) → project_id` as a hint. A successful UI rename updates this link before inventory publishes its refreshed session list; ended sessions have links removed when a full `sessions.changed` snapshot no longer includes them. Removing a project cascades its session links and recent commands, publishes `projects.changed` with action `deleted`, and clients immediately re-resolve those live sessions against the remaining longest path matches (or Other sessions); the tmux sessions and attached terminals are unaffected. The placement service is event-driven and adds no polling loop.

---

## 6. Interactive terminal (`term`)

**Native output reader (M8 T8):** authenticated `GET /api/machines/:id/sessions/:name/output` reads the active pane with `tmux capture-pane -p -e -J -S - -t =<name>:` through the timeout-bounded `sshx` executor. It includes all history still retained by tmux, including output before browser attachment, and joins soft-wrapped terminal rows. It does not enter copy mode or mutate tmux. Responses use `Cache-Control: no-store`; output is never persisted or logged. The browser renders text with SGR color/style spans (Vue escaping, no HTML injection), native selection/copy and vertical scrolling. Long lines wrap at the view width. Opening starts at the latest output; closing discards the capture. History already discarded by tmux or kept only inside an application's own UI cannot be recovered by capture-pane.

- Browser opens `WSS /ws/term?machine=<id>&session=<name>&cols=&rows=`.
- Server spawns, inside a PTY (`creack/pty`):
  `ssh -F cfg -tt <alias> -- tmux attach-session -t '=<name>'` (with `TERM=xterm-256color`). When the inventory reports tmux 3.2 or newer it is `tmux -T sync attach-session …`: this client (only) declares the synchronized-output terminal feature, so tmux wraps each redraw in DEC mode 2026 marks and xterm shows it as one frame (M8 T6). No server option or user config changes.
- Protocol: **binary frames** for terminal I/O in both directions; small **JSON text frames** for control (`{"type":"resize","cols":..,"rows":..}`, `{"type":"ping"}`; server → `{"type":"exit","code":..}`).
- Resize → `pty.Setsize` → ssh forwards window-change → tmux resizes.
- WebSocket close ⇒ kill the ssh process (tmux session survives). The server holds **no terminal state**. The server pings each client every 25 s and drops one that doesn't answer within 10 s (a vanished browser), which also ends its ssh process.
- Before upgrade, terminal attaches reserve one of `HOSTBUD_MAX_TERMINALS` (default 128) slots, and of `HOSTBUD_MAX_TERMINALS_PER_USER` when set (default 0: no per-account cap). A full cap returns HTTP 429 before starting ssh; reservations are released when the attach process exits. `/api/runtime/terminal-slots` lets the browser recognize a rejected upgrade and stop retrying until the user retries.
- The first-output watchdog uses `HOSTBUD_EXEC_TIMEOUT`; if the host produces no terminal data before it expires, the server closes with `4408` (`host didn't answer`). The client treats that close as a dropped connection and retries.
- **Auto-reconnect** (`web/src/api/term.ts`, `TermSession`): the client sends `{"type":"ping"}` every 10 s, and 25 s without any frame (a network cut hangs TCP without a close) counts as a drop. After a drop it re-attaches with backoff 0.5 s, 1, 2, 4, 8, then every 10 s (±20 % jitter, no cap), right away on the browser's `online` event or when the page becomes visible; the backoff starts over once an attach has stayed up for 5 s. It re-attaches at the current size and keeps xterm's buffer (tmux's redraw replaces the screen; the scrollback stays searchable). Keys typed meanwhile are dropped, not queued. It does **not** retry after an `exit` frame (detach, or the session ended), after the view closed, for a session that left the live list, or when an attempt that failed before opening finds the sign-in gone (`/api/auth/me` 401 ⇒ the sign-in form). Exit code 255 is ssh's own failure (host unreachable, a dead ControlMaster), not tmux ending, so it is retried like a drop. While retrying, a strip over the terminal says "Reconnecting… (attempt n)" with **Retry now**.
- Each layout/split pane = its own WebSocket + ssh process, multiplexed over the machine's long-lived ControlMasters (§2). Inactive layouts detach terminal clients and reconnect on activation; the layout has no cap on open views (only the server-wide attach cap, §11).
- Same session open in two views: tmux sizes per its `window-size` option; document this, don't override it.
- Backpressure: queued terminal output is bounded to 2 MiB per connection. Crossing the byte bound or a 10 s blocked WebSocket write closes with 1013 and kills only that ssh attach; the tmux session remains, and the browser retries and redraws.
- `/ws/events` allows 16 open sockets per account. The socket is read-only; a client data frame closes with 1003. If the events subscriber queue fills, the server closes with 1013 and the next connection starts with a fresh snapshot.
- WebSocket query strings over 2 KiB are rejected with 400 before any ssh process starts. The terminal input message cap remains 1 MiB; when PTY input backs up, only that connection's reader blocks.

**Keys:** Mac "natural text editing" shortcuts (`web/src/lib/terminalKeys.ts`) send what a macOS terminal sends: Option+Backspace `ESC DEL`, Cmd+Backspace `Ctrl-U`, Option+←/→ `ESC b`/`ESC f`, Cmd+←/→ `Ctrl-A`/`Ctrl-E`. Copy is Ctrl+Shift+C / Cmd+Shift+C (and Cmd+C with a selection); paste is Ctrl+Shift+V / Cmd+Shift+V / Cmd+V. Ctrl+C and Ctrl+V stay the program's. Global chords are dispatched from the window capture phase and xterm's custom key handler returns `false` for them, so they never reach a terminal program: ⌘K / Ctrl+Shift+K for the palette, ⌘/ / Ctrl+Shift+/ for shortcuts, ⌘⇧E / Ctrl+Shift+E for tree ↔ terminal focus, Ctrl+Shift+] / [ for cycling open sessions whose tree rows are visible, and Ctrl+Shift+D for the last selected session (Ctrl+⌘+D is also supported on Mac). The registry (`web/src/lib/shortcuts.ts`) is shared with the help dialog and palette. Outside-terminal shortcuts ignore terminal and text-entry focus; tree shortcuts require a focused tree row. Other keys are xterm's.

**Clipboard** (`web/src/lib/clipboard.ts`): copy writes the selection with `navigator.clipboard.writeText` and keeps it; a refusal shows a toast. Text paste keys keep the browser's default, so their `paste` event reaches xterm, which wraps the text in `ESC[200~ … ESC[201~` when the program enabled bracketed paste (no clipboard-read permission needed). An image-file paste is intercepted before xterm and uploaded unchanged through the authenticated SFTP photo-upload path to the active session's repo directory; plain text remains terminal input and non-image files are ignored. On a filename conflict the browser retries with `-1`, `-2`, etc. before the extension. After each successful image upload, its path relative to the session's recorded directory is pasted at the terminal cursor through xterm, preserving bracketed-paste behavior. The context menu's Paste uses `readText()` + `term.paste()`. The context menu (Copy, Paste, Select all) opens on right-click or a long press, and with Shift/Option+right-click when the program captures the mouse (a plain right-click then belongs to the program, e.g. tmux's menu). Since terminal text is rendered on canvas, touch long-press selects the word under the finger using its absolute xterm buffer row and exposes a Copy action; moving the finger cancels the selection timer. Shift+drag (Option+drag on macOS, `macOptionClickForcesSelection`) selects when the mouse is captured. The clipboard API needs a secure context: `http://localhost` and the HTTPS domain qualify, a plain-HTTP LAN address doesn't.

**OSC 52** (`@xterm/addon-clipboard` with a custom provider) is **write-only**: writes (any selection parameter; tmux sends an empty one) go to the browser clipboard, without awaiting, so a refused write never stalls the terminal; payloads over 1 MiB decoded, empty or undecodable ones are ignored. Queries (`52;c;?`) are swallowed by a handler registered after the addon, so nothing answers them and no program on the host can read the browser clipboard. tmux's default `set-clipboard external` forwards copy-mode yanks as OSC 52 (`xterm*` has the `clipboard` feature); programs inside tmux (vim, Claude Code) need `set -g set-clipboard on` in the user's `~/.tmux.conf`, which hostbud documents and never changes.

**Links** (`web/src/lib/links.ts`): printed URLs (`web-links` addon) and OSC 8 hyperlinks (xterm's `linkHandler`, e.g. `ls --hyperlink`, Claude Code) open on click or tap, only for `http:`/`https:`, in a new tab with `noopener,noreferrer`; anything else (`javascript:`, `file:`, `data:`, `ssh:` …) is ignored. An OSC 8 link's text can differ from its target, so hovering shows the target in a tooltip. tmux forwards OSC 8 only to terminals with its `hyperlinks` feature, which isn't in its defaults: users add `set -as terminal-features ',xterm*:hyperlinks'` (documented; hostbud never changes tmux config).

**Scrollback under tmux** (`web/src/lib/scrollback.ts`): tmux draws in the alternate screen, where xterm keeps no scrollback, so the browser would have nothing to search or scroll back through. xterm therefore ignores the alternate-screen switches (DECSET/DECRST 1049, 1047, 47): tmux draws on the normal screen, and lines scrolled off the top of its pane region go to xterm's scrollback (5000 lines). tmux scrolls bursts with `CSI n S`, which xterm would drop; a handler saves those lines too when the region starts at the top row. That handler uses xterm 6 internals (no public API scrolls into the scrollback) and falls back to xterm's default if they change; `scrollback.spec.ts` pins it against the real xterm. Lines tmux never sends (what scrolls past between two of its screen updates) aren't recovered; tmux copy mode has them. Side effects: with tmux `mouse off`, the wheel scrolls xterm's scrollback instead of sending arrow keys, and full-screen programs that scroll a top-anchored region (e.g. vim's Ctrl-E) add their lines to it.

**Search** (`TerminalSearch.vue`, `@xterm/addon-search`): Ctrl+Shift+F, Cmd+F, Cmd+Shift+F or the 🔍 button (plain Ctrl+F stays readline's). It searches xterm's buffer: the screen plus the scrollback received since attaching (kept across auto-reconnects). tmux history from before the attach is copy mode's job (`prefix [`, then `?`). Match case and Regex options, a match count, all matches highlighted and the current one emphasized, Enter/Shift+Enter for next/previous, Escape closes and refocuses the terminal; an invalid regex shows "Invalid pattern". The addon only re-highlights for a new term, so an option change clears its cache first.

**Frontend terminal:** `@xterm/xterm` + addons `fit`, `webgl` (fallback to canvas/DOM), `web-links`, `unicode11`, `search`, `clipboard`. Font: a bundled Nerd-Font-compatible monospace (self-hosted, no external CDN).

---

## 7. File browser (`fsbrowse`)

- SFTP uses `github.com/pkg/sftp` over a pipe to the system command `ssh -F /data/ssh/config hostbud-host -s sftp`; it reuses the generated SSH config and host-key pin, on a long-lived ControlMaster like terminal attaches. Filesystem paths are sent as SFTP protocol paths and never enter a remote shell command.
- The browser keeps one lazy SFTP client for the active host, closes it after one minute idle, and bounds each operation by `HOSTBUD_SFTP_TIMEOUT` (10 s default). Four operations may run at once; excess requests wait within their own deadline. Cancellation or timeout closes the SSH subsystem stream because an in-flight SFTP packet may be desynchronized; the next operation opens a fresh subsystem. App shutdown also closes the client and child process.
- Paths use target POSIX rules: empty, `~`, and relative paths resolve from SFTP home; all results are cleaned absolute paths. Paths are limited to 4096 bytes. Listings return at most 2000 entries and 1 MiB of JSON, sort directories before other entries, then names in stable byte order, and set `truncated: true` when a limit omits data; names are capped at 1024 bytes in the response. Dot entries are omitted unless `hidden=true`.
- Listing uses `Lstat` metadata and does not follow symlinks. Explicit `stat` reports whether a symlink target resolved, is broken, loops, or is unreadable. Creating a folder accepts one child name (1–255 bytes), rejects empty, dot, dot-dot, slash and NUL names. The browser has no user-facing delete or rename action for existing files.
- Photo upload (M8 T10) is a separate authenticated, Origin-checked raw `PUT /api/machines/:id/fs/upload?directory=&name=`. The PWA passes the selected browser `File` directly as `application/octet-stream`; it never decodes, transforms or re-encodes image data. The API caps files at 100 MiB. SFTP writes to a random exclusive temporary sibling and renames it after the exact declared byte count is written; existing names return 409 and incomplete temporary files are removed when the SFTP connection allows cleanup. The UI offers `image/*`, HEIC/HEIF and DNG selections in the three-dot menu and targets the active session's recorded repository path.
- API responses: home is `{path}`; a listing is `{path, entries, truncated}` (`name`, absolute `path`, `kind`, `size`, UTC `modifiedAt`, and optional `symlinkState`); stat returns one entry plus `symlink`; mkdir returns `{path}` with 201; photo upload returns `{path,size}` with 201. Directory operations use `HOSTBUD_SFTP_TIMEOUT`; photo transfers use `HOSTBUD_UPLOAD_TIMEOUT` (5 min default, 30 s–10 min) and return 504 with an actionable subsystem hint when it expires. Filesystem errors use the standard `{error,hint}` shape and do not include target paths in info logs.
- API routes are authenticated: `GET /api/machines/:id/fs/home`, `GET /api/machines/:id/fs?path=&hidden=`, `GET /api/machines/:id/fs/stat?path=`, Origin-checked `POST /api/machines/:id/fs/mkdir` with `{path,name}`, and raw octet-stream `PUT /api/machines/:id/fs/upload`. Unknown machine IDs return 404; unsupported methods and delete/rename routes do not mutate the target.
- UI: Browse files opens in a modal dialog from the app bar's icon-only **Browse files** (`FolderSearch`) action; it remains a modal outside the sidebar. The app bar also has an icon-only **New session** (`SquareTerminal`) action; both follow the host name (M8 T1). The header's **Hide sidebar** / **Show sidebar** icon toggles the desktop sidebar and remembers its state in browser storage; on compact screens it opens the drawer, whose **Hide sidebar** button, swipe and Escape close it. The aside is labelled **Sessions**; the drawer keeps an accessible, visually hidden **Project tree** title. The browser provides a breadcrumb, path input with autocomplete, keyboard navigation, favorites = projects, and recents per machine. The path bar has an icon-only FolderPlus action to add the currently shown directory, changing to FolderOpen when that path is already a project; it uses the same project selection/create logic as child-directory actions and is disabled while the listing is loading or failed. Directory project actions are icon-only FolderPlus (add) and FolderOpen (existing project) buttons with accessible labels and tooltips. Other actions are **Create folder** and **New session here**. **Density (M8 T3):** the dialog is sized by its content, at most `min(38rem, 100vw − 2rem)` wide and `min(40rem, 85vh)` tall on desktop (a bottom sheet up to 85 dvh on phones), with tight padding and rows; controls use compact visual padding, and `touch-target` still gives them 44 px on touch screens.
- **Browser autocomplete (M8 T3):** every application input, textarea and combobox declares `autocomplete="off"`, except the login screen's password input, which keeps `current-password` / `new-password` unchanged. `web/src/lib/autocomplete.spec.ts` audits every control in the Vue sources.
- **Photo transfer (M8 T10):** from a session's three-dot terminal menu, select photos or paste image files, and send them to that session's recorded repo path. The browser uses the selected `File` object directly, including HEIC/HEIF and DNG where iOS exposes them. Existing names receive an incrementing suffix; they are never overwritten. After upload, the file path relative to the session's recorded directory is pasted into the terminal. The Queue panel's instruction textareas (new and edited items) intercept image pastes the same way: the file goes to the queue's project path and its project-relative path is inserted at the caret. The 100 MiB request-body cap and upload deadline are enforced at both API/SFTP layers. Transfer preserves the selected file's bytes; whether iOS Photos supplies the library original or an exported representation depends on the system picker choice and remains an owner device check. iOS Safari does not currently support registering a PWA as a Web Share Target ([WebKit issue 194593](https://bugs.webkit.org/show_bug.cgi?id=194593)), so the in-app file picker is the supported PWA route.

---

## 8. Persistence (`store`)

- PostgreSQL via a pinned official image, with the application connecting over the private Compose network. Database name, user, host, port and password come from `HOSTBUD_DB_*` environment variables. The password is never committed, logged or placed in an image.
- Migrations: embedded SQL files (`internal/store/migrations/`) run at startup with `pressly/goose`; append-only. Queries via `sqlc` or a hand-written repository interface — **no SQL outside the store package**.
- Startup waits up to 90 s for PostgreSQL and returns an actionable error if it stays unavailable. The pool allows 20 open and 5 idle connections, with 30 min max lifetime and 5 min max idle time. Every connection sets `statement_timeout=5s`, `lock_timeout=3s`, `idle_in_transaction_session_timeout=30s`, and `connect_timeout=5s`. Store methods use the caller's context.
- IDs: ULIDs (text). Timestamps: UTC.
- The owner can inspect and maintain the database with `docker compose exec hostbud-postgres psql -U <HOSTBUD_DB_USER> -d <HOSTBUD_DB_NAME>` using values in the local `.env`, or from the host through the loopback-only `HOSTBUD_DB_LOCAL_PORT` mapping. This is an operator access path, not an application API.
- UI state is per account: `store.UIStateForUser`/`PutUIStateForUser` namespace the key as `user:<user-id>:<key>` in `ui_state` (no migration), so accounts never see each other's layout, tree customizations or theme. The `theme` value is `{version: 1, mode: "dark" | "light" | "system"}`; clients treat any unsupported version or mode as System.
- M6 tree preferences use the per-account `tree` UI-state value; the legacy/global `projects.pinned`, `projects.sort_order` and `machines.hidden` columns are reserved and remain unused by the UI.
- PostgreSQL is initialized as a fresh application database for M1. The provisional SQLite database is not imported because the pre-M1 deployment is unused; if an old SQLite file remains in the app data volume, it is left untouched. PostgreSQL schema migrations are append-only.

**v1 schema (sketch)** — `machines` is seeded with the single built-in `host` row.
```sql
machines(id, source TEXT CHECK(source IN ('host','sshconfig','custom')),
         ssh_alias, label, active BOOL, sort_order, hidden BOOL,
         os, tmux_version, tmux_missing BOOL, last_seen_at, created_at, updated_at)
-- V2-M13 (0021): a server's connection lives on its machines row:
--   host_name, port (1–65535, default 22), ssh_user, host_keys ("<type> <base64>" lines the owner confirmed)
projects(id, machine_id FK, path, name, sort_order, pinned BOOL,
         last_used_at, created_at, UNIQUE(machine_id, path))
session_links(machine_id, session_name, project_id, created_at, PRIMARY KEY(machine_id, session_name))
recent_commands(id, project_id, command, last_used_at)    -- start commands like `claude`, `codex`
ui_state(key PK, value_json, updated_at)                    -- per account: key = user:<user-id>:<key> (layout; M4: tree; M6: theme)
users(id, email, email_normalized UNIQUE, password_hash, created_at, updated_at,
      last_login_at, disabled BOOL)
email_allowlist(email_normalized PK, enabled BOOL, note, created_at, updated_at)
auth_sessions(id_hash PK, user_id FK, expires_at, created_at, last_seen_at,
              user_agent, created_ip)
login_rate_limits(scope_key PK, failures INT, blocked_until, last_failure_at,
                   updated_at)
-- v2 agent queue (V2-M1, migration 0005; docs/roadmap-v2/ARCHITECTURE.md §6)
queues(id, machine_id, project_id, name, status, created_at, updated_at)
queue_items(id, queue_id, machine_id, position, agent, flags, instruction, status, …, UNIQUE(queue_id, position))
runs(id ULID, item_id, machine_id, session_name, agent_session_id, transcript_path, transcript_offset,
     client_version, token_hash (SHA-256), status, started_at, ended_at, last_signal_at, detail)
run_events(id, run_id, machine_id, source, kind, payload_json ≤ 64 KiB, created_at)
```
The queue tables have no implicit cascades: deleting a queue is one explicit transaction and is refused while a run is active, and a project with a queue can't be deleted until its queue is.

Project paths are absolute POSIX paths, cleaned at the repository boundary and
limited to 4096 bytes; project names are trimmed and limited to 255 bytes.
Projects are unique by `(machine_id, path)`, and creating a duplicate returns
the existing row. Session links use `(machine_id, session_name)` as their key
and a composite foreign key to a project on the same machine. A session rename
updates that key in one statement; ending a session removes its link. Recent
commands preserve the exact command text, reject blank/NUL/oversize values,
and keep the 20 newest distinct strings per project. The store prunes older
entries in the same transaction as each upsert.

**Backups and restore:** `make backup` writes a PostgreSQL custom-format dump named `backups/hostbud-<UTC timestamp>.dump`. The running app checks that `pg_dump` is at least as new as the PostgreSQL server, writes under `/tmp` (the runtime root is read-only), and passes `PGPASSWORD` only in the child environment, never argv or output. Compose copies the dump to the host; `backups/` is mode 700 and each dump is mode 600. The runtime pins PostgreSQL 15 client tools to match the PostgreSQL 15 server image.

`make restore-check FILE=…` validates the custom dump and hostbud migration table, restores it into a uniquely named temporary database, checks the migration version and counts for users, allowlist, projects and UI state, then drops that database on every exit path. `make restore FILE=…` validates before asking for the configured database name (interactive typed confirmation or `CONFIRM=<name>`), takes a safety backup, stops only the app, and applies `pg_restore --clean --if-exists --single-transaction --no-owner --exit-on-error`. It starts the app and waits for the DB-backed healthcheck. A failed restore transaction leaves the database unchanged; if the restore commits but the app does not recover, the safety backup filename and recovery command are printed. Neither workflow runs as part of deploy or e2e.

A dump excludes `/data` (generated SSH config, ControlMaster sockets, and `auth-key` for rate-limit hashing), Caddy certificates, and `.env`. Keep `.env` in a password manager; certificates can be reissued (subject to Let's Encrypt rate limits). Copy backups off the host.

### 8.1 Authentication

- Registration and sign-in use email plus password. There is no password-reset, email-delivery or email-verification flow in M1.
- Email is normalized before every lookup (trimmed and case-folded). Passwords are stored only as Argon2id password hashes; plaintext passwords never enter logs or the database.
- An email must be enabled in `email_allowlist` before registration. The same whitelist check is required at sign-in, so disabling an address prevents it from creating a new session without deleting the account.
- The owner manages the whitelist with plain SQL in PostgreSQL. There is deliberately no web UI or public API for modifying it. Documentation uses placeholders only; real addresses remain in the local database and are never committed.
- Successful sign-in creates a server-side session. The browser receives only an opaque, high-entropy cookie marked `HttpOnly`, `Secure` when HTTPS is in use, `SameSite=Lax`, with a bounded expiry and rotation on sign-in. Store only a hash of the cookie token in `auth_sessions`.
- All application API and WebSocket routes except health, registration and sign-in require an authenticated session. State-changing requests and WebSocket upgrades still enforce the Origin allowlist.
- Registration and sign-in responses must not reveal whether an email is registered or whitelisted. Authentication failures use a generic message.
- Implementation (`internal/auth`): the cookie is `hostbud_session` (256-bit random token; `auth_sessions` stores its SHA-256). Argon2id uses 64 MiB, t=3, p=2 in PHC format; sign-in for an unknown address verifies a dummy hash so timing doesn't reveal accounts, and registration hashes before checking the whitelist for the same reason. Registration doesn't sign in; the UI signs in right after. The SPA's static files are public (they hold no data and render the sign-in screen); every other `/api/*` and `/ws/*` route answers 401 without a session, including unknown ones (fail closed).

### 8.2 Login rate limiting

- Rate limiting applies to registration and sign-in, with the strictest policy on sign-in. Track failures by a privacy-preserving combination of normalized email and source IP, plus an IP-wide bucket to prevent rotating email addresses.
- A failed sign-in increments the bucket. Repeated rate-limit hits increase the block duration exponentially with a configured ceiling; successful sign-in clears the email+IP failure bucket but does not clear an active IP-wide abuse block.
- Return HTTP `429` with a generic error and `Retry-After`; do not disclose account or whitelist state. Apply the policy before password verification.
- Rate-limit state is stored in PostgreSQL so all app instances share it. Expired buckets may be cleaned up safely.
- Implementation: bucket keys are HMAC-SHA256 of (scope, email, IP) or (IP) with an install-local key (`/data/auth-key`, created on first start), so rows hold no raw emails or IPs. A bucket blocks once its failures reach the limit (`HOSTBUD_LOGIN_MAX_FAILURES`, `HOSTBUD_REGISTER_MAX_FAILURES`, `HOSTBUD_IP_MAX_FAILURES`); every further attempt, including one made while blocked, blocks for `BLOCK_BASE × MULTIPLIER^n` up to `BLOCK_MAX`. Failures older than `HOSTBUD_LOGIN_FAILURE_WINDOW` are forgotten. Updates lock the row (`SELECT … FOR UPDATE`), so concurrent attempts all count. The client IP comes from `X-Forwarded-For` only when the direct peer is in `HOSTBUD_TRUSTED_PROXIES`.
- Limits, backoff multiplier, ceiling and proxy/IP trust configuration are environment-backed with safe defaults. Never trust forwarded client IP headers except from the known Caddy proxy.

---

## 9. API surface (v1)

REST (JSON), all under `/api`:
```
POST   /api/auth/register             {email, password} — whitelist required
POST   /api/auth/login                {email, password} — whitelist required
POST   /api/auth/logout               revoke current session
GET    /api/auth/me                   current account, or 401
GET    /api/runtime/limits            authenticated runtime limits used by the browser client
GET    /api/runtime/terminal-slots    authenticated terminal-capacity check used after a refused WebSocket upgrade
GET    /api/machines                      list (with status) — v1: just the host
GET    /api/machines/:id/sessions
POST   /api/machines/:id/sessions         {name, path, startCommand?} — the command runs as `"$SHELL" -lic '<command>'; exec "$SHELL" -l` (login-shell PATH; the session outlives the command)
PATCH  /api/machines/:id/sessions/:name   rename
DELETE /api/machines/:id/sessions/:name   kill (UI confirms)
POST   /api/machines/:id/sessions/kill    {names} (1–256) → 200 {killed, failed: [{name, error, hint}]}; kills in order, one inventory refresh at the end; a failure doesn't stop the rest (UI confirms)
POST   /api/machines/:id/sessions/:name/copy-mode
GET    /api/machines/:id/sessions/:name/output   {output} — the session's retained tmux history as text (terminal text view)
GET|POST /api/machines/:id/sessions/:name/windows  the window/pane listing (POST answers 405 with Allow: GET)
POST|GET /api/machines/:id/sessions/:name/select  {window, pane?} (GET answers 405 with Allow: POST)
GET    /api/machines/:id/fs?path=         list dir
GET    /api/machines/:id/fs/stat?path=    one entry (+ symlink state)
POST   /api/machines/:id/fs/mkdir
PUT    /api/machines/:id/fs/upload?path=&name=  raw application/octet-stream photo upload (≤ 100 MiB)
GET    /api/machines/:id/fs/home
GET    /api/projects?machine=<id>     list projects for one machine
POST   /api/projects                  {machineId, path, name?}
GET    /api/projects/:id
PATCH  /api/projects/:id              {name}
DELETE /api/projects/:id              remove shared project metadata; linked sessions keep running and are re-placed
POST   /api/projects/:id/sessions     {name?, startCommand?} — creates through the shared session service at the saved project path
GET    /api/projects/:id/recent-commands — the project's recent start-command strings, newest first
GET|PUT /api/ui-state/:key            the account's JSON (GET 404 before the first PUT; PUT 204)
GET    /api/health
GET    /api/supervisor              authenticated V2-M5 status; enabled/provider/model/budget only, never credentials

# v2 agent queue (V2-M1, V2-M2, V2-M4; docs/roadmap-v2)
GET    /api/queues                    {queues: [queue], parallelQueues} — each queue with its project, items (each with its latest run summary and, V2-M2, waitingForSlot) and warnings (shared_directory)
GET    /api/queue-history?limit=100&offset=0  V2-M9: newest-first metadata snapshots; limit 1–200; survives queue/item deletion; excludes session content
POST   /api/queues                    {projectId, name} — 201; several queues either way, names unique per project (409); with parallel queues off only one runs at a time (start/resume of another → 409)
GET    /api/queues/:id
PATCH  /api/queues/:id                {name}
PUT    /api/queues/:id/default-prompt {enabled: bool, text: string} — the queue's default prompt (Queue panel); opt-in per queue (off by default; text defaults to ", commit regularly."), one line ≤ 1000 bytes (400 otherwise); when enabled the queue's new-item instruction starts with it (prefill only: the server never rewrites an instruction); the queue view carries `defaultPrompt` once it differs from the default; publishes queue.changed
PUT    /api/queues/:id/loop           {enabled, maxRuntime?} — looping on/off and its runtime limit (Go duration, 1s–30d, default "5h"); applies at the next pass boundary
PUT    /api/queues/:id/link           {afterRunId?, afterSession?} — "Start after" on an existing queue, in any state: an active tracked goal or any existing session (at most one; both empty clears); a session link gates the next item started
DELETE /api/queues/:id                204; refused (409) while a run is active or an item is verifying; run sessions stay open
POST   /api/queues/:id/items          {agent, flags, instruction, verifyCommand?, requiresApproval?} — appended; agent claude|codex, flags split like a shell, instruction "/goal <condition>"; V2-M4 gates: verifyCommand splits like flags, ≤ 4096 bytes, one line ("" = none)
PATCH  /api/queue-items/:id           any subset of {agent, flags, instruction, verifyCommand, requiresApproval}; queued items only, or only the two gate fields on a needs_attention item (V2-M4); 409 otherwise
DELETE /api/queue-items/:id           queued items only
PUT    /api/queues/:id/order          {itemIds} — exactly the queued items, in their new order
POST   /api/queues/:id/start|pause|resume        an invalid transition → 409 naming the current state; switch off: 409 while another queue is active; the response carries warnings
GET|PUT /api/machines/:id/capacity    {maxConcurrentRuns: 1–32 | null} — the per-machine cap on active runs (V2-M2; PUT out of range or not a whole number → 400; publishes queue.changed)
PUT    /api/machines/:id/parallel-queues {parallelQueues: bool} — the parallel-queues switch (Queue panel), stored in machine_capacity.parallel_queues over the HOSTBUD_PARALLEL_QUEUES default; publishes queue.changed (each payload carries parallelQueues); switching off stops no run
POST   /api/queue-items/:id/retry|skip|mark-done needs_attention items only (409 otherwise); an active run is cancelled first
POST   /api/queue-items/:id/approve|reject      V2-M4: awaiting_approval items only (409 naming the state otherwise); approve ⇒ done and the queue advances, reject ⇒ needs_attention and the queue pauses
POST   /api/queue-items/:id/reverify           V2-M4: Re-run verify on a needs_attention item whose latest run achieved and that has a verify command (409 otherwise); no new run or session
POST   /api/hooks/:run_id/:event      token-authenticated run hook (session_start|turn_end|session_end): 204/401/404/410/429/413/400

# v2 notifications (V2-M3, opt-in per account; docs/roadmap-v2)
GET|PUT /api/notifications/settings   the caller's account only: {enabled, onDone, onAttention, onFinished} (PUT: any subset), plus push {available, reason} and vapidPublicKey when push is available; no row = off
POST   /api/notifications/subscriptions {endpoint, keys: {p256dh, auth}} — 204; this device's push subscription for the caller (an endpoint another account had moves here); https, port 443, DNS host (400 otherwise); push off → 409 with the reason
DELETE /api/notifications/subscriptions {endpoint} — 204; the caller's own subscription only (sign-out, revoked permission)
POST   /api/notifications/test          {endpoint} — 202; a test push to that device of the caller only (404 if it isn't theirs, 409 push off); 3 at once, then one per 10 s per account (429 + Retry-After)

# V2-M13 servers (other SSH targets)
POST   /api/machines/scan                 {host, port?} → {hostKeys: [{type, key, fingerprint}]}; nothing is trusted (502/504 with a hint when no keys come back)
POST   /api/machines                      {label, host, port?, user, hostKeys: [{type, key}]} → 201 machine; the confirmed keys are pinned (400 invalid, 409 nickname taken)
PATCH  /api/machines/:id                  {label} — a server's nickname (404 for the host)
DELETE /api/machines/:id                  servers only; 409 while projects use it; 204, publishes machine.removed {id}; tmux untouched
GET    /api/projects?machine=*            every machine's projects, host first

# later (multi-machine)
POST   /api/machines/:id/activate | /deactivate
POST   /api/machines/refresh              re-scan ~/.ssh/config
```
The copy-mode endpoint body is `{action, lines?}` and its response is `{inMode, scrollPosition, historySize}`. `lines` is valid only for `scroll-up`/`scroll-down`/`wheel-up`/`wheel-down`, from 1 to 500. Unknown actions, names and values are rejected before SSH execution; an old tmux version returns 409 with an upgrade hint.
The windows endpoint returns `{windows, truncated}` with windows and panes in index order; it does not include pane paths. `select` returns the refreshed listing after changing the active window and optional pane. These routes read tmux state on demand and publish no hostbud event: tmux owns the window layout and all attached clients observe selection directly.
Session records in the list endpoint and initial/live events WebSocket payload include `projectId` when the placement service matches an explicit session link or a project path. Optional `agents` lists recognized Codex/Claude foreground marks; optional `status` is `working`, `blocked`, or `ended` from pane hook metadata; optional `title` is the session's active pane title (tmux's default hostname title is omitted). None of them changes the session name. `activity` changes publish `sessions.changed` only when its minute changes, so the tree's relative ages stay current without an event per keystroke. The browser uses project placement before applying the same-machine longest path-component match as a fallback. A successful project session start records its non-empty command; opening the picker or selecting a suggestion never starts a command by itself. Recent commands preserve their exact text, reject blank/NUL/oversize values, and keep the 20 newest distinct strings per project. Reusing a command moves it to the front; there is no manual clear action, and older values are pruned during an upsert.
`/api/ui-state/:key` accepts only allowlisted keys (`layout`; M4 adds `tree` for per-account left-bar session/group order; M6 adds `theme`; others 404). A PUT body must be valid JSON (400) of at most 64 KiB (413). The server stores it without interpreting it and publishes no event (it's a per-account preference); the client validates what it reads back.

Queue routes use the cookie session, the Origin check and the JSON limits like every v1 route; errors are `{error, hint}`. Every queue change publishes `queue.changed` (the queue's view, or none when deleted) and every run transition `run.changed` (`{runId, itemId, queueId, status, detail, flag}`) on `/ws/events`; reads publish nothing. `GET /api/supervisor` reports whether the optional V2-M5 supervisor is configured and safe settings, never the key. `POST /api/hooks/:run_id/:event` is the only token-authenticated route (`token_auth` in `internal/api/testdata/routes.json`): the per-run bearer token is its only credential, and it is exempt from the cookie session, the Origin allowlist, the Tailscale gate and the generic body limits (it caps its own body at 64 KiB after the token check). See docs/roadmap-v2/ARCHITECTURE.md §8.

WebSockets: `/ws/events` (server → client state events), `/ws/term` (interactive). `/ws/events` also sends `{"type":"heartbeat"}` every 15 s (WebSocket pings are invisible to page scripts); the browser treats 40 s of silence as a hung connection and reconnects, and the next snapshot resyncs the list.

**Security middleware (all routes):**
- Public routes are limited to `GET /api/health`, `POST /api/auth/register` and `POST /api/auth/login`. `POST /api/hooks/:run_id/:event` needs a per-run bearer token instead of the cookie (v2). All other routes require the server-side session cookie.
- Authentication cookies are opaque, HttpOnly, SameSite and Secure under HTTPS. Never put credentials, session cookies or bearer tokens in URLs, logs, WebSocket query parameters or client storage.
- Origin allowlist: `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`. Reject WebSocket upgrades and state-changing requests whose `Origin` is not in it. (`localhost` is a secure context, so clipboard APIs work over plain HTTP.)
- Optional Tailscale identity allowlist (off by default): Caddy overwrites `X-Hostbud-Via` with `domain` or `local`; hostbud trusts the marker only from a configured trusted proxy. Missing or unknown markers from trusted peers count as `domain`; loopback and `/api/health` are exempt. On the domain path, hostbud resolves the client IP from trusted `X-Forwarded-For` through tailscaled LocalAPI `whois` over a unix socket (2 s timeout). Login names match case-insensitively; tagged nodes require their `tag:…` value in the list. Allowed identities cache for 60 s and rejected identities for 10 s (LRU, at most 1024 IPs). Whois errors, timeouts and unlisted users fail closed with 403 before auth, Origin or rate limiting; the SPA gets a static error page and WebSocket upgrades are refused before acceptance. Info logs include only a reason code. Changing the list needs an app restart.
- CSRF: JSON-only API + SameSite cookies + Origin check on state-changing requests.
- Every state-changing request with a body must use `Content-Type: application/json`; JSON bodies are limited to 64 KiB and typed decoders reject unknown fields. UI state accepts valid raw JSON up to 64 KiB. Headers are limited to 32 KiB; REST handlers have a 30 s deadline. `/api/*` responses use `Cache-Control: no-store`.
- `GET /api/health` is public and pings PostgreSQL with a 1 s deadline. It returns `200 {"status":"ok"}` or `503 {"status":"degraded","db":"unreachable"}`. Database timeouts and outages on authenticated routes return `503 {"error":"The database isn't answering","hint":"Check `docker compose ps hostbud-postgres`; hostbud recovers when it's back."}`; a failed session lookup never turns into a 401.
- Never log command strings containing user paths at info level.
- Never log user-chosen project or session names at info level; include session names only at debug level.
- Never log passwords, session cookies, password hashes, database passwords, full email addresses or whitelist contents.
- Every hostbud response carries `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin` and `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()`. Hostbud itself never sends HSTS; Caddy adds it only on the TLS domain site.
- HTML carries a CSP computed before the server starts. It hashes the sole inline script in the embedded `index.html` only if it is the expected theme boot script; any second or unexpected inline script is a startup error. `script-src` has no `unsafe-inline` or `unsafe-eval`. `connect-src` includes `'self'`, `ws://localhost:${HOSTBUD_LOCAL_PORT}` and `wss://${HOSTBUD_DOMAIN}` from the Origin allowlist.

---

## 10. v2 — agent task queue

The v2 design and plan live in [roadmap-v2/ARCHITECTURE.md](roadmap-v2/ARCHITECTURE.md) and [roadmap-v2/ROADMAP.md](roadmap-v2/ROADMAP.md). V2-M1 (one sequential queue per installation: a queued item runs in its own interactive tmux session and the next starts only when the client's own `/goal` is recorded as achieved) is implemented in `internal/queue` and `internal/agents`. V2-M2 (opt-in, `HOSTBUD_PARALLEL_QUEUES`) adds several queues that run in parallel, each still sequential, and an optional per-machine cap on active runs with FIFO slots (`machine_capacity`, `queues.waiting_since`; roadmap-v2 ARCHITECTURE §5.5). This replaces the earlier `tasks`/`machine_capacity` sketch that used to be here. V2-M3 (opt-in per account) adds notifications for item done, needs attention and queue finished: in the open app from `queue.changed`, and through Web Push (`internal/notify`, VAPID, an outbox written in the transition's transaction). V2-M4 adds opt-in verify and owner approval gates. V2-M5 adds the optional `internal/llm` stale-run supervisor: it classifies a bounded pane capture and stores only a scrubbed label/reason flag; it cannot mutate queue state.

**v1 obligations that v2 builds on (still binding)**
1. All state changes flow through the `events` bus (typed events), not direct UI pushes.
2. Session creation goes through one service function that accepts env vars and a start command (`session.Service.Create`, with `StartArgv` for runs; env is fed to tmux through stdin).
3. Store layer is dialect-agnostic; migrations are append-only.
4. The API has room for token-authenticated machine-to-server endpoints (`/api/hooks/*`) that bypass the browser-origin check but require per-run tokens.

---

## 11. Frontend

- **Vue 3 + Vite + TypeScript**, Pinia stores, Vue Router (minimal).
- **Tailwind CSS + Reka UI** (headless, accessible primitives) for a dense IDE-like UI in Dark / Light / System modes. The resolved theme is set on `html[data-theme]`; token values in `main.css` are the only component surface colors. `stores/theme.ts` keeps the per-account `{version: 1, mode}` value in `ui_state`, follows `prefers-color-scheme` changes only in System mode, and updates mounted xterm palettes and search decorations without reattaching terminals. A sub-1 KiB inline script in `index.html` applies the browser's last theme mirror before stylesheets to avoid a first-paint flash; the CSP hashes this exact script during startup.

### 11.1 Content Security Policy

`internal/api.BuildContentSecurityPolicy` hashes the one inline theme boot script in the embedded `index.html` and fails startup if another inline script appears or the approved script markers are missing. The script hash is calculated from its exact UTF-8 bytes. `script-src` allows only same-origin assets and that hash; `unsafe-eval` is not allowed. `style-src 'unsafe-inline'` is retained because Vue, Reka UI, splitpanes and xterm set style attributes at runtime. The remaining HTML policy is `default-src 'self'`, `img-src 'self' data: blob:`, `font-src 'self'`, `worker-src 'self'`, `manifest-src 'self'`, `frame-ancestors 'none'`, `base-uri 'none'`, `form-action 'self'`, and `object-src 'none'`. `connect-src` contains `'self'` plus the explicit WebSocket equivalents of both configured Origins (`ws://localhost:${HOSTBUD_LOCAL_PORT}` and `wss://${HOSTBUD_DOMAIN}`); this is needed by older WebKit, which does not consistently treat WebSocket connections as `'self'`. V2-M3 changes nothing here: push is sent by the server, and the browser receives it through its own push service in the same-origin service worker (`worker-src 'self'`).
- Layout: resizable left gutter (tree) | main area with saved terminal layouts, each layout may be **split** (horizontal/vertical, via `splitpanes`). No tab strip is rendered; the left tree selects sessions with open views. Layout is persisted in `ui_state`; desktop sidebar visibility is a browser-storage preference.
- **Terminal layouts (M3, `lib/layout.ts`, `stores/layout.ts`):** persisted shape remains `{version: 1, tabs: [{id, root, focusedPane}], activeTab}` where `root` is a pane `{type: 'pane', id, machine, session}` (splits: M3 T8); IDs come from `crypto.randomUUID`. Picking a session in the tree focuses the layout and pane that show it, or opens a new layout; **New session** opens a new layout. The client doesn't cap open views (the server-wide attach cap still applies). **Close terminal view** in the command palette removes its views without killing the tmux session. Inactive layouts stay mounted (`v-show`) but detach their terminal clients, as do terminals in hidden browser pages. This prevents stale tmux clients from constraining the shared session size. Activating a layout or returning to a visible page refits and reattaches at the current dimensions without focusing xterm. Keyboard input goes to the active layout's focused terminal only. Ctrl+Shift+] / [ cycle open sessions with visible tree rows in rendered tree order, skipping collapsed and hidden rows. The data model retains `tabs` terminology for compatibility, but no tab bar is rendered.
  - **Sessions that change:** a rename from the UI relabels every pane showing the session, without re-attaching (the list may show the new name before the request returns, so the store remembers renames in flight). A session missing from a fresh list closes its panes with one toast ("Session <name> ended"). Only a list from a reachable host counts: right after an app restart the first snapshot can come before the first poll with an empty list, and an unreachable host's list is stale. A rename done in a real terminal therefore looks like an end.
  - **Persistence:** the layout is loaded from `GET /api/ui-state/layout` on sign-in, before any terminal mounts and before `/ws/events` starts; it's validated (shape, `version`; a missing focus or active tab is repaired) and invalid data falls back to an empty layout with a console warning. Every change is saved with `PUT`, debounced 500 ms. Sign-out forgets it without saving.
- **Compact screens (M5):** `(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)` matches phones in portrait and landscape without matching tablets. The project tree fills the home screen when no terminal is open. With terminals open, the header's **Show sidebar** icon opens a labelled modal drawer over the active terminal; it traps focus, closes on Escape, backdrop, **Hide sidebar**, left swipe or session/action selection, and returns focus to its trigger. The compact layout shows the active layout's focused pane; there is no tab switcher. Inactive views detach and refit/reconnect at the visible size when selected. New session, Rename, Kill and file-browser dialogs become full-width bottom sheets. The header Account menu contains the per-account Theme radio group, email and Sign out. No edge-swipe-to-open gesture is used.
- **Sidebar controls (M6 T13):** the header has one icon toggle for the desktop sidebar or compact drawer, with the state exposed through `aria-expanded` and `aria-controls`; desktop visibility survives reload in browser storage. The aside is named **Sessions**, and the drawer's accessible **Project tree** title is visually hidden.
- **Header actions (M8 T1):** icon-only **New session** and **Browse files** sit in the app bar immediately after the host name, on desktop and compact screens alike; the sidebar and drawer no longer duplicate them. App-bar and drawer icon buttons share `IconButton`: a bordered visual box of the icon plus 4 px padding, a visible focus ring, a tooltip matching the accessible name, and `touch-target` for a 44 px hit area on touch screens.
- **Confirmations:** every confirmation uses the app's own Reka UI alert dialog (`ConfirmDialog`, or `KillSessionDialog`/`RemoveProjectDialog` for their flows), a bottom sheet on compact screens; the app never calls `window.confirm`, `window.prompt` or `window.alert`. Destructive actions (kill, including a queue's done-item sessions, queue and queue-item delete, project removal) and setting changes with wide effect (the parallel-queues switch, the run cap) confirm first.
- **Focus mode (M8 T29):** the signed-in app header has a **Focus mode** toggle (`aria-pressed`). While it is on, the mouse leaving the browser viewport for 2 s shows a local, opaque, full-viewport web overlay with the word “focus” centered; returning sooner cancels it. The mouse re-entering the viewport hides the overlay at once, as does activating it (click, tap, Escape); the toggle stays on until the user turns it off. Focus returns to the element focused before the overlay appeared. Phones and touch devices (`COMPACT_QUERY` or `(pointer: coarse)`) and the installed PWA (`display-mode: standalone` or `navigator.standalone`) don't render the toggle and keep the mode off; switching to one disarms it. It does not call the server or alter tmux sessions.
- **Touch and readability (M5):** a shared `touch-target` utility provides 44×44 CSS px hit areas under a coarse pointer, and global coarse-pointer sizing covers native and Reka UI controls. Text entry controls use 16px type to prevent iOS focus zoom; the viewport allows user scaling. The app root uses `touch-action: manipulation` and long-pressing a session row opens its existing action menu after 500 ms (movement over 10 px cancels it).
- **Splits (M3 T8):** a tab's `root` may be a split `{type: 'split', id, dir: 'row' | 'column', sizes, children}` (row = side by side; `sizes` are percents summing to 100). At most 4 panes per tab. **Split right/down** in the focused pane's header opens a session picker (live sessions + **New session…**, whose session then lands in the new pane); a list row's ⋯ menu has **Open in split right/down** (beside the active tab's focused pane, or a new tab when none is open). Inside a split of the same direction the new pane is a sibling and the space is shared equally; the other direction nests a new split. The new pane gets focus. **Close pane** (× in its header) detaches that view and gives its space back to its siblings in proportion; a split left with one child collapses to it, a child split of the same direction merges into its parent, and the last pane closes the tab. The same rules remove panes of ended sessions. Clicking into a pane focuses it (an accent outline when the tab has several); keyboard input goes to the focused pane only.
  - Rendering: `splitpanes` renders the tree recursively (`LayoutNodeView.vue`), minimum pane size 10 %. Dragging a divider resizes live (each pane's ResizeObserver refits it and resizes its tmux window); the sizes are saved when the drag ends. Stored splits are validated (≥ 2 children, a known direction, ≤ 4 panes per tab; bad sizes become equal shares).
  - Compact screens: a split tab shows only its focused pane, full size, with a "Pane n of m" header button that cycles focus; the layout itself isn't changed, so a wide screen shows the split again. The query remains compact across phone rotation, so terminals do not re-mount or re-attach.
- Tree: projects → sessions → lazily loaded windows and panes; a machine level appears only once multiple machines exist. Status dots (● attached/active, ○ detached; a header banner for host unreachable / tmux missing). Projects, sessions and project sections have explicit user-controlled ordering persisted per account; newly observed rows append without re-sorting existing rows, and there is no automatic alphabetical/activity/recency sort. The account's `tree` UI-state value is version 4, adding `sections: [{id, name, color}, ...]`, `projectSections: {projectId: sectionId, ...}` and `collapsedSections: [sectionId, ...]` to the version 2 shape. Sections are per-account visual groupings with six red, green, blue, yellow, orange and purple accents. A section adds no tree level or project indentation; its thin tinted border surrounds its projects. Project actions assign a project to a section. Empty sections can be created, renamed, recolored, reordered, collapsed or deleted; deleting one unassigns its projects and removes its collapse state. The color dot toggles collapse, which hides project rows but keeps the section visible; collapse state is per account. A selected session in a collapsed project marks its parent project row, and a selected session in a collapsed section marks that section without expanding it. Versions 1, 2 and 3 upgrade on load without changing existing order or membership; malformed/future versions are ignored. The account's `tree` UI-state value is version 4: `{version: 4, projects: [projectId, ...], sessions: {projectId: [sessionName, ...], __other__: [sessionName, ...]}, pinned: [projectId, ...], hidden: {projects: [...], sessions: [machineId/sessionName, ...]}, collapsed: [projectId | '__other__', ...], collapsedSections: [sectionId, ...], expanded: [machineId/sessionName[/@windowId], ...], showHidden: boolean}`. Version 1 order is upgraded on load without changing its order; malformed/future versions are ignored. Session-keyed values are pruned only after that machine is reachable and returns an authoritative session list; project-keyed values are pruned only after the project list loads. Window keys are pruned when an expanded session's window list arrives. Saves are debounced 500 ms and flushed on `pagehide`; serialized values over 60 KiB are skipped with a console warning. No global project pin/sort or hidden-machine columns are changed. The left bar is a WAI-ARIA tree labelled “Projects and sessions” with one roving tab stop, project/Other headers at level 1, sessions at level 2, and grouped children. Headers show a folder or list icon, semibold name, tinted background, and indented children with a vertical guide. Project paths appear below the name, abbreviated with `~` for the host home directory and retained in the full-path title. Collapsing a group preserves order and never detaches or resizes its terminals; hidden project and session ids are per-account presentation state and never affect tmux, open tabs, split pickers or the session API. A hidden project hides its whole group, including future sessions placed there. Show hidden temporarily renders hidden rows in their saved positions, dimmed and labelled hidden; Unhide restores ordinary visibility without moving the row. The Show hidden toggle persists with the tree state, and when no visible rows remain the tree says “Everything is hidden.” An empty Other group is hidden while its saved state is retained. Projects and sessions rename inline from their pencil, F2, a fine-pointer double-click, or the row's more-actions menu. The project menu, long-press menu, command palette and Delete on a focused project row offer **Remove project…**, which confirms its session count and destinations and explains that files are untouched and removal applies to every account. Removing deletes only shared project metadata and cascading links/history; sessions and open tabs stay active, and live placement falls back immediately to the next-longest project path or Other sessions. The project menu (and long-press menu) also offers **Kill all…**, disabled when the project has none; it asks twice (first the count, including hidden sessions, then the listed session names) before killing them all in one request (`POST /api/machines/:id/sessions/kill`, which refreshes the session list once rather than once per session), reporting any that failed; the project itself stays. The selected name is edited in a 16 px input; Enter or blur saves, Escape cancels, and empty or unchanged values cancel. Errors stay beside the input. A successful session rename re-keys its saved order, hidden state, expansion and expanded-window keys together, preserving the row's position and presentation state; open panes relabel through the M3 rename lifecycle without reattaching. Project names are trimmed and limited to 255 UTF-8 bytes. Session rows retain the kill × and more-actions ⋯; kill remains confirmation-gated. Session rows do not show tmux window counts. Tree session rows start with two fixed slots, agent logo then status emoji, reserved even when empty so names line up. The desktop and compact header's Account menu contains the Theme radio group, signed-in email and Sign out, and is reachable without opening the tree drawer.
- **Tree row actions and counts (M8 T24/T26):** project and session names use the row’s available width on desktop; trailing actions appear on mouse hover, while touch layouts retain visible inline actions. Project rows never show their session total.
- **Terminal session header (M8 T25):** a directory icon begins the row, followed by the focused session name and its directory. The name uses its assigned project’s section color when one exists; the full directory remains available through an accessible label and tooltip.
- **Pinned projects (M6 T6):** pinned projects appear in a Pinned section above unpinned projects, each retaining its manual order. Pin and Unpin move a project to the end of the target section; drag and Alt+↑/↓ reorder only within a section. The accessible Pinned icon unpins, and the project menu and `P` tree shortcut toggle pin state. Pin state stays in the account's `tree` UI-state value; the global `projects.pinned` column is reserved and never changed.
- **Compact, name-first rows (M8 T2):** project and session rows use compact visual padding (touch-target keeps 44 px on touch screens); a project's drag handle and expand control sit side by side. A session row leads with its name, then its status dot, chevron (if any), actions and drag handle. M8 T13 can prefix the gutter name with a separate agent-status emoji (🟢 working, 🚧 blocked/waiting, 🎯 ended); this is presentation metadata and never part of the tmux or stored session name. It comes from pane-scoped tmux user options written by optional Codex/Claude Code hooks and read during the ordinary inventory poll. Per-session status precedence is blocked, then working, then ended; unknown means no mark. If a tracked active status remains after a recognized interactive shell resumes in that pane, inventory reports ended. No provider hook text, prompt, or command arguments are stored or returned. A session gets a chevron and `aria-expanded` only when it really expands: more than one window (the inventory's window count), or an already-loaded single window split into panes; the inventory reports no pane counts, so a single window's panes are reachable once its windows are loaded. A kept expanded state for a row that no longer expands shows nothing.
- **Window and pane rows (M6 T3):** expanding a session lazily loads its window list; a multi-pane window can expand into pane rows. Only expanded sessions make requests. The rows show the target's window/pane indices, names/commands and active state; a loading row, actionable error with Retry, and truncation row cover the response states. Expanded session/window keys live in `tree.expanded`, restore after reload, and are pruned against a reachable host's returned list. While expanded, `sessions.changed` events that alter that session's window count or activity schedule a 300 ms refresh with at most one request in flight and one queued refresh; no window polling loop runs. Selecting a row opens/focuses the session through the layout store, then calls the select route for its window and optional pane. A vanished row shows a toast and refreshes the list. In compact mode selection closes the drawer and shows the terminal; tmux switches the shared current window for attached clients.
- **Keyboard shortcuts (M6 T8):** `web/src/lib/shortcuts.ts` is the single registry for shortcut ids, labels, scopes and platform bindings; the global dispatcher, help dialog and command palette hints use it. Global chords use ⌘ or Ctrl+Shift and are captured before xterm. Outside-terminal chords do not fire from the terminal or editable controls. Tree navigation and actions run only from a focused tree row, including `N` to start a session in the row's project or use the default path for Other. The help dialog groups every shortcut under General, Tabs and Tree and restores focus on Escape. Tab switching keeps a per-page most-recently-selected list in memory; restored tabs do not seed it, and closed tabs are removed.
- **Command palette (M6, T23):** ⌘K on Mac or Ctrl+Shift+K globally; Ctrl+K only outside the terminal so shell readline keeps its kill-line binding. Touch and compact layouts expose a header button. The modal combobox searches live sessions in tree order, only already-loaded windows, projects and registered app actions. Fuzzy matching is case-insensitive subsequence matching with word-start/contiguous bonuses and stable source-order ties; it caps results at 50 and marks hidden sessions. Results are grouped into Sessions, Windows, Projects, Create, Open, Organize, Terminal, Appearance, Account and Destructive. Group headings and rows use theme-aware semantic color markers; destructive actions use the danger color and still open their normal confirmation dialog. Selecting a session/window focuses its terminal; selecting a project reveals and focuses its row; Escape restores the prior focus. Compact terminal selection closes the palette and tree drawer.
- **Command palette layout (M8 T26):** on desktop, semantic result groups occupy three columns; narrower screens use fewer columns. Group headings show their filtered item counts. Left/right arrows move the highlighted item between adjacent groups, while up/down navigate items and Enter activates the highlight.
- **Theme boot (M6 T7):** the pre-style inline script reads only the browser's theme mirror to prevent first-paint flashes on the signed-out screen. After sign-in, the account's `ui_state.theme` is authoritative and updates that mirror.
- **Phones (M2):** the app is pinned to the visual viewport (`--app-height`, `lib/appHeight.ts`), so the on-screen keyboard shrinks the terminal instead of covering it; the terminal's hidden input uses a 16px font (no iOS zoom on focus) with autocorrect/autocapitalize off; a ⌨ button on touch screens refocuses it.
- **Mobile controls (M5):** the focused terminal shows an on-screen key bar on coarse-pointer devices with Esc, Tab, sticky Ctrl/Alt, arrows, `|`, `~`, `/`, `-` and Scroll history. Keys enter through xterm's `input` path; arrows respect DECCKM, Ctrl/Alt modify the next key or soft-keyboard character, and double-tap locks a modifier. Arrow keys repeat after 400 ms and every 80 ms. The bar can collapse for the current page load and scrolls inside itself; hardware-keyboard behavior is unchanged.
- **Scroll history (M5):** Scroll history enters tmux copy mode through the authenticated `copy-mode` API and replaces the key bar with Top, Page up, Line up/down, Page down, Bottom and Done controls plus the reported history position. The UI follows each tmux response, clears mode on pane detach, and exits before forwarding new typed input.
- **Installable app (PWA, M5; shell refresh fix M8 T13):** `manifest.webmanifest` (`display: standalone`, theme colors, bundled icons incl. `apple-touch-icon`; no external assets) and a small hand-written service worker served from `/` with `Cache-Control: no-cache`. It precaches the app shell (hashed assets, manifest, icons, favicon) into a versioned `hostbud-shell-*` cache. Navigations use network-first and update the cached shell on success, falling back to cache offline; precached assets stay cache-first. This prevents a regular refresh from reopening an old app shell after a hard reload got the current build. A new worker calls `skipWaiting()` and `clients.claim()` so it takes over promptly, and activation deletes only stale hostbud shell caches. `/api/*`, `/ws/*` and anything else pass straight to the network, uncached, so auth and live data never come from a cache. It registers only in production in a secure context (the HTTPS domain, or `localhost`); failure is debug-only. On iOS an installed app has its own cookie store, separate from Safari's, so it asks to sign in once. Standalone mode pads the layout with `env(safe-area-inset-*)`.
- **Notifications (V2-M3, opt-in per account):** `stores/notifications.ts` shows a browser `Notification` for a live `queue.changed` that carries a `notification` payload, only when the account is on, the event is chosen, permission is `granted`, and this device has no push subscription (push shows it then). The text is the server's payload only; the tag is its dedupe key; keys seen since page load never show again (a reconnect doesn't replay). A click (or the service worker's `hostbud.open` message, or a `/queues/<id>?item=<id>` launch URL) opens the Queue panel on that item. The service worker gains `push` (shows the payload's title and body, tag = key) and `notificationclick` (focuses an open window and tells it the item, or opens the item's same-origin path); its cache rules are unchanged. Settings → Notifications asks for permission only on the switch's click, explains a denied permission and the iOS home-screen requirement, sets this device up (push when the server has VAPID keys and the browser a push service, else in-app), and removes this device's subscription on sign-out, on switching off, and when the permission was revoked (checked at app start). E2E builds wrap `Notification` in a spy (`window.__notifications`); `check-dist` rejects it in a production build.
- **Startup reachability (M5):** the initial `/api/auth/me` check distinguishes 401 (sign-in), network/timeout and 502/503/504 (the **Can't reach hostbud** view with retry), and other server errors (an explicit server-error message). The unreachable view retries with 1/2/4/8/15-second backoff, plus immediate retries on `online` and page visibility. Sign-in network failures show a connection message and are not counted by the server.
- **Terminal context menu (M3):** Reka UI `ContextMenu` around the terminal (`TerminalMenu.vue`): Copy (disabled with no selection), Paste, Select all; see §6 *Clipboard*.
- **Editable text inputs (M8 T5):** Option-click (Alt-click on non-macOS platforms) places the caret at the clicked character/line in single-line and multiline inputs. It must not jump to a different line or position; ordinary click and selection retain their normal behavior. The app's own inputs are single-line `<input>`s with the browser's native caret handling (nothing intercepts Alt). The affected input was the terminal's: xterm 6's `altClickMovesCursor` sends one burst of left/right arrows counted as if everything between the cursor and the click were a single soft-wrapped line of full terminal width (the normal buffer, which hostbud always uses under tmux, see *Scrollback under tmux*). That count is wrong for multiline prompts with prefixes and borders (Codex, Claude Code), tmux split panes and clicks past a line's end, so the caret landed on unrelated lines. `web/src/lib/altClick.ts` replaces it (xterm's option is off): on a short, motionless Alt/Option+click at the live bottom of the buffer, with no selection and no copy mode, it sends left/right arrows one row's worth at a time, waits for the program's redraw (writes quiet for 40 ms, at most 400 ms), re-reads the real cursor and continues, halving the step after an overshoot. It never sends up/down (a shell would recall history), stops when the editor stops moving (the start of the editable text, the end of a shorter line) and is cancelled by another click, a key press or unmount. With the mouse captured, Alt+click belongs to the program except on macOS, where Option+click is forced to local selection.
- **Terminal scrolling readability (M8 T6):** mouse-wheel movement follows the existing direction and scrollback positions, while rendered text remains visually trackable during motion, including repeated output. Rendering or event handling changes must preserve terminal input, scrollback content, copy mode. Measured in headless Chromium with xterm 6 (numbered lines between runs of identical ones), the wheel has two paths with different causes. With tmux `mouse on` the wheel drives tmux copy mode, which scrolls one line at a time (`CSI 1 L` plus a redrawn position indicator per line, 5 lines per notch). Without synchronization, network-sized chunks made xterm render intermediate states: 8–10 torn frames out of about 20 for 24 notches. With `-T sync` (above) there were none. With `mouse off` the wheel scrolls xterm's own scrollback, and each notch jumped 3 rows in a single frame. `smoothScrollDuration: 100` (`WHEEL_SMOOTH_SCROLL_MS`) animates a notch at one row per frame, with the same rows, direction and end position; trackpad deltas and output-driven scrolls stay immediate (xterm only smooths classified wheel notches).
- **Long-lived terminal theme contrast (M8 T7):** System theme changes update the mounted xterm palette without reattaching its tmux client. This guarantees hostbud-controlled terminal colors update; a terminal application that writes explicit foreground/background colors may own its own contrast and may not respond to hostbud or OS theme changes. Diagnose that boundary before changing the palette, and document a supported workaround if the client owns the colors. **Diagnosis:** hostbud's palettes give every text color at least 4.5:1 on their own background (only dark-theme ANSI black, by convention, doesn't), and a theme change reaches mounted terminals through `options.theme` without a reset or re-attach. The reported prompt is client-owned: Codex queries the default colors (OSC 10/11; the queries are in its binary) and paints the “Ask Codex to do anything” composer with an explicit truecolor background derived from the dark default background, while the prompt text uses the default foreground. After a dark-to-light switch the foreground flips to `#1f2328` but the explicit background stays about `#2c2e32`: 1.16:1. **What hostbud does:** `minimumContrastRatio: 4.5` (`TERMINAL_MIN_CONTRAST`, WCAG AA, also VS Code's terminal default). Both renderers raise or lower a cell's foreground until it reaches that contrast against the cell's actual background, so the text renders as a light grey on the still-dark composer (measured with xterm 6: `rgb(31,35,40)` became `rgb(150,151,155)`). The client's own colors are never rewritten, and dim cells need half the ratio. **Boundary and workaround:** the composer keeps the colors of the theme it started in. Restart the client after switching (for Codex, quit and run `codex resume`), or choose a fixed Dark or Light theme instead of System.
- No external CDNs at runtime (fonts and assets bundled).

---

## 12. Configuration (env)

All config comes from environment (`.env`, gitignored). See `.env.example` for the full list. The app must start with sensible defaults and fail loudly (clear error) only for truly required values.

Authentication/PostgreSQL configuration adds `HOSTBUD_DB_HOST`, `HOSTBUD_DB_PORT`,
`HOSTBUD_DB_NAME`, `HOSTBUD_DB_USER`, `HOSTBUD_DB_PASSWORD`,
`HOSTBUD_DB_LOCAL_PORT`, and the rate-limit settings. Real values belong only in
the untracked `.env`; documentation and fixtures use placeholders. The database
maintenance port is optional, loopback-only, and must use an uncommon operator
chosen port. Before starting Compose, verify that the chosen host port is free
(for example with `ss -ltn`); deployment must fail or be corrected rather than
silently selecting or colliding with another service. It must never be published
on `0.0.0.0`, the Tailscale address, or through Caddy.

M7 also configures the exec and SFTP deadlines and terminal attachment caps:
`HOSTBUD_EXEC_TIMEOUT` (10 s, 2 s–2 min), `HOSTBUD_SFTP_TIMEOUT` (10 s,
2 s–2 min), `HOSTBUD_UPLOAD_TIMEOUT` (5 min, 30 s–10 min; M8 T10), `HOSTBUD_MAX_TERMINALS_PER_USER` (0 = none, 0–1024), and
`HOSTBUD_MAX_TERMINALS` (128, 1–1024). See §15 for the full inventory.
v2 queues add `HOSTBUD_RUN_STALE_AFTER` (2 h, 10 s–24 h), `HOSTBUD_PARALLEL_QUEUES`
(V2-M2), the `HOSTBUD_VAPID_*` keys (V2-M3) and `HOSTBUD_VERIFY_TIMEOUT` (V2-M4:
10 min, 5 s–2 h; the bound on an item's verify command, see §15 and v2 §5.6).
The optional domain identity gate uses `HOSTBUD_ALLOWED_TS_USERS` and
`TAILSCALED_SOCKET` only when `deploy/compose.tailscale.yml` is enabled.

v2 (V2-M1) adds two optional settings: `HOSTBUD_RUN_STALE_AFTER` (2 h,
10 s–24 h), how long a queue run may go without a hook before it is flagged
stale and its queue pauses, and `HOSTBUD_HOOK_BASE_URL` (empty = Caddy's
loopback site `http://127.0.0.1:${HOSTBUD_LOCAL_PORT}`), the `HOSTBUD_URL`
a run session's hooks call. Both reach only the `hostbud` service.
`HOSTBUD_SERVER_HOOK_BASE_URL` (empty = `https://${HOSTBUD_DOMAIN}`, also only
for `hostbud`) is the `HOSTBUD_URL` of run sessions on servers added in the UI.
V2-M2 adds `HOSTBUD_PARALLEL_QUEUES` (`true` or `false`, default `false`, also
only for `hostbud`): several queues and parallel runs. It is only the default:
the Queue panel's switch (`PUT /api/machines/:id/parallel-queues`) is stored
in the database and wins once set. The per-machine run cap
is not an env var; it is set in Settings (`PUT /api/machines/:id/capacity`).
`HOSTBUD_QUEUE_DISPATCHER` (`true` or `false`, default `true`) must stay on in
a normal install: exactly one instance per database runs the queue dispatcher.
Only an extra instance sharing a database (the e2e suite's secondary apps)
sets it to `false`; that instance starts no runs and handles no run hooks.
V2-M3 adds the optional Web Push identity `HOSTBUD_VAPID_PUBLIC_KEY`,
`HOSTBUD_VAPID_PRIVATE_KEY` and `HOSTBUD_VAPID_SUBJECT` (`make vapid-keys`
prints a pair; `hostbud` only). None, some or an invalid value never fails
startup: push is off, one warn log names the vars (never values), and
Settings shows the reason; in-app notifications still work.
`HOSTBUD_PUSH_TEST_ENDPOINT` is e2e only (a push endpoint prefix exempt from
the endpoint rules) and never reaches a production service. Notification
settings are per account in the database (`notification_prefs`, no row = off).

---

## 13. Testing strategy

- **Unit:** quoting/escaping, name validation, tmux output parsing, SSH config generation ordering, project-path matching.
- **Integration:** `test/sshd/` — a disposable container with `openssh-server` + `tmux` (plus a tmux-less variant) and a generated throwaway key; the test suite runs hostbud's sshx/tmux/inventory/term/fsbrowse packages against it (probe, list, create, attach via PTY, mkdir, kill, error mapping). It also checks the real deploy config (`docker compose config`: published ports, `user`, mounts). Runs via `make test` in Docker: `scripts/test-sshd.sh` keeps `hostbud-test-sshd` (+ `-notmux`) running on the `hostbud-test` network with throwaway keys in `.cache/test-sshd/` (recreated only when `test/sshd` changes; `make test-down` removes them), and the Go toolbox joins that network. The deploy-config check reads `docker compose config` rendered with placeholder values, never the real `.env`, and the Caddy checks read `caddy list-modules` / `caddy adapt` output of the built `hostbud-caddy` image (`scripts/caddy-config.sh`), also with placeholders.
- **Host-facing coverage matrix (M7 T10):** each package owns the behavior in its cell; shared transport behavior is referenced where a higher layer delegates it. `n/a` cells state which layer owns that case instead.

| Package | Happy path | Not found | Invalid input | Timeout | Unreachable | tmux missing | Host-key mismatch | Concurrency |
|---|---|---|---|---|---|---|---|---|
| `sshx` | `TestIntegrationExecEcho` | `TestIntegrationRemoteExitCode` (remote exit mapping) | `TestArgs`, `TestIntegrationQuotingRoundTrip` | `TestIntegrationTimeoutKillsRemoteCall`, `TestIntegrationTmuxStallTimesOutAndRecovers` | `TestIntegrationConnectionRefused`, `TestIntegrationUnresolvableHost` | n/a: command discovery belongs to `inventory` | `TestIntegrationPinnedKeyMismatchRefused` | `TestIntegrationConcurrentExecsShareControlMaster` |
| `tmux` | `TestIntegrationLifecycle` | `TestIntegrationListNoServerIsEmpty` | `TestNewSessionArgsErrors`, `TestValidateName` | n/a: builders/parsers do not execute; `sshx` owns command deadlines | n/a: builders/parsers do not connect; `sshx` owns transport errors | n/a: `inventory` owns capability detection | n/a: `sshx` pins and verifies keys | n/a: service-level create races are handled by `session` and the API |
| `inventory` | `TestIntegrationPollerSeesCreateAndKill` | `TestIntegrationTmuxServerKilledListsEmptyAndCanRecreate` | n/a: polls fixed commands and parses their output | `TestIntegrationPollerTimeoutAndRecovery` | `TestIntegrationUnreachableSSHDMarksMachine` | `TestIntegrationProbeTmuxMissing` | `TestIntegrationPinnedHostKeyErrorAndRecovery` | `TestPublishesOnlyOnChange`, `TestRefreshNeedsRun` |
| `session` | `TestIntegrationCreateWithDefaults` | `TestIntegrationErrorsMapped`, `TestIntegrationSessionKilledDuringAttachHandshakeSendsExit` | `TestCreateValidation`, `TestWindowsRejectInvalidBeforeExecAndMapsMissing` | `TestIntegrationTmuxStallTimesOutAndRecovers` (`sshx` executor used by session) | `TestMachineNotReady`, `TestIntegrationUnreachableSSHDMarksMachine` | `TestIntegrationCopyModeTmuxMissing` | `TestIntegrationPinnedHostKeyErrorAndRecovery` (unavailable machine blocks session use) | `TestIntegrationConcurrentTypedSessionCreateReturnsConflict`, `TestIntegrationRenameRacesKilledSession` |
| `term` | `TestIntegrationAttachTypeResizeClose` | `TestIntegrationSessionKilledDuringAttachHandshakeSendsExit` | `TestBadRequests` | `TestIntegrationSilentAttachGetsTimeoutClose`, `TestIntegrationSlowTerminalClientDropped` | n/a: SSH transport errors come from `sshx`; user-visible recovery is covered by T10 host-key E2E | n/a: inventory marks a missing `tmux` | n/a: `sshx` owns host-key verification; T10 E2E proves the user-visible failure | `TestConcurrentTerminalReservationsNeverOvershoot`, `TestIntegrationTerminalAttachLimit` |
| `fsbrowse` | `TestIntegrationSFTPHomeListStatAndMkdir` | `TestIntegrationSFTPHomeListStatAndMkdir` (missing stat path) | `TestNormalize`, `TestValidChildName` | `TestIntegrationSFTPStallTimesOutClosesAndRecovers` | `TestIntegrationSFTPUnreachableTarget` | n/a: SFTP does not invoke tmux | n/a: `sshx` pins the SFTP transport host key | `TestIntegrationCancelledSFTPRequestsFreeSlots`, `TestOperationSlotsBoundConcurrencyAndFreeOnCancel` |
| `projects` | `TestIntegrationProjectSessionPlacementRenameEndAndRecreate` | `TestRecentCommandsAreProjectScopedAndUnknownProjectsFail` | `TestCreateSessionRejectsOversizeCommandBeforeRemoteCreate` | n/a: metadata operations do not call the host; session creation inherits `session` deadlines | n/a: metadata operations do not call the host; session creation inherits `sshx` errors | n/a: metadata operations do not inspect tmux; session creation inherits `session` status | n/a: metadata operations do not connect over SSH; session creation inherits `sshx` verification | `TestIntegrationConcurrentTypedSessionCreateReturnsConflict` (project session creation delegates to `session`) |
| `store` | `TestDataSurvivesReopenAndMigrationRerunIsNoop` | `TestMachineNotFound` | `TestNormalizeProjectPathAndName`, `TestNormalizeRecentCommand` | `TestPostgresStatementAndLockTimeouts` | `TestOpenStartupHonorsEarlierCallerDeadline` | n/a: `store` only talks to PostgreSQL | n/a: `store` only talks to PostgreSQL | `TestConcurrentOpenMigratesOnce`, `TestRateLimitUpdatesAreAtomic` |
| `auth` | `TestLoginSessionsAndRotation` | `TestLoginFailuresAreGeneric` | `TestNormalizeEmail`, `TestPasswordHashing` | n/a: PostgreSQL deadlines are enforced in `store` | n/a: `auth` does not connect to host machines | n/a: `auth` does not inspect tmux | n/a: `auth` does not connect over SSH | `TestRateLimitUpdatesAreAtomic` (`store` repository operation used by auth) |

The full info-level canary cycle is `TestIntegrationInfoLogsOmitCanariesAcrossAccountAndHostCycle` in `internal/api`: it signs up and in, creates a project and session with a start command, lists, renames, attaches, makes and browses a folder, kills the session and signs out. `TestSessionLifecycleLogsNamesOnlyAtDebug` verifies that session names remain out of info logs while debug logs retain them for diagnosis.
- **Frontend:** Vitest for stores/utilities. All run in containers.
- **E2E (`make e2e`):** simulates a real user end to end — see §13.1.

**Coverage rule — three layers per acceptance criterion.** Every acceptance criterion (`docs/roadmap/M*-acceptance.md`) names its unit, integration and e2e tests and the task that writes each. A layer is n/a only with a stated reason (e.g. pure byte passthrough has no unit logic, or a UI-only concern has no remote side). "Manual" is reserved for what no automated layer can observe (credentials, real iOS). Tests land in the same commit as the behavior they cover.

### 13.1 E2E environment
A separate Compose project `hostbud-e2e` (`test/e2e/`), started, run and torn down by `make e2e`. It **never touches the real host**: the target is a throwaway container.

| Service | Role |
|---|---|
| `hostbud-e2e-target` | Throwaway "host": `openssh-server`, `tmux`, `vim`, `htop`, user `dev`; host keys and a client key generated per run. A variant without tmux is used for the "tmux missing" scenario. |
| `hostbud-e2e-agent` | `ssh-agent` holding the throwaway client key; its socket is shared with the app, mirroring the production agent-socket mount. |
| `hostbud-e2e-app` | The real hostbud image, with `HOSTBUD_HOST_ADDR=hostbud-e2e-target`, the target's host keys mounted at `/run/host-keys`, and a short poll interval. It runs with the production app's read-only root, `/tmp` tmpfs, dropped capabilities, `no-new-privileges`, PID limit and healthcheck. |
| `hostbud-e2e-postgres` | Disposable PostgreSQL with the same reduced capabilities, `no-new-privileges`, 128 MiB shared memory and log rotation as production. |
| `hostbud-e2e-caddy` | The production Caddy image (`deploy/caddy/Dockerfile`) and proxy config (`deploy/caddy/hostbud.caddy`: loopback-port site), so traffic goes through the production proxy path. It uses the production Caddy capability and read-only filesystem settings, and waits for both app healthchecks. `test/e2e/Caddyfile` adds the domain path for the test domain `hostbud.example.test` (the app's `HOSTBUD_DOMAIN`) with `tls internal`, since e2e has no Cloudflare token or tailnet; the name resolves to Caddy through `extra_hosts`. The production site's DNS-01 issuer is checked by `make test` (`caddy adapt`). |
| `hostbud-e2e-target-notmux`, `hostbud-e2e-app-notmux` | The tmux-less target and a second app instance for it (same database), served by Caddy on `:9056` through `test/e2e/Caddyfile`, which imports the production Caddyfile unchanged and adds only that site. The app uses the same production hardening and healthcheck. |
| `hostbud-e2e-tsfake`, `hostbud-e2e-app-ts` | A unix-socket LocalAPI fake with a ctl-controlled identity map, and a third hardened app sharing the e2e database. Caddy serves the domain path on `hostbud-ts.example.test` and loopback on `:9057`; the test allowlist contains only `allowed@example.com`. |
| `hostbud-e2e-ctl` | Failure switches for the runner, which has no Docker access: a tiny HTTP service with the Docker socket that runs only fixed commands, including identity map changes and restarting the Tailscale test app. App starts poll `/api/health` through Caddy. |
| `hostbud-e2e-runner` | Playwright. Uses `network_mode: service:hostbud-e2e-caddy`, so the browser opens `http://localhost:9055` exactly like the port-forward path (and the Origin check is exercised for real). Also has SSH access to the target to act as "a real terminal". |

**Profiles:** Chromium desktop, and Playwright's `iPhone 13 Pro` device (WebKit, 390×844, touch), both on `http://localhost:9055`; plus `iphone-13-pro-domain`, the same device on `https://hostbud.example.test` (the phone and domain scenarios). Profile targeting lives in `test/e2e/playwright.config.ts`, never in runtime `test.skip()` calls (lint-enforced), so off-profile scenarios aren't scheduled and the report's skipped count stays at 0: file names (`*.phone.spec.ts`, `*.api.spec.ts`, `*.domain.spec.ts`) plus tags on scenarios in shared specs, `@desktop` (desktop Chromium only: mouse, hover, hardware keys, clipboard permissions), `@phone` (phone projects only) and `@loopback` (phone scenarios the domain project leaves to `iphone-13-pro`, because their setup uses the loopback site or the multi app). WebKit on Linux is not real iOS Safari; iOS-specific behavior (on-screen keyboard, gestures) stays on the manual checklist.

**How tests simulate a user**
- Service workers are blocked in every Playwright project by default; `pwa.spec.ts` and the M8 T13 agent-status refresh regression opt in. This avoids caches affecting non-PWA scenarios.
- Drive the UI only through what a user sees: roles, labels, visible text; `data-testid` only where there is no accessible handle (e.g. the terminal container).
- Out-of-band actions like a user's real terminal: the runner runs `tmux` on the target over SSH (create/kill/attach elsewhere) and asserts the UI follows within one poll interval.
- `window.__hostbud` exists only in images built with `VITE_E2E=1`. Every mounted terminal registers under its pane id; `termText(session?)` and the other terminal hooks answer for the named session, or by default for the focused pane of the active tab, and `panes()` lists `{session, active, focused}`. The e2e `page` fixture resets the account's saved layout to empty before each test, so tabs never leak between scenarios: every use is guarded by the statically replaced `import.meta.env.VITE_E2E === '1'`, and `web/scripts/check-dist.mjs` fails any other build whose output still mentions it.
- Terminal content is asserted two ways: what tmux really shows (`tmux capture-pane -p` on the target, the ground truth) and what the browser shows (xterm buffer read through `window.__hostbud.termText()`, exposed only in builds with `VITE_E2E=1`). The same hook's `termTheme(session?)` reports the mounted terminal's background and foreground in E2E builds.
- M6 state-persistence scenarios create their own account with the test helper's `newAccount()` and whitelist it, so per-account tree and theme preferences cannot leak between scenarios. `ui.waitForSave(key)` waits for the debounced `PUT /api/ui-state/{key}` before reloads; one tree-state scenario intentionally reloads immediately to cover the `pagehide` flush.
- Failure scenarios: restart `hostbud-e2e-app` (UI and terminals recover), stop sshd on the target (unreachable banner, then recovery), tmux-less target (install hint), cut and restore the app's network (terminals re-attach by themselves). The throwaway target's sshd sets `ClientAliveInterval 5` so a client that vanished in a cut is dropped along with its stale tmux client.
- No real TUIs that need credentials (Claude Code, Codex); vim and htop cover full-screen apps. Claude Code stays a manual check.
- Traces, screenshots and videos on failure → `test/e2e/results/` (gitignored).

**Driver:** `test/e2e/run.sh` behind `make e2e` (fresh stack → run → `down -v`, even on failure), plus a persistent loop for development: `make e2e-up` / `e2e-run` / `e2e-down`. The runner idles (`sleep infinity`) and each run is a `docker exec`; the specs are bind-mounted, and images rebuild only when a content hash of their inputs changes (stamped in `.cache/e2e/`). The target's per-run keys come from a one-shot `hostbud-e2e-keygen` service; the app sees only the public host keys (`hostbud-e2e-hostpub` volume) and the agent socket.

**When it runs:** not part of `make test`, and **only on demand**: agents never run the suite while implementing features or working through tasks, checkpoints or milestones; commits only type-check it. A full run (or a focused one) happens when the owner asks, and every full run gets a committed report under `docs/e2e-triage/`. No milestone's definition of done waits on a run; E items count as written until a requested run passes them.

**When scenarios are written:** in the same commit as the behavior, never later. The harness exists from early M1 (before any feature), so there is no "e2e phase". API-level scenarios cover backend endpoints before their UI exists; UI scenarios follow with the UI task. Every roadmap task has an *E2E:* line, and every acceptance-checklist E2E item names the task that adds it.
- No committed fixtures containing real hostnames, usernames, or paths — use `example.com`, `server-a`, `/home/dev`.

---

## 14. Repo layout

```
hostbud/
├─ cmd/hostbud/main.go
├─ internal/{api,events,inventory,sshx,tmux,fsbrowse,term,store,config}/
├─ internal/store/migrations/             # embedded SQL (goose), append-only
├─ internal/archtest/                    # import-boundary tests (SQL only in store, …)
├─ internal/{queue,agents}/              # v2: queue service, dispatcher, run hooks; agent adapters
├─ internal/llm/                         # V2-M5: bounded provider classifier and quiet-run supervisor
├─ internal/machines/                    # V2-M13: runtime registry of the host and added servers
├─ web/                                  # Vue app (built into web/dist, embedded)
├─ deploy/caddy/{Dockerfile,Caddyfile}
├─ test/sshd/                            # target image (integration tests + e2e target)
├─ test/e2e/                             # Playwright suite + hostbud-e2e Compose project
├─ Dockerfile
├─ docker-compose.yml
├─ Makefile
├─ .env.example
├─ docs/{ARCHITECTURE.md,ROADMAP.md}
├─ AGENTS.md  CLAUDE.md  README.md  LICENSE
```

## 15. Limits and timeouts

Every bound is recorded here with its enforcement point and the result when it is reached. T1 records current behavior and gaps; M7 T2–T5 fill the marked gaps and update this table in the same task commit.

| What | Limit | Enforced in | On hit (server) | On hit (user sees) | Configurable |
|---|---|---|---|---|---|
| Non-interactive SSH exec | 10 s default; `WaitDelay` 1 s | `sshx.Client.Exec` | Deadline kills the ssh process group and returns `KindTimeout`; after two consecutive timeouts, the ControlMaster is reset with bounded control checks | 504 with the configured duration and a recovery hint; poller marks the host unreachable | Yes: `HOSTBUD_EXEC_TIMEOUT`, 2 s–2 min |
| SSH agent probe | 3 s | `sshx.checkAgent` | Probe context expires and reports agent unavailable | SSH auth hint | No |
| ControlMaster shutdown | 5 s in the server shutdown path | `cmd/hostbud` passes a deadline to `sshx.Client.Close` | Stops waiting when shutdown context expires | None during normal shutdown | No |
| ControlMaster recovery | Two consecutive exec timeouts; `ssh -O exit`/`check` bounded at 3 s each | `sshx.Client` | Stops the master (when a stopped process can't answer `check`, finds it by its unique control-socket path) and removes only sockets confined to `/data/ssh/cm` | Next exec starts a fresh master | No |
| Inventory polling | `HOSTBUD_POLL_INTERVAL` default 3 s, minimum 500 ms; exponential failure backoff default 8× interval, capped at 30 s and never below interval | `inventory.Run` | Poller waits for retry, marks host unreachable, recovers on a successful poll | Machine status banner | Interval: yes; backoff: no |
| SFTP operation | 10 s default; subsystem idle close 1 min; 4 active per machine | `fsbrowse.Service` | Per-operation context closes the stream on timeout or cancellation; waiting for a slot uses the same deadline | 504 with duration and subsystem hint; file browser offers Retry | Yes: `HOSTBUD_SFTP_TIMEOUT`, 2 s–2 min |
| Photo upload | 100 MiB per file; 5 min default; 4 active SFTP operations per machine | API request limits and `fsbrowse.Service.Upload` | Rejects oversized bodies; deadline/cancel closes SFTP; incomplete temporary sibling is removed; completed file is renamed into place | 413 size error, 504 timeout, or actionable filesystem error in the upload dialog | Yes: `HOSTBUD_UPLOAD_TIMEOUT`, 30 s–10 min |
| SFTP path/listing | Path 4096 bytes; 2000 entries; 1 MiB JSON; name 1024 bytes in response | `fsbrowse` validation and listing | Rejects overlong paths and truncates a listing at the entry or response cap | `truncated: true` in the listing response | No |
| Terminal WebSocket input | 1 MiB per message | `term.Handler` read limit | Oversized frame closes the connection | Terminal reconnects after a dropped socket | No |
| Terminal WebSocket output | 2 MiB queued bytes per client; write timeout 10 s | `term.Handler` | Byte overflow or blocked write closes 1013 and kills only the ssh attach | Terminal reconnects and tmux redraws | No |
| Terminal WebSocket liveness | Server ping every 25 s, 10 s pong timeout; browser drops after 25 s without a frame; browser ping every 10 s | `term.Handler`, `web/src/api/term.ts` | Unanswered peer ends attach; silent browser connection is replaced | Reconnecting strip and automatic reattach | No |
| Terminal attachments | No client cap; server cap globally, optionally per account | `term.Handler` reservation before upgrade | HTTP 429 before ssh start; reservation released on process exit | Limit notice with a manual Retry | Yes: `HOSTBUD_MAX_TERMINALS_PER_USER` default 0 = none (0–1024), `HOSTBUD_MAX_TERMINALS` default 128 (1–1024) |
| Events WebSocket | 16 per account; 64-event subscriber buffer; heartbeat 15 s; ping 25 s; write timeout 10 s; browser silence 40 s | `events.Bus`, `api.eventsSocket`, `web/src/api/live.ts` | 429 over account cap; read-only socket closes data senders with 1003; slow subscriber closes with 1013 | Live state reconnects and receives a fresh snapshot | No |
| HTTP server | Header read 10 s; max headers 32 KiB; idle connection 2 min; shutdown 10 s | `cmd/hostbud` | Slow headers are closed; oversized headers get 431; idle connections expire; shutdown is bounded | Browser request fails or reconnects | No |
| JSON request bodies | 64 KiB; state-changing bodies require `application/json`; typed decoders reject unknown fields | API middleware and `decode` | Oversized request gets 413; wrong content type 415; unknown field 400 | Form/API error | No |
| UI state | 64 KiB per saved value | `api.putUIState` | Oversized value gets 413 | Previous saved value remains | No |
| REST request duration | 30 s per `/api/*` request; photo upload uses `HOSTBUD_UPLOAD_TIMEOUT` | API request middleware | Cancels handler context and bounds request/response I/O | Request returns with a timeout error instead of hanging | Upload: yes, 30 s–10 min |
| Database pool/query | 20 open / 5 idle; 30 min lifetime / 5 min idle; statement 5 s, lock 3 s, idle transaction 30 s, connect 5 s; startup 90 s | `store` | PostgreSQL enforces query/connection timeouts; startup fails actionably if DB does not return | Outage/timeouts are 503; health reports degraded | No |
| Authentication | Session TTL 720 h; failures: 5 login, 10 registration, 20 per IP; blocks 30 s ×2 up to 1 h; failure window 1 h | `config`, `auth`, PostgreSQL rate-limit store | Rejects with 429 and `Retry-After` | Sign-in throttle message | Yes: existing `HOSTBUD_*` auth settings |
| v2 run hook body | 64 KiB, JSON; checked after the token | `queue.Hooks.Receive` | 413 / 400 with no side effect | n/a (agent hooks ignore the answer) | No |
| v2 run hook rate | Token bucket per run: 60 a minute, burst 20 | `queue.Hooks` | 429 with no side effect; logged at info with the run id only | n/a | No |
| v2 goal-state reads | Claude transcript: SFTP, 4 MiB per read from the stored offset, `HOSTBUD_SFTP_TIMEOUT`; Codex: one `codex app-server proxy` JSON-RPC call bounded by `HOSTBUD_EXEC_TIMEOUT`; follow-up reads 2/5/15/30/60 s after a pending turn end | `agents.Claude`, `agents.Codex`, `queue.Dispatcher` | A failed read is retried on the next signal or follow-up | Run stays running; unknown formats set needs attention | Via the SFTP and exec timeouts |
| v2 stale window | `HOSTBUD_RUN_STALE_AFTER` default 2 h, 10 s–24 h, from the last signal (or start) | `queue.Dispatcher` stale timer | One last read, then the run is `stale`, its item needs attention and the queue pauses; nothing is killed | Needs-attention badge with the reason | Yes: `HOSTBUD_RUN_STALE_AFTER` |
| V2-M4 verify command | `HOSTBUD_VERIFY_TIMEOUT` default 10 min, 5 s–2 h, by the remote `timeout -k 10s`; local deadline + 15 s; preflight 30 s; 16 KiB output tail; command 4096 bytes, one line | `queue.Verifier`, `sshx.Client.ExecTo`, `store.NormalizeVerifyCommand` | Remote `timeout` stops the command's process group (SIGKILL 10 s later); the item needs attention and the queue pauses; only the tail is kept | "verify timed out after 10m" as the item's reason; the output tail with a "truncated" note | Yes: `HOSTBUD_VERIFY_TIMEOUT` |
| V2-M3 notification payload | 1 KiB JSON; allowlisted fields only; project name 80 characters | `notify.Build`, `Payload.JSON`, `notification_outbox` CHECK | The name is cut; nothing else can enter the payload | n/a | No |
| Push delivery | 10 s per POST (dial, TLS, headers); no redirects; public addresses only; at most 3 retries on network errors, 429 and 5xx (1/5/25 s, `Retry-After` up to 60 s); 4 devices at a time | `notify.Sender` | 404/410 delete the subscription; other 4xx and exhausted retries mark the delivery failed; a claimed delivery is never re-sent | The notification doesn't arrive; Settings → Send test notification checks a device | No |
| Push message lifetime | `TTL` 24 h; `Topic` = hashed dedupe key; `Urgency: high` for needs attention | `notify.Sender` | The push service drops it after 24 h and keeps one per key | n/a | No |
| Notification outbox | Rows older than 7 days pruned (at start, then at most hourly when there is work); one row per account and key | `notify.Sender`, `store.PruneNotifications` | Old rows are deleted; a repeated key adds nothing | n/a | No |
| Test notification | 3 at once, then one per 10 s per account | `notify.Service.SendTest` | 429 with `Retry-After` | "Too many test notifications. Try again in n s." | No |
| Push endpoint | https, port 443, a DNS host with a dot, 2048 bytes; keys a P-256 point and a 16-byte secret | `notify.CheckEndpoint`, `notify.CheckKeys` | 400 | Settings shows the error | No (e2e: `HOSTBUD_PUSH_TEST_ENDPOINT`) |
| Browser layout | 4 panes per tab; layout save debounce 500 ms; reconnect 0.5/1/2/4/8 s then 10 s cap; events backoff up to 10 s | `web/src/lib/layout.ts`, `web/src/stores/layout.ts`, `web/src/api/*` | Client rejects invalid/over-limit layout or retries network | Layout limit notice and reconnect indicator | No |

**Static architecture guard:** `internal/archtest` rejects uncontextualized `exec.Command`, HTTP helpers/default clients or `http.Client` values without a timeout, and WebSocket accepts without a read limit. It also enforces the documented `context.Background()` allowlist with a reason and occurrence count.
