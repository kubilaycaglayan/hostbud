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
(“no server running” ⇒ empty list, not an error.) Window and pane listing is fetched lazily when a session node is expanded, in one side-channel exec using `list-windows -t '=<name>'` followed by `list-panes -s -t '=<name>'`, under `LC_ALL=C.UTF-8` so tmux preserves tab separators and Unicode for the non-interactive SSH client. The response is sorted by window/pane index and capped at 256 windows and 64 panes per window; free-form names and commands are sanitized and capped at 256 bytes. This layout is not polled or published as an event. An authenticated `POST /api/machines/:id/sessions/:name/select` validates window/pane ids against that session before issuing `select-window` / `select-pane`; selecting a window is visible to every client attached to the session, as with tmux's `prefix n`.

The poller diffs against the in-memory cache and publishes `sessions.changed` / `machine.status` events on the bus. Browsers receive them over the events WebSocket. Pollers back off exponentially on failure and mark the machine `unreachable`.

*Later optimization:* tmux control mode (`tmux -C attach`) for push-based `%sessions-changed` notifications. The poller interface should allow swapping the implementation.

### 5.2 Mutations
- **Create:** `tmux new-session -d -s <name> -c <path> [-e KEY=VAL …] [<start-cmd>]` then attach. The user may give a custom name; otherwise the default is the directory's last path segment (`/home/dev/docs` → `docs`; characters a name can't hold become `-`). If either an auto-derived or typed name is already in use, creation picks the first free `<name>-<n>` (`work` → `work-1`; a typed `work-1` → `work-1-1`) and returns that actual name; a colliding 64-character name is trimmed to fit the suffix. A tmux race retries up to 20 times. Rename still returns 409 for a taken name. `-e` requires tmux ≥ 3.2 (needed in v2 for hook env vars; degrade gracefully).
- **Rename:** `tmux rename-session -t '=<old>' <new>`.
- **Kill:** `tmux kill-session -t '=<name>'` — **always behind a confirmation dialog**.
- **Copy/scroll mode** (for mobile): authenticated `POST /api/machines/:id/sessions/:name/copy-mode` accepts `enter`, `scroll-up`/`scroll-down` (1–500 lines), `page-up`/`page-down`, `top`, `bottom` or `exit`. It uses the exact `=<name>:` pane target and side-channel `tmux copy-mode -e -u` / `send-keys -X` commands, so it never depends on the user's prefix key, `mode-keys` or `mouse` setting. Each command reads back `{inMode, scrollPosition, historySize}` from tmux. If tmux has already left copy mode, a repeated scroll/exit reports its current state instead of an error. Requires tmux ≥ 2.4. Copy mode is transient pane state: it is neither stored nor polled and publishes no hostbud event. Since copy mode belongs to the pane, all clients attached to that pane see its scrolling together.
- Always use `=`-prefixed exact targets.
- **Never modify the user's tmux config or global options.**

### 5.3 Project ↔ session mapping
A session belongs to its `session_links` project when a link exists on the same machine; otherwise it belongs to the project whose `path` is the **longest path-component prefix** of `session_path` on that machine. Unmatched sessions appear under an "Other sessions" node with a "Save as project" action. Project-created sessions record `(machine_id, session_name) → project_id` as a hint. A successful UI rename updates this link before inventory publishes its refreshed session list; ended sessions have links removed when a full `sessions.changed` snapshot no longer includes them. Removing a project cascades its session links and recent commands, publishes `projects.changed` with action `deleted`, and clients immediately re-resolve those live sessions against the remaining longest path matches (or Other sessions); the tmux sessions and attached terminals are unaffected. The placement service is event-driven and adds no polling loop.

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

**Keys:** Mac "natural text editing" shortcuts (`web/src/lib/terminalKeys.ts`) send what a macOS terminal sends: Option+Backspace `ESC DEL`, Cmd+Backspace `Ctrl-U`, Option+←/→ `ESC b`/`ESC f`, Cmd+←/→ `Ctrl-A`/`Ctrl-E`. Copy is Ctrl+Shift+C / Cmd+Shift+C (and Cmd+C with a selection); paste is Ctrl+Shift+V / Cmd+Shift+V / Cmd+V. Ctrl+C and Ctrl+V stay the program's. M6 global chords are dispatched from the window capture phase and xterm's custom key handler returns `false` for them, so they never reach a terminal program: ⌘K / Ctrl+Shift+K for the palette, ⌘/ / Ctrl+Shift+/ for shortcuts, ⌘⇧E / Ctrl+Shift+E for tree ↔ terminal focus, Ctrl+Shift+] / [ for tab cycling and Ctrl+Shift+D for the last selected tab. The registry (`web/src/lib/shortcuts.ts`) is shared with the help dialog and palette. Outside-terminal shortcuts ignore terminal and text-entry focus; tree shortcuts require a focused tree row. Other keys are xterm's.

**Clipboard** (`web/src/lib/clipboard.ts`): copy writes the selection with `navigator.clipboard.writeText` and keeps it; a refusal shows a toast. The paste keys keep the browser's default, so its `paste` event reaches xterm, which wraps the text in `ESC[200~ … ESC[201~` when the program enabled bracketed paste (no clipboard-read permission needed); the context menu's Paste uses `readText()` + `term.paste()`. The context menu (Copy, Paste, Select all) opens on right-click or a long press, and with Shift/Option+right-click when the program captures the mouse (a plain right-click then belongs to the program, e.g. tmux's menu). Shift+drag (Option+drag on macOS, `macOptionClickForcesSelection`) selects when the mouse is captured. The clipboard API needs a secure context: `http://localhost` and the HTTPS domain qualify, a plain-HTTP LAN address doesn't.

**OSC 52** (`@xterm/addon-clipboard` with a custom provider) is **write-only**: writes (any selection parameter; tmux sends an empty one) go to the browser clipboard, without awaiting, so a refused write never stalls the terminal; payloads over 1 MiB decoded, empty or undecodable ones are ignored. Queries (`52;c;?`) are swallowed by a handler registered after the addon, so nothing answers them and no program on the host can read the browser clipboard. tmux's default `set-clipboard external` forwards copy-mode yanks as OSC 52 (`xterm*` has the `clipboard` feature); programs inside tmux (vim, Claude Code) need `set -g set-clipboard on` in the user's `~/.tmux.conf`, which hostbud documents and never changes.

**Links** (`web/src/lib/links.ts`): printed URLs (`web-links` addon) and OSC 8 hyperlinks (xterm's `linkHandler`, e.g. `ls --hyperlink`, Claude Code) open on click or tap, only for `http:`/`https:`, in a new tab with `noopener,noreferrer`; anything else (`javascript:`, `file:`, `data:`, `ssh:` …) is ignored. An OSC 8 link's text can differ from its target, so hovering shows the target in a tooltip. tmux forwards OSC 8 only to terminals with its `hyperlinks` feature, which isn't in its defaults: users add `set -as terminal-features ',xterm*:hyperlinks'` (documented; hostbud never changes tmux config).

**Scrollback under tmux** (`web/src/lib/scrollback.ts`): tmux draws in the alternate screen, where xterm keeps no scrollback, so the browser would have nothing to search or scroll back through. xterm therefore ignores the alternate-screen switches (DECSET/DECRST 1049, 1047, 47): tmux draws on the normal screen, and lines scrolled off the top of its pane region go to xterm's scrollback (5000 lines). tmux scrolls bursts with `CSI n S`, which xterm would drop; a handler saves those lines too when the region starts at the top row. That handler uses xterm 6 internals (no public API scrolls into the scrollback) and falls back to xterm's default if they change; `scrollback.spec.ts` pins it against the real xterm. Lines tmux never sends (what scrolls past between two of its screen updates) aren't recovered; tmux copy mode has them. Side effects: with tmux `mouse off`, the wheel scrolls xterm's scrollback instead of sending arrow keys, and full-screen programs that scroll a top-anchored region (e.g. vim's Ctrl-E) add their lines to it.

**Search** (`TerminalSearch.vue`, `@xterm/addon-search`): Ctrl+Shift+F, Cmd+F, Cmd+Shift+F or the 🔍 button (plain Ctrl+F stays readline's). It searches xterm's buffer: the screen plus the scrollback received since attaching (kept across auto-reconnects). tmux history from before the attach is copy mode's job (`prefix [`, then `?`). Match case and Regex options, a match count, all matches highlighted and the current one emphasized, Enter/Shift+Enter for next/previous, Escape closes and refocuses the terminal; an invalid regex shows "Invalid pattern". The addon only re-highlights for a new term, so an option change clears its cache first.

**Frontend terminal:** `@xterm/xterm` + addons `fit`, `webgl` (fallback to canvas/DOM), `web-links`, `unicode11`, `search`, `clipboard`. Font: a bundled Nerd-Font-compatible monospace (self-hosted, no external CDN).

---

## 7. File browser (`fsbrowse`)

- SFTP uses `github.com/pkg/sftp` over a pipe to the system command `ssh -F /data/ssh/config hostbud-host -s sftp`; it reuses the generated SSH config, host-key pin and ControlMaster. Filesystem paths are sent as SFTP protocol paths and never enter a remote shell command.
- The browser keeps one lazy SFTP client for the active host, closes it after one minute idle, and bounds each operation to ten seconds. Request cancellation interrupts SFTP startup or closes its SSH subsystem stream to stop an in-flight operation. App shutdown also closes the client and child process.
- Paths use target POSIX rules: empty, `~`, and relative paths resolve from SFTP home; all results are cleaned absolute paths. Paths are limited to 4096 bytes. Listings are limited to 2000 entries and sort directories before other entries, then names in stable byte order; dot entries are omitted unless `hidden=true`.
- Listing uses `Lstat` metadata and does not follow symlinks. Explicit `stat` reports whether a symlink target resolved, is broken, loops, or is unreadable. Creating a folder accepts one child name (1–255 bytes), rejects empty, dot, dot-dot, slash and NUL names, and has no matching delete or remote-rename operation.
- API responses: home is `{path}`; a listing is `{path, entries}` (`name`, absolute `path`, `kind`, `size`, UTC `modifiedAt`, and optional `symlinkState`); stat returns one entry plus `symlink`; mkdir returns `{path}` with 201. Filesystem errors use the standard `{error,hint}` shape and do not include target paths in info logs.
- API routes are authenticated: `GET /api/machines/:id/fs/home`, `GET /api/machines/:id/fs?path=&hidden=`, `GET /api/machines/:id/fs/stat?path=`, and Origin-checked `POST /api/machines/:id/fs/mkdir` with `{path,name}`. Unknown machine IDs return 404; unsupported methods and delete/rename routes do not mutate the target.
- UI: Browse files opens in a modal dialog from the sidebar's icon-only **Add project** (`FolderPlus`) action; it remains a modal outside the sidebar. The sidebar toolbar also has an icon-only **New session** (`SquareTerminal`) action. The header's **Hide sidebar** / **Show sidebar** icon toggles the desktop sidebar and remembers its state in browser storage; on compact screens it opens the drawer, whose **Hide sidebar** button, swipe and Escape close it. The aside is labelled **Sessions**; the drawer keeps an accessible, visually hidden **Project tree** title. The toolbar is shared by the desktop sidebar, compact tree screen and drawer. The browser provides a breadcrumb, path input with autocomplete, keyboard navigation, favorites = projects, and recents per machine. The path bar has an icon-only FolderPlus action to add the currently shown directory, changing to FolderOpen when that path is already a project; it uses the same project selection/create logic as child-directory actions and is disabled while the listing is loading or failed. Directory project actions are icon-only FolderPlus (add) and FolderOpen (existing project) buttons with accessible labels and tooltips. Other actions are **Create folder** and **New session here**.

---

## 8. Persistence (`store`)

- PostgreSQL via a pinned official image, with the application connecting over the private Compose network. Database name, user, host, port and password come from `HOSTBUD_DB_*` environment variables. The password is never committed, logged or placed in an image.
- Migrations: embedded SQL files (`internal/store/migrations/`) run at startup with `pressly/goose`; append-only. Queries via `sqlc` or a hand-written repository interface — **no SQL outside the store package**.
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
-- later (multi-machine): created by a future migration
custom_connections(machine_id PK/FK, hostname, user, port, proxy_jump, identity_agent, extra_options_json)
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
```

Project paths are absolute POSIX paths, cleaned at the repository boundary and
limited to 4096 bytes; project names are trimmed and limited to 255 bytes.
Projects are unique by `(machine_id, path)`, and creating a duplicate returns
the existing row. Session links use `(machine_id, session_name)` as their key
and a composite foreign key to a project on the same machine. A session rename
updates that key in one statement; ending a session removes its link. Recent
commands preserve the exact command text, reject blank/NUL/oversize values,
and keep the 20 newest distinct strings per project. The store prunes older
entries in the same transaction as each upsert.

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
GET    /api/machines/:id/sessions/:name/windows
POST   /api/machines/:id/sessions/:name/select  {window, pane?}
GET    /api/machines/:id/fs?path=         list dir
POST   /api/machines/:id/fs/mkdir
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

# later (multi-machine)
POST   /api/machines                      add custom connection
PATCH  /api/machines/:id                  label, sort, hidden, connection fields
DELETE /api/machines/:id                  custom only
POST   /api/machines/:id/activate | /deactivate
POST   /api/machines/refresh              re-scan ~/.ssh/config
GET    /api/machines/:id/hostkey          keyscan fingerprints
POST   /api/machines/:id/hostkey/trust
```
The copy-mode endpoint body is `{action, lines?}` and its response is `{inMode, scrollPosition, historySize}`. `lines` is valid only for `scroll-up`/`scroll-down`, from 1 to 500. Unknown actions, names and values are rejected before SSH execution; an old tmux version returns 409 with an upgrade hint.
The windows endpoint returns `{windows, truncated}` with windows and panes in index order; it does not include pane paths. `select` returns the refreshed listing after changing the active window and optional pane. These routes read tmux state on demand and publish no hostbud event: tmux owns the window layout and all attached clients observe selection directly.
Session records in the list endpoint and initial/live events WebSocket payload include `projectId` when the placement service matches an explicit session link or a project path. The browser uses that placement before applying the same-machine longest path-component match as a fallback. A successful project session start records its non-empty command; opening the picker or selecting a suggestion never starts a command by itself. Recent commands preserve their exact text, reject blank/NUL/oversize values, and keep the 20 newest distinct strings per project. Reusing a command moves it to the front; there is no manual clear action, and older values are pruned during an upsert.
`/api/ui-state/:key` accepts only allowlisted keys (`layout`; M4 adds `tree` for per-account left-bar session/group order; M6 adds `theme`; others 404). A PUT body must be valid JSON (400) of at most 64 KiB (413). The server stores it without interpreting it and publishes no event (it's a per-account preference); the client validates what it reads back.

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
- **Tailwind CSS + Reka UI** (headless, accessible primitives) for a dense IDE-like UI in Dark / Light / System modes. The resolved theme is set on `html[data-theme]`; token values in `main.css` are the only component surface colors. `stores/theme.ts` keeps the per-account `{version: 1, mode}` value in `ui_state`, follows `prefers-color-scheme` changes only in System mode, and updates mounted xterm palettes and search decorations without reattaching terminals. A sub-1 KiB inline script in `index.html` applies the browser's last theme mirror before stylesheets to avoid a first-paint flash; M7 CSP must allow it by hash.
- Layout: resizable left gutter (tree) | main area with **tabs**, each tab may be **split** (horizontal/vertical, via `splitpanes`). Tab layout is persisted in `ui_state`; desktop sidebar visibility is a browser-storage preference.
- **Tabs (M3, `lib/layout.ts`, `stores/layout.ts`):** the layout is `{version: 1, tabs: [{id, root, focusedPane}], activeTab}` where `root` is a pane `{type: 'pane', id, machine, session}` (splits: M3 T8); IDs come from `crypto.randomUUID`. The focused pane of the active tab is "the selected session". Picking a session in the list focuses the tab that shows it, or opens a new tab at the end; **New session** opens its session in a new tab. At most 16 terminals are open in total; a 17th is refused with a toast. Closing a tab (×, middle click, or Delete on the focused tab) only detaches its views; no confirmation. Inactive tabs stay mounted (`v-show`) and attached; a hidden terminal doesn't refit (it keeps tmux at its last size) and refits when shown. Keyboard input goes to the active tab's focused terminal only.
  - **Sessions that change:** a rename from the UI relabels every pane showing the session, without re-attaching (the list may show the new name before the request returns, so the store remembers renames in flight). A session missing from a fresh list closes its panes with one toast ("Session <name> ended"). Only a list from a reachable host counts: right after an app restart the first snapshot can come before the first poll with an empty list, and an unreachable host's list is stale. A rename done in a real terminal therefore looks like an end.
  - **Persistence:** the layout is loaded from `GET /api/ui-state/layout` on sign-in, before any terminal mounts and before `/ws/events` starts; it's validated (shape, `version`, the 16-terminal limit: extra tabs are dropped, a missing focus or active tab is repaired) and invalid data falls back to an empty layout with a console warning. Every change is saved with `PUT`, debounced 500 ms. Sign-out forgets it without saving.
- **Compact screens (M5):** `(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)` matches phones in portrait and landscape without matching tablets. The project tree fills the home screen when no terminal is open. With terminals open, the header's **Show sidebar** icon opens a labelled modal drawer over the active terminal; it traps focus, closes on Escape, backdrop, **Hide sidebar**, left swipe or session/action selection, and returns focus to its trigger. The terminal stays mounted and attached at the same size. Compact layout shows the active tab's focused pane and puts its tab switcher in the terminal header. New session, Rename, Kill and file-browser dialogs become full-width bottom sheets. The header Account menu contains the per-account Theme radio group, email and Sign out. No edge-swipe-to-open gesture is used.
- **Sidebar controls (M6 T13):** the header has one icon toggle for the desktop sidebar or compact drawer, with the state exposed through `aria-expanded` and `aria-controls`; desktop visibility survives reload in browser storage. The sidebar toolbar has icon-only **New session** and **Add project** actions. The aside is named **Sessions**, and the drawer's accessible **Project tree** title is visually hidden. These controls are shared by desktop, compact home and drawer tree panels.
- **Touch and readability (M5):** a shared `touch-target` utility provides 44×44 CSS px hit areas under a coarse pointer, and global coarse-pointer sizing covers native and Reka UI controls. Text entry controls use 16px type to prevent iOS focus zoom; the viewport allows user scaling. The app root uses `touch-action: manipulation` and long-pressing a session row opens its existing action menu after 500 ms (movement over 10 px cancels it).
- **Splits (M3 T8):** a tab's `root` may be a split `{type: 'split', id, dir: 'row' | 'column', sizes, children}` (row = side by side; `sizes` are percents summing to 100). At most 4 panes per tab (and 16 in total). **Split right/down** in the focused pane's header opens a session picker (live sessions + **New session…**, whose session then lands in the new pane); a list row's ⋯ menu has **Open in split right/down** (beside the active tab's focused pane, or a new tab when none is open). Inside a split of the same direction the new pane is a sibling and the space is shared equally; the other direction nests a new split. The new pane gets focus. **Close pane** (× in its header) detaches that view and gives its space back to its siblings in proportion; a split left with one child collapses to it, a child split of the same direction merges into its parent, and the last pane closes the tab. The same rules remove panes of ended sessions. Clicking into a pane focuses it (an accent outline when the tab has several); keyboard input goes to the focused pane only.
  - Rendering: `splitpanes` renders the tree recursively (`LayoutNodeView.vue`), minimum pane size 10 %. Dragging a divider resizes live (each pane's ResizeObserver refits it and resizes its tmux window); the sizes are saved when the drag ends. Stored splits are validated (≥ 2 children, a known direction, ≤ 4 panes per tab; bad sizes become equal shares).
  - Compact screens: a split tab shows only its focused pane, full size, with a "Pane n of m" header button that cycles focus; the layout itself isn't changed, so a wide screen shows the split again. The query remains compact across phone rotation, so terminals do not re-mount or re-attach.
- Tree: projects → sessions → lazily loaded windows and panes; a machine level appears only once multiple machines exist. Status dots (● attached/active, ○ detached; a header banner for host unreachable / tmux missing). Projects and sessions have explicit user-controlled ordering persisted per account; newly observed rows append without re-sorting existing rows, and there is no automatic alphabetical/activity/recency sort. The account's `tree` UI-state value is version 2: `{version: 2, projects: [projectId, ...], sessions: {projectId: [sessionName, ...], __other__: [sessionName, ...]}, pinned: [projectId, ...], hidden: {projects: [...], sessions: [machineId/sessionName, ...]}, collapsed: [projectId | '__other__', ...], expanded: [machineId/sessionName[/@windowId], ...], showHidden: boolean}`. Version 1 order is upgraded on load without changing its order; malformed/future versions are ignored. Session-keyed values are pruned only after that machine is reachable and returns an authoritative session list; project-keyed values are pruned only after the project list loads. Window keys are pruned when an expanded session's window list arrives. Saves are debounced 500 ms and flushed on `pagehide`; serialized values over 60 KiB are skipped with a console warning. No global project pin/sort or hidden-machine columns are changed. The left bar is a WAI-ARIA tree labelled “Projects and sessions” with one roving tab stop, project/Other headers at level 1, sessions at level 2, and grouped children. Headers show a folder or list icon, semibold name, tinted background, and indented children with a vertical guide. Project paths appear below the name, abbreviated with `~` for the host home directory and retained in the full-path title. Collapsing a group preserves order and never detaches or resizes its terminals; hidden project and session ids are per-account presentation state and never affect tmux, open tabs, split pickers or the session API. A hidden project hides its whole group, including future sessions placed there. Show hidden temporarily renders hidden rows in their saved positions, dimmed and labelled hidden; Unhide restores ordinary visibility without moving the row. The Show hidden toggle persists with the tree state, and when no visible rows remain the tree says “Everything is hidden.” An empty Other group is hidden while its saved state is retained. Projects and sessions rename inline from their pencil, F2, a fine-pointer double-click, or the row's more-actions menu. The project menu, long-press menu, command palette and Delete on a focused project row offer **Remove project…**, which confirms its session count and destinations and explains that files are untouched and removal applies to every account. Removing deletes only shared project metadata and cascading links/history; sessions and open tabs stay active, and live placement falls back immediately to the next-longest project path or Other sessions. The selected name is edited in a 16 px input; Enter or blur saves, Escape cancels, and empty or unchanged values cancel. Errors stay beside the input. A successful session rename re-keys its saved order, hidden state, expansion and expanded-window keys together, preserving the row's position and presentation state; open panes relabel through the M3 rename lifecycle without reattaching. Project names are trimmed and limited to 255 UTF-8 bytes. Session rows retain the kill × and more-actions ⋯; kill remains confirmation-gated. Session rows do not show tmux window counts. The desktop and compact header's Account menu contains the Theme radio group, signed-in email and Sign out, and is reachable without opening the tree drawer.
- **Pinned projects (M6 T6):** pinned projects appear in a Pinned section above unpinned projects, each retaining its manual order. Pin and Unpin move a project to the end of the target section; drag and Alt+↑/↓ reorder only within a section. The accessible Pinned icon unpins, and the project menu and `P` tree shortcut toggle pin state. Pin state stays in the account's `tree` UI-state value; the global `projects.pinned` column is reserved and never changed.
- **Window and pane rows (M6 T3):** expanding a session lazily loads its window list; a multi-pane window can expand into pane rows. Only expanded sessions make requests. The rows show the target's window/pane indices, names/commands and active state; a loading row, actionable error with Retry, and truncation row cover the response states. Expanded session/window keys live in `tree.expanded`, restore after reload, and are pruned against a reachable host's returned list. While expanded, `sessions.changed` events that alter that session's window count or activity schedule a 300 ms refresh with at most one request in flight and one queued refresh; no window polling loop runs. Selecting a row opens/focuses the session through the layout store, then calls the select route for its window and optional pane. A vanished row shows a toast and refreshes the list. In compact mode selection closes the drawer and shows the terminal; tmux switches the shared current window for attached clients.
- **Keyboard shortcuts (M6 T8):** `web/src/lib/shortcuts.ts` is the single registry for shortcut ids, labels, scopes and platform bindings; the global dispatcher, help dialog and command palette hints use it. Global chords use ⌘ or Ctrl+Shift and are captured before xterm. Outside-terminal chords do not fire from the terminal or editable controls. Tree navigation and actions run only from a focused tree row, including `N` to start a session in the row's project or use the default path for Other. The help dialog groups every shortcut under General, Tabs and Tree and restores focus on Escape. Tab switching keeps a per-page most-recently-selected list in memory; restored tabs do not seed it, and closed tabs are removed.
- **Command palette (M6):** ⌘K on Mac or Ctrl+Shift+K globally; Ctrl+K only outside the terminal so shell readline keeps its kill-line binding. Touch and compact layouts expose a header button. The modal combobox searches live sessions in tree order, only already-loaded windows, projects and registered app actions. Fuzzy matching is case-insensitive subsequence matching with word-start/contiguous bonuses and stable source-order ties; it caps results at 50 and marks hidden sessions. Actions use existing app/store handlers; Kill opens the normal confirmation dialog. Selecting a session/window focuses its terminal; selecting a project reveals and focuses its row; Escape restores the prior focus. Compact terminal selection closes the palette and tree drawer.
- **Theme boot (M6 T7):** the pre-style inline script reads only the browser's theme mirror to prevent first-paint flashes on the signed-out screen. After sign-in, the account's `ui_state.theme` is authoritative and updates that mirror.
- **Phones (M2):** the app is pinned to the visual viewport (`--app-height`, `lib/appHeight.ts`), so the on-screen keyboard shrinks the terminal instead of covering it; the terminal's hidden input uses a 16px font (no iOS zoom on focus) with autocorrect/autocapitalize off; a ⌨ button on touch screens refocuses it.
- **Mobile controls (M5):** the focused terminal shows an on-screen key bar on coarse-pointer devices with Esc, Tab, sticky Ctrl/Alt, arrows, `|`, `~`, `/`, `-` and Scroll history. Keys enter through xterm's `input` path; arrows respect DECCKM, Ctrl/Alt modify the next key or soft-keyboard character, and double-tap locks a modifier. Arrow keys repeat after 400 ms and every 80 ms. The bar can collapse for the current page load and scrolls inside itself; hardware-keyboard behavior is unchanged.
- **Scroll history (M5):** Scroll history enters tmux copy mode through the authenticated `copy-mode` API and replaces the key bar with Top, Page up, Line up/down, Page down, Bottom and Done controls plus the reported history position. Touch swipes become coalesced, timeout-bounded tmux scroll requests (≤ 500 lines each, at most 20/s); xterm keeps normal touch scrolling outside copy mode. The UI follows each tmux response, clears mode on pane detach, and exits before forwarding new typed input.
- **Installable app (PWA, M5):** `manifest.webmanifest` (`display: standalone`, theme colors, bundled icons incl. `apple-touch-icon`; no external assets) and a small hand-written service worker served from `/` with `Cache-Control: no-cache`. It precaches the app shell (hashed assets, manifest, icons, favicon) into a versioned `hostbud-shell-*` cache and serves navigation and precached assets cache-first. `/api/*`, `/ws/*` and anything else pass straight to the network, uncached, so auth and live data never come from a cache. It does not use `skipWaiting()` or `clients.claim()`; a new worker waits and takes over on the next launch, and activation deletes only stale hostbud shell caches. It registers only in production in a secure context (the HTTPS domain, or `localhost`); failure is debug-only. On iOS an installed app has its own cookie store, separate from Safari's, so it asks to sign in once. Standalone mode pads the layout with `env(safe-area-inset-*)`.
- **Startup reachability (M5):** the initial `/api/auth/me` check distinguishes 401 (sign-in), network/timeout and 502/503/504 (the **Can't reach hostbud** view with retry), and other server errors (an explicit server-error message). The unreachable view retries with 1/2/4/8/15-second backoff, plus immediate retries on `online` and page visibility. Sign-in network failures show a connection message and are not counted by the server.
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

M7 also configures the exec and SFTP deadlines and terminal attachment caps:
`HOSTBUD_EXEC_TIMEOUT` (10 s, 2 s–2 min), `HOSTBUD_SFTP_TIMEOUT` (10 s,
2 s–2 min), `HOSTBUD_MAX_TERMINALS_PER_USER` (32, 1–256), and
`HOSTBUD_MAX_TERMINALS` (128, 1–1024). See §15 for the full inventory.

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
| `hostbud-e2e-ctl` | Failure switches for the runner, which has no Docker access: a tiny HTTP service with the Docker socket that runs only fixed commands (restart or stop/start `hostbud-e2e-app`, stop/start sshd on the target, disconnect/reconnect `hostbud-e2e-app` from the `hostbud-e2e` network). App start polls `/api/health` through Caddy. |
| `hostbud-e2e-runner` | Playwright. Uses `network_mode: service:hostbud-e2e-caddy`, so the browser opens `http://localhost:9055` exactly like the port-forward path (and the Origin check is exercised for real). Also has SSH access to the target to act as "a real terminal". |

**Profiles:** Chromium desktop, and Playwright's `iPhone 13 Pro` device (WebKit, 390×844, touch), both on `http://localhost:9055`; plus `iphone-13-pro-domain`, the same device on `https://hostbud.example.test` (the phone and domain scenarios). WebKit on Linux is not real iOS Safari; iOS-specific behavior (on-screen keyboard, gestures) stays on the manual checklist.

**How tests simulate a user**
- Service workers are blocked in every Playwright project by default; only `pwa.spec.ts` opts in. This avoids caches affecting non-PWA scenarios.
- Drive the UI only through what a user sees: roles, labels, visible text; `data-testid` only where there is no accessible handle (e.g. the terminal container).
- Out-of-band actions like a user's real terminal: the runner runs `tmux` on the target over SSH (create/kill/attach elsewhere) and asserts the UI follows within one poll interval.
- `window.__hostbud` exists only in images built with `VITE_E2E=1`. Every mounted terminal registers under its pane id; `termText(session?)` and the other terminal hooks answer for the named session, or by default for the focused pane of the active tab, and `panes()` lists `{session, active, focused}`. The e2e `page` fixture resets the account's saved layout to empty before each test, so tabs never leak between scenarios: every use is guarded by the statically replaced `import.meta.env.VITE_E2E === '1'`, and `web/scripts/check-dist.mjs` fails any other build whose output still mentions it.
- Terminal content is asserted two ways: what tmux really shows (`tmux capture-pane -p` on the target, the ground truth) and what the browser shows (xterm buffer read through `window.__hostbud.termText()`, exposed only in builds with `VITE_E2E=1`). The same hook's `termTheme(session?)` reports the mounted terminal's background and foreground in E2E builds.
- M6 state-persistence scenarios create their own account with the test helper's `newAccount()` and whitelist it, so per-account tree and theme preferences cannot leak between scenarios. `ui.waitForSave(key)` waits for the debounced `PUT /api/ui-state/{key}` before reloads; one tree-state scenario intentionally reloads immediately to cover the `pagehide` flush.
- Failure scenarios: restart `hostbud-e2e-app` (UI and terminals recover), stop sshd on the target (unreachable banner, then recovery), tmux-less target (install hint), cut and restore the app's network (terminals re-attach by themselves). The throwaway target's sshd sets `ClientAliveInterval 5` so a client that vanished in a cut is dropped along with its stale tmux client.
- No real TUIs that need credentials (Claude Code, Codex); vim and htop cover full-screen apps. Claude Code stays a manual check.
- Traces, screenshots and videos on failure → `test/e2e/results/` (gitignored).

**Driver:** `test/e2e/run.sh` behind `make e2e` (fresh stack → run → `down -v`, even on failure), plus a persistent loop for development: `make e2e-up` / `e2e-run` / `e2e-down`. The runner idles (`sleep infinity`) and each run is a `docker exec`; the specs are bind-mounted, and images rebuild only when a content hash of their inputs changes (stamped in `.cache/e2e/`). The target's per-run keys come from a one-shot `hostbud-e2e-keygen` service; the app sees only the public host keys (`hostbud-e2e-hostpub` volume) and the agent socket.

**When it runs:** not part of `make test`. **Paused until the end of M7:** while milestones M4–M7 are built, scenarios are written but not run (commits only type-check the suite). The full suite runs once in M7's last task, where failures are fixed and it must pass twice in a row. After that, it's required again before every commit that changes behavior e2e can reach (UI, or HTTP/WebSocket API through Caddy), and for every milestone's definition of done.

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

## 15. Limits and timeouts

Every bound is recorded here with its enforcement point and the result when it is reached. T1 records current behavior and gaps; M7 T2–T5 fill the marked gaps and update this table in the same task commit.

| What | Limit | Enforced in | On hit (server) | On hit (user sees) | Configurable |
|---|---|---|---|---|---|
| Non-interactive SSH exec | 10 s default; `WaitDelay` 1 s | `sshx.Client.Exec` | Context cancellation terminates ssh; timeout is currently a generic SSH timeout (T2 adds value-aware 504 mapping and ControlMaster recovery) | Existing actionable host error; T2 adds a duration-bearing message | Yes: `HOSTBUD_EXEC_TIMEOUT`, 2 s–2 min |
| SSH agent probe | 3 s | `sshx.checkAgent` | Probe context expires and reports agent unavailable | SSH auth hint | No |
| ControlMaster shutdown | 5 s in the server shutdown path | `cmd/hostbud` passes a deadline to `sshx.Client.Close` | Stops waiting when shutdown context expires | None during normal shutdown | No |
| ControlMaster recovery | Not implemented (T2) | `sshx` | T2 bounds checks and self-heals after two exec timeouts | T2 keeps later operations from hanging | No |
| Inventory polling | `HOSTBUD_POLL_INTERVAL` default 3 s, minimum 500 ms; exponential failure backoff default 8× interval, capped at 30 s and never below interval | `inventory.Run` | Poller waits for retry, marks host unreachable, recovers on a successful poll | Machine status banner | Interval: yes; backoff: no |
| SFTP operation | 10 s default; subsystem idle close 1 min | `fsbrowse.Service` | Per-operation context closes a failed SFTP stream; T3 adds concurrency limit and explicit timeout response | File-browser error; T3 adds Retry state | Yes: `HOSTBUD_SFTP_TIMEOUT`, 2 s–2 min |
| SFTP path/listing | Path 4096 bytes; 2000 entries; name 255 bytes | `fsbrowse` validation and listing | Rejects long paths/names; listing currently rejects over 2000 entries (T3 changes to bounded truncation) | Actionable validation or listing error | No |
| Terminal WebSocket input | 1 MiB per message | `term.Handler` read limit | Oversized frame closes the connection | Terminal reconnects after a dropped socket | No |
| Terminal WebSocket output | Queue 64 × 32 KiB; write timeout 10 s | `term.Handler` | Full queue cancels ssh attach; write context bounds socket write | Terminal reconnects and tmux redraws | No |
| Terminal WebSocket liveness | Server ping every 25 s, 10 s pong timeout; browser drops after 25 s without a frame; browser ping every 10 s | `term.Handler`, `web/src/api/term.ts` | Unanswered peer ends attach; silent browser connection is replaced | Reconnecting strip and automatic reattach | No |
| Terminal attachments | Client layout allows 16 panes; no server/account cap (T4) | `web/src/lib/layout.ts`; server gap | T4 rejects over-cap upgrades before ssh starts | T4 shows a limit error with manual Retry | Yes: `HOSTBUD_MAX_TERMINALS_PER_USER` default 32 (1–256), `HOSTBUD_MAX_TERMINALS` default 128 (1–1024) |
| Events WebSocket | 64-event subscriber buffer; heartbeat 15 s; ping 25 s; write timeout 10 s; browser silence 40 s | `events.Bus`, `api.eventsSocket`, `web/src/api/live.ts` | Slow subscriber is closed and reconnects for a snapshot | Live state reconnects and receives a fresh snapshot | No |
| HTTP server | Header read 10 s; idle connection 2 min; shutdown 10 s | `cmd/hostbud` | Slow headers are closed; idle connections expire; shutdown is bounded | Browser request fails or reconnects | No |
| JSON request bodies | 64 KiB for session and UI-state handlers; other state-changing handlers do not yet share the decoder (T5) | API handlers | Oversized request gets 413; T5 makes content type, field and header handling uniform | Form/API error | No |
| UI state | 64 KiB per saved value | `api.putUIState` | Oversized value gets 413 | Previous saved value remains | No |
| REST request duration | No common handler deadline (T5) | API middleware gap | T5 adds 30 s request deadline | Actionable 503/504 instead of a hang | No |
| Database pool/query | No explicit pool or query bounds (T5) | `store` | T5 sets pool limits and PostgreSQL timeouts | T5 maps outage/timeout to 503 and health to degraded | No |
| Authentication | Session TTL 720 h; failures: 5 login, 10 registration, 20 per IP; blocks 30 s ×2 up to 1 h; failure window 1 h | `config`, `auth`, PostgreSQL rate-limit store | Rejects with 429 and `Retry-After` | Sign-in throttle message | Yes: existing `HOSTBUD_*` auth settings |
| Browser layout | 16 terminals globally; 4 panes per tab; layout save debounce 500 ms; reconnect 0.5/1/2/4/8 s then 10 s cap; events backoff up to 10 s | `web/src/lib/layout.ts`, `web/src/stores/layout.ts`, `web/src/api/*` | Client rejects invalid/over-limit layout or retries network | Layout limit notice and reconnect indicator | No |

**Static architecture guard:** `internal/archtest` rejects uncontextualized `exec.Command`, HTTP helpers/default clients or `http.Client` values without a timeout, and WebSocket accepts without a read limit. It also enforces the documented `context.Background()` allowlist with a reason and occurrence count.
