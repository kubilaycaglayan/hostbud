# M1 — tmux manager in the browser: tasks

Goal: from another machine, `ssh -L 9055:localhost:9055 <host>` → `http://localhost:9055` lists the host's tmux sessions, lets you create/rename/kill them, and attach in a full terminal.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m1--tmux-manager-in-the-browser-local-access) · checklist: [M1-acceptance.md](M1-acceptance.md).

## Progress
Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Go skeleton | ✅ done |
| T2 Dockerized toolchain | ✅ done |
| T3 Frontend scaffold | ✅ done |
| T4 Container and compose | ✅ done |
| T5 E2E harness | ✅ done |
| T6 Store | ✅ done |
| T7 `sshx` | ✅ done |
| T8 Integration test target | ✅ done |
| T8A PostgreSQL persistence | ✅ done |
| T8B Account authentication | ✅ done |
| T9 `tmux` package | ✅ done (ahead of T8A/T8B, independent of them) |
| T10 Events bus and inventory | ✅ done |
| T11 Session service | ✅ done |
| T12 REST + events WebSocket | ✅ done |
| T13 Terminal bridge | ✅ done |
| T14–T18 | ⬜ todo |

Work top to bottom; each task ends with a green `make lint test` **and `make e2e`**, a clean `make gitleaks`, and its own conventional commit(s). Tasks marked *(host)* need the real host (agent socket, sshd) to verify.

**E2E rule — no deferral.** The e2e harness (T5) comes before any feature work, so every later task is covered from its first commit:
- Every task has an **E2E:** line. It lists the scenarios that task adds (tagged with the same task number in [M1-acceptance.md](M1-acceptance.md#e2e-make-e2e-simulated-user)), or says why nothing user-reachable changed.
- Scenarios land **in the same commit** as the behavior they cover, never in a later task. A task isn't done until they pass in both Playwright projects.
- "User-reachable" includes the HTTP/WebSocket API through Caddy, not only the UI. Backend tasks with an endpoint get API-level scenarios before the UI exists.
- When you add or split a task, give it an E2E line and tag its items in the acceptance checklist.

**Three-layer rule.** Every acceptance criterion in [M1-acceptance.md](M1-acceptance.md#test-coverage-rule) has a coverage line naming its **unit**, **integration** and **e2e** tests and the task that writes each (n/a only with a reason). Each task's **Tests:** line lists the unit and integration tests it owes; its **E2E:** line lists the scenarios. A task is done only when everything the acceptance file assigns to it exists and passes, all in the same commit(s) as the behavior. If you change what a task builds, update the coverage lines too.

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
- `Makefile` targets running in containers: `build`, `test`, `lint`, `fmt`, `tidy`, `gitleaks`, `gitleaks-staged`, `hooks` (`deploy`/`logs` arrive with T4, `backup` with T6).
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
- `docker-compose.yml` (`name: hostbud`; names per the AGENTS.md naming rule):
  - `hostbud`: no published ports; `user: ${HOST_UID}:${HOST_GID}`; `hostbud-data:/data`; agent socket `${HOST_SSH_AUTH_SOCK}:/run/ssh-agent.sock`; `/etc/ssh/ssh_host_*_key.pub` → `/run/host-keys/` (ro); `extra_hosts: host.docker.internal:host-gateway`.
  - `hostbud-caddy`: stock `caddy:2` image for now; publishes `127.0.0.1:${HOSTBUD_LOCAL_PORT}` only; `deploy/caddy/Caddyfile` with the `http://:{$HOSTBUD_LOCAL_PORT}` site → `hostbud:8080`.
- Compose network `hostbud` with a fixed subnet inside `172.16.0.0/12` (the host's `authorized_keys` entry for the hostbud key only allows that range).
- `make deploy` = `docker compose up -d --build`; `make logs`.

**E2E:** none. The harness is built next (T5) on top of this image and Caddyfile.

**Done:** *(host)* `make deploy` → `curl http://localhost:9055/api/health` returns ok; `ss -ltn` shows 9055 on `127.0.0.1` only.

### T5 — E2E harness
Design: [ARCHITECTURE §13.1](../ARCHITECTURE.md#131-e2e-environment). Built now, before any feature, so every later task adds its scenarios as it goes.
- `test/sshd/`: target image (`openssh-server`, `tmux`, `vim`, `htop`, user `dev`). Host keys and the client key are generated per run and never committed. T8 reuses it for integration tests.
- `test/e2e/`: Compose project `hostbud-e2e`, with no published ports and its own volumes, so it never touches the production `hostbud` project:
  - `-target` (from `test/sshd`) and `-agent` (ssh-agent holding the client key);
  - `-app`: the real image, with `HOSTBUD_HOST_ADDR=hostbud-e2e-target`, the target's host keys at `/run/host-keys`, the agent socket, and a short poll interval;
  - `-caddy`: the real Caddyfile;
  - `-runner`: pinned Playwright image, `network_mode: service:hostbud-e2e-caddy`.
- Playwright + TypeScript with projects `desktop-chromium` and `iphone-13-pro` (WebKit). Specs are type-checked and linted by `make lint`.
- Helpers:
  - `target.tmux(...)`: SSH to the target, acting as "a real terminal";
  - `target.capture(session)`: `tmux capture-pane -p`;
  - `ui.*` page helpers, and a fixture that fails a test on console errors or failed requests.
- `make e2e` builds, starts, runs both projects, collects `test/e2e/results/` (gitignored: traces, screenshots, videos on failure) and tears everything down (`down -v`), even on failure.

**Tests:** the harness is the test. `test/sshd` also builds a tmux-less variant (build arg), used by T10's integration test and T15's e2e.

**E2E:**
- **Harness smoke (T5):** `target.tmux` can create, list and kill a session, and `target.capture` reads its pane.
- **Open the app (T5):** the shell loads at `http://localhost:9055` through Caddy in both projects with no console errors; `GET /api/health` returns ok.

**Done:** `make e2e` green twice in a row from a clean checkout. A deliberately broken assertion fails and leaves a trace in `test/e2e/results/`. After a run, no `hostbud-e2e*` containers or volumes remain, and the production `hostbud` containers are untouched.

## B. Persistence

### T6 — Store
- `internal/store`: `modernc.org/sqlite`, WAL, `busy_timeout`, DB at `${HOSTBUD_DATA_DIR}/hostbud.db`.
- Embedded migrations (goose); `0001`: `machines`, `ui_state`; built-in `host` machine row seeded idempotently.
- Repository interface; no SQL outside `store`.
- `make backup` (`VACUUM INTO` → `./backups/`, gitignored).

**Tests:** U: open/migrate/seed on a temp DB; data survives close + reopen; migration re-run is a no-op. I: n/a (local SQLite, no remote side).

**E2E:** nothing user-visible yet. The e2e app now creates its DB on its data volume at startup; `make e2e` must stay green.

**Done:** unit tests against a temp DB; restart keeps data; migration re-run is a no-op.

## C. SSH and tmux backend

### T7 — `sshx`
- `HOSTBUD_HOST_ADDR` config (default `host.docker.internal`) for the host entry's `HostName`; e2e points it at its target container.
- Single-quote shell-escaping helper; command builder `ssh -F /data/ssh/config <alias> -- <quoted args>` — the only way to run remote commands.
- Generated `/data/ssh/config` (host entry `hostbud-host` with `HostKeyAlias`, then `Host *` defaults: ControlMaster, `ControlPath /data/ssh/cm/%C`, `StrictHostKeyChecking yes`, `BatchMode yes`, `UserKnownHostsFile /data/ssh/known_hosts`). Dirs `0700`.
- Host-key pinning: read `/run/host-keys/*.pub` → write `hostbud-host <key>` lines; missing keys → startup error with instructions.
- `Exec(ctx, machine, args…)` with default 10s timeout; context cancel kills the process.
- Map common failures to actionable errors: agent socket missing/empty, permission denied (key not in `authorized_keys`), connection refused (sshd), host key mismatch.

**Tests:** U: quoting (quotes, spaces, `$`, newlines); config generation and ordering; known_hosts built from `*.pub` only; error mapping (refused, permission denied, agent missing/empty, host-key mismatch); architecture test that `os/exec` is imported only by `sshx` and `term`. I: in T8, once the target is wired into `make test`.

**E2E:** no endpoint yet. The e2e app now pins the target's host keys at startup and must still boot healthy (the *Open the app* scenario stays green). Mismatch rejection is an integration test (T8).

**Done:** unit tests for quoting (incl. quotes, spaces, `$`, newlines), config ordering, known_hosts generation, error mapping.

### T8 — Integration test target
- Container `hostbud-test-sshd` from the `test/sshd` image built in T5; throwaway key generated at test time (never committed).
- `make test` brings it up (compose profile), runs `-tags=integration` tests, tears it down.
- sshx integration test: exec `echo`, ControlMaster reuse, host-key pin + mismatch rejection.

**Tests:** I: exec `echo`; ControlMaster reuse; pinned key accepted, wrong key refused with the mapped error; quoting round-trip (`printf %s` with hostile args returns them verbatim); stopped sshd and missing agent socket ⇒ mapped actionable errors; deploy-config check on `docker compose config` (hostbud has no ports, every Caddy port is on `127.0.0.1`, `user` is set and the image's `USER` isn't root, mounts are only the data volume, agent socket and `*.pub`).

**E2E:** none (integration-only); `make e2e` stays green.

**Done:** `make test` runs unit + integration from a clean checkout.

### T8A — PostgreSQL persistence and operator access
- Replace the provisional SQLite implementation with PostgreSQL without destructive migrations.
- Add a pinned `hostbud-postgres` Compose service and named data volume; hostbud reaches it only over the private Compose network.
- The optional operator port uses `127.0.0.1:${HOSTBUD_DB_LOCAL_PORT}:5432`, with an uncommon, configurable default. Startup/deploy documentation requires checking that the chosen host port is unused; never auto-select a port or bind broadly.
- Database name, user and password are supplied through `.env`-backed `HOSTBUD_DB_*` variables. Never commit, print or bake credentials into images. Document `docker compose exec hostbud-postgres psql ...` and the loopback connection path.

**Tests:** U: migration/repository tests against disposable PostgreSQL; backup/restore smoke test. I: `docker compose config` verifies private networking, loopback-only optional port, non-secret environment wiring and persistent volume. E: stack boot check through Caddy and `/api/health` against disposable PostgreSQL, covered by the T5 *Open the app* smoke scenario.

**E2E:** no user-visible behavior; update the E2E Compose stack so later authentication scenarios use a disposable PostgreSQL service and never the production volume.

**Done:** a fresh deployment starts PostgreSQL, hostbud connects, owner SQL access works through the documented loopback path, and no real credential appears in tracked files or logs.

### T8B — Whitelist-gated account authentication
- Add `users`, `email_allowlist`, `auth_sessions` and `login_rate_limits` migrations in PostgreSQL.
- Add account creation and sign-in UI/API using normalized email and Argon2id password hashes. An enabled whitelist row is required both before registration and again at sign-in; disabling a row prevents new sessions. No password reset, email delivery or email verification in M1.
- Use opaque, server-side sessions in an HttpOnly/SameSite cookie; require authentication for all routes except health, registration and sign-in. Preserve Origin checks for state-changing requests and WebSockets.
- Owner-only whitelist maintenance is plain SQL; there is no whitelist admin endpoint. Generic responses must not disclose account or whitelist state.
- Add escalating login throttling keyed by email+source-IP and an IP-wide bucket. Repeated rate-limit hits increase the block duration exponentially up to a configured ceiling; return `429` and `Retry-After`; successful login clears only the email+IP failure bucket.
- Document all auth/rate-limit env vars without writing real values into tracked files.

**Tests:** U: email normalization, Argon2id verification, session rotation/revocation, whitelist gating, generic errors, exponential backoff and `Retry-After`, trusted-proxy IP extraction. I: PostgreSQL migrations, concurrent rate-limit updates, cookie flags, and SQL whitelist changes. E: registration blocked before whitelist insertion; registration succeeds after insertion; login succeeds while whitelisted; disabling the row blocks subsequent login; repeated bad logins receive increasing `Retry-After`; valid login reaches the protected app; logout revokes access.

**E2E:** desktop and iPhone projects add registration, sign-in, logout, whitelist-gated registration/login, and escalating login-rate-limit scenarios through Caddy against the throwaway PostgreSQL database. Test data uses generated example addresses and passwords only.

**Done:** unauthenticated users cannot reach the tmux UI/API, the complete account flow works for whitelisted email addresses, owner SQL whitelist changes take effect, and rate limiting cannot be bypassed by changing only the email address.

### T9 — `tmux` package
- Session-name validation `^[A-Za-z0-9_-]{1,64}$`.
- Builders: `list-sessions -F …`, `new-session -d -s <name> -c <path> [-e K=V…] [cmd]`, `rename-session -t =<old> <new>`, `kill-session -t =<name>` — always `=` exact targets.
- Parser for the tab-separated list format; "no server running" ⇒ empty list.
- `tmux -V` version parsing; `-e` only when ≥ 3.2.

**Tests:** U: name validation; builders (`=` targets, `-c`, `-e` only on ≥ 3.2, command); list parser (attached, windows); "no server running" ⇒ empty; `tmux -V` parsing. I: on test sshd: list with no server ⇒ empty; create, list (name, attached while a PTY client is attached, window count), rename, kill.

**E2E:** none (no endpoint yet; covered through the API in T12); `make e2e` stays green.

**Done:** unit tests for builders/parser/validation; integration test creates, lists, renames, kills a session on the test sshd.

### T10 — Events bus and inventory
- `internal/events`: typed in-process pub/sub (`machine.status`, `sessions.changed`).
- Capability probe on startup: `uname -s`, `command -v tmux`, `tmux -V`, home dir → stored on the machine row; status `ok | unreachable | tmux_missing`.
- Poller for the host every `HOSTBUD_POLL_INTERVAL`: diff against cache, publish only on change; exponential backoff + `unreachable` on failure; recover automatically. Interface allows a control-mode implementation later.
- `Refresh()` for an immediate re-poll after mutations.

**Tests:** U: fake executor: diffing (add, remove, attached/windows change), publish only on change, backoff, `ok` ⇄ `unreachable` ⇄ `tmux_missing` transitions, `Refresh()`. I: the poller sees a create and a kill on test sshd within one interval; the probe against the tmux-less variant ⇒ `tmux_missing`.

**E2E:** none yet (events become reachable at T12); `make e2e` stays green.

**Done:** unit tests with a fake executor (diffing, backoff, status transitions).

### T11 — Session service
- Single `CreateSession(ctx, {machine, name, path, env, startCommand})`: custom name as given, else the default name: the directory's last path segment (`/root/docs/dev` → `dev`; characters a name can't hold become `-`); if taken, `dev-1`, `dev-2`, …; path default = probed home; `~/` expanded against home; validate name; refresh after.
- `RenameSession`, `KillSession` (service assumes UI confirmed); refresh after.
- Actionable errors (duplicate name, path doesn't exist, tmux missing).

**Tests:** U: fake executor: default name from the last path segment, `-1`, `-2`, … on clash, custom name kept, `~/` expansion, validation, refresh after each mutation, error mapping. I: on test sshd: create with defaults (`#{session_path}` matches); create with start command (`#{pane_current_command}`); duplicate name and missing path ⇒ real tmux errors mapped to actionable ones.

**E2E:** none yet (reachable through the API in T12); `make e2e` stays green.

**Done:** unit tests with fake executor; integration test for create with start command.

## D. API

### T12 — REST + events WebSocket
- `internal/api` (`coder/websocket`):
  - `GET /api/machines`, `GET /api/machines/:id/sessions`, `POST …/sessions`, `PATCH …/sessions/:name`, `DELETE …/sessions/:name`.
  - `/ws/events`: snapshot on connect, then bus events; ping/keepalive.
- JSON error shape `{error, hint?}`.
- Origin middleware: allowlist `https://${HOSTBUD_DOMAIN}` (if set) + `http://localhost:${HOSTBUD_LOCAL_PORT}`; applied to WebSocket upgrades and non-GET requests.
- No user paths or command strings in info logs.

**Tests:** U: `httptest` for every route, `{error, hint}` shape, validation 400s, Origin middleware (allowed, foreign, missing) on non-GET and upgrades, `/ws/events` snapshot then events; captured info-level logs from create/rename/kill contain no path or command. I: n/a (the API is covered by U with a fake service, and end to end by e2e).

**E2E (API level, through Caddy, desktop project):**
- **API list (T12):** a session made with `target.tmux` shows up in `GET …/sessions`.
- **API mutations (T12):** create, rename and kill via the API are reflected in `tmux ls` on the target.
- **API validation (T12):** an invalid name returns 400 `{error, hint}`.
- **Events (T12):** `/ws/events` sends a snapshot, then `sessions.changed` within one poll interval of a real-terminal create.
- **Origin (T12):** a foreign-`Origin` POST and WebSocket upgrade are rejected.
- **Logs clean (T12):** after the run, `hostbud-e2e-app` info logs contain none of the scenarios' paths, commands or markers.

**Done:** `httptest` tests incl. Origin rejection and validation errors.

### T13 — Terminal bridge
- `internal/term`: `/ws/term?machine=&session=&cols=&rows=` → PTY (`creack/pty`) running `ssh -F … -tt hostbud-host -- tmux attach-session -t =<name>`, `TERM=xterm-256color`.
- Binary frames for I/O; JSON text control frames: `resize`, `ping`; server → `exit {code}`.
- WS close ⇒ kill ssh process; bounded write buffer, drop stalled clients.
- Origin-checked.

**Tests:** U: frame codec (binary I/O, `resize`, `ping`, `exit`); attach-command builder; WS close cancels the process (fake); stalled client dropped. I: on test sshd: attach, send keys, read output; resize changes the window size; close ⇒ process gone, session alive; `detach-client` ⇒ `exit` frame; vim (`i` ⇒ `-- INSERT --`, `:q`) and htop (`q` quits) via `capture-pane`.

**E2E (API level, through Caddy, desktop project):**
- **Terminal WS (T13):** attach over `/ws/term`, send `echo e2e-<rand>` + Enter → the marker is in `target.capture`.
- **Terminal WS (T13):** a `resize` frame changes `#{window_width}x#{window_height}`.
- **Terminal WS (T13):** closing the socket leaves the session in `tmux ls`.
- **Origin (T13):** a foreign `Origin` is rejected on `/ws/term` too.

**Done:** integration test: attach, send keys, read output, resize, close ⇒ process gone, tmux session still alive.

## E. Frontend

### T14 — Client and stores
- Typed API client; Pinia `machines` and `sessions` stores updated from `/ws/events` (auto-reconnect with backoff, resync on snapshot). No polling.

**Tests:** U (Vitest): store reducers for snapshot and `sessions.changed`; WS reconnect with backoff; resync on snapshot; typed client error shape. I: n/a (frontend; covered end to end by e2e).

**E2E:** *Open the app* is extended (T14): on load the page connects to `/ws/events` with no console errors, and after `docker restart hostbud-e2e-app` it reconnects and resyncs.

**Done:** Vitest for store reducers.

### T15 — App shell and session list
- Accessible roles/labels on every control (e2e drives the UI by them); `data-testid` only where there is no accessible handle.
- Left gutter: flat session list (name, attached/detached dot, window count); "Other sessions" grouping comes in M4.
- Banner for host `unreachable` / `tmux_missing` with the actionable hint.
- Selecting a session opens it in the terminal view.

**Tests:** U (Vitest): session list (name, dot, window count); banner texts for `unreachable` / `tmux_missing`; selection opens the terminal view. I: n/a (frontend).

**E2E (both projects):**
- **Empty list (T15):** a fresh target shows an empty list with no error.
- **Real-terminal create/kill (T15):** a `tmux new -d` on the target appears within one poll interval; `tmux kill-session` removes it.
- **Attached state (T15):** a second client attaching on the target flips the indicator; `tmux new-window` updates the window count.
- **App restart (T15):** after restarting `hostbud-e2e-app` the list recovers with the same sessions.
- **Host unreachable (T15):** stopping sshd on the target shows the banner with its hint; starting it again recovers without a reload.
- **tmux missing (T15):** adds `hostbud-e2e-target-notmux`; the UI shows the install hint.

### T16 — Session actions
- Create dialog: name (optional), path (default `~`), start command (optional); inline validation matching the backend regex.
- Rename (dialog or inline); Kill via Reka UI `AlertDialog` confirmation.
- Error toasts showing the backend's actionable message.

**Tests:** U (Vitest): create-dialog defaults and inline validation (same regex as the backend); error toast shows `{error, hint}`; rename dialog; kill dialog: Cancel makes no API call, Confirm calls DELETE. I: n/a (frontend).

**E2E (both projects):**
- **Create with defaults (T16):** path only → the session is named after the directory, and `#{session_path}` matches.
- **Create with start command (T16):** name + path + `htop` → `#{pane_current_command}` is `htop`.
- **Invalid input (T16):** `a.b`, `a:b` and `a b` are rejected in the form; a duplicate name and a missing path show the actionable text.
- **Rename (T16):** the new name shows in `tmux ls` and in the list.
- **Kill (T16):** Cancel keeps the session; Confirm removes it from `tmux ls` and the list.

### T17 — Terminal view
- `@xterm/xterm` + `fit`, `webgl` (fallback), `web-links`, `unicode11`; bundled font.
- Connect `/ws/term`; `ResizeObserver` → `fit` → `resize` frame; show exit/disconnect state with a "Reconnect" button (auto-reconnect is M3).
- One terminal at a time (tabs/splits are M3).
- `window.__hostbud.termText()` (xterm buffer as text) only when built with `VITE_E2E=1` (Dockerfile build arg, set only by `test/e2e`); never in production builds.

**Tests:** U (Vitest): terminal WS client with a fake socket (binary I/O, `exit` ⇒ exit state + Reconnect); ResizeObserver ⇒ fit ⇒ `resize` frame; `__hostbud` hook absent unless `VITE_E2E=1`. I: n/a (frontend).

**E2E (both projects):**
- **Attach and type (T17):** click a session, type `echo e2e-$RANDOM` + Enter → the marker is in `capture-pane` and in `termText()`.
- **Full-screen apps (T17):** in vim, insert text and `:wq` → the file is written on the target; htop renders and `q` quits.
- **Resize (T17):** a viewport change updates `#{window_width}x#{window_height}`.
- **Leave without killing (T17):** closing the page ends the attach; the session stays in `tmux ls`.
- **Exit state (T17):** `prefix d` or the program exiting shows the exit state; Reconnect re-attaches.
- **App restart (T17):** after restarting `hostbud-e2e-app` the terminal can reconnect.
- **Create with start command (T17):** the htop session from T16 is visible in the terminal.

**Done (E):** *(host)* manual pass of the functional section of [M1-acceptance.md](M1-acceptance.md).

## F. Wrap-up

### T18 — Docs and release
- README: M1 usage (port forward, `make deploy`, required `.env` vars) and host setup: dedicated `~/.ssh/hostbud_ed25519` key, `authorized_keys` entry with `from="172.16.0.0/12",no-agent-forwarding,no-port-forwarding,no-X11-forwarding`, and a systemd user unit (`hostbud-ssh-add.service`) that loads it into the stable agent socket at boot (needs `loginctl enable-linger`).
- `.env.example` for any new vars; ARCHITECTURE updated if the design moved.
- E2E audit: every item in the E2E section of [M1-acceptance.md](M1-acceptance.md) has its scenario (added by the task it's tagged with) and passes in both projects, twice in a row from a clean checkout. Any gap is a bug in the task that missed it: fix it there, don't just add it here.
- Coverage audit: every functional and security criterion's U / I / E tests exist and pass; each n/a has its reason.
- Manual-only checks: Claude Code, htop mouse clicks, `ss -ltn`, `docker compose exec hostbud id`.
- Run the full [M1-acceptance.md](M1-acceptance.md) checklist.
- Summary to the owner: what changed, env vars to set, manual host steps.
