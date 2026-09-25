# M1 — tmux manager in the browser: tasks

Goal: from another machine, `ssh -L 9055:localhost:9055 <host>` → `http://localhost:9055` lists the host's tmux sessions, lets you create/rename/kill them, and attach in a full terminal.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m1--tmux-manager-in-the-browser-local-access) · checklist: [M1-acceptance.md](M1-acceptance.md).

Work top to bottom; each task ends with a green `make lint test`, a clean `make gitleaks`, and its own conventional commit(s). Tasks marked *(host)* need the real host (agent socket, sshd) to verify.

---

## A. Repo and toolchain

### T1 — Go skeleton
- `go.mod`, `cmd/hostbud/main.go`.
- `internal/config`: load env with defaults (`HOSTBUD_LISTEN`, `HOSTBUD_DATA_DIR`, `HOSTBUD_POLL_INTERVAL`, `HOSTBUD_LOG_LEVEL`, `HOSTBUD_LOCAL_PORT`, `HOSTBUD_DOMAIN`, `HOST_SSH_USER`, `HOSTBUD_HOST_LABEL`); clear error for invalid values.
- `log/slog` JSON/text handler by level.
- HTTP server with `/api/health`, graceful shutdown on SIGTERM.
- Unit tests for config parsing.

**Done:** `make test` passes; binary serves `/api/health`.

### T2 — Dockerized toolchain
- `Makefile` targets running in containers: `build`, `test`, `lint`, `fmt`, `gitleaks`, `deploy`, `logs`, `backup`, `hooks`.
- Pinned tool images (golang, golangci-lint, node/pnpm, gitleaks); Go module + pnpm caches in named volumes.
- `.golangci.yml`.
- `.githooks/pre-commit` running gitleaks via Docker on staged changes; `make hooks` sets `core.hooksPath` (repo-local).

**Done:** on a host with only Docker, `make lint test gitleaks` works; a commit with a fake secret is blocked.

### T3 — Frontend scaffold
- `web/`: Vue 3 + Vite + TS, Pinia, Tailwind, Reka UI; pnpm.
- `eslint`, `vue-tsc`, Vitest wired into `make lint` / `make test`.
- Build to `web/dist`; embedded with `go:embed`; SPA fallback to `index.html`; placeholder page when `dist` is empty so Go builds without the frontend.
- Dark theme base via CSS variables; bundled monospace font (no CDN).

**Done:** `make build` produces a binary that serves the empty app shell.

### T4 — Container and compose
- Multi-stage `Dockerfile` (node → golang `CGO_ENABLED=0` → `debian:stable-slim` + `openssh-client`, `ca-certificates`, `tini`), linux/amd64.
- `docker-compose.yml`:
  - `hostbud`: no published ports; `user: ${HOST_UID}:${HOST_GID}`; `hostbud-data:/data`; agent socket `${HOST_SSH_AUTH_SOCK}:/run/ssh-agent.sock`; `/etc/ssh/ssh_host_*_key.pub` → `/run/host-keys/` (ro); `extra_hosts: host.docker.internal:host-gateway`.
  - `caddy`: stock `caddy:2` image for now; publishes `127.0.0.1:${HOSTBUD_LOCAL_PORT}` only; `deploy/caddy/Caddyfile` with the `http://:{$HOSTBUD_LOCAL_PORT}` site → `hostbud:8080`.
- `make deploy` = `docker compose up -d --build`; `make logs`.

**Done:** *(host)* `make deploy` → `curl http://localhost:9055/api/health` returns ok; `ss -ltn` shows 9055 on `127.0.0.1` only.

## B. Persistence

### T5 — Store
- `internal/store`: `modernc.org/sqlite`, WAL, `busy_timeout`, DB at `${HOSTBUD_DATA_DIR}/hostbud.db`.
- Embedded migrations (goose); `0001`: `machines`, `ui_state`; built-in `host` machine row seeded idempotently.
- Repository interface; no SQL outside `store`.
- `make backup` (`VACUUM INTO` → `./backups/`, gitignored).

**Done:** unit tests against a temp DB; restart keeps data; migration re-run is a no-op.

## C. SSH and tmux backend

### T6 — `sshx`
- Single-quote shell-escaping helper; command builder `ssh -F /data/ssh/config <alias> -- <quoted args>` — the only way to run remote commands.
- Generated `/data/ssh/config` (host entry `hb-host` with `HostKeyAlias`, then `Host *` defaults: ControlMaster, `ControlPath /data/ssh/cm/%C`, `StrictHostKeyChecking yes`, `BatchMode yes`, `UserKnownHostsFile /data/ssh/known_hosts`). Dirs `0700`.
- Host-key pinning: read `/run/host-keys/*.pub` → write `hb-host <key>` lines; missing keys → startup error with instructions.
- `Exec(ctx, machine, args…)` with default 10s timeout; context cancel kills the process.
- Map common failures to actionable errors: agent socket missing/empty, permission denied (key not in `authorized_keys`), connection refused (sshd), host key mismatch.

**Done:** unit tests for quoting (incl. quotes, spaces, `$`, newlines), config ordering, known_hosts generation, error mapping.

### T7 — Integration test target
- `test/sshd/`: container with `openssh-server` + `tmux`, throwaway key generated at test time (never committed).
- `make test` brings it up (compose profile), runs `-tags=integration` tests, tears it down.
- sshx integration test: exec `echo`, ControlMaster reuse, host-key pin + mismatch rejection.

**Done:** `make test` runs unit + integration from a clean checkout.

### T8 — `tmux` package
- Session-name validation `^[A-Za-z0-9_-]{1,64}$`.
- Builders: `list-sessions -F …`, `new-session -d -s <name> -c <path> [-e K=V…] [cmd]`, `rename-session -t =<old> <new>`, `kill-session -t =<name>` — always `=` exact targets.
- Parser for the tab-separated list format; "no server running" ⇒ empty list.
- `tmux -V` version parsing; `-e` only when ≥ 3.2.

**Done:** unit tests for builders/parser/validation; integration test creates, lists, renames, kills a session on the test sshd.

### T9 — Events bus and inventory
- `internal/events`: typed in-process pub/sub (`machine.status`, `sessions.changed`).
- Capability probe on startup: `uname -s`, `command -v tmux`, `tmux -V`, home dir → stored on the machine row; status `ok | unreachable | tmux_missing`.
- Poller for the host every `HOSTBUD_POLL_INTERVAL`: diff against cache, publish only on change; exponential backoff + `unreachable` on failure; recover automatically. Interface allows a control-mode implementation later.
- `Refresh()` for an immediate re-poll after mutations.

**Done:** unit tests with a fake executor (diffing, backoff, status transitions).

### T10 — Session service
- Single `CreateSession(ctx, {machine, name, path, env, startCommand})`: default name from path basename (`-<n>` suffix on clash); path default = probed home; `~/` expanded against home; validate name; refresh after.
- `RenameSession`, `KillSession` (service assumes UI confirmed); refresh after.
- Actionable errors (duplicate name, path doesn't exist, tmux missing).

**Done:** unit tests with fake executor; integration test for create with start command.

## D. API

### T11 — REST + events WebSocket
- `internal/api` (`coder/websocket`):
  - `GET /api/machines`, `GET /api/machines/:id/sessions`, `POST …/sessions`, `PATCH …/sessions/:name`, `DELETE …/sessions/:name`.
  - `/ws/events`: snapshot on connect, then bus events; ping/keepalive.
- JSON error shape `{error, hint?}`.
- Origin middleware: allowlist `https://${HOSTBUD_DOMAIN}` (if set) + `http://localhost:${HOSTBUD_LOCAL_PORT}`; applied to WebSocket upgrades and non-GET requests.
- No user paths or command strings in info logs.

**Done:** `httptest` tests incl. Origin rejection and validation errors.

### T12 — Terminal bridge
- `internal/term`: `/ws/term?machine=&session=&cols=&rows=` → PTY (`creack/pty`) running `ssh -F … -tt hb-host -- tmux attach-session -t =<name>`, `TERM=xterm-256color`.
- Binary frames for I/O; JSON text control frames: `resize`, `ping`; server → `exit {code}`.
- WS close ⇒ kill ssh process; bounded write buffer, drop stalled clients.
- Origin-checked.

**Done:** integration test: attach, send keys, read output, resize, close ⇒ process gone, tmux session still alive.

## E. Frontend

### T13 — Client and stores
- Typed API client; Pinia `machines` and `sessions` stores updated from `/ws/events` (auto-reconnect with backoff, resync on snapshot). No polling.

**Done:** Vitest for store reducers.

### T14 — App shell and session list
- Left gutter: flat session list (name, attached/detached dot, window count); "Other sessions" grouping comes in M4.
- Banner for host `unreachable` / `tmux_missing` with the actionable hint.
- Selecting a session opens it in the terminal view.

### T15 — Session actions
- Create dialog: name (optional), path (default `~`), start command (optional); inline validation matching the backend regex.
- Rename (dialog or inline); Kill via Reka UI `AlertDialog` confirmation.
- Error toasts showing the backend's actionable message.

### T16 — Terminal view
- `@xterm/xterm` + `fit`, `webgl` (fallback), `web-links`, `unicode11`; bundled font.
- Connect `/ws/term`; `ResizeObserver` → `fit` → `resize` frame; show exit/disconnect state with a "Reconnect" button (auto-reconnect is M3).
- One terminal at a time (tabs/splits are M3).

**Done (E):** *(host)* manual pass of the functional section of [M1-acceptance.md](M1-acceptance.md).

## F. Wrap-up

### T17 — Docs and release
- README: M1 usage (port forward, `make deploy`, required `.env` vars, stable agent socket, own key in `authorized_keys`).
- `.env.example` for any new vars; ARCHITECTURE updated if the design moved.
- Run the full [M1-acceptance.md](M1-acceptance.md) checklist.
- Summary to the owner: what changed, env vars to set, manual host steps.
