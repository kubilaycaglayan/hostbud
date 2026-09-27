# M7 — Hardening: tasks

Goal: v1 is safe to leave running. Every remote call, SFTP operation, WebSocket, HTTP request and database query has a documented bound, and hitting one gives the user an actionable error, never a hang. A stalled browser is dropped and comes back on its own. Responses carry security headers and a strict CSP, and the containers run with least privilege. An optional Tailscale identity allowlist sits on the domain path. `make backup` and a documented restore work against PostgreSQL. The `test/sshd` integration suite covers every package that talks to the host, including failure modes. The README takes a fresh host to a working install, and the AGENTS.md security checklist is fully satisfied, with a test as evidence for each item. M7 ends with the first full `make e2e` since M3: every milestone's scenarios, fixed until green, then green twice in a row from a clean checkout.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m7--hardening) · checklist: [M7-acceptance.md](M7-acceptance.md).

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Limits inventory and configuration | Done |
| T2 Exec timeouts and ControlMaster recovery | Done |
| T3 SFTP bounds | Done |
| T4 WebSocket limits and stalled clients | Done |
| T5 HTTP, request and database limits | Done |
| T6 Security headers and CSP | Not started |
| T7 Container and deploy hardening | Not started |
| T8 Tailscale identity allowlist | Not started |
| T9 Backup and restore | Not started |
| T10 Integration suite completion and log hygiene | Not started |
| T11 Fresh-host install and `make doctor` | Not started |
| T12 Docs and security audit | Not started |
| T13 Full e2e run | Not started |
| T14 Release | Not started |
| T15 Safe Docker cleanup | Not started |

## Rules for this milestone

- Work top to bottom, one task at a time, and don't start a task until the previous one is done. **Before starting M7, check that M4, M5 and M6 meet their acceptance checklists**, except the e2e runs that were deferred to M7 and open owner items (their *Manual checks (owner)* lists). M7 hardens what those milestones built (the M5 service worker and the M6 theme boot script constrain the CSP; M6's routes need the same limits), so don't harden a moving target.
- Before each task, read the U/I/E coverage lines that [M7-acceptance.md](M7-acceptance.md) assigns to it, plus its own **Tests:** and **E2E:** lines. Write all of them in the same commit(s) as the behavior. Never leave tests or scenarios for a later task.
- **Batched test checkpoints** (continuing M3–M6): each commit runs only fast checks: `go build`/`go vet` for the Go packages it touches, `vue-tsc` for the frontend, and `tsc` for the e2e suite when it changes. `make gitleaks` runs on every commit through the pre-commit hook. Full suites run at the checkpoints below. A checkpoint failure is fixed (with a regression test if it's a bug) before the next task starts, and the checkpoint re-runs until green.
- **E2E runs stay paused until T13.** T1–T12 write their scenarios and type-check them, but never run `make e2e`, `e2e-up` or `e2e-run`. T13 is the one full run, and from then on e2e runs are allowed and required (T14 records the policy change).
- Every task has an **E2E:** line. A scenario is tagged with the task that writes it, both here and in the acceptance checklist.
- Tasks marked *(host)* need the real host, a desktop browser or the owner's iPhone. The agent does the host parts itself. Anything that needs the owner's device, account or decision is an open owner item: record it and continue, never wait (AGENTS.md, *Owner items never block agents*).
- **Hardening must not break the running deployment.** The dev machine is the deploy host. Don't run `make deploy` before T14. Don't restart, stop or recreate the production containers, and never touch the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data` or `hostbud-caddy-config`. Restore (T9) is exercised only against throwaway databases, never the production one. Don't change the owner's tmux config, shell config or `~/.ssh` files, and never kill or detach the owner's tmux sessions.
- **New env vars:** T1 adds the few operator-tunable limits (listed in T1). Each goes to `.env.example` with a placeholder and a comment, to `internal/config` with a validated range, and to the `hostbud` service's explicit `environment:` list in `docker-compose.yml`. List every new var in the final summary. Don't add a var for a limit nobody needs to tune: those stay constants, documented in the ARCHITECTURE §15 table.
- **Failure switches for tests** are fixed commands with a time-to-live. T2/T3 add stall switches to the `test/sshd` image (shared by the integration targets and the e2e target), and T4/T8/T10 add ctl actions. Each switch turns itself off after at most 60 s, so a crashed scenario can't leave the shared target stalled for the rest of the serial suite. Scenarios also switch them off in `finally`.
- **Bugs found on the way** follow the bug-fix workflow: a failing regression test in one commit, the fix in the next.
- When a task moves the design, it updates ARCHITECTURE in the same commit (mostly §3, §4, §6, §7, §8, §9, §13 and the new §15 *Limits and timeouts*).
- Conventional commits, small and focused; commit only this task's files. Other agents may be working in the tree at the same time: stage explicit paths, never use `git add -A`, and check `git status` before touching a shared file (`docker-compose.yml`, `test/e2e/compose.yml`, `test/e2e/helpers/*.ts`, `playwright.config.ts`, `Makefile`, `.env.example`, `internal/config/config.go`). Coordinate rather than overwrite.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T1 + T2 + T3 (config, exec and SFTP bounds) | `make lint test` | Passed |
| CP2 | T4 + T5 (WebSocket, HTTP and database limits) | `make lint test` | Passed |
| CP3 | T6 + T7 (headers/CSP, container hardening) | `make lint test`, plus `make build` (the CSP hash is read from the built `index.html`) and `scripts/compose-config.sh` | Not run |
| CP4 | T8 + T9 (Tailscale allowlist, backup/restore) | `make lint test` | Not run |
| CP5 | T10 + T11 + T12 (integration completion, install, audit) | `make lint test` **three times in a row** (flake check for the integration suite), `make gitleaks`, e2e `tsc` | Not run |
| CP6 | T13 (full e2e) | `make e2e` until green, then **twice in a row from a clean checkout**; `make lint test` after the last fix | Not run |

**What e2e can and can't reach.**
- **Stalls** use the `test/sshd` stall switches through new `hostbud-e2e-ctl` actions: `/stall/tmux/on|off` (T2) and `/stall/sftp/on|off` (T3). While a switch is on, every `tmux` or `sftp-server` process started on the target sleeps until the switch expires (60 s TTL), so hostbud's call hangs exactly as it would against a wedged host. The suite runs serially (`workers: 1`), so a target-wide switch can't disturb another scenario, as long as every scenario turns it off in `finally`.
- **A stalled browser** (T4) is `hostbud-e2e-caddy` frozen with `docker pause` through ctl `/caddy/pause` (auto-unpause after 30 s) while the terminal floods output. The app's writes to Caddy back up, the terminal socket's output queue fills, and the app drops the client. After `/caddy/unpause`, the browser's socket is closed and M3's reconnect re-attaches. The API-level variant uses a raw WebSocket (Node's `ws` in the runner) whose socket is paused and never read.
- **Tailscale identity** (T8) can't use a real tailnet. A fake LocalAPI (`hostbud-e2e-tsfake`, a unix socket in a shared volume) answers `whois` from a mapping that ctl sets (`/ts/map/allowed|stranger|unknown`), and a third app instance (`hostbud-e2e-app-ts`, same database) has `HOSTBUD_ALLOWED_TS_USERS=allowed@example.com`. Caddy serves it on `https://hostbud-ts.example.test` (domain path, checked) and `http://localhost:9057` (loopback path, exempt).
- **Host-key mismatch** (T10) uses ctl `/hostkey/rotate` (new target host key, sshd restarted) and `/hostkey/restore`.
- **What stays out of e2e:** deploy config (container hardening, published ports) is checked by `internal/deploytest`, not e2e. Backup/restore are operator `make` targets with no UI or API. Real Tailscale, real TLS, real iOS and the fresh-host install are manual checks.
- **A CSP violation guard** (T6) in `helpers/fixtures.ts` fails any UI scenario whose page reports a `securitypolicyviolation`, so T13's full run proves the CSP against every screen the suite visits.

---

## T1 — Limits inventory and configuration

Before changing any bound, write down every one: what's limited, where, the default, and what the user sees when it's hit. The inventory drives T2–T5 and is the ARCHITECTURE reference afterwards.

- **Inventory** (new ARCHITECTURE §15 *Limits and timeouts*): one table with columns *what* · *limit* · *enforced in* · *on hit (server)* · *on hit (user sees)* · *configurable*. Start from what exists today and mark gaps for T2–T5 to fill:
  - exec: `sshx.DefaultTimeout` 10 s, `WaitDelay` 1 s, the 3 s `ssh -O` checks, the capability probe;
  - poller: interval, exponential backoff and its cap;
  - SFTP: `fsbrowse.DefaultOpTimeout` 10 s, idle close 1 min, 4096-byte paths, 2000 entries, 255-byte names;
  - terminal WebSocket: 1 MiB input message, 64 × 32 KiB output queue, 10 s write timeout, 25 s ping + 10 s pong, client 25 s silence;
  - events WebSocket: 64-event subscriber buffer, 15 s heartbeat, 25 s ping, 10 s write timeout, client 40 s silence;
  - HTTP: `ReadHeaderTimeout` 10 s, `IdleTimeout` 2 min, 64 KiB JSON bodies (`decode`), 64 KiB UI state, shutdown 10 s;
  - database: none today (T5);
  - auth: session TTL, rate-limit knobs (already configurable);
  - client: 16 terminals, 4 panes per tab, 500 ms layout debounce, reconnect backoff.
- **Config** (`internal/config`, `.env.example`, `docker-compose.yml` `environment:`), each a Go duration or integer with a validated range. A value outside the range is a startup error that names the variable and the range:
  - `HOSTBUD_EXEC_TIMEOUT` (default `10s`, 2 s–2 m): every non-interactive `sshx` exec and the attach watchdog (T2);
  - `HOSTBUD_SFTP_TIMEOUT` (default `10s`, 2 s–2 m): each SFTP operation (T3);
  - `HOSTBUD_MAX_TERMINALS_PER_USER` (default `32`, 1–256) and `HOSTBUD_MAX_TERMINALS` (default `128`, 1–1024): concurrent `/ws/term` attaches (T4).
  Everything else stays a named constant in its package, listed in §15. Wire the four values through `cmd/hostbud/main.go` in this task. Where the behavior they control doesn't exist yet (the caps), T4 builds it.
- **Architecture tests** (`internal/archtest`), so later code can't reintroduce unbounded calls:
  - no `exec.Command(` (only `exec.CommandContext`) outside tests;
  - no `http.Get`/`http.Post`/`http.DefaultClient` and no `&http.Client{}` without a `Timeout` (T8's LocalAPI client must set one);
  - every `websocket.Accept` call site is followed by `SetReadLimit` or `CloseRead` in the same function;
  - `context.Background()` appears in `internal/` only in the allowlisted spots: shutdown paths and the tmux/SFTP service roots that already derive per-call deadlines. The list lives in the test with a reason per entry.
- ARCHITECTURE §15 (new), §12 (the four vars). README: nothing yet (T12).

**Tests:** U (Go): config parsing of the four vars (default, valid, below/above range, garbage → error naming the var); `main` wiring through a small `buildDeps(cfg)` seam (the sshx client gets the exec timeout, fsbrowse the SFTP timeout); the archtest rules, each with a fixture package that violates it (kept under `internal/archtest/testdata`, excluded from the build). I (Go): `internal/deploytest` renders `docker compose config` with placeholders and checks that the four vars reach the `hostbud` service and nothing else (the explicit-list rule).

**E2E:** n/a: no behavior reachable through the UI or API changes. The values are exercised by T2–T5's scenarios, and the e2e app keeps the production defaults on purpose, so the suite tests what ships.

**Done:** ARCHITECTURE §15 lists every bound with its gaps marked, the four vars are validated, documented and wired, and the architecture tests fail on an unbounded exec, HTTP client or WebSocket.

## T2 — Exec timeouts and ControlMaster recovery

A host that stops answering (overloaded, a hung sshd, a wedged tmux server) must give the user an error within the exec timeout, and hostbud must heal once the host answers again.

- **Stall switch in `test/sshd`** (shared by the integration targets and the e2e target): a `/usr/local/bin/tmux` wrapper, earlier in `PATH` than `/usr/bin/tmux`, that sleeps while `/home/dev/.hostbud-stall/tmux` holds a future epoch deadline, then `exec`s the real tmux. `sshd-ctl.sh stall tmux <seconds>|off` writes or removes the file, capping `<seconds>` at 60. The wrapper uses no shell features the target lacks, and the tmux-less variant doesn't get it.
- **ssh itself:** the generated config's app-default block (§4.1) gains `ConnectTimeout 10`, so an unreachable host fails at connect time, not at the exec deadline. It goes in the `Host *` block last, so a user's config (later, multi-machine) still wins.
- **Every exec path is bounded and mapped:** list-sessions (poller), capability probe, create/rename/kill, copy-mode (M5), windows/select (M6). Each runs under `HOSTBUD_EXEC_TIMEOUT` through `sshx`, and cancelling kills the ssh process group (`WaitDelay` stays). An `sshx` `KindTimeout` maps to **504** `{"error": "The host didn't answer within 10s", "hint": "It may be overloaded or tmux may be stuck. hostbud will retry; check the host with `ssh <host> tmux ls`."}`, with the real timeout value in the message. The hint names no path or session. The poller treats a timeout like any failure: status `unreachable` with the reason "timed out", exponential backoff (cap from §15), and M1's banner.
- **ControlMaster recovery** (`sshx`): a master that stops forwarding makes every later exec time out. After **two consecutive timeouts** on a machine, `sshx` resets the master: `ssh -O exit` (bounded by 3 s), and if that fails, kill the master's pid from `ssh -O check` and remove the socket under `/data/ssh/cm`. The next exec starts a new master. Only hostbud's own sockets are touched. Log the reset at warn without host details.
- **Attach watchdog** (`term`): if the attach process produces no output within `HOSTBUD_EXEC_TIMEOUT` of starting (tmux always draws at once), close the socket with application code `4408` and reason "host didn't answer". The client (`TermSession`) treats `4408` as a drop, so it retries with M3's backoff and shows "Reconnecting… (attempt n)", not an exit.
- **UI:** the create-session dialog/sheet shows the 504 message inline and stays open with the fields kept; inline rename (M6) shows it under the field; kill, copy-mode and select show it as a toast. No spinner runs longer than the exec timeout plus 5 s: the client's `fetch` for these routes aborts at timeout + 5 s with the same message ("hostbud didn't answer"), so a stuck proxy can't hang the UI either.
- ARCHITECTURE §4.1 (`ConnectTimeout`), §4.4 (timeouts, error mapping, master reset), §6 (watchdog, `4408`), §15. §13.1: the stall switch.

**Tests:** U (Go): `KindTimeout` → 504 with the configured value in the message for every tmux-backed handler (table over routes); poller status and backoff on timeout; the reset logic with a fake runner (two timeouts → reset, a success in between clears the count, reset failure falls back to kill + socket removal, only paths under the cm dir are removed); watchdog closes with 4408 when a fake process stays silent and not when it writes. U (Vitest): `TermSession` treats 4408 as retryable; API client aborts at timeout + 5 s with the message; the create dialog keeps its fields and shows the error. I (`test/sshd`): with the tmux stall on, list/create/rename/kill/copy-mode/windows each return `KindTimeout` within timeout + 2 s and leave no ssh child behind (`pgrep -f` in the toolbox); `SIGSTOP` the ControlMaster (pid from `ssh -O check`) → two execs time out → the master is reset → the third exec succeeds; the generated config contains `ConnectTimeout 10` in the last block (golden file); attach with the stall on closes with 4408.

**E2E:** add `hardening.api.spec.ts` (desktop) **(T2) Stalled tmux times out (API):** `ctl.stallTmux(30)`; `POST /api/machines/host/sessions` answers 504 with the documented shape within timeout + 5 s; `ctl.stallTmux('off')`; the same request then succeeds. Add `hardening.spec.ts` (desktop) and `hardening.phone.spec.ts` (`iphone-13-pro`) **(T2) Stalled host shows an error, not a spinner:** stall on; New session → the dialog shows "didn't answer" within timeout + 5 s and keeps the typed name; the unreachable banner appears with "timed out"; stall off → the banner clears within the backoff cap, and Create now works and the session appears. Add the ctl actions `/stall/tmux/on?ttl=` and `/stall/tmux/off` to `test/e2e/ctl/server.mjs` and `helpers/ctl.ts`. Type-check only.

**Done:** every tmux exec is bounded by the configured timeout and mapped to an actionable 504/banner, a wedged ControlMaster heals itself, a silent attach retries instead of hanging, and the scenarios compile.

## T3 — SFTP bounds

The file browser (M4) already bounds each operation to 10 s. This task makes that bound configurable, proves it against a stalled SFTP server, and closes the gaps: concurrency, response size, a desynced client after a timeout, and the UI.

- **Stall switch in `test/sshd`:** `Subsystem sftp /usr/local/bin/sftp-wrap` in `sshd_config`. The wrapper sleeps while `/home/dev/.hostbud-stall/sftp` holds a future deadline, then `exec`s the real `sftp-server`. `sshd-ctl.sh stall sftp <seconds>|off` works like T2's.
- **Timeouts:** each operation (home, list, stat, mkdir, and the SFTP startup handshake) is bounded by `HOSTBUD_SFTP_TIMEOUT`. A timed-out or cancelled operation **closes the SFTP client** (the stream may be mid-packet) and the next one starts a new subsystem. A timeout maps to 504 `{"error": "The host's file service didn't answer within 10s", "hint": "Try again; if it keeps happening, check that sftp-server works: `ssh <host> -s sftp`."}` (M4 has the 504 path; the message gains the value and the hint).
- **Concurrency:** at most 4 operations in flight per machine. A fifth waits for a slot within its own deadline, and a wait that runs out is the same 504. The client serializes autocomplete: typing aborts the previous request (`AbortController`), and the server sees the cancellation and frees the slot.
- **Size:** the 2000-entry cap stays. Also cap the listing's response at 1 MiB of JSON: entries past the cap are dropped and `truncated: true` is set, as with the entry cap. Names over 255 bytes already can't be created, but a listing may contain them from elsewhere: send them as they are, but cap each at 1024 bytes in the response (marked truncated).
- **UI** (`FileBrowser.vue` / `FileBrowserDialog.vue`): a timed-out listing shows the message with **Retry** in place of the entries; the dialog stays usable (breadcrumbs, Cancel) and never shows a spinner longer than the timeout + 5 s; mkdir keeps the typed name on error.
- ARCHITECTURE §7 (bounds, client reset, concurrency, response cap), §15.

**Tests:** U (Go): per-op deadline with a fake opener that never answers and observes cancellation/close; the 4-slot semaphore (fifth waits, times out, frees on cancel); 1 MiB response cap and `truncated`; 504 shape. U (Vitest): the file browser's error state with Retry; autocomplete aborts the previous request; mkdir keeps the name. I (`test/sshd`): with the sftp stall on, list returns the timeout within `HOSTBUD_SFTP_TIMEOUT` + 2 s, the ssh subsystem process is gone afterwards, and after the stall ends the next list works on a fresh client; a directory with 2500 entries is capped at 2000; a request cancelled mid-flight frees its slot (five cancelled requests followed by a sixth that succeeds).

**E2E:** add to `hardening.api.spec.ts` **(T3) Stalled SFTP times out (API):** stall on; `GET /api/machines/host/fs?path=~` → 504 with the documented shape within timeout + 5 s; stall off → 200. Add to `hardening.spec.ts` / `hardening.phone.spec.ts` (desktop and `iphone-13-pro`) **(T3) Stalled file browser shows Retry:** open the file browser with the stall on → the error and **Retry** appear within timeout + 5 s, and the dialog's Cancel works; stall off → Retry lists the folder. Add ctl `/stall/sftp/on?ttl=` and `/stall/sftp/off`. Type-check only.

**Done:** SFTP calls are bounded by the configured timeout, recover on a fresh client, can't pile up, can't return unbounded JSON, the UI offers Retry, and the scenarios compile.

## T4 — WebSocket limits and stalled clients

The terminal and events sockets must never let one slow or greedy client hold server memory or ssh processes.

- **Terminal output backpressure** (`internal/term`): replace the chunk-count queue with a **byte** bound: at most 2 MiB of queued output per client (§15). Over the bound, or a single write blocked past the 10 s write timeout, drops the client with close code **1013** ("client too slow; reconnect") and ends its ssh process; the tmux session survives. The client (`TermSession`) treats 1013 as a drop and re-attaches with M3's backoff, and tmux redraws. Log the drop at info with no session name (§ log hygiene, T10).
- **Caps on concurrent attaches:** `HOSTBUD_MAX_TERMINALS_PER_USER` per account and `HOSTBUD_MAX_TERMINALS` server-wide. Check them **before** the upgrade: over a cap → HTTP **429** `{"error": "Too many open terminals (32)", "hint": "Close some tabs or panes; each open terminal keeps an ssh process on the host."}`, and no ssh process is started. The client shows the message as a toast and doesn't retry automatically (the user must close something); a manual **Retry now** is still offered. The slot is released when the ssh process ends, not when the socket closes.
- **Events socket:** at most 16 per account (tabs × devices), 429 over the cap, oldest never kicked. It accepts no application data; a client data frame is rejected with 1003. The 64-event subscriber buffer and its "fell behind" close (1013) stay, and the client resyncs from the next snapshot (M1 behavior).
- **Handshake:** the upgrade request itself is bounded by `ReadHeaderTimeout` (exists). Upgrades with a query string over 2 KiB are rejected with 400 before any ssh work.
- **Input side:** the 1 MiB message limit stays (one paste). A burst of input faster than the PTY accepts blocks the reader goroutine, not the server. Document that, since the PTY write is bounded by the kernel buffer and the ssh process's liveness.
- ARCHITECTURE §6 (backpressure in bytes, 1013, caps), §9 (the 429s), §15.

**Tests:** U (Go): byte-bound helper rejects 2 MiB + 1 while accepting exactly 2 MiB; caps reject before `Accept`, release on process exit, and concurrent reservations never overshoot under `-race` with 64 goroutines; events socket 429 over 16 and 1003 on a client data frame; 400 on an oversize query. U (Vitest): `TermSession` retries on 1013; a 429 from the slot endpoint stops retries and the view shows a toast with manual Retry now. I (`test/sshd`): a real attach that runs `yes | head -c 50M` against a client that never reads is dropped; `tmux list-clients` no longer lists it, the session survives, and a fresh attach works. Opening 32 attaches for one account succeeds; the 33rd gets 429 and the target shows 32 clients.

**E2E:** `hardening.api.spec.ts` contains **(T4) Stalled terminal client is dropped (API)** using a raw upgraded socket paused during a target flood and **(T4) Terminal caps (API)** with 32 attaches and a 33rd 429. `reconnect.spec.ts` contains desktop **(T4) Stalled browser recovers and the event list resyncs** through a bounded Caddy pause. Ctl `/caddy/pause?ttl=` auto-unpauses and `/caddy/unpause` is used in `finally`. Type-check only.

**Done:** terminal output is bounded in bytes, slow clients are dropped and reconnect by themselves, attaches and event sockets are capped per account and globally before any ssh work, and the scenarios compile.

## T5 — HTTP, request and database limits

- **Requests:**
  - **shared route list** `internal/api/testdata/routes.json` (method, path pattern, state-changing, auth required, websocket, JSON body). A Go test fails if the router and the file differ, so a new route can't skip the limit, Origin (T12) or auth tables. The e2e suite reads the same file (mounted into the runner with the specs);
  - every JSON route decodes through the one `decode` helper (64 KiB, unknown fields rejected). A route-table test fails if a state-changing route reads its body another way (UI state keeps its raw 64 KiB reader, which is allowlisted);
  - state-changing requests with a body must send `Content-Type: application/json` (415 otherwise), which also closes the "simple request" CSRF gap for text/plain bodies;
  - `http.Server.MaxHeaderBytes` 32 KiB (Go answers 431);
  - REST handlers (not `/ws/*`) run under a 30 s request deadline (a middleware sets the context deadline and uses `http.ResponseController.SetWriteDeadline`), so a handler waiting on a lock or a slow query can't hold a connection forever;
  - `/api/*` responses carry `Cache-Control: no-store` (T6 adds the rest of the headers).
- **Database** (`internal/store`, still the only package with SQL):
  - pool: `SetMaxOpenConns(20)`, `SetMaxIdleConns(5)`, `SetConnMaxLifetime(30m)`, `SetConnMaxIdleTime(5m)`;
  - per-connection settings through the pgx DSN/runtime params: `statement_timeout=5s`, `lock_timeout=3s`, `idle_in_transaction_session_timeout=30s`, `connect_timeout=5`;
  - every store method takes the caller's context; the rate-limit `SELECT … FOR UPDATE` path is covered by `lock_timeout`;
  - a query that times out or can't connect maps to **503** `{"error": "The database isn't answering", "hint": "Check `docker compose ps hostbud-postgres`; hostbud recovers when it's back."}`. Auth failures during a DB outage are 503, never a 401 that signs the user out: the client keeps the session on 503;
  - startup: the existing open/retry loop gets an overall bound (90 s, matching the Postgres healthcheck `start_period`), then exits with an actionable error.
- **Health:** `GET /api/health` pings the database (1 s timeout): 200 `{"status":"ok"}`, or 503 `{"status":"degraded","db":"unreachable"}`. It stays public and reveals nothing else. T7's container healthcheck uses it.
- ARCHITECTURE §8 (pool, timeouts), §9 (415, 431, 503, health), §15.

**Tests:** U (Go): router ↔ `routes.json` consistency; route-table test (every POST/PUT/PATCH/DELETE route: oversize → 413, unknown field → 400, wrong content type → 415; the list of routes comes from the router, so a new route can't skip it); header limit → 431; request deadline middleware cancels a slow fake handler at 30 s (fake clock); 503 mapping for a store error of kind timeout/unavailable; auth middleware answers 503 (not 401) when the session lookup fails on the DB; client keeps the session on 503 (Vitest). I (Go, PostgreSQL): `SELECT pg_sleep(6)` through a store helper fails with the statement timeout; two transactions contending on a rate-limit row, the second fails within `lock_timeout`; pool settings read back from `db.Stats()`; health returns 503 against a stopped test database (the test's own container, paused with the testenv helper) and 200 after it resumes.

**E2E:** add to `hardening.api.spec.ts` **(T5) Request limits through Caddy:** for each state-changing route in the shared route list, a 65 KiB body → 413, `text/plain` → 415; a 40 KiB header → 431 (or Caddy's 4xx, asserted as a 4xx with no app side effect); `/api/*` responses have `Cache-Control: no-store`; `/api/health` is 200 `{"status":"ok"}`. Type-check only.

**Done:** every request body, header, handler and query is bounded, a database outage gives a 503 with a hint and never signs anyone out, health reports the database, and the scenario compiles.

## T6 — Security headers and CSP

- **Where:** headers are set by hostbud (a middleware in `internal/api`), so both sites and the e2e stack get the same ones, except **HSTS**, which only the domain site may send. Caddy adds `Strict-Transport-Security: max-age=31536000` on the `${HOSTBUD_DOMAIN}` site (no `includeSubDomains`, no `preload`: the owner's other subdomains aren't ours to pin).
- **Every response:** `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=()`. The clipboard isn't restricted: M3 needs it.
- **CSP** on HTML responses (the SPA shell, including the service worker's precached `index.html`: the header comes from the server, so M5's worker must not strip it):
  `default-src 'self'; script-src 'self' 'sha256-<M6 theme boot script>'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self' <ws origin(s)>; worker-src 'self'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'`.
  - `style-src 'unsafe-inline'` is needed for style attributes set by Vue, Reka UI, splitpanes and xterm. ARCHITECTURE §11 records why, and that scripts stay strict.
  - `connect-src` lists `ws://localhost:${HOSTBUD_LOCAL_PORT}` and `wss://${HOSTBUD_DOMAIN}` explicitly (from the Origin allowlist), because older WebKit doesn't cover WebSockets with `'self'`.
  - The boot-script hash is **computed at startup** from the embedded `index.html` (every inline `<script>` is hashed; an inline script that isn't the known boot script is a startup error). A future edit to the script can't silently break the CSP. `check-dist` (M6) also fails a build with a second inline script.
  - No `unsafe-eval`: verify xterm (WebGL addon) and Vue's runtime build run without it. If a dependency needs it, don't add it: use a CSP-safe alternative (a build or option that doesn't eval, or xterm's DOM renderer instead of WebGL), record the choice and why in ARCHITECTURE §11 and the summary, and continue.
- **E2E CSP guard:** `helpers/fixtures.ts` adds a page listener (an init script that records `securitypolicyviolation` events plus console CSP errors). The shared `page` fixture fails the test in teardown if any was recorded. Every UI scenario from M1–M7 is therefore a CSP test at T13.
- ARCHITECTURE §9 (security middleware), §11 (CSP and why each directive is as it is; replaces M6's "if added" note), §3 (HSTS on the domain site).

**Tests:** U (Go): the middleware sets every header on API, SPA, asset, 404 and error responses; CSP built from the allowlist (local only, domain only, both); the boot-script hash matches a fixture `index.html`, and a second inline script is a startup error; HSTS absent from hostbud's own responses. U (node): `check-dist` rejects a second inline script. I (Go): the built `web/dist/index.html` (after `make build` at CP3) hashes to what the server computes; `scripts/caddy-config.sh` adapts the Caddyfile and the domain site has the HSTS header while the loopback site doesn't.

**E2E:** add `headers.api.spec.ts` (desktop) **(T6) Security headers on both sites:** on `http://localhost:9055` and `https://hostbud.example.test`, `/`, an asset, `/api/health` and a 404 carry the headers; CSP is present on `/`; HSTS only on the domain. Add **(T6) No CSP violations anywhere** as the fixture-level guard (every UI scenario, all three profiles), plus one explicit scenario in `hardening.spec.ts` (desktop and `iphone-13-pro`): sign in, open a terminal with WebGL, run vim, open the file browser, the palette and a theme switch, and install check (manifest + service worker from M5) → zero violations. Type-check only.

**Done:** every response has the headers, HTML has a strict CSP that allows exactly the boot script, the domain sends HSTS, every UI scenario fails on a CSP violation, and the scenarios compile.

## T7 — Container and deploy hardening

- **`hostbud`:** `read_only: true` with `tmpfs: [/tmp]` (the app writes only to `/data` and `/tmp`; T10's integration run proves that); `cap_drop: [ALL]`; `security_opt: [no-new-privileges:true]`; `pids_limit: 1024` (each terminal is an ssh process plus the PTY; 128 terminals leave headroom); a `healthcheck` that runs a new `hostbud healthcheck` subcommand (GET `http://127.0.0.1:8080/api/health`, 2 s timeout, exit 0/1; no curl in the image).
- **`hostbud-caddy`:** `cap_drop: [ALL]`, `cap_add: [NET_BIND_SERVICE]`, `no-new-privileges`, `read_only: true` with `tmpfs: [/tmp]` (certs live in its volumes); `depends_on: hostbud: {condition: service_healthy}`.
- **`hostbud-postgres`:** `no-new-privileges`; `cap_drop: [ALL]` with the smallest add-back set the official image's entrypoint needs (start from `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `SETGID`, `SETUID`; prove it with a fresh-volume start in the integration test; drop any that aren't needed); `shm_size: 128m`.
- **All three:** `logging: {driver: json-file, options: {max-size: "10m", max-file: "5"}}`, so logs can't fill the disk.
- **Unchanged and re-asserted:** published ports (Caddy on `127.0.0.1:${HOSTBUD_LOCAL_PORT}` and `${TAILSCALE_IP}` only; Postgres on `127.0.0.1:${HOSTBUD_DB_LOCAL_PORT}` only; hostbud none), `user: ${HOST_UID}:${HOST_GID}`, the agent socket and host public keys as the only host mounts (keys read-only, `create_host_path: false`), no `~/.ssh` mount, no Docker socket mount.
- **E2E stack mirrors it:** `test/e2e/compose.yml` gives `hostbud-e2e-app` (and `-notmux`, `-ts`) the same `read_only`, `tmpfs`, `cap_drop`, `no-new-privileges` and `pids_limit`, so T13 runs the whole suite under the production restrictions.
- **Rollout risk:** the owner's running stack picks these up at T14's deploy. Before that, CP3 starts the hardened app once in the e2e project (not e2e tests: `docker compose -p hostbud-e2e up -d hostbud-e2e-app` and `/api/health`, then down) to catch a read-only-filesystem write early. Record that in Progress notes; it isn't an e2e run.
- ARCHITECTURE §3 (the table and container settings), §13.1 (the e2e app mirrors production hardening).

**Tests:** U (Go): `hostbud healthcheck` exit codes against a fake server (ok, 503, refused, slow). I (Go, `internal/deploytest` on the placeholder-rendered config): every setting above for each service (read_only, tmpfs, cap_drop/cap_add sets exactly, no-new-privileges, pids_limit, logging, healthcheck, depends_on condition); the existing port/user/mount assertions, plus "no `docker.sock`, no `.ssh` directory, no private key path mounted" for all services; `test/e2e/compose.yml` app services carry the same hardening as production (compared field by field). I (`test/sshd` toolbox): the hostbud binary runs its integration flows with `/` read-only (a Docker `--read-only` run of the built image with `/data` and `/tmp` writable; its health and one session list pass).

**E2E:** n/a for new scenarios: deploy settings aren't reachable through the UI or API; `internal/deploytest` owns them. The e2e app runs under the same restrictions, so T13's full run is the behavioral check (recorded in the acceptance checklist as the T13 run).

**Done:** all three containers run with least privilege and bounded logs, hostbud has a healthcheck Caddy waits for, the e2e stack mirrors it, and the deploy-config tests pin every setting.

## T8 — Tailscale identity allowlist

Optional and off by default (ARCHITECTURE §9): when `HOSTBUD_ALLOWED_TS_USERS` is set, a request that came in on the **domain** path must come from a tailnet user in the list. It's a second gate before sign-in, not a replacement for accounts.

- **Which requests:** Caddy tells hostbud which site a request used: `header_up X-Hostbud-Via domain` on the domain site and `header_up X-Hostbud-Via local` on the loopback site. `header_up` replaces a client-sent value. hostbud trusts the header only from `HOSTBUD_TRUSTED_PROXIES` peers; a missing or unknown value from a trusted peer counts as `domain` (fail closed). The loopback path is exempt (it's already behind the user's SSH login; documented).
- **Whois** (`internal/tsauth`, new package): an HTTP client over the unix socket `TAILSCALED_SOCKET` (mounted at `/run/tailscale/tailscaled.sock`), `GET /localapi/v0/whois?addr=<ip>:<port>` with `Host: local-tailscaled.sock`, 2 s timeout. The client IP comes from `X-Forwarded-For` via the existing trusted-proxy logic (`auth.ClientIP`). The login is `UserProfile.LoginName`, compared case-insensitively against the comma-separated list. Tagged nodes (no user) are rejected unless the list names the tag (`tag:…`).
- **Cache:** positive results 60 s, negative 10 s, per IP; at most 1024 entries (LRU). Changing the list needs a restart (documented).
- **Fail closed:** allowlist set but the socket isn't mounted or readable → **startup error** naming `TAILSCALED_SOCKET` and the compose override. Whois error or timeout at request time → **403** `{"error": "Your Tailscale identity couldn't be verified", "hint": "Check that tailscaled is running on the host."}`. Not in the list → 403 `{"error": "This tailnet user isn't allowed", "hint": "Ask the owner to add your Tailscale login to HOSTBUD_ALLOWED_TS_USERS."}`. The SPA shell gets a minimal static 403 page with the same text; WebSocket upgrades get 403 before `Accept`. `/api/health` is exempt, so health checks keep working.
- **Order:** the check runs first, before auth, Origin and rate limiting, so a rejected tailnet user can't probe sign-in.
- **Mount:** the socket isn't mounted by default (the file may not exist on hosts without Tailscale, and `create_host_path: false` would fail). A committed override `deploy/compose.tailscale.yml` adds the read-only bind of `${TAILSCALED_SOCKET}` and passes `HOSTBUD_ALLOWED_TS_USERS`. The owner enables it with `COMPOSE_FILE=docker-compose.yml:deploy/compose.tailscale.yml` in `.env`. Add this line to `.env.example`, commented out.
- **Logs:** a rejection logs at info as "tailscale identity rejected" with a reason code, never the login or IP (debug may include the login).
- **E2E fake:** `hostbud-e2e-tsfake` (tiny Node server on a unix socket in volume `hostbud-e2e-ts`) answers whois like tailscaled (200 with `UserProfile.LoginName`, 404 "no match" for unknown). It reads the current mapping from a file that ctl sets: `/ts/map/allowed`, `/ts/map/stranger`, `/ts/map/unknown` (404), `/ts/map/error` (500). `hostbud-e2e-app-ts` shares the database, mounts the socket, and has `HOSTBUD_ALLOWED_TS_USERS=allowed@example.com`. `test/e2e/Caddyfile` serves it on `https://hostbud-ts.example.test` (domain path, `tls internal`) and `http://:9057` (loopback path). Global setup signs in there too.
- README: *Tailscale identity allowlist (optional)*: enabling, the override, what it protects (the domain path only), how to find a login (`tailscale whois <ip>`). ARCHITECTURE §9 (the rules above), §3 (the override), §12, §13.1 (the fake and third instance).

**Tests:** U (Go): whois client against a fake unix-socket server (200 → login; 404 → unknown; 500/timeout → error; request shape and `Host` header); matching (case, tags, empty list = off); cache TTLs and LRU bound (fake clock); middleware (domain vs local header, header from an untrusted peer ignored → treated as domain, missing header → domain, health exempt, WS upgrade 403 before accept, SPA 403 page, runs before auth/Origin); startup error when enabled without a socket; info-log redaction. I (Go): `internal/deploytest` renders with `COMPOSE_FILE` including the override: the socket bind is read-only and `create_host_path: false`, and without the override there's no tailscale mount; `scripts/caddy-config.sh` shows `X-Hostbud-Via` set by both sites.

**E2E:** add `tailscale.api.spec.ts` (desktop) and `tailscale.spec.ts` (desktop) **(T8) Tailscale allowlist on the domain path:** mapping `allowed` → `https://hostbud-ts.example.test` loads and `/api/auth/me` works; `stranger` → the SPA shows the 403 page and the API answers 403 with the documented body, and a `/ws/events` upgrade is refused; `unknown` and `error` → 403 with the "couldn't be verified" text; `/api/health` stays 200 throughout; back to `allowed` → works again after the 10 s negative cache. **(T8) Loopback path is exempt:** with `stranger` mapped, `http://localhost:9057` signs in and lists sessions. Every scenario resets the mapping to `allowed` in `finally`. Type-check only.

**Done:** with the allowlist set and the override enabled, only listed tailnet users reach the domain path, failures are closed and explained, the loopback path is unaffected, and the scenarios compile.

## T9 — Backup and restore

- **Fix `make backup`:** today's recipe still describes SQLite (`VACUUM INTO`, a `.db` name), though `Store.Backup` runs `pg_dump --format=custom`. Rename the output `hostbud-<UTC timestamp>.dump`, fix the help text, and write the dump to `/tmp` in the container (read-only root fs from T7; `/tmp` is a tmpfs) before `docker compose cp` to `./backups/`. `chmod 600` the file and create `backups/` with mode 700. Print only the file name and size. `PGPASSWORD` stays in the child's environment, never argv or output (existing test extended).
- **Version skew:** the runtime image's `postgresql-client` must be ≥ the server's major version (15). Pin it: install `postgresql-client-15` from the PGDG apt repo, or pin the image so its client is ≥ 15. Add a startup/backup check that compares `pg_dump --version` with `SHOW server_version_num` and refuses with an actionable error if the client is older.
- **`make restore FILE=backups/<name>.dump`** (destructive; the UI-confirmation rule applies to the CLI as a typed confirmation):
  1. validates that the file exists, is a custom-format dump (`pg_restore --list` succeeds) and was made from a hostbud schema (the `goose_db_version` table is in the list);
  2. prints what will happen and requires typing the database name (`CONFIRM=<name>` for non-interactive use; no `-y` shortcut);
  3. takes a **safety backup** first (`make backup`, name printed);
  4. stops only the `hostbud` app container (`docker compose stop hostbud`; Postgres and Caddy keep running), so no writes race the restore;
  5. `pg_restore --clean --if-exists --single-transaction --no-owner --exit-on-error` into the configured database (password via environment);
  6. starts `hostbud` again; migrations run (append-only) if the dump is older; waits for `/api/health`;
  7. on any failure before step 5 commits, nothing changed; on failure after it, prints the safety backup's name and the command to restore it.
- **`make restore-check FILE=…`** (non-destructive, what the owner runs routinely): restores into a temporary database `hostbud_restore_check_<random>` on the same server, runs sanity queries (migrations table at the expected version; counts of users, allowlist, projects, ui_state), prints the counts, and drops the temporary database in all cases (`trap`). Never touches the production database.
- **What a backup doesn't contain** (README): the `hostbud-data` volume (regenerated SSH config and ControlMaster sockets, the `auth-key` for rate-limit hashing: losing it only resets throttling buckets), Caddy's certificates (re-issued, mind Let's Encrypt rate limits; back up `hostbud-caddy-data` if needed), `.env` (keep a copy in a password manager). Recommend copying `backups/` off the host.
- README: *Backup and restore* (backup, restore-check, restore, what's not included, off-host copies). ARCHITECTURE §8 *Backups* (replaces the one-line note).

**Tests:** U (Go): `Store.Backup` argv and env (no password in argv; refuses an existing file); the version check (older client → error, same/newer → ok). U (shell, run by `make test` in the toolbox): the Makefile/scripts' argument validation (missing FILE, not a dump, not a hostbud dump, wrong confirmation → no side effect), with `docker` stubbed. I (Go, PostgreSQL via testenv on a **throwaway** database): backup → mutate (add a user, a project, a ui_state row) → restore → the mutations are gone and pre-backup rows are back; restore of a dump taken before the latest migration → migrations bring it up to date on the next app start; `restore-check` leaves no temporary database behind even when its sanity query fails.

**E2E:** n/a: backup and restore are operator `make` targets with no UI or HTTP/WebSocket API; the integration tests run them against a throwaway database.

**Done:** `make backup` produces a private PostgreSQL dump, `make restore` is guarded, takes a safety backup and restores cleanly, `make restore-check` verifies a dump without touching production, and the README explains all three and what isn't included.

## T10 — Integration suite completion and log hygiene

The ROADMAP asks for "an integration test suite against `test/sshd`". Most of it exists (M1–M6). This task maps what's covered, fills the gaps, and makes the suite trustworthy (no flakes, no leaks).

- **Coverage matrix** (ARCHITECTURE §13, a table): rows are the packages that touch the host (`sshx`, `tmux`, `inventory`, `session`, `term`, `fsbrowse`, `projects`) and PostgreSQL (`store`, `auth`); columns are the happy path, not found, invalid input, timeout (T2/T3), unreachable host, tmux missing, host-key mismatch, and concurrency. Each cell names a test or says n/a with a reason.
- **Gaps to fill** (confirm each against the matrix first; some may already exist):
  - **host-key mismatch:** the integration target's host key rotated → `sshx` returns `KindHostKey`; inventory status carries the actionable message ("The host's SSH key changed. If you expected this, …"); no ControlMaster is created; restoring the key recovers;
  - **agent problems:** empty agent (no identities) and a missing socket → `KindAuth` with a hint about `HOST_SSH_AUTH_SOCK` and `ssh-add`;
  - **sshd down:** `unreachable` with `ConnectTimeout` (T2) in effect, recovery on restart;
  - **tmux server killed on the target** (`kill-server` on the test target only) → empty list, not an error, and creating a session starts a new server;
  - **races:** creating the same name twice concurrently (one 409, one 201); renaming a session while it's killed; attaching to a session killed during the handshake (exit frame);
  - **SFTP permissions:** listing a directory without read permission → 403-style actionable error; mkdir in a read-only directory;
  - **boundaries:** session names of 1 and 64 characters, Unicode and spaces in paths and window names, a path of exactly 4096 bytes.
- **Log hygiene (AGENTS security checklist):** an integration test runs a full cycle at **info** level with a JSON logger capturing to a buffer: sign-up/sign-in, create a project and a session (with a start command), rename, list, attach, mkdir, browse, kill, sign-out. Every value is a **canary** (`canary-path-7f3e`, `canary-cmd-…`, `canary-email@example.com`, the password, the cookie token). The test asserts none of them appears in the log. Existing info logs that carry session names (`term` attached/detached, `session` created/renamed/killed) move the name to **debug** and keep the event at info. ARCHITECTURE §9 already forbids paths and commands; this makes the rule "no user-chosen names at info" and records it.
- **ctl for e2e:** `/hostkey/rotate` (generate a new ed25519 host key on the target, restart sshd) and `/hostkey/restore` (put back the one mounted into the app).
- **Flake check:** at CP5, `make lint test` three times in a row, each with `-count=1`. Any flaky test is fixed at its cause (a missed wait, a shared tmux server, a port reuse), never with a retry or a longer sleep.

**Tests:** I (Go, `test/sshd` and PostgreSQL): every gap above, plus the log-hygiene cycle. U (Go): the new error kinds' messages and hints; the debug/info split in `term` and `session`. The matrix itself is checked at T12's audit.

**E2E:** add `hardening.spec.ts` (desktop) **(T10) Host key change is a hard error:** `ctl.rotateHostKey()` → within one poll interval plus backoff, the banner says the host's SSH key changed with the documented text; opening a terminal fails with the same message (no attach); `ctl.restoreHostKey()` → the banner clears and a terminal attaches. `finally` restores the key. Type-check only.

**Done:** the coverage matrix is complete with no unexplained gaps, the new failure tests pass, info logs carry no user-chosen names, paths, commands or credentials (proven by the canary test), three consecutive full test runs are green, and the scenario compiles.

## T11 — Fresh-host install and `make doctor`

The ROADMAP accepts M7 when "fresh-host install from README works end to end". Make that likely before anyone tries it, and diagnosable when it doesn't work.

- **`make doctor`** (`scripts/doctor.sh`, POSIX sh plus `docker`; runs on the host, read-only, changes nothing). Each check prints ✓/✗ and a one-line fix:
  - Docker and Compose v2 present, and the user can reach the daemon;
  - `.env` exists, mode 600 or stricter, and every required var is set (no placeholder values left: `replace-with-a-random-password`, `hostbud.example.com`, `100.64.0.1`);
  - `HOST_UID`/`HOST_GID` match `id -u`/`id -g`;
  - `HOST_SSH_AUTH_SOCK` is a socket and `ssh-add -l` against it lists at least one key;
  - `sshd` answers on the host (`ssh -o BatchMode=yes -o ConnectTimeout=5 ${HOST_SSH_USER}@localhost true`, using the agent) and `tmux -V` works on the host;
  - the three `/etc/ssh/ssh_host_*_key.pub` files exist;
  - `TAILSCALE_IP` is assigned to a local interface (`ip -o addr`), and the domain resolves to it (`getent hosts`), with a warning, not an error, if resolution fails from the host;
  - `HOSTBUD_LOCAL_PORT` and `HOSTBUD_DB_LOCAL_PORT` are free, or held by hostbud's own containers (`ss -ltnp` / `docker ps`);
  - `HOSTBUD_SUBNET` is inside 172.16.0.0/12 and doesn't overlap another Docker network;
  - with the Tailscale override enabled: `TAILSCALED_SOCKET` exists.
  It never prints secret values (it only says "set" or "placeholder"). Exit status 0 when all required checks pass.
- **Docs consistency check** (`scripts/check-docs.sh`, run by `make lint`): every `make <target>` named in README exists in the Makefile; every env var read in `internal/config` appears in `.env.example` and in the `hostbud` service's `environment:`; every `HOSTBUD_*` in `.env.example` is read somewhere or marked as v2/unused in its comment; README links to `docs/` files resolve.
- **README Quick start** rewritten as a numbered, copy-pasteable path for a fresh Debian/Ubuntu host: prerequisites → dedicated key and systemd agent unit → `authorized_keys` restriction line → sshd/tmux → Tailscale + DNS → `.env` → `make doctor` → `make deploy` → first account (the whitelist `INSERT` in `psql`) → port-forward check → domain check → optional Tailscale allowlist → `make backup`. Each step says how to verify it. Placeholders only (`example.com`, `/home/dev`, `server-a`).
- **Troubleshooting** section gathered from M1–M7 (host key changed, agent empty, `cannot assign requested address`, DNS split, 429 on sign-in, "didn't answer" timeouts, too many terminals, Tailscale 403).
- *(host)* Run `make doctor` on the real host and record the output with values redacted. A failure caused by `doctor.sh` itself (a wrong check) is a bug: fix it. A failure caused by the host's setup is not the agent's to fix: don't change the host. Record the failing check and its printed fix as an open owner item in [M7-acceptance.md](M7-acceptance.md#manual-checks-owner-t14) and the summary, and continue.

**Tests:** U (shell, in the toolbox via `make test`): `doctor.sh` checks as functions against fixture `.env` files and stubbed commands (placeholder detected, mode 644 flagged, overlapping subnet, missing socket, busy port held by another process vs by hostbud); `check-docs.sh` passes on the tree and fails on fixtures (unknown make target, undocumented var, dangling link). I: `check-docs.sh` against the real repo in `make lint`; `internal/deploytest` still renders with `.env.example` alone (a fresh clone with only placeholders produces a valid config). E: n/a (host tooling and docs).

**E2E:** n/a: `make doctor` and the README run on the host, outside anything the UI or API reaches.

**Done:** `make doctor` diagnoses every prerequisite with a fix, docs and config can't drift, the README takes a fresh host to a working install step by step, and `make doctor` ran on the real host with no `doctor.sh` bugs left (host-setup failures are recorded as open owner items).

## T12 — Docs and security audit

- **AGENTS.md security checklist, item by item,** with evidence in [M7-acceptance.md](M7-acceptance.md#security-checklist-agentsmd): for each item, the test(s) that prove it (file and test name) and the task that wrote them. The v2 hook-token item is recorded as n/a (no hook endpoints in v1) with the §10 obligation still intact. An item without automated evidence isn't ticked; add the test or explain why it's manual.
- **Origin allowlist on every route:** T5's shared route list `internal/api/testdata/routes.json` drives it. A Go test and a new e2e scenario both iterate it: every state-changing route and both WebSockets with a foreign Origin → 403 and no side effect; with the local and domain Origins → not 403. The scattered per-milestone Origin scenarios stay.
- **Secrets and personal data:** `make gitleaks` over the whole history; a tree-wide grep for Tailscale addresses (`100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.`) outside the documented placeholder `100.64.0.1`; for `/home/` paths other than `/home/dev`; for email addresses outside `example.com`/`example.test`. Also check that `.gitignore` covers `.env`, `data/`, `backups/`, `.cache/` and `test/e2e/results/`.
- **Docs reconcile:** ARCHITECTURE §3, §4, §6–§9, §11–§15 match what was built. ROADMAP M7 bullets match what shipped (scope moves noted). README: status line says v1 (it still says M2); sections for limits and errors users may see, Tailscale allowlist, backup/restore, doctor, troubleshooting. `.env.example`: every new var with a placeholder and comment.
- **Coverage audit:** every criterion in [M7-acceptance.md](M7-acceptance.md) has U/I/E lines with the right task, and every E item exists in the named spec file, is tagged, and type-checks. Also re-audit M4–M6 checklists: every E item they list exists in its spec file, since T13 will run them.
- CP5: `make lint test` ×3, `make gitleaks`, e2e `tsc`. Don't run e2e.

**Tests:** U (Go): a foreign-Origin table over every state-changing route and both WebSockets in `routes.json` (403, handler not called). Anything else the audit finds follows the bug-fix workflow.

**E2E:** add `origin.api.spec.ts` (desktop) **(T12) Origin allowlist on every route:** iterate `routes.json`: foreign Origin → 403 and a follow-up GET shows no change; no Origin on a state-changing request → 403; allowed Origins → not 403. Type-check only.

**Done:** the security checklist has evidence for every item, the Origin rule is proven route by route from one list, no secrets or personal data are tracked, docs match the code, and every milestone's E items exist and compile.

## T13 — Full e2e run

The first `make e2e` since M3 (ROADMAP: *Full e2e run*). The last code task: everything after it is release and cleanup.

1. **Preconditions.** T1–T12 done; `make lint test` green; working tree clean for this task's files. **Nobody else is using the e2e stack:** `docker compose -p hostbud-e2e ps -q` is empty and `pgrep -af 'test/e2e/run.sh'` shows nothing. If not, another run is using it: wait until it's free (re-check every few minutes), and never tear down a stack someone else is using. Enough disk: `docker system df`, and at least 10 GB free for the e2e images.
2. **First full run:** `make e2e` (fresh stack, all three profiles, torn down after). Record passed/failed/flaky counts and wall time in Progress notes.
3. **Triage every failure,** one at a time, into exactly one class, and record the class in the fix commit:
   - **Product bug** (the scenario is right, the app is wrong): bug-fix workflow. The failing e2e scenario is the regression test (plus a U/I test at the lowest layer that can catch it), committed first, then the fix.
   - **Stale scenario** (intended behavior changed in a later milestone, e.g. M4 locators after M6's tree roles, M2 phone layout after M5's drawer): fix the scenario, citing the ARCHITECTURE line or the task that changed the behavior. Never weaken what it asserts about the user-visible outcome.
   - **Harness problem** (timing, readiness, fixture leaks between scenarios): fix it at the cause: wait for the observable condition (a response, a `capture-pane` match), not a sleep.
   - **Environment** (disk, a stale image): fix the environment, not the code; record what happened.
   Forbidden to get green: `retries > 0`, raising the global `timeout`/`expect.timeout`, `test.skip`/`test.fixme`/`test.only`, deleting a scenario, or loosening an assertion to match a bug. If a scenario truly can't run in e2e (Playwright can't observe the behavior at all, not just "it's hard"), move it to the M7 manual checklist with the reason, record the move in [M7-acceptance.md](M7-acceptance.md#full-e2e-run) and list it in the summary for the owner to review later. Don't wait for approval. The owner may send it back to e2e.
4. **Fast loop while fixing:** now that e2e runs are allowed, use `make e2e-up` and `make e2e-run ARGS="<spec> -g '<name>' --project=<profile>"` for the failing scenario, then the whole spec file, then the whole suite with `make e2e`.
5. **After the last fix:** `make lint test` green.
6. **Stability: twice in a row from a clean checkout.** Create a throwaway worktree at `HEAD` in the scratch directory (`git worktree add <scratch>/hostbud-m7-e2e HEAD`). In it, run `make e2e`, then `make e2e` again. Both must be fully green in all three profiles, with no retries. A failure in either run → back to step 3, then both runs again from a fresh worktree. This closes M3's open two-run check (CP3) as well. Remove the worktree afterwards (`git worktree remove`), only the one this task created.
7. **Record:** in each of [M1](M1-acceptance.md)–[M7](M7-acceptance.md)'s acceptance checklists, mark the E items as passed at the M7 run (date, commit), and tick the M4–M6 "part of the M7 full run" DoD lines and M3's CP3 note. Commit the checklist updates together with the Progress table.

**Tests:** whatever the triage adds (regression tests per the bug-fix workflow).

**E2E:** runs the whole suite (M1–M7, all profiles) until green, then twice in a row from a clean checkout; adds no scenario except regressions.

**Done:** `make e2e` is green twice in a row from a clean checkout with no retries, skips or weakened assertions, every fix is classified and committed, and every milestone's checklist records the pass.

## T14 — Release

- *(host)* `make deploy`. This is the first deploy with T7's hardening, T5's pool and T6's CSP. Before it, run `make backup` and `make restore-check` on the result, and keep the file name. After it, check:
  - `docker compose ps`: three containers up, `hostbud` and `hostbud-postgres` healthy;
  - `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` → `{"status":"ok"}`;
  - `docker inspect` shows `ReadonlyRootfs` true, `CapDrop` ALL, and the log options on `hostbud`;
  - response headers on `/` through the loopback port (CSP present);
  - `make doctor` runs clean, or its host-setup failures are recorded as open owner items (T11).
  If the hardened app fails to start, roll back with `git stash`/checkout of the previous deploy commit and `make deploy`, report, and fix before retrying. The data is untouched either way (migrations are append-only).
- *(host)* Owner's manual checks, listed in [M7-acceptance.md](M7-acceptance.md#manual-checks-owner-t14): record date and result for each there. If the owner hasn't done them yet, list them as open in the summary rather than ticking them, and don't wait for them: they don't block T14, T15 or M7's done state.
- **E2E policy switches back on:** from now on e2e runs before every commit that changes behavior e2e can reach (ARCHITECTURE §13.1 already says so after M7). Update AGENTS.md (the "paused until the end of M7" paragraph becomes the resumed rule), ROADMAP (the paused note becomes history), and the project memory note about paused e2e runs.
- **Version:** don't tag, and don't wait for an answer. List "tag `v1.0.0` locally" (no remote exists) as an open owner item in the summary.
- **Summary to the owner:** what changed; **new env vars:** `HOSTBUD_EXEC_TIMEOUT`, `HOSTBUD_SFTP_TIMEOUT`, `HOSTBUD_MAX_TERMINALS_PER_USER`, `HOSTBUD_MAX_TERMINALS` (all optional, defaults fine), and the optional `COMPOSE_FILE` line for the Tailscale override with `HOSTBUD_ALLOWED_TS_USERS` / `TAILSCALED_SOCKET`; manual steps (run `make doctor`; optionally enable the Tailscale allowlist; set up an off-host copy of `backups/`); the e2e results (counts, the two green runs); open manual checks.

**Tests:** none new.

**E2E:** none run beyond T13 unless the deploy step finds a bug. Then the bug-fix workflow applies, followed by `make e2e` green again before re-deploying.

**Done:** the hardened stack runs on the host and is healthy, the owner's checks are recorded or listed as open (backlog, not blockers), the e2e policy is back in force, and the summary has been delivered.

## T15 — Safe Docker cleanup

Free the disk the milestone's builds and e2e runs used, **without touching the running deployment, its data, other projects, or work another agent may be doing at the same time.** This is the last step of the milestone, after T14's deploy and checks. M7 is the first milestone since M3 that ran e2e, so there is more to clean than in M5/M6: the e2e images, possibly a leftover stack, and T13's worktree.

1. **Check that nothing is in use.** If any of these hold, skip the cleaning (steps 3–6), record "cleanup skipped: <reason>" in Progress and the summary, and treat the task as done. Cleanup can run again later:
   - a `make` test/lint/build or e2e run is in progress from this or another session (`pgrep -af 'scripts/tool.sh|docker exec hostbud-tools|test/e2e/run.sh|docker compose .*hostbud'`);
   - a `hostbud-tools-*` container is running a process other than its idle entrypoint (`docker top <container>` for each one listed by `docker ps --filter label=hostbud.tools=1`);
   - the `hostbud-e2e` stack is up (`docker compose -p hostbud-e2e ps -q` is non-empty) and `pgrep` shows a run using it. If it's up but idle (a leftover from T13's `e2e-up` fast loop), it's this milestone's own: `make e2e-down` removes it and its volumes. That's allowed, but only after confirming no run is active;
   - a `docker build` or `docker compose build` for hostbud is running (`pgrep -af 'docker (compose )?build'`);
   - `make restore` or `make restore-check` is running (`pgrep -af 'pg_restore|restore-check'`).
2. **Record the before state:** `docker system df`; `docker compose ps` (the production stack: `hostbud`, `hostbud-caddy`, `hostbud-postgres` must be running and healthy before and after); `docker volume ls --filter name=hostbud`; `git worktree list`.
3. **Remove T13's worktree** if it's still there: only `<scratch>/hostbud-m7-e2e`, only if `git -C <it> status --porcelain` is empty, with `git worktree remove` (never `--force`) and then `git worktree prune`. Any other worktree belongs to someone else: leave it.
4. **Check restore leftovers:** list databases named `hostbud_restore_check_%` (`docker compose exec -T hostbud-postgres psql … -Atc "SELECT datname FROM pg_database WHERE datname LIKE 'hostbud\_restore\_check\_%'"`). T9's `trap` should have dropped them. If any remain and no restore-check is running, drop exactly those names, one by one, and report them. Never drop any other database.
5. **Clean with the repo's own target:** `make docker-clean` **without** `CACHE=1`. It removes only hostbud's own disposable artifacts: the e2e stack and its images (`hostbud-e2e-*:local`), the toolbox containers labelled `hostbud.tools=1` (recreated on the next `make`, at a few seconds' cost), and dangling images labelled `hostbud.image=1`. Before running it, read the `docker-clean` recipe in the Makefile and confirm it still matches this list. T8 added `hostbud-e2e-tsfake` and `hostbud-e2e-app-ts`: if the recipe was extended to remove their images, that's expected; if it removes anything else, don't run it: record the difference in Progress and the summary as an open owner item and finish the rest of the task.
6. **Never, in this task:**
   - `docker system prune`, `docker volume prune`, `docker image prune -a` without the `hostbud.image=1` label filter, `docker builder prune` (all projects' build cache; `CACHE=1` does this), or `docker network prune`;
   - removing the volumes `hostbud-data`, `hostbud-postgres-data`, `hostbud-caddy-data` or `hostbud-caddy-config`, or anything with another project's prefix;
   - stopping, recreating or removing the production containers, or removing the images they run from (`hostbud:local`, `hostbud-caddy:local`, the pinned `postgres` image);
   - removing the toolbox *images* (`hostbud-toolbox-*`), the `test/sshd` integration targets (`make test-down` is not part of cleanup; the owner keeps them warm for speed), or anything under `.cache/`, `data/` or `backups/` (T14's pre-deploy backup is there);
   - deleting any file in `backups/`, even old ones: retention is the owner's call.
   Build cache and the rest go only if the owner asks explicitly, as a separate step.
7. **Verify after:** `docker compose ps` shows the same three production containers still up and healthy; `curl -fsS http://127.0.0.1:${HOSTBUD_LOCAL_PORT}/api/health` answers `{"status":"ok"}`; `docker volume ls --filter name=hostbud` still lists the four production volumes; the `hostbud-test-sshd` containers are still there if they were before; `git worktree list` shows only the main tree plus any worktree that isn't this milestone's; `docker system df` again. Report the space reclaimed (before/after).
8. Commit only the Progress table update (this task changes no code).

**Tests:** n/a (operations only, no behavior change). The post-cleanup health check and `docker compose ps` above are the verification.

**E2E:** n/a: nothing reachable changes. T13 already ran the suite, and cleanup must not start a new run.

**Done:** hostbud's disposable Docker artifacts, T13's worktree and any restore-check leftovers are removed; the deployment, every volume and every backup are intact and healthy; nothing outside hostbud was touched; and reclaimed space is reported.
