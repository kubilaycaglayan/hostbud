# hostbud — Architecture

hostbud is a self-hosted web app for managing **tmux sessions** from the browser, built for terminal-first and agentic-coding workflows. It runs in Docker on a single host. **v1 manages tmux on that host only**; the design keeps a `machine` abstraction so more SSH targets can be added later.

It is reachable two ways:
1. **Port forward:** Caddy serves plain HTTP on `127.0.0.1:${HOSTBUD_LOCAL_PORT}` (default `9055`); from another machine run `ssh -L 9055:localhost:9055 <host>` and open `http://localhost:9055`.
2. **Domain:** `https://${HOSTBUD_DOMAIN}`, bound to the host's Tailscale IP, so it is only reachable inside the tailnet (e.g. from a phone).

> The important state (tmux sessions and whatever runs in them) lives on the target machines. hostbud only remembers *metadata*: connections, projects (directories), UI layout — and in v2, tasks.

---

## 1. Goals and non-goals

**v1 goals**
- Single target: the **host machine**, reached over SSH from the container. It is always active; hostbud continuously reads its tmux sessions.
- Left-gutter tree: **Project → Session**, expandable/collapsible, sortable, renameable, persisted. (The data model is Machine → Project → Session; the machine level is hidden while there is only one.)
- Full-fidelity terminal in the browser (attach to tmux as if in a local terminal). Multiple tabs, split view, mobile-friendly.
- File-system browser to pick a directory, create folders, and start a session there. Chosen directories become **projects**.
- Deployed via Docker Compose behind Caddy: plain HTTP on loopback for port-forward access, TLS on the Tailscale IP for the domain.

**v1 non-goals**
- Multiple machines: `~/.ssh/config` discovery, custom connections, activation, host-key trust UI (deferred; see ROADMAP *Later*).
- Storing terminal output, scrollback, or agent state.
- Installing software on targets (missing tmux → warning only).
- File editing / previews, git integration.
- Multi-user authorization beyond the single-user v1 account model. v1 still has one trusted host machine, but the web app requires an account; reachability is not sufficient by itself.

**v2 (design only; not implemented)** — see §10: task queue, per-machine capacity, hook-based session status, LLM supervisor (OpenAI first, pluggable), dispatcher that spawns sessions for queued tasks.

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
- **System `ssh` binary, not a Go SSH library.** Gets `~/.ssh/config` semantics (ProxyJump, Include, Match, IdentityAgent, …), ssh-agent, and known_hosts for free and exactly as the user's terminal behaves. **ControlMaster** makes every non-interactive call (poll, list dir) reuse one TCP/SSH connection → millisecond-level round trips.
- **xterm.js (WebGL renderer).** The same terminal engine as VS Code; handles full-screen TUIs (Claude Code, Codex, vim) correctly.
- **PostgreSQL behind a repository layer.** Authentication needs durable, concurrent relational state and owner-managed SQL access to the email whitelist. The database is a Compose service with credentials supplied only through the untracked `.env`; the repository remains the only place with SQL.

---

## 3. Deployment topology (Docker)

Services in `docker-compose.yml` (Compose project `hostbud`; everything named with the `hostbud` prefix):

| Service | Image | Notes |
|---|---|---|
| `hostbud-caddy` | custom build (`caddy:builder` + `xcaddy` + `caddy-dns/cloudflare`) | Publishes `${TAILSCALE_IP}:443:443`, `${TAILSCALE_IP}:80:80` and `127.0.0.1:${HOSTBUD_LOCAL_PORT}:${HOSTBUD_LOCAL_PORT}` **only** (never `0.0.0.0`). Certificates and the ACME account live in the `hostbud-caddy-data` / `hostbud-caddy-config` volumes, so restarts don't re-issue. Two sites: `${HOSTBUD_DOMAIN}` with a cert via DNS-01 using `CLOUDFLARE_API_TOKEN` (read from the environment at runtime; `ACME_EMAIL` optional), and `http://:${HOSTBUD_LOCAL_PORT}` (plain HTTP, loopback only, for SSH port forwarding). Both reverse-proxy to `hostbud:8080` (WebSocket upgrade supported by default). Config: `deploy/caddy/hostbud.caddy` holds the proxy snippet and the loopback site (shared with e2e); `deploy/caddy/Caddyfile` adds the global options (admin off; HTTP/1.1 and HTTP/2 only, since UDP isn't published) and the domain site. |
| `hostbud` | built from repo `Dockerfile` | No published ports. Runs as `${HOST_UID}:${HOST_GID}`. |
| `hostbud-postgres` | pinned official PostgreSQL image | No public or tailnet exposure. Owner maintenance access is optional and loopback-only at `127.0.0.1:${HOSTBUD_DB_LOCAL_PORT}:5432`; credentials come from the untracked `.env`. |

**DNS:** Cloudflare `A` record `${HOSTBUD_DOMAIN}` → host's Tailscale IP (100.x.y.z), **DNS only (grey cloud)**. Never proxied (orange) and never a Cloudflare Tunnel — both would expose the app publicly. The name resolves publicly but the address is only routable inside the tailnet.

**hostbud container mounts / settings**
- `hostbud-data:/data` — generated SSH config, ControlMaster sockets and app known_hosts. PostgreSQL data lives in the separate `hostbud-postgres-data` volume.
- ssh-agent socket: `${HOST_SSH_AUTH_SOCK}:/run/ssh-agent.sock` and `SSH_AUTH_SOCK=/run/ssh-agent.sock`. Private keys never enter the container. The host uses a **dedicated key** (`~/.ssh/hostbud_ed25519`) loaded into the agent at boot by a systemd user unit; its `authorized_keys` entry is restricted with `from="172.16.0.0/12"` (Docker networks) and `no-agent-forwarding,no-port-forwarding,no-X11-forwarding`, so the compose network uses a fixed subnet in that range. The host should use a **stable** agent socket path (e.g. systemd user `ssh-agent.socket`, or `ssh-agent -a ~/.ssh/agent.sock`) — document this in README.
- The host's **public** host keys, read-only: `/etc/ssh/ssh_host_ed25519_key.pub`, `…_ecdsa_key.pub`, `…_rsa_key.pub` → `/run/host-keys/` (used to pin the host key, §4.3). Each file is bound individually with `create_host_path: false` (so a missing key fails loudly instead of Docker creating a directory).
- *Later (multi-machine):* user SSH config, **read-only, at the same absolute path as on the host**, with `HOME=${HOST_HOME}` inside the container so `~` and absolute `Include` paths resolve identically (`~/.ssh/config`, `~/.ssh/known_hosts`, optional `~/.ssh/config.d` via long-syntax bind with `create_host_path: false`). Never mount the whole `~/.ssh` (keeps private key files out).
- `extra_hosts: ["host.docker.internal:host-gateway"]` — the host machine is reached over SSH like any other target (requires `sshd` on the host and the user's own key in `authorized_keys`).
- *Later (multi-machine):* tailnet reachability from the container — traffic to 100.x routes through the host. Verify MagicDNS names resolve inside the container; if not, set `dns: [100.100.100.100]` on the service. Fallback (documented, not default): `network_mode: host`.

**Runtime image:** multi-stage — `node` (build SPA) → `golang` (build with SPA embedded via `go:embed`, `CGO_ENABLED=0`) → `debian:stable-slim` with `openssh-client`, `ca-certificates`, `tini`. Target arch: **linux/amd64**. The image creates a `hostbud` user with `${HOST_UID}:${HOST_GID}` (build args) because `ssh` refuses to run for a uid without a passwd entry, and pre-creates `/data` owned by it so the named volume is writable. The Compose network's subnet is `${HOSTBUD_SUBNET}` (default `172.29.55.0/24`).

**Build & deploy:** the dev machine is the host; `docker compose up -d --build` (wrapped in `make deploy`). No registry.

**Dockerized toolchain:** everything that can run in Docker does. `make build`, `test`, `lint` (golangci-lint, eslint/vue-tsc) and `gitleaks` run in containers, so the host needs only Docker. The gitleaks pre-commit hook also runs via Docker. Tools run in long-lived toolbox containers (`hostbud-tools-<tool>`, `scripts/tool.sh`) that `make` reaches with `docker exec`: creating a container costs seconds per call on a busy daemon, while an exec is near-instant. No CI for now.

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

# (later) 2. App-managed custom connections (from DB): Host hostbud-custom-<id> …
# (later) 3. The user's real config (read-only mount): Include ~/.ssh/config

# 4. App defaults — last, so user settings win
Host *
  ControlMaster auto
  ControlPath /data/ssh/cm/%C
  ControlPersist 10m
  ServerAliveInterval 15
  ServerAliveCountMax 3
  UserKnownHostsFile /data/ssh/known_hosts
  StrictHostKeyChecking yes
  BatchMode yes                # never prompt inside non-interactive calls
```

- `ControlPath` uses `%C` (hash) to stay under the Unix socket path length limit.
- `BatchMode yes` for all non-interactive calls; interactive attach also relies on agent auth (no password prompts in v1).

### 4.2 Host discovery (later — multi-machine)
- Parse the user's config with `github.com/kevinburke/ssh_config` **for display only** (list concrete `Host` aliases; skip wildcard/negated patterns). Resolve effective settings for display via `ssh -G <alias>`.
- Machine sources: `host` (built-in, `host.docker.internal`, user `${HOST_SSH_USER}`, label `${HOSTBUD_HOST_LABEL}`), `sshconfig` (discovered; re-scanned on startup and via "Refresh"), `custom` (DB).
- Discovered hosts are shown but **inactive by default**; activation state persists in DB.

### 4.3 Host-key trust
Never trust on first use.
- **v1 (host machine):** on startup hostbud reads the read-only mounted `/run/host-keys/ssh_host_*_key.pub` and writes them to `/data/ssh/known_hosts` as `hostbud-host <key>`. The key comes from the host's filesystem, not from the network, so no UI confirmation is needed. Missing key files → startup error with instructions.
- *Later (other machines):* `ssh-keyscan` → show fingerprints in UI → on user confirmation append to `/data/ssh/known_hosts`.
- Mismatch → hard error surfaced in UI.

### 4.4 Command execution rules
- Every remote command goes through one function that builds `ssh -F cfg <alias> -- <cmd>`; `<cmd>` is assembled only from **shell-quoted** arguments (single-quote escaping helper). Never interpolate user input unquoted.
- Validate tmux session names: `^[A-Za-z0-9_-]{1,64}$` (tmux forbids `.` and `:`; we are stricter).
- Per-command timeouts (default 10s); context cancellation kills the process.
- Machine capability probe on startup (and on activation, later): `uname -s; command -v tmux; tmux -V` → store `os`, `tmux_version`, `tmux_missing`. If tmux is missing, mark the machine and show the install command (`apt install tmux` / `brew install tmux`); never auto-install.

---

## 5. tmux integration (`tmux` + `inventory`)

### 5.1 Reading state
Per **active** machine (v1: the host, always active), one poller goroutine runs every `HOSTBUD_POLL_INTERVAL` (default 3s):

```
tmux list-sessions -F '#{session_id}\t#{session_name}\t#{session_path}\t#{session_attached}\t#{session_windows}\t#{session_created}\t#{session_activity}'
```
(“no server running” ⇒ empty list, not an error.) Window/pane listing (`list-windows -a`) is fetched lazily when a session node is expanded.

The poller diffs against the in-memory cache and publishes `sessions.changed` / `machine.status` events on the bus. Browsers receive them over the events WebSocket. Pollers back off exponentially on failure and mark the machine `unreachable`.

*Later optimization:* tmux control mode (`tmux -C attach`) for push-based `%sessions-changed` notifications. The poller interface should allow swapping the implementation.

### 5.2 Mutations
- **Create:** `tmux new-session -d -s <name> -c <path> [-e KEY=VAL …] [<start-cmd>]` then attach. The user may give a custom name; otherwise the default is the directory's last path segment (`/root/docs/dev` → `dev`; characters a name can't hold become `-`); if taken, `dev-1`, `dev-2`, …. `-e` requires tmux ≥ 3.2 (needed in v2 for hook env vars; degrade gracefully).
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
- WebSocket close ⇒ kill the ssh process (tmux session survives). The server holds **no terminal state**. The server pings each client every 25 s and drops one that doesn't answer within 10 s (a vanished browser), which also ends its ssh process.
- **Auto-reconnect** (`web/src/api/term.ts`, `TermSession`): the client sends `{"type":"ping"}` every 10 s, and 25 s without any frame (a network cut hangs TCP without a close) counts as a drop. After a drop it re-attaches with backoff 0.5 s, 1, 2, 4, 8, then every 10 s (±20 % jitter, no cap), right away on the browser's `online` event or when the page becomes visible; the backoff starts over once an attach has stayed up for 5 s. It re-attaches at the current size and keeps xterm's buffer (tmux's redraw replaces the screen; the scrollback stays searchable). Keys typed meanwhile are dropped, not queued. It does **not** retry after an `exit` frame (detach, or the session ended), after the view closed, for a session that left the live list, or when an attempt that failed before opening finds the sign-in gone (`/api/auth/me` 401 ⇒ the sign-in form). Exit code 255 is ssh's own failure (host unreachable, a dead ControlMaster), not tmux ending, so it is retried like a drop. While retrying, a strip over the terminal says "Reconnecting… (attempt n)" with **Retry now**.
- Each tab/split pane = its own WebSocket + ssh process, all multiplexed over the machine's ControlMaster. Inactive tabs stay attached (their scrollback keeps filling), which is why the layout caps open terminals at 16 (§11).
- Same session open in two views: tmux sizes per its `window-size` option; document this, don't override it.
- Backpressure: bounded write buffer per connection; drop the connection if the client stalls beyond a limit (it will reconnect and redraw).

**Keys:** Mac "natural text editing" shortcuts (`web/src/lib/terminalKeys.ts`) send what a macOS terminal sends: Option+Backspace `ESC DEL`, Cmd+Backspace `Ctrl-U`, Option+←/→ `ESC b`/`ESC f`, Cmd+←/→ `Ctrl-A`/`Ctrl-E`. Copy is Ctrl+Shift+C / Cmd+Shift+C (and Cmd+C with a selection); paste is Ctrl+Shift+V / Cmd+Shift+V / Cmd+V. Ctrl+C and Ctrl+V stay the program's. Other keys are xterm's.

**Clipboard** (`web/src/lib/clipboard.ts`): copy writes the selection with `navigator.clipboard.writeText` and keeps it; a refusal shows a toast. The paste keys keep the browser's default, so its `paste` event reaches xterm, which wraps the text in `ESC[200~ … ESC[201~` when the program enabled bracketed paste (no clipboard-read permission needed); the context menu's Paste uses `readText()` + `term.paste()`. The context menu (Copy, Paste, Select all) opens on right-click or a long press, and with Shift/Option+right-click when the program captures the mouse (a plain right-click then belongs to the program, e.g. tmux's menu). Shift+drag (Option+drag on macOS, `macOptionClickForcesSelection`) selects when the mouse is captured. The clipboard API needs a secure context: `http://localhost` and the HTTPS domain qualify, a plain-HTTP LAN address doesn't.

**OSC 52** (`@xterm/addon-clipboard` with a custom provider) is **write-only**: writes (any selection parameter; tmux sends an empty one) go to the browser clipboard, without awaiting, so a refused write never stalls the terminal; payloads over 1 MiB decoded, empty or undecodable ones are ignored. Queries (`52;c;?`) are swallowed by a handler registered after the addon, so nothing answers them and no program on the host can read the browser clipboard. tmux's default `set-clipboard external` forwards copy-mode yanks as OSC 52 (`xterm*` has the `clipboard` feature); programs inside tmux (vim, Claude Code) need `set -g set-clipboard on` in the user's `~/.tmux.conf`, which hostbud documents and never changes.

**Links** (`web/src/lib/links.ts`): printed URLs (`web-links` addon) and OSC 8 hyperlinks (xterm's `linkHandler`, e.g. `ls --hyperlink`, Claude Code) open on click or tap, only for `http:`/`https:`, in a new tab with `noopener,noreferrer`; anything else (`javascript:`, `file:`, `data:`, `ssh:` …) is ignored. An OSC 8 link's text can differ from its target, so hovering shows the target in a tooltip. tmux forwards OSC 8 only to terminals with its `hyperlinks` feature, which isn't in its defaults: users add `set -as terminal-features ',xterm*:hyperlinks'` (documented; hostbud never changes tmux config).

**Scrollback under tmux** (`web/src/lib/scrollback.ts`): tmux draws in the alternate screen, where xterm keeps no scrollback, so the browser would have nothing to search or scroll back through. xterm therefore ignores the alternate-screen switches (DECSET/DECRST 1049, 1047, 47): tmux draws on the normal screen, and lines scrolled off the top of its pane region go to xterm's scrollback (5000 lines). tmux scrolls bursts with `CSI n S`, which xterm would drop; a handler saves those lines too when the region starts at the top row. That handler uses xterm 6 internals (no public API scrolls into the scrollback) and falls back to xterm's default if they change; `scrollback.spec.ts` pins it against the real xterm. Lines tmux never sends (what scrolls past between two of its screen updates) aren't recovered; tmux copy mode has them. Side effects: with tmux `mouse off`, the wheel scrolls xterm's scrollback instead of sending arrow keys, and full-screen programs that scroll a top-anchored region (e.g. vim's Ctrl-E) add their lines to it.

**Search** (`TerminalSearch.vue`, `@xterm/addon-search`): Ctrl+Shift+F, Cmd+F, Cmd+Shift+F or the 🔍 button (plain Ctrl+F stays readline's). It searches xterm's buffer: the screen plus the scrollback received since attaching (kept across auto-reconnects). tmux history from before the attach is copy mode's job (`prefix [`, then `?`). Match case and Regex options, a match count, all matches highlighted and the current one emphasized, Enter/Shift+Enter for next/previous, Escape closes and refocuses the terminal; an invalid regex shows "Invalid pattern". The addon only re-highlights for a new term, so an option change clears its cache first.

**Frontend terminal:** `@xterm/xterm` + addons `fit`, `webgl` (fallback to canvas/DOM), `web-links`, `unicode11`, `search`, `clipboard`. Font: a bundled Nerd-Font-compatible monospace (self-hosted, no external CDN).

---

## 7. File browser (`fsbrowse`)

- SFTP via `github.com/pkg/sftp` over a pipe to `ssh -F cfg <alias> -s sftp` (reuses ControlMaster). Portable across Linux and macOS (no reliance on GNU `find -printf`) and safe with odd filenames.
- Operations: `home` (Getwd), `list(path)` (dirs first, hidden toggle, symlinks resolved lazily), `stat`, `mkdir`. No delete/rename in v1.
- UI: breadcrumb + path input with autocomplete, keyboard navigation, favorites = projects, recents per machine. Actions: **Create folder**, **Open as project**, **New session here**.
- Keep one SFTP client per active machine (lazy, idle-timeout close).

---

## 8. Persistence (`store`)

- PostgreSQL via a pinned official image, with the application connecting over the private Compose network. Database name, user, host, port and password come from `HOSTBUD_DB_*` environment variables. The password is never committed, logged or placed in an image.
- Migrations: embedded SQL files (`internal/store/migrations/`) run at startup with `pressly/goose`; append-only. Queries via `sqlc` or a hand-written repository interface — **no SQL outside the store package**.
- IDs: ULIDs (text). Timestamps: UTC.
- The owner can inspect and maintain the database with `docker compose exec hostbud-postgres psql -U <HOSTBUD_DB_USER> -d <HOSTBUD_DB_NAME>` using values in the local `.env`, or from the host through the loopback-only `HOSTBUD_DB_LOCAL_PORT` mapping. This is an operator access path, not an application API.
- UI state is per account: `store.UIStateForUser`/`PutUIStateForUser` namespace the key as `user:<user-id>:<key>` in `ui_state` (no migration), so accounts never see each other's layout.
- PostgreSQL is initialized as a fresh application database for M1. The provisional SQLite database is not imported because the pre-M1 deployment is unused; if an old SQLite file remains in the app data volume, it is left untouched. PostgreSQL schema migrations are append-only.

**v1 schema (sketch)** — `machines` is seeded with the single built-in `host` row.
```sql
machines(id, source TEXT CHECK(source IN ('host','sshconfig','custom')),
         ssh_alias, label, active BOOL, sort_order, hidden BOOL,
         os, tmux_version, tmux_missing BOOL, last_seen_at, created_at, updated_at)
-- later (multi-machine): created by a future migration
custom_connections(machine_id PK/FK, hostname, user, port, proxy_jump, identity_agent, extra_options_json)
projects(id, machine_id FK, path, name, sort_order, pinned BOOL,
         last_used_at, created_at, UNIQUE(machine_id, path))
session_links(machine_id, session_name, project_id, created_at, PRIMARY KEY(machine_id, session_name))
recent_commands(id, project_id, command, last_used_at)    -- start commands like `claude`, `codex`
ui_state(key PK, value_json, updated_at)                    -- per account: key = user:<user-id>:<key> (layout; M6: tree, theme)
users(id, email, email_normalized UNIQUE, password_hash, created_at, updated_at,
      last_login_at, disabled BOOL)
email_allowlist(email_normalized PK, enabled BOOL, note, created_at, updated_at)
auth_sessions(id_hash PK, user_id FK, expires_at, created_at, last_seen_at,
              user_agent, created_ip)
login_rate_limits(scope_key PK, failures INT, blocked_until, last_failure_at,
                   updated_at)
```

**Backups:** `make backup` runs a consistent PostgreSQL dump using the running database credentials and copies it to `./backups/` (gitignored). It must never print the password or include it in the backup command arguments shown in logs.

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
GET    /api/machines                      list (with status) — v1: just the host
GET    /api/machines/:id/sessions
POST   /api/machines/:id/sessions         {name, path, startCommand?}
PATCH  /api/machines/:id/sessions/:name   rename
DELETE /api/machines/:id/sessions/:name   kill (UI confirms)
POST   /api/machines/:id/sessions/:name/copy-mode
GET    /api/machines/:id/fs?path=         list dir
POST   /api/machines/:id/fs/mkdir
GET    /api/machines/:id/fs/home
GET|POST|PATCH|DELETE /api/projects[/:id]
GET|PUT /api/ui-state/:key            the account's JSON (GET 404 before the first PUT; PUT 204)
GET    /api/health

# later (multi-machine)
POST   /api/machines                      add custom connection
PATCH  /api/machines/:id                  label, sort, hidden, connection fields
DELETE /api/machines/:id                  custom only
POST   /api/machines/:id/activate | /deactivate
POST   /api/machines/refresh              re-scan ~/.ssh/config
GET    /api/machines/:id/hostkey          keyscan fingerprints
POST   /api/machines/:id/hostkey/trust
```
`/api/ui-state/:key` accepts only allowlisted keys (`layout`; M6 adds `tree` and `theme`; others 404). A PUT body must be valid JSON (400) of at most 64 KiB (413). The server stores it without interpreting it and publishes no event (it's a per-account preference); the client validates what it reads back.

WebSockets: `/ws/events` (server → client state events), `/ws/term` (interactive). `/ws/events` also sends `{"type":"heartbeat"}` every 15 s (WebSocket pings are invisible to page scripts); the browser treats 40 s of silence as a hung connection and reconnects, and the next snapshot resyncs the list.

**Security middleware (all routes):**
- Public routes are limited to `GET /api/health`, `POST /api/auth/register` and `POST /api/auth/login`. All other routes require the server-side session cookie.
- Authentication cookies are opaque, HttpOnly, SameSite and Secure under HTTPS. Never put credentials, session cookies or bearer tokens in URLs, logs, WebSocket query parameters or client storage.
- Origin allowlist: `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`. Reject WebSocket upgrades and state-changing requests whose `Origin` is not in it. (`localhost` is a secure context, so clipboard APIs work over plain HTTP.)
- Optional Tailscale identity allowlist (off by default; the tailnet is trusted): if `HOSTBUD_ALLOWED_TS_USERS` is set and the tailscaled socket is mounted, resolve the client IP (from Caddy's `X-Forwarded-For`, trusted only from the Caddy container) via Tailscale LocalAPI `whois` and reject unknown users.
- CSRF: JSON-only API + SameSite cookies + Origin check on state-changing requests.
- Never log command strings containing user paths at info level.
- Never log passwords, session cookies, password hashes, database passwords, full email addresses or whitelist contents.

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
- **Tailwind CSS + Reka UI** (headless, accessible primitives) for an IDE-like dense dark UI; light theme too via CSS variables. The theme setting is Dark / Light / System (default System, following `prefers-color-scheme` live), stored in `ui_state` and applied to the xterm palette as well.
- Layout: resizable left gutter (tree) | main area with **tabs**, each tab may be **split** (horizontal/vertical, via `splitpanes`). Layout persisted in `ui_state`.
- **Tabs (M3, `lib/layout.ts`, `stores/layout.ts`):** the layout is `{version: 1, tabs: [{id, root, focusedPane}], activeTab}` where `root` is a pane `{type: 'pane', id, machine, session}` (splits: M3 T8); IDs come from `crypto.randomUUID`. The focused pane of the active tab is "the selected session". Picking a session in the list focuses the tab that shows it, or opens a new tab at the end; **New session** opens its session in a new tab. At most 16 terminals are open in total; a 17th is refused with a toast. Closing a tab (×, middle click, or Delete on the focused tab) only detaches its views; no confirmation. Inactive tabs stay mounted (`v-show`) and attached; a hidden terminal doesn't refit (it keeps tmux at its last size) and refits when shown. Keyboard input goes to the active tab's focused terminal only.
  - **Sessions that change:** a rename from the UI relabels every pane showing the session, without re-attaching (the list may show the new name before the request returns, so the store remembers renames in flight). A session missing from a fresh list closes its panes with one toast ("Session <name> ended"). Only a list from a reachable host counts: right after an app restart the first snapshot can come before the first poll with an empty list, and an unreachable host's list is stale. A rename done in a real terminal therefore looks like an end.
  - **Persistence:** the layout is loaded from `GET /api/ui-state/layout` on sign-in, before any terminal mounts and before `/ws/events` starts; it's validated (shape, `version`, the 16-terminal limit: extra tabs are dropped, a missing focus or active tab is repaired) and invalid data falls back to an empty layout with a console warning. Every change is saved with `PUT`, debounced 500 ms. Sign-out forgets it without saving.
  - **Narrow screens:** M2's list-or-terminals switch stays; the terminal side shows the tab bar (compact) and the active tab, and **Back to sessions** returns to the list without closing tabs.
- **Splits (M3 T8):** a tab's `root` may be a split `{type: 'split', id, dir: 'row' | 'column', sizes, children}` (row = side by side; `sizes` are percents summing to 100). At most 4 panes per tab (and 16 in total). **Split right/down** in the focused pane's header opens a session picker (live sessions + **New session…**, whose session then lands in the new pane); a list row's ⋯ menu has **Open in split right/down** (beside the active tab's focused pane, or a new tab when none is open). Inside a split of the same direction the new pane is a sibling and the space is shared equally; the other direction nests a new split. The new pane gets focus. **Close pane** (× in its header) detaches that view and gives its space back to its siblings in proportion; a split left with one child collapses to it, a child split of the same direction merges into its parent, and the last pane closes the tab. The same rules remove panes of ended sessions. Clicking into a pane focuses it (an accent outline when the tab has several); keyboard input goes to the focused pane only.
  - Rendering: `splitpanes` renders the tree recursively (`LayoutNodeView.vue`), minimum pane size 10 %. Dragging a divider resizes live (each pane's ResizeObserver refits it and resizes its tmux window); the sizes are saved when the drag ends. Stored splits are validated (≥ 2 children, a known direction, ≤ 4 panes per tab; bad sizes become equal shares).
  - Narrow screens: a split tab shows only its focused pane, full size, with a "Pane n of m" header button that cycles focus; the layout itself isn't changed, so a wide screen shows the split again. Crossing the breakpoint re-mounts the tab's terminals (they re-attach).
- Tree: projects → sessions (→ windows, lazily); a machine level appears only once multiple machines exist. Status dots (● attached/active, ○ detached; a header banner for host unreachable / tmux missing). Drag-to-sort (`vue-draggable-plus`), inline rename, collapse state persisted, context menus (attach, attach in split, new session, rename, kill, open folder, save as project).
- Command palette (⌘/Ctrl-K): jump to session/project.
- **Narrow screens (M1):** the session list and the open terminal take turns (a "Back to sessions" button), so the terminal gets the full width; the host banner stays above both.
- **Phones (M2):** the app is pinned to the visual viewport (`--app-height`, `lib/appHeight.ts`), so the on-screen keyboard shrinks the terminal instead of covering it; the terminal's hidden input uses a 16px font (no iOS zoom on focus) with autocorrect/autocapitalize off; a ⌨ button on touch screens refocuses it.
- **Mobile:** tree becomes a drawer; single terminal view; an on-screen key bar (Esc, Tab, Ctrl, Alt, arrows, `|`, `~`, `/`, Scroll-mode button → copy-mode API); larger touch targets.
- **Installable app (PWA, M5):** `manifest.webmanifest` (`display: standalone`, theme colors, bundled icons incl. `apple-touch-icon`; no external assets) and a small hand-written service worker served from `/` with `Cache-Control: no-cache`. It precaches the hashed build output and serves the app shell cache-first; `/api/*`, `/ws/*` and anything else pass straight to the network, uncached, so auth and live data never come from a cache. A new worker waits and takes over on the next launch. The worker only registers in a secure context (the HTTPS domain, or `localhost`). On iOS an installed app has its own cookie store, separate from Safari's, so it asks to sign in once. Standalone mode pads the layout with `env(safe-area-inset-*)`.
- **Terminal context menu (M3):** Reka UI `ContextMenu` around the terminal (`TerminalMenu.vue`): Copy (disabled with no selection), Paste, Select all; see §6 *Clipboard*.
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

---

## 13. Testing strategy

- **Unit:** quoting/escaping, name validation, tmux output parsing, SSH config generation ordering, project-path matching.
- **Integration:** `test/sshd/` — a disposable container with `openssh-server` + `tmux` (plus a tmux-less variant) and a generated throwaway key; the test suite runs hostbud's sshx/tmux/inventory/term/fsbrowse packages against it (probe, list, create, attach via PTY, mkdir, kill, error mapping). It also checks the real deploy config (`docker compose config`: published ports, `user`, mounts). Runs via `make test` in Docker: `scripts/test-sshd.sh` keeps `hostbud-test-sshd` (+ `-notmux`) running on the `hostbud-test` network with throwaway keys in `.cache/test-sshd/` (recreated only when `test/sshd` changes; `make test-down` removes them), and the Go toolbox joins that network. The deploy-config check reads `docker compose config` rendered with placeholder values, never the real `.env`, and the Caddy checks read `caddy list-modules` / `caddy adapt` output of the built `hostbud-caddy` image (`scripts/caddy-config.sh`), also with placeholders.
- **Frontend:** Vitest for stores/utilities. All run in containers.
- **E2E (`make e2e`):** simulates a real user end to end — see §13.1.

**Coverage rule — three layers per acceptance criterion.** Every acceptance criterion (`docs/roadmap/M*-acceptance.md`) names its unit, integration and e2e tests and the task that writes each. A layer is n/a only with a stated reason (e.g. pure byte passthrough has no unit logic, or a UI-only concern has no remote side). "Manual" is reserved for what no automated layer can observe (credentials, real iOS). Tests land in the same commit as the behavior they cover.

### 13.1 E2E environment
A separate Compose project `hostbud-e2e` (`test/e2e/`), started, run and torn down by `make e2e`. It **never touches the real host**: the target is a throwaway container.

| Service | Role |
|---|---|
| `hostbud-e2e-target` | Throwaway "host": `openssh-server`, `tmux`, `vim`, `htop`, user `dev`; host keys and a client key generated per run. A variant without tmux is used for the "tmux missing" scenario. |
| `hostbud-e2e-agent` | `ssh-agent` holding the throwaway client key; its socket is shared with the app, mirroring the production agent-socket mount. |
| `hostbud-e2e-app` | The real hostbud image, with `HOSTBUD_HOST_ADDR=hostbud-e2e-target`, the target's host keys mounted at `/run/host-keys`, and a short poll interval. |
| `hostbud-e2e-caddy` | The production Caddy image (`deploy/caddy/Dockerfile`) and proxy config (`deploy/caddy/hostbud.caddy`: loopback-port site), so traffic goes through the production proxy path. `test/e2e/Caddyfile` adds the domain path for the test domain `hostbud.example.test` (the app's `HOSTBUD_DOMAIN`) with `tls internal`, since e2e has no Cloudflare token or tailnet; the name resolves to Caddy through `extra_hosts`. The production site's DNS-01 issuer is checked by `make test` (`caddy adapt`). |
| `hostbud-e2e-target-notmux`, `hostbud-e2e-app-notmux` | The tmux-less target and a second app instance for it (same database), served by Caddy on `:9056` through `test/e2e/Caddyfile`, which imports the production Caddyfile unchanged and adds only that site. |
| `hostbud-e2e-ctl` | Failure switches for the runner, which has no Docker access: a tiny HTTP service with the Docker socket that runs only fixed commands (restart `hostbud-e2e-app`, stop/start sshd on the target, disconnect/reconnect `hostbud-e2e-app` from the `hostbud-e2e` network). |
| `hostbud-e2e-runner` | Playwright. Uses `network_mode: service:hostbud-e2e-caddy`, so the browser opens `http://localhost:9055` exactly like the port-forward path (and the Origin check is exercised for real). Also has SSH access to the target to act as "a real terminal". |

**Profiles:** Chromium desktop, and Playwright's `iPhone 13 Pro` device (WebKit, 390×844, touch), both on `http://localhost:9055`; plus `iphone-13-pro-domain`, the same device on `https://hostbud.example.test` (the phone and domain scenarios). WebKit on Linux is not real iOS Safari; iOS-specific behavior (on-screen keyboard, gestures) stays on the manual checklist.

**How tests simulate a user**
- Drive the UI only through what a user sees: roles, labels, visible text; `data-testid` only where there is no accessible handle (e.g. the terminal container).
- Out-of-band actions like a user's real terminal: the runner runs `tmux` on the target over SSH (create/kill/attach elsewhere) and asserts the UI follows within one poll interval.
- `window.__hostbud` exists only in images built with `VITE_E2E=1`. Every mounted terminal registers under its pane id; `termText(session?)` and the other terminal hooks answer for the named session, or by default for the focused pane of the active tab, and `panes()` lists `{session, active, focused}`. The e2e `page` fixture resets the account's saved layout to empty before each test, so tabs never leak between scenarios: every use is guarded by the statically replaced `import.meta.env.VITE_E2E === '1'`, and `web/scripts/check-dist.mjs` fails any other build whose output still mentions it.
- Terminal content is asserted two ways: what tmux really shows (`tmux capture-pane -p` on the target, the ground truth) and what the browser shows (xterm buffer read through `window.__hostbud.termText()`, exposed only in builds with `VITE_E2E=1`).
- Failure scenarios: restart `hostbud-e2e-app` (UI and terminals recover), stop sshd on the target (unreachable banner, then recovery), tmux-less target (install hint), cut and restore the app's network (terminals re-attach by themselves). The throwaway target's sshd sets `ClientAliveInterval 5` so a client that vanished in a cut is dropped along with its stale tmux client.
- No real TUIs that need credentials (Claude Code, Codex); vim and htop cover full-screen apps. Claude Code stays a manual check.
- Traces, screenshots and videos on failure → `test/e2e/results/` (gitignored).

**Driver:** `test/e2e/run.sh` behind `make e2e` (fresh stack → run → `down -v`, even on failure), plus a persistent loop for development: `make e2e-up` / `e2e-run` / `e2e-down`. The runner idles (`sleep infinity`) and each run is a `docker exec`; the specs are bind-mounted, and images rebuild only when a content hash of their inputs changes (stamped in `.cache/e2e/`). The target's per-run keys come from a one-shot `hostbud-e2e-keygen` service; the app sees only the public host keys (`hostbud-e2e-hostpub` volume) and the agent socket.

**When it runs:** not part of `make test`. It's required before every commit that changes behavior e2e can reach (UI, or HTTP/WebSocket API through Caddy), and for every milestone's definition of done.

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
├─ internal/{llm,orchestrator}/          # v2
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
