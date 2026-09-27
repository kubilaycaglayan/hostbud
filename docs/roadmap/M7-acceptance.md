# M7 — Hardening: acceptance checklist

M7 is done when every box is ticked, except the *Manual checks (owner)* list and the *Manual* notes on criteria, which are the owner's backlog and never block. Tasks: [M7-tasks.md](M7-tasks.md).

M7 hardens v1 so it can be left running. Every remote exec, SFTP operation, WebSocket, HTTP request and database query has a documented bound (ARCHITECTURE §15), and hitting one gives the user an actionable error instead of a hang. A stalled browser is dropped and reconnects by itself. Responses carry security headers and a strict CSP. All three containers run with least privilege and bounded logs. An optional Tailscale identity allowlist guards the domain path. `make backup`, `make restore-check` and `make restore` work against PostgreSQL. The `test/sshd` integration suite covers every host-facing package, including failure modes. `make doctor` and the README take a fresh host to a working install. M7 ends with the first full `make e2e` since M3, green twice in a row from a clean checkout.

It adds four optional env vars (`HOSTBUD_EXEC_TIMEOUT`, `HOSTBUD_SFTP_TIMEOUT`, `HOSTBUD_MAX_TERMINALS_PER_USER`, `HOSTBUD_MAX_TERMINALS`) and starts using the existing `HOSTBUD_ALLOWED_TS_USERS` / `TAILSCALED_SOCKET` through an opt-in Compose override. It adds no migration, no published port and no new route except the Tailscale 403 responses. Authentication, the Origin allowlist, `sshx` as the only exec path, `store` as the only SQL, the single session service and event-driven UI rules all still apply.

Setup for the manual checks: `make deploy` on the host (T14); a desktop browser on the port-forward path and the owner's iPhone on `https://${HOSTBUD_DOMAIN}`; for the fresh-install check, a separate machine or VM with Debian/Ubuntu and Docker, **not** a second clone on the production host (container and volume names would clash with the running deployment).

## Test coverage rule

Same as M1 ([M1-acceptance.md](M1-acceptance.md#test-coverage-rule)): every criterion names its **U** (unit: Go with fakes, Vitest, node/shell script checks), **I** (integration: `test/sshd`, PostgreSQL, or the rendered deploy and Caddy config) and **E** (e2e) tests and the task that writes each. n/a needs a one-line reason, and "manual" is used only where automation can't observe the behavior (a real tailnet, a real certificate, real iOS, a fresh host). Scenarios written in T1–T12 are type-checked when written and **run for the first time in T13**. A ticked box means its U/I tests pass and, from T13 on, its E scenario passed in the full run.

"Stall" in the criteria below means the `test/sshd` stall switch for `tmux` or `sftp-server` (T2/T3): the process sleeps until the switch's TTL (≤ 60 s) expires or it's switched off.

## Limits inventory and configuration

- [ ] ARCHITECTURE §15 *Limits and timeouts* lists every bound in the system (exec, poller, SFTP, terminal and events WebSockets, HTTP, database, auth, client), each with its value, where it's enforced, what the server does and what the user sees when it's hit, and whether it's configurable. Nothing in the code bounds a call without being in the table, and nothing in the table is missing from the code.
  - U: T1 the four config vars parse with defaults and reject out-of-range values with an error naming the variable (Go).
  - I: T12 audit compares §15 against the constants and config (listed in the audit notes); T1 `internal/deploytest`: the four vars reach only the `hostbud` service (Go).
  - E: n/a: documentation and configuration; the behaviors are covered by the criteria below.
- [ ] `HOSTBUD_EXEC_TIMEOUT` (default 10 s), `HOSTBUD_SFTP_TIMEOUT` (default 10 s), `HOSTBUD_MAX_TERMINALS_PER_USER` (default 32) and `HOSTBUD_MAX_TERMINALS` (default 128) are in `.env.example` with a comment, validated in `internal/config` (a value out of range is a startup error that names it), passed explicitly in `docker-compose.yml`, and wired to their packages.
  - U: T1 parsing and range table; `buildDeps(cfg)` passes each value to its package (Go).
  - I: T1 deploy-config render; T11 `check-docs.sh` (every config var is in `.env.example` and the compose `environment:`).
  - E: n/a: e2e keeps the production defaults on purpose; T2–T4 scenarios exercise them.
- [ ] Architecture tests keep code bounded: no `exec.Command` without a context, no HTTP client without a timeout, no WebSocket accepted without a read limit, and `context.Background()` only in allowlisted places, each with a reason.
  - U: T1 `internal/archtest` rules, each with a violating fixture that makes it fail (Go).
  - I: n/a: static analysis of the source tree.
  - E: n/a: static analysis.

## Exec timeouts and recovery

- [x] Every non-interactive tmux call (poll, probe, create, rename, kill, copy-mode, windows, select) is bounded by `HOSTBUD_EXEC_TIMEOUT`. Cancelling or timing out kills the ssh process, and no child is left behind.
  - U: T2 fake runner: the deadline applies to each call; cancellation kills the process (Go).
  - I: T2 with the tmux stall on, each call returns a timeout within timeout + 2 s, and `pgrep` finds no leftover ssh child (Go, `test/sshd`).
  - E: T2 *Stalled tmux times out (API)*.
- [x] A timeout reaches the user as an actionable error, never a hang. The API answers 504 with `{error, hint}` naming the timeout value (no path or session name in the hint). The create dialog keeps its fields and shows the message, inline rename shows it under the field, and other actions show a toast. The client aborts any of these requests at timeout + 5 s with "hostbud didn't answer", so a stuck proxy can't hang the UI either.
  - U: T2 504 mapping table over every tmux-backed route (Go); dialog/rename/toast error states and the client abort (Vitest).
  - I: T2 the stall integration above asserts the error kind and message.
  - E: T2 *Stalled host shows an error, not a spinner* (desktop and `iphone-13-pro`).
- [x] The poller treats a timeout as unreachable: status `unreachable` with "timed out", exponential backoff up to the §15 cap, and M1's banner. It recovers without a restart once the host answers.
  - U: T2 poller status and backoff on `KindTimeout` (Go).
  - I: T2 stall on → unreachable; stall off → ok within the backoff cap (Go, `test/sshd`).
  - E: T2 *Stalled host shows an error, not a spinner* (the banner appears and clears).
- [x] A wedged ControlMaster heals itself: after two consecutive timeouts on a machine, `sshx` resets the master (`ssh -O exit`, else kill its pid and remove its socket under `/data/ssh/cm` only), and the next call opens a new one.
  - U: T2 reset logic with a fake runner (count, reset on the second timeout, a success clears it, fallback to kill + remove, path confinement) (Go).
  - I: T2 `SIGSTOP` the real master → two timeouts → reset → the third call succeeds (Go, `test/sshd`).
  - E: n/a: a frozen master inside the app container can't be produced from the runner, and the stall scenarios cover the user-visible side.
- [x] The generated SSH config sets `ConnectTimeout 10` in the app-defaults block (last, so user settings win later), so an unreachable host fails at connect time.
  - U: T2 config generation golden file (Go).
  - I: T10 sshd down → `unreachable` within `ConnectTimeout` + the exec timeout (Go, `test/sshd`).
  - E: covered by M1's *sshd stopped* scenario (runs at T13).
- [x] An attach that produces no output within `HOSTBUD_EXEC_TIMEOUT` is closed with code 4408 ("host didn't answer"). The browser treats 4408 as a drop and re-attaches with M3's backoff, showing "Reconnecting… (attempt n)".
  - U: T2 watchdog with a silent fake process vs. a writing one (Go); `TermSession` retries on 4408 (Vitest).
  - I: T2 attach with the tmux stall on closes with 4408 (Go, `test/sshd`).
  - E: T2 *Stalled host shows an error, not a spinner* (an open terminal shows Reconnecting and recovers after the stall ends).

## SFTP bounds

- [x] Each SFTP operation (home, list, stat, mkdir and the subsystem handshake) is bounded by `HOSTBUD_SFTP_TIMEOUT` and answers 504 with `{error, hint}` on timeout. A timed-out or cancelled operation closes the SFTP client, and the next operation starts a fresh subsystem.
  - U: T3 never-answering fake opener: deadline, close, reopen (Go).
  - I: T3 sftp stall: timeout within the bound + 2 s, the subsystem process is gone, and a later call works on a fresh client (Go, `test/sshd`).
  - E: T3 *Stalled SFTP times out (API)*.
- [x] At most 4 SFTP operations run per machine at once. More wait within their own deadline, and a cancelled request frees its slot. Autocomplete aborts its previous request while the user types.
  - U: T3 semaphore (fifth waits, times out, frees on cancel) (Go); autocomplete abort (Vitest).
  - I: T3 five cancelled requests, then a sixth succeeds (Go, `test/sshd`).
  - E: n/a: concurrency limits aren't observable from a single serial user; the unit and integration tests cover them.
- [x] A listing response is capped at 2000 entries **and** 1 MiB of JSON (`truncated: true`), and a name in it is cut at 1024 bytes.
  - U: T3 response cap and name cap (Go).
  - I: T3 a 2500-entry directory returns 2000 with `truncated` (Go, `test/sshd`).
  - E: n/a: M4's file browser scenarios cover listing; building a 1 MiB listing in e2e adds time without new coverage.
- [x] The file browser shows a timeout as an error with **Retry** in place of the entries, never a spinner longer than timeout + 5 s. Breadcrumbs and Cancel keep working, and mkdir keeps the typed name on error.
  - U: T3 error state, Retry, mkdir name kept (Vitest).
  - I: n/a: frontend; the server side is above.
  - E: T3 *Stalled file browser shows Retry* (desktop and `iphone-13-pro`).

## WebSocket limits and stalled clients

- [x] Terminal output queued for one client is bounded at 2 MiB. Over it, or with a write blocked past 10 s, the server closes the socket with 1013 ("client too slow; reconnect") and ends its ssh process. The tmux session survives, and the browser re-attaches with M3's backoff.
  - U: T4 flooding fake PTY + non-draining socket → 1013 at 2 MiB + 1 and the process is killed; the blocked-write path (Go); `TermSession` retries on 1013 (Vitest).
  - I: T4 a real attach flooding a non-reading client is dropped within the write timeout + 2 s; `tmux list-clients` no longer lists it; the session lives; a new attach redraws (Go, `test/sshd`).
  - E: T4 *Stalled terminal client is dropped (API)* · T4 *Stalled browser recovers on its own* (desktop).
- [x] Concurrent attaches are capped per account (`HOSTBUD_MAX_TERMINALS_PER_USER`) and server-wide (`HOSTBUD_MAX_TERMINALS`). The cap is checked before the upgrade and before any ssh process starts. Over it the server answers 429 with `{error, hint}`, and a slot frees when the ssh process ends. The browser shows the message and doesn't auto-retry (a manual **Retry now** stays).
  - U: T4 caps before `Accept`, release on process exit, no overshoot under `-race` with 64 concurrent attaches (Go); no auto-retry on 429 (Vitest).
  - I: T4 33 attaches for one account: the 33rd gets 429, and the target shows 32 clients (Go, `test/sshd`).
  - E: T4 *Terminal caps (API)*.
- [x] The events socket is capped at 16 per account (429 over it). The server rejects client data frames with 1003, and a subscriber that falls 64 events behind is closed with 1013 and resyncs from the next snapshot.
  - U: T4 the cap, the 1003 close, the fall-behind close (Go).
  - I: n/a: no host involvement; the bus and socket are covered by unit tests with the real `coder/websocket` server.
  - E: T4 *Stalled browser recovers on its own* (the session list follows a change made after the stall).
- [x] WebSocket upgrades with a query string over 2 KiB are rejected with 400 before any ssh work. The 1 MiB input message limit stays.
  - U: T4 oversize query → 400; the existing input limit test (Go).
  - I: n/a: request validation, no host involvement.
  - E: n/a: covered by the unit test; an oversize URL through Caddy adds nothing the route test doesn't show.

## HTTP, request and database limits

- [x] Every state-changing JSON route decodes through the one helper: bodies over 64 KiB → 413, unknown fields → 400, a body without `Content-Type: application/json` → 415. Headers over 32 KiB → 431. REST handlers run under a 30 s deadline. `/api/*` responses carry `Cache-Control: no-store`.
  - U: T5 route-table test driven by the router (413/400/415 on every state-changing route), 431, the deadline middleware cancels a slow test handler, the no-store header (Go).
  - I: n/a: request handling needs no host. T5's `routes.json` (checked against the router) is shared with e2e.
  - E: T5 *Request limits through Caddy*.
- [x] The database pool and queries are bounded: 20 open / 5 idle connections, 30 min lifetime, 5 min idle time; `statement_timeout` 5 s, `lock_timeout` 3 s, `idle_in_transaction_session_timeout` 30 s, `connect_timeout` 5 s; startup waits at most 90 s for the database, then exits with an actionable error.
  - U: T5 DSN/runtime params and pool settings built from config; the startup bound with a fake clock (Go).
  - I: T5 `pg_sleep(6)` hits the statement timeout; a contended rate-limit row hits the lock timeout; `db.Stats()` shows the pool bounds (Go, PostgreSQL).
  - E: n/a: a database outage can't be staged in the e2e stack without restarting shared services mid-suite; unit and integration tests cover it.
- [x] A database timeout or outage answers 503 with `{error, hint}`. Session checks that fail on the database answer 503, never 401, and the browser keeps the user signed in on a 503. `/api/health` pings the database (1 s): 200 `{"status":"ok"}` or 503 `{"status":"degraded","db":"unreachable"}`, and stays public.
  - U: T5 503 mapping; auth middleware 503-not-401; health both ways (Go); the client keeps the session on 503 (Vitest).
  - I: T5 health is 503 when the test store pool is closed and 200 after a fresh pool reconnects (Go, PostgreSQL).
  - E: T5 *Request limits through Caddy* checks the healthy `/api/health`.

## Security headers and CSP

- [ ] Every response (API, SPA, assets, errors, 404) carries `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin` and a restrictive `Permissions-Policy` (clipboard left alone). The domain site adds HSTS (`max-age=31536000`, no `includeSubDomains`/`preload`); the loopback site and hostbud itself never send it.
  - U: T6 middleware on each response kind; no HSTS from hostbud (Go).
  - I: T6 `caddy adapt`: HSTS on the domain site only (Go, `scripts/caddy-config.sh`).
  - E: T6 *Security headers on both sites*.
- [ ] HTML responses carry the CSP: `default-src 'self'`; `script-src 'self'` plus exactly the M6 theme boot script's hash, computed at startup from the embedded `index.html` (any other inline script is a startup error); `style-src 'self' 'unsafe-inline'` (style attributes; reasons in §11); `connect-src 'self'` plus the explicit `ws://localhost:${HOSTBUD_LOCAL_PORT}` / `wss://${HOSTBUD_DOMAIN}`; `worker-src`, `manifest-src`, `font-src` `'self'`; `img-src 'self' data: blob:`; `frame-ancestors 'none'`; `base-uri 'none'`; `form-action 'self'`; `object-src 'none'`. There's no `unsafe-eval`.
  - U: T6 CSP from the allowlist (local, domain, both); hash of a fixture `index.html`; a second inline script → startup error (Go); `check-dist` rejects a second inline script (node).
  - I: T6 the built `web/dist/index.html` hashes to what the server computes (Go, after `make build` at CP3).
  - E: T6 *Security headers on both sites* (CSP present on `/`).
- [ ] The whole app runs under the CSP with no violations: terminals (WebGL), vim, the file browser, dialogs, the palette, theme switching, the PWA manifest and service worker, on all three profiles.
  - U: n/a: violations only happen in a real browser.
  - I: n/a: browser-side.
  - E: T6 *No CSP violations anywhere* (the fixture-level guard fails any UI scenario that triggers a violation, in all three profiles, plus the explicit tour scenario on desktop and `iphone-13-pro`). **Manual (T14):** Safari on the iPhone and the installed app show no CSP errors in Web Inspector.

## Container and deploy hardening

- [ ] `hostbud` runs with a read-only root filesystem (`/tmp` as tmpfs, `/data` as its volume), `cap_drop: [ALL]`, `no-new-privileges`, `pids_limit: 1024`, and a healthcheck (`hostbud healthcheck` → `/api/health`).
  - U: T7 `hostbud healthcheck` exit codes (ok, 503, refused, slow) (Go).
  - I: T7 `internal/deploytest` pins every setting; the built image runs its health and a session list with `--read-only` and only `/data` and `/tmp` writable (Go).
  - E: T13 the full suite runs against `hostbud-e2e-app` with the same restrictions (T7 mirrors them into `test/e2e/compose.yml`).
- [ ] `hostbud-caddy` runs with `cap_drop: [ALL]` + `NET_BIND_SERVICE`, `no-new-privileges`, a read-only root filesystem, and waits for `hostbud` to be healthy. `hostbud-postgres` runs with `no-new-privileges` and `cap_drop: [ALL]` plus only the capabilities its entrypoint proves it needs.
  - U: n/a: Compose settings have no unit.
  - I: T7 `internal/deploytest` (exact cap sets, `depends_on` condition); a fresh-volume Postgres start with the reduced capabilities (Go).
  - E: T13 the e2e Caddy mirrors the settings, and the suite runs through it.
- [ ] All three services rotate logs (`json-file`, `max-size: 10m`, `max-file: 5`).
  - U: n/a: Compose settings.
  - I: T7 `internal/deploytest`.
  - E: n/a: log rotation isn't observable through the UI or API.
- [ ] Unchanged and re-asserted: Caddy publishes only on `127.0.0.1:${HOSTBUD_LOCAL_PORT}` and `${TAILSCALE_IP}`; Postgres only on `127.0.0.1:${HOSTBUD_DB_LOCAL_PORT}`; hostbud publishes nothing. `hostbud` runs as `${HOST_UID}:${HOST_GID}`. The only host mounts are the agent socket and the host's public keys (read-only, `create_host_path: false`), plus the tailscaled socket with the T8 override (read-only). No service mounts the Docker socket, `~/.ssh` or a private key.
  - U: n/a: Compose settings.
  - I: T7 `internal/deploytest` extended with the no-docker-socket / no-`.ssh` / no-private-key assertions for every service, with and without the Tailscale override (Go).
  - E: n/a: port binding isn't observable from inside the e2e network. **Manual (T14):** `ss -ltnp` on the host shows no `0.0.0.0` listener from hostbud's containers.

## Tailscale identity allowlist

- [ ] Off by default. When `HOSTBUD_ALLOWED_TS_USERS` is set, requests on the **domain** path must come from a listed tailnet login (case-insensitive; tagged nodes only if the tag is listed). The site comes from Caddy's `X-Hostbud-Via` header, trusted only from `HOSTBUD_TRUSTED_PROXIES`; a missing or unknown value counts as domain. The loopback path is exempt, and `/api/health` is exempt.
  - U: T8 middleware (domain vs local, untrusted header ignored, missing → domain, health exempt, runs before auth/Origin/rate limit) and matching (case, tags, empty = off) (Go).
  - I: T8 `caddy adapt`: both sites set `X-Hostbud-Via` with `header_up` (overwriting a client value) (Go).
  - E: T8 *Tailscale allowlist on the domain path* · T8 *Loopback path is exempt*.
- [ ] Identity comes from tailscaled's LocalAPI `whois` over the mounted socket (2 s timeout), using the client IP from the trusted `X-Forwarded-For`. Results are cached per IP (60 s allowed, 10 s rejected, ≤ 1024 entries).
  - U: T8 whois client against a fake unix-socket server (200/404/500/timeout, request shape, `Host` header); cache TTLs and LRU bound (Go).
  - I: n/a: no real tailscaled in the test environment; the fake socket server is the unit layer, and the e2e fake is the end-to-end one.
  - E: T8 *Tailscale allowlist on the domain path* (mapping flips take effect after the negative-cache TTL). **Manual (T14):** with the real tailscaled, the owner's device is allowed; setting the list to a login that isn't the owner's gets the 403 page; restoring it works.
- [ ] It fails closed and explains itself. Enabled without a readable socket → startup error naming `TAILSCALED_SOCKET` and the override. A whois error or timeout → 403 "couldn't be verified". A login not in the list → 403 "isn't allowed" with the hint. The SPA gets a static 403 page, and WebSocket upgrades are refused before accept. Info logs carry a reason code, never the login or IP.
  - U: T8 startup error; 403 bodies and page; WS 403 before accept; log redaction (Go).
  - I: T8 `internal/deploytest` with the override: the socket bind is read-only and `create_host_path: false`; without it, no tailscale mount (Go).
  - E: T8 *Tailscale allowlist on the domain path* (`stranger`, `unknown` and `error` mappings).
- [ ] Enabling it is documented: `deploy/compose.tailscale.yml`, the commented `COMPOSE_FILE=…` line in `.env.example`, README *Tailscale identity allowlist (optional)*, and the restart needed after changing the list.
  - U: n/a: documentation.
  - I: T11 `check-docs.sh` (the override file and vars are referenced and exist).
  - E: n/a: documentation.

## Backup and restore

- [ ] `make backup` writes a PostgreSQL custom-format dump `backups/hostbud-<UTC>.dump` (file mode 600, directory 700) through `/tmp` in the read-only container. It prints only the name and size, and the password never appears in argv or output. The runtime `pg_dump` is at least the server's major version, else an actionable error.
  - U: T9 `Store.Backup` argv/env; version-check table (Go); script argument validation with `docker` stubbed (shell).
  - I: T9 a dump from the test database restores (below) (Go, PostgreSQL).
  - E: n/a: an operator `make` target with no UI or API.
- [ ] `make restore FILE=…` checks that the file is a hostbud custom-format dump, requires the database name typed (or `CONFIRM=<name>`), takes a safety backup, stops only the `hostbud` container, restores in a single transaction (`--clean --if-exists --no-owner --exit-on-error`), starts `hostbud`, and waits for health. On failure it names the safety backup and how to restore it.
  - U: T9 validation and confirmation paths with `docker` stubbed: wrong or missing confirmation, not a dump, not a hostbud dump → no side effect (shell).
  - I: T9 backup → mutate → restore on a **throwaway** database brings back the pre-backup rows; an older dump plus the next app start applies the newer migrations (Go, PostgreSQL).
  - E: n/a: an operator `make` target; destructive, so never run against the e2e or production database from the suite.
- [ ] `make restore-check FILE=…` restores into a temporary `hostbud_restore_check_<random>` database, prints the migration version and row counts, and always drops the temporary database, even when a check fails. It never touches the production database.
  - U: T9 the `trap` cleanup path (shell).
  - I: T9 no temporary database remains after a passing and a failing check (Go, PostgreSQL).
  - E: n/a: operator tooling. **Manual (T14):** run on a fresh `make backup` before the M7 deploy.
- [ ] README *Backup and restore* covers backup, restore-check, restore, what isn't in a dump (`hostbud-data` including `auth-key`, Caddy's certificates, `.env`) and off-host copies. ARCHITECTURE §8 *Backups* matches.
  - U: n/a: documentation.
  - I: T11 `check-docs.sh` (the targets named in README exist).
  - E: n/a: documentation.

## Integration suite against `test/sshd`

- [ ] ARCHITECTURE §13 has a coverage matrix: packages (`sshx`, `tmux`, `inventory`, `session`, `term`, `fsbrowse`, `projects`, `store`, `auth`) × cases (happy path, not found, invalid input, timeout, unreachable, tmux missing, host-key mismatch, concurrency). Every cell names a test or gives an n/a reason.
  - U: n/a: the matrix documents the I layer.
  - I: T10 fills every gap the matrix shows; T12 audits it.
  - E: n/a: documentation of integration coverage.
- [ ] The failure cases are covered against the real target: host-key mismatch (a hard error with the actionable message, no master created, recovery on restore); empty or missing agent (`KindAuth` with the `HOST_SSH_AUTH_SOCK`/`ssh-add` hint); sshd down; tmux server killed (empty list, then create works); concurrent create of one name (one 201, one 409); rename during kill; attach to a session killed mid-handshake; SFTP permission denied; name and path boundaries (1/64 chars, Unicode, 4096-byte path).
  - U: T10 messages and hints for the new error kinds (Go).
  - I: T10 each case (Go, `test/sshd`).
  - E: T10 *Host key change is a hard error* (desktop); the others are user-visible through M1/M4 scenarios already or aren't reachable from a serial single user.
- [ ] Info-level logs contain no user-chosen names or secrets. A full cycle run at info level with canary values (path, start command, session and project names, email, password, cookie token) logs none of them. Session names are logged only at debug.
  - U: T10 `term` and `session` log the name at debug only (Go).
  - I: T10 canary log test (Go, `test/sshd` + PostgreSQL).
  - E: n/a: logs aren't visible to the browser.
- [ ] The suite is stable: `make lint test` passes three times in a row with `-count=1` at CP5, and any flake found was fixed at its cause (no retries, no longer sleeps).
  - U: n/a: a property of the whole run.
  - I: T10/T12 CP5 record.
  - E: n/a: e2e stability is its own criterion (T13).

## Fresh-host install

- [ ] `make doctor` checks every prerequisite read-only and prints a fix for each failure: Docker/Compose; `.env` present, mode ≤ 600, no placeholders left; UID/GID; the agent socket has a key; sshd and tmux on the host; the host public keys; `TAILSCALE_IP` assigned and the domain resolving to it (warning only); ports free or held by hostbud; the subnet inside 172.16.0.0/12 without overlap; the tailscaled socket when the override is on. It never prints secret values.
  - U: T11 `doctor.sh` checks against fixture `.env` files and stubbed commands (shell).
  - I: T11 *(host)* `make doctor` runs on the real host (output recorded, values redacted); `doctor.sh` bugs are fixed, and host-setup failures are recorded as open owner items.
  - E: n/a: host tooling outside the UI and API.
- [ ] Docs and config can't drift: `make lint` runs `check-docs.sh` (README `make` targets exist, every config var is in `.env.example` and the compose `environment:`, every `.env.example` var is used or marked, README links resolve). A clone with only `.env.example` renders a valid Compose config.
  - U: T11 `check-docs.sh` fails on fixtures (unknown target, undocumented var, dangling link) (shell).
  - I: T11 `check-docs.sh` on the repo; `internal/deploytest` renders from `.env.example` alone (Go).
  - E: n/a: documentation checks.
- [ ] The README Quick start is a numbered, copy-pasteable path for a fresh Debian/Ubuntu host (key and agent unit, `authorized_keys` restriction, sshd/tmux, Tailscale and DNS, `.env`, `make doctor`, `make deploy`, first account via the whitelist, port-forward and domain checks, optional allowlist, first backup), each step with how to verify it, plus a Troubleshooting section. Placeholders only.
  - U: n/a: documentation.
  - I: T11 `check-docs.sh`.
  - E: n/a: documentation. **Manual (T14, owner backlog, doesn't block M7):** the owner follows the README verbatim on a separate fresh machine or VM up to a working port-forward login and a terminal; every deviation is fixed in the README and re-checked. The ROADMAP's *fresh-host install* acceptance.

## Security checklist (AGENTS.md)

Each item needs automated evidence (T12 audit). Ticked only when every listed test exists and passes.

- [ ] **Published ports:** Caddy only on `${TAILSCALE_IP}` and `127.0.0.1:${HOSTBUD_LOCAL_PORT}`, never `0.0.0.0`; hostbud publishes none.
  - Evidence: `internal/deploytest` `TestCaddyPublishesOnlyLoopbackAndTailscale` and the hostbud/Postgres port assertions (M1/M2, re-run T7). **Manual (T14):** `ss -ltnp`.
- [ ] **Remote commands via `sshx` only, shell-quoted; session names validated.**
  - Evidence: `internal/archtest` (no exec outside `sshx`; T1 context rule); `sshx` quoting tests; `tmux` name-validation tables; T10 boundary integration tests.
- [ ] **`StrictHostKeyChecking yes`, host key pinned from the mounted public keys (no TOFU), `BatchMode yes`.**
  - Evidence: `sshx` config golden test; T10 host-key-mismatch integration test and the T10 e2e scenario.
- [ ] **Origin check** on WebSockets and state-changing requests against `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
  - Evidence: T5 router ↔ `routes.json` test; T12 Origin table over `routes.json` (Go); T12 *Origin allowlist on every route* (e2e).
- [ ] **Destructive actions require UI confirmation** (kill from the tree, the menu, the Delete key, the palette), and `make restore` requires a typed confirmation.
  - Evidence: M1/M6 kill-confirmation Vitest and e2e scenarios; T9 restore confirmation shell tests.
- [ ] **Non-root `${HOST_UID}:${HOST_GID}`; private keys never mounted** (agent socket only).
  - Evidence: `internal/deploytest` user and mount assertions (T7 extension).
- [ ] **No secrets or user paths in info logs.**
  - Evidence: T10 canary log test; auth redaction tests (M1).
- [ ] **(v2) Hook endpoints use per-run tokens (stored hashed):** n/a in v1 (no hook endpoints exist). ARCHITECTURE §10's obligations still hold (T12 audit notes).

## Full e2e run

- [ ] **(T13) Full suite green:** `make e2e` passes the complete suite from M1–M7 in all three profiles (`desktop-chromium`, `iphone-13-pro`, `iphone-13-pro-domain`) with `retries: 0`, `forbidOnly: true`, the global timeouts unchanged, and no `skip`/`fixme`/deleted scenarios. A scenario moved to manual (only when Playwright truly can't observe it) is recorded here with the reason and listed in the summary for the owner to review later. No approval is awaited.
- [ ] **(T13) Twice in a row from a clean checkout:** two consecutive `make e2e` runs in a fresh worktree at the final commit are fully green. This also closes M3's open two-run check (M3-tasks CP3).
- [ ] **(T13) Every failure classified and fixed properly:** each fix commit names its class (product bug with a regression test committed first, stale scenario with the ARCHITECTURE line or task that changed the behavior, harness cause, or environment). Progress notes list them.
- [ ] **(T13) Results recorded:** M1–M7 acceptance checklists mark their E items as passed at this run (date, commit), and M4–M6's "part of the M7 full run" lines are ticked.

## E2E scenarios (`make e2e`, simulated user)

Profiles: `desktop-chromium`, `iphone-13-pro` (`http://localhost:9055`) and `iphone-13-pro-domain` (`https://hostbud.example.test`); the Tailscale scenarios use the third app instance at `https://hostbud-ts.example.test` and `http://localhost:9057`. All run against the throwaway `hostbud-e2e-target` only, never the real host. New files: `hardening.api.spec.ts`, `hardening.spec.ts`, `hardening.phone.spec.ts`, `headers.api.spec.ts`, `tailscale.api.spec.ts`, `tailscale.spec.ts`, `origin.api.spec.ts`. Every scenario that turns on a stall switch, pauses Caddy, rotates the host key or changes the whois mapping undoes it in `finally`. Each scenario is tagged with the task that writes it. Written and type-checked in T1–T12; first run in T13.

- [ ] **(T2) Stalled tmux times out (API):** with the tmux stall on, `POST /api/machines/host/sessions` → 504 `{error, hint}` within the exec timeout + 5 s; after the stall is off, the same request succeeds (desktop, API-level).
- [ ] **(T2) Stalled host shows an error, not a spinner:** stall on; New session shows "didn't answer" and keeps the typed name; the unreachable banner says "timed out"; an open terminal shows Reconnecting; stall off → the banner clears, the terminal re-attaches, and Create works (desktop and `iphone-13-pro`).
- [ ] **(T3) Stalled SFTP times out (API):** with the sftp stall on, a listing → 504 within the SFTP timeout + 5 s; stall off → 200 (desktop, API-level).
- [ ] **(T3) Stalled file browser shows Retry:** the file browser shows the error with Retry within the timeout + 5 s, Cancel works, and Retry lists the folder after the stall ends (desktop and `iphone-13-pro`).
- [ ] **(T4) Stalled terminal client is dropped (API):** a raw WebSocket with its socket paused during a flood is dropped (its `#{client_pid}` leaves `list-clients` within 30 s); the session survives; a new attach gets output (desktop, API-level).
- [ ] **(T4) Terminal caps (API):** 32 attaches for one account succeed, the 33rd gets 429 with the documented body, and closing one frees a slot (desktop, API-level).
- [ ] **(T4) Stalled browser recovers on its own:** with a flood running, Caddy paused for 20 s then resumed → the terminal re-attaches without a click, `#{client_pid}` changed, one client remains, typing works, and the session list follows a change made afterwards (desktop).
- [ ] **(T5) Request limits through Caddy:** for every state-changing route in `routes.json`, a 65 KiB body → 413 and `text/plain` → 415; an oversize header → 4xx with no side effect; `/api/*` has `Cache-Control: no-store`; `/api/health` is 200 `{"status":"ok"}` (desktop, API-level).
- [ ] **(T6) Security headers on both sites:** `/`, an asset, `/api/health` and a 404 carry the headers on the loopback and the domain site; CSP on `/`; HSTS only on the domain (desktop, API-level).
- [ ] **(T6) No CSP violations anywhere:** the fixture-level guard fails any UI scenario with a `securitypolicyviolation` (all profiles), plus an explicit tour (terminal with WebGL, vim, file browser, palette, theme switch, manifest and service worker) with zero violations (desktop and `iphone-13-pro`).
- [ ] **(T8) Tailscale allowlist on the domain path:** `allowed` works; `stranger` gets the 403 page, API 403 and a refused `/ws/events`; `unknown` and `error` get "couldn't be verified"; `/api/health` stays 200; back to `allowed` works after the negative-cache TTL (desktop, API-level and UI).
- [ ] **(T8) Loopback path is exempt:** with `stranger` mapped, `http://localhost:9057` signs in and lists sessions (desktop).
- [ ] **(T10) Host key change is a hard error:** after the target's host key is rotated, the banner shows the "SSH key changed" message and a terminal can't attach; after restoring it, both recover (desktop).
- [ ] **(T12) Origin allowlist on every route:** every state-changing route and both WebSockets from `routes.json`: a foreign or missing Origin → 403 with no side effect; the allowed Origins → not 403 (desktop, API-level).
- [ ] **(T13) Every earlier milestone's scenarios pass:** see *Full e2e run* above.

## Manual checks (owner, T14)

These are the owner's backlog, not blockers: they don't hold back M7's done state or the next milestone, and no agent waits for them. Record the date and the result here when the owner does one; an unchecked item stays open in the summary.

Agents add items here when they hit something only the owner can do or decide (a host-setup failure from `make doctor`, a scenario moved out of e2e, the `v1.0.0` tag) and keep going.

- [ ] Fresh-host install: the README Quick start followed verbatim on a separate fresh machine or VM reaches a port-forward sign-in and a working terminal; deviations were fixed in the README.
- [ ] `make backup` then `make restore-check` on the production data before the M7 deploy: counts look right, and no temporary database remains.
- [ ] After `make deploy`: `ss -ltnp` shows no `0.0.0.0` listener from hostbud's containers; `docker inspect hostbud` shows the read-only root filesystem and dropped capabilities; `make doctor` passes (fix any host-setup failure T11/T14 recorded).
- [ ] Tag `v1.0.0` locally if wanted (T14 doesn't tag).
- [ ] iPhone over the domain: the app, the installed PWA, WebGL terminal, clipboard copy and paste, and theme switching work under the CSP; Web Inspector shows no CSP errors; HSTS is present on the domain.
- [ ] Tailscale allowlist with the real tailscaled (only if the owner wants it on): the owner's devices are allowed; a list without the owner's login shows the 403 page; the loopback path still works; the list is restored afterwards.
- [ ] A long real workload: Claude Code in a session for a while, plus a large `cat` of a log file, shows no dropped terminal on a normal connection. A 1013 drop is expected only on a truly stalled client.

## Definition of done

- [ ] Every functional and security criterion above is satisfied: its U/I tests pass and its E scenario passed in the T13 run.
- [ ] `make e2e` is green twice in a row from a clean checkout (T13), with all earlier milestones' scenarios included, and every milestone's checklist records the pass.
- [ ] `make lint test` (three times in a row at CP5, and once after T13's last fix) and `make gitleaks` are green; no secrets, real hostnames, Tailscale IPs, owner paths or emails are tracked.
- [ ] README (status v1, limits and errors, Tailscale allowlist, backup/restore, doctor, Quick start, Troubleshooting), ARCHITECTURE (§3, §4, §6–§9, §11–§13, new §15), ROADMAP and `.env.example` (the four new vars, the commented `COMPOSE_FILE` override line) match what was built; no new migration.
- [ ] The E2E policy is back in force: AGENTS.md, ROADMAP and ARCHITECTURE §13.1 say e2e runs before every commit that changes reachable behavior.
- [ ] *(host)* `make deploy` done with the hardened stack healthy; the owner's manual checks are recorded above or listed as open.
- [ ] T15 safe Docker cleanup done: the production stack, all volumes and all backups intact and healthy; T13's worktree and any restore-check database removed; nothing outside hostbud touched; reclaimed space reported.
- [ ] Summary delivered: what changed, the new env vars (four optional limits, the optional Tailscale override), manual steps on the host (`make doctor`, optional allowlist, off-host backup copies), e2e results, open manual checks.
