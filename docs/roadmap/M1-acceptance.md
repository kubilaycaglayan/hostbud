# M1 — acceptance checklist

M1 is done when every box is ticked. Tasks: [M1-tasks.md](M1-tasks.md).

Setup for the manual checks: `make deploy` on the host, then from another machine `ssh -L 9055:localhost:9055 <host>` and open `http://localhost:9055`. Keep a real terminal on the host open alongside (`ssh <host>`).

## Test coverage rule
Every functional and security criterion below has a coverage line naming its tests at all three layers, and the task that writes each one:
- **U (unit):** Go tests with fakes, no network (`make test`), or Vitest for the frontend.
- **I (integration):** `-tags=integration` Go tests against the real `test/sshd` container, or checks on the real deploy config (`make test`).
- **E (e2e):** a scenario from the [E2E section](#e2e-make-e2e-simulated-user) (`make e2e`).

A layer may be **n/a** only with a one-line reason (e.g. pure byte passthrough has no logic to unit-test). "Manual" is allowed only where no automated layer can observe the behavior (e.g. Claude Code needs credentials). The tests land in the task named, **in the same commit as the behavior**. A ticked box means its automated tests exist and pass, not only that the manual check worked.

## Functional

### Authentication and PostgreSQL
- [ ] PostgreSQL is the application database; the owner can connect using credentials from the untracked `.env` or through `docker compose exec hostbud-postgres psql`. The optional maintenance port is uncommon, configurable, loopback-only, and deployment requires verifying it is unused.
  - U: T8A migration/config validation. I: T8A deploy-config and connection/backup checks. E: T8A authenticated E2E stack boots against disposable PostgreSQL. **Manual:** owner verifies the chosen host port with `ss -ltn` before deployment.
- [ ] An email not present and enabled in `email_allowlist` cannot create an account; adding it with plain SQL permits registration.
  - U: T8B email normalization and whitelist gating. I: T8B PostgreSQL migration and SQL update. E: T8B *Whitelist-gated registration*.
- [ ] A whitelisted user can register with email and password, sign in, reach the app, and log out; protected API and WebSocket routes reject unauthenticated requests.
  - U: T8B Argon2id, session creation/revocation, cookie flags and auth middleware. I: T8B PostgreSQL session persistence and protected-route checks. E: T8B *Registration/sign-in/logout*.
- [ ] Disabling an existing email in `email_allowlist` prevents subsequent sign-in without deleting the account; responses do not reveal account or whitelist state.
  - U: T8B generic auth errors and whitelist check. I: T8B SQL whitelist change against PostgreSQL. E: T8B *Whitelist-gated login*.
- [ ] Repeated failed login/rate-limit hits produce HTTP `429` with increasing `Retry-After` values, bounded by the configured ceiling; changing only the email does not bypass the IP-wide bucket.
  - U: T8B backoff and bucket logic. I: T8B concurrent PostgreSQL updates and trusted-proxy IP extraction. E: T8B *Escalating login rate limit*.
- [ ] No password, session token, password hash, database credential, full email or whitelist contents appear in logs or tracked fixtures.
  - U: T8B redaction tests. I: T8A/T8B deploy and log inspection. E: T8B *Authentication logs clean*.

### Session list
- [ ] The UI lists every tmux session on the host (name, attached/detached, window count).
  - U: T9 list-format parser · T14 sessions store reducer · T15 session-list component (Vitest). I: T9 list on test sshd (name, attached, windows). E: T15 *Real-terminal create/kill*, *Attached state*.
- [ ] With no tmux server running, the list is empty — no error.
  - U: T9 "no server running" ⇒ empty list. I: T9 list against a test sshd with no tmux server. E: T15 *Empty list*.
- [ ] `tmux new -d -s acc-a` in the real terminal → `acc-a` appears within one poll interval (default 3s), without reloading.
  - U: T10 poller diffing (fake executor) · T14 store applies `sessions.changed`. I: T10 poller sees a session created on test sshd within one interval. E: T12 *Events* · T15 *Real-terminal create/kill*.
- [ ] `tmux kill-session -t acc-a` in the real terminal → it disappears within one poll interval.
  - U: T10 diff on removal · T14 store removal. I: T10 poller sees a kill on test sshd. E: T15 *Real-terminal create/kill*.
- [ ] `tmux attach -t acc-b` / detach in the real terminal → the attached dot updates.
  - U: T9 parser (attached field) · T10 diff on attached change · T15 dot component. I: T9 list shows attached while a PTY client is attached on test sshd. E: T15 *Attached state*.

### Create / rename / kill
- [ ] Create with only a path (default `~`) → session named after the directory's last path segment (`/root/docs/dev` → `dev`; `dev-1`, `dev-2`, … if taken), started in that directory. A custom name is used as given.
  - U: T11 default name from the last path segment, `-1`, `-2`, … on clash, custom name kept, `~` expansion (fake executor) · T16 create-dialog defaults (Vitest). I: T11 create on test sshd; `#{session_path}` matches. E: T16 *Create with defaults*.
- [ ] Create with a name, a path like `~/some/dir`, and a start command (e.g. `htop`) → session runs the command in that directory; `tmux ls` in the real terminal shows it.
  - U: T9 `new-session` builder (`-c`, `-e`, command, `=` targets) · T11 `~/` expansion. I: T11 create with start command on test sshd (`#{pane_current_command}`). E: T16 *Create with start command* · T17 visible in terminal.
- [ ] Invalid name (e.g. `a.b`, `a:b`, a space) is rejected in the form and by the API.
  - U: T9 name validation · T12 `httptest` 400 `{error, hint}` · T16 form validation (Vitest). I: n/a (validation runs before any remote call; T8 checks that nothing reaches ssh unvalidated). E: T12 *API validation* · T16 *Invalid input*.
- [ ] Duplicate name or a non-existent path → clear, actionable error.
  - U: T11 error mapping (fake executor) · T16 error toast (Vitest). I: T11 duplicate name and missing path against real tmux on test sshd (real error text is mapped). E: T16 *Invalid input*.
- [ ] Rename from the UI → `tmux ls` shows the new name; the UI updates.
  - U: T9 `rename-session` builder · T11 refresh after rename · T16 rename dialog (Vitest). I: T9 rename on test sshd. E: T12 *API mutations* · T16 *Rename*.
- [ ] Kill from the UI asks for confirmation; Cancel leaves the session alive; Confirm kills it.
  - U: T9 `kill-session` builder · T16 dialog: Cancel makes no API call, Confirm calls DELETE (Vitest). I: T9 kill on test sshd. E: T16 *Kill*.

### Terminal
- [ ] Clicking a session attaches in a full terminal; typing works.
  - U: T13 frame codec and attach-command builder · T17 terminal WS client (Vitest, fake socket). I: T13 attach, send keys, read output on test sshd. E: T13 *Terminal WS* · T17 *Attach and type*.
- [ ] Claude Code renders and behaves correctly (input, scrolling output, colors).
  - U: n/a (byte passthrough, covered by T13 codec). I: n/a (needs credentials). E: n/a (needs credentials; vim/htop stand in). **Manual** (T18).
- [ ] vim works (insert mode, `:q`, arrow keys, colors).
  - U: n/a (byte passthrough, covered by T13 codec). I: T13 attach running vim: `i` shows `-- INSERT --` in `capture-pane`, `:q` exits. E: T17 *Full-screen apps*.
- [ ] htop renders correctly and responds to keys; mouse clicks work if tmux `mouse` is on.
  - U: n/a (byte passthrough). I: T13 htop starts and `q` quits (pane command changes). E: T17 *Full-screen apps*. Mouse clicks: **manual** (T18).
- [ ] Resizing the browser window resizes the tmux window (`tmux display -p '#{window_width}x#{window_height}'`).
  - U: T13 `resize` frame → PTY size · T17 ResizeObserver → fit → `resize` frame (Vitest). I: T13 resize changes the window size on test sshd. E: T13 *Terminal WS* · T17 *Resize*.
- [ ] Closing the tab ends only the attach: the session keeps running (`tmux ls`).
  - U: T13 WS close cancels the process (fake process). I: T13 close ⇒ ssh process gone, session alive. E: T13 *Terminal WS* · T17 *Leave without killing*.
- [ ] Detaching (`prefix d`) or the session exiting shows an exit state with a Reconnect button.
  - U: T13 process exit ⇒ `exit {code}` frame · T17 exit state + Reconnect (Vitest). I: T13 `detach-client` ⇒ exit frame. E: T17 *Exit state*.

### Robustness
- [ ] `docker compose restart hostbud` → the UI comes back and sessions are still listed; attached terminals can reconnect.
  - U: T6 data survives reopening the DB · T14 WS reconnect with backoff + resync on snapshot (Vitest). I: n/a (container restart, covered by e2e). E: T14 *Live connection* · T15 *App restart* · T17 *Terminal after restart*.
- [ ] Stopping sshd on the host (or breaking the agent socket) shows an "unreachable" banner with a hint; restoring it recovers without restarting hostbud.
  - U: T7 error mapping (refused, agent missing/empty) · T10 backoff and `unreachable` ⇄ `ok` transitions · T15 banner (Vitest). I: T8 stopped test sshd and a missing agent socket ⇒ mapped actionable errors. E: T15 *Host unreachable*.
- [ ] With tmux missing (test sshd image without tmux), the UI shows "tmux not found — install with …".
  - U: T10 probe ⇒ `tmux_missing` · T15 banner text (Vitest). I: T10 probe against the tmux-less `test/sshd` variant. E: T15 *tmux missing*.

## E2E (`make e2e`, simulated user)
Every scenario passes in **both** Playwright projects (`desktop-chromium`, `iphone-13-pro`) against the throwaway target, never the real host. API-level items run in the desktop project only. UI actions go through what a user sees; outcomes are checked in the UI **and** on the target (`tmux ls` / `capture-pane`).

**Each item is tagged with the task that must add it, in the same commit as the behavior.** An unchecked item whose task is ✅ done in [M1-tasks.md](M1-tasks.md) is a bug in that task, not work for the end of the milestone.

Authentication and database
- [ ] **(T8A) PostgreSQL stack:** the throwaway E2E app starts against its disposable PostgreSQL service; no production database volume or credentials are used.
- [ ] **(T8B) Whitelist-gated registration:** an unlisted generated email is rejected; after the test inserts an enabled row through the owner SQL path, registration succeeds.
- [ ] **(T8B) Registration/sign-in/logout:** the user registers, signs in, reaches the protected app, logs out, and is rejected from protected API/WebSocket routes afterward.
- [ ] **(T8B) Whitelist-gated login:** disabling the generated email's allowlist row blocks a new sign-in without deleting the account.
- [ ] **(T8B) Escalating login rate limit:** repeated invalid sign-ins return `429` and increasing `Retry-After` values, including when the email changes but the source IP remains the same.
- [ ] **(T8B) Authentication logs clean:** test passwords, session tokens, database credentials and full test emails are absent from app logs and tracked fixtures.

Harness
- [x] **(T5) `make e2e`** runs from a clean checkout. It tears down even on failure, leaves no `hostbud-e2e*` containers or volumes, and doesn't touch the production `hostbud` project.
- [x] **(T5) Harness smoke:** `target.tmux` creates, lists and kills a session; `target.capture` reads its pane.
- [x] **(T5) Open the app:** `http://localhost:9055` loads the shell through Caddy with no console errors; `/api/health` is ok.

API (through Caddy)
- [ ] **(T12) API list:** a session made with `target.tmux` shows up in `GET …/sessions`.
- [ ] **(T12) API mutations:** create, rename and kill via the API are reflected in `tmux ls`.
- [ ] **(T12) API validation:** an invalid name returns 400 `{error, hint}`.
- [ ] **(T12) Events:** `/ws/events` sends a snapshot, then `sessions.changed` within one poll interval of a real-terminal create.
- [ ] **(T12) Origin:** a foreign-`Origin` POST and `/ws/events` upgrade are rejected.
- [ ] **(T13) Terminal WS:** attach, send a marker (it shows up in `capture-pane`), resize (the window size changes), close (the session survives).
- [ ] **(T13) Origin:** a foreign-`Origin` `/ws/term` upgrade is rejected.
- [ ] **(T12) Logs clean:** after the run, `hostbud-e2e-app` info-level logs contain none of the paths, commands or markers the scenarios used.

UI
- [ ] **(T14) Live connection:** the page connects to `/ws/events` on load, and reconnects and resyncs after `hostbud-e2e-app` restarts.
- [ ] **(T15) Empty list:** a fresh target shows an empty session list, with no error.
- [ ] **(T15) Real-terminal create/kill:** a session created with `tmux new -d` on the target appears within one poll interval; `tmux kill-session` makes it disappear.
- [ ] **(T15) Attached state:** attaching from a second client on the target flips the attached indicator; `tmux new-window` updates the window count.
- [ ] **(T15) App restart:** after restarting `hostbud-e2e-app` the UI recovers and lists the same sessions.
- [ ] **(T15) Host unreachable:** stopping sshd on the target shows the unreachable banner with its hint; starting it again recovers without reloading.
- [ ] **(T15) tmux missing:** against the tmux-less target, the UI shows the install hint.
- [ ] **(T16) Create with defaults:** the user opens the create dialog and gives only a path → the session appears named after the directory, and `tmux display -p '#{session_path}'` matches.
- [ ] **(T16) Create with start command:** name + path + `htop` → the session's pane is running htop (`#{pane_current_command}`). T17 adds the check that it's visible in the terminal.
- [ ] **(T16) Invalid input:** names like `a.b`, `a:b`, `a b` are rejected in the form; a duplicate name and a missing path show the actionable error text.
- [ ] **(T16) Rename:** rename via the UI → the new name is in `tmux ls` and the list.
- [ ] **(T16) Kill:** Cancel keeps the session; Confirm removes it from `tmux ls` and the list.
- [ ] **(T17) Attach and type:** the user clicks a session and types `echo e2e-$RANDOM` + Enter → the marker is in `capture-pane` and in the browser terminal.
- [ ] **(T17) Full-screen apps:** vim (insert text, `:wq` writes the file on the target) and htop (renders, `q` quits) behave correctly.
- [ ] **(T17) Resize:** changing the viewport changes `#{window_width}x#{window_height}` on the target.
- [ ] **(T17) Leave without killing:** closing the page ends the attach; the session is still in `tmux ls`.
- [ ] **(T17) Exit state:** detaching (`prefix d`) or the program exiting shows the exit state; Reconnect re-attaches.
- [ ] **(T17) Terminal after restart:** after restarting `hostbud-e2e-app` the terminal can reconnect.

Overall
- [ ] **(every task) Kept green:** each task's commit ran `make e2e` green, including tasks with no new scenario.
- [ ] **(T18) Stable:** two consecutive full runs pass (no flakes); failures leave traces/screenshots/videos in `test/e2e/results/`.

## Security (AGENTS.md checklist, M1 scope)
- [ ] Authentication is required for all application API and WebSocket routes except health, registration and sign-in; sessions use opaque HttpOnly/SameSite cookies and passwords use Argon2id hashes.
  - U: T8B auth middleware, cookie and password-hash tests. I: T8B protected-route and session persistence checks. E: T8B *Registration/sign-in/logout*.
- [ ] PostgreSQL credentials never appear in tracked files, images or logs; the optional database port is uncommon, configurable, loopback-only and verified free before deployment.
  - U: T8A config/redaction tests. I: T8A Compose/mount/port inspection. E: n/a (runner does not inspect the host network); **Manual** owner checks `.env` permissions and `ss -ltn`.
- [ ] `ss -ltn` on the host: 9055 bound on `127.0.0.1` only; nothing on `0.0.0.0` from hostbud/Caddy; hostbud container publishes no ports.
  - U: n/a (no code). I: T8 deploy-config check: `docker compose config` shows `hostbud` with no ports and every Caddy port bound to `127.0.0.1` (or `${TAILSCALE_IP}` from M2). E: n/a (the e2e project publishes nothing by design). **Manual** `ss -ltn` (T4, T18).
- [ ] A request with a foreign `Origin` (e.g. `curl -H 'Origin: http://evil.example.com' -X POST …`) and a WebSocket upgrade with a foreign Origin are rejected.
  - U: T12/T13 `httptest` Origin middleware (allowed, foreign, missing). I: n/a (pure HTTP middleware, no remote side). E: T12 and T13 *Origin*.
- [ ] Remote commands only go through the `sshx` builder with quoted args (code review); session names validated server-side.
  - U: T7 quoting (quotes, spaces, `$`, newlines) · T7 architecture test: `os/exec` is imported only by `sshx` and `term` · T9 name validation. I: T8 quoting round-trip: `printf %s` with hostile args on test sshd returns them verbatim. E: T12 *API validation*.
- [ ] `/data/ssh/config` has `StrictHostKeyChecking yes` and `BatchMode yes`; `/data/ssh/known_hosts` contains only the pinned host keys from `/run/host-keys`.
  - U: T7 config generation and ordering · T7 known_hosts from `*.pub` only. I: T8 connects to test sshd with pinned keys only. E: n/a (not user-visible; the e2e app only boots and connects because pinning works, T7).
- [ ] Swapping in a wrong pinned key → connection refused with a host-key mismatch error (integration test).
  - U: T7 mismatch error mapping. I: T8 wrong key ⇒ refused, mapped error. E: n/a (needs tampering with the app's mounts; integration covers it).
- [ ] Kill requires confirmation in the UI.
  - U: T16 kill dialog (Vitest). I: n/a (UI-only). E: T16 *Kill*.
- [ ] `docker compose exec hostbud id` shows `${HOST_UID}:${HOST_GID}`, not root.
  - U: n/a (no code). I: T8 deploy-config check: `hostbud` has `user: ${HOST_UID}:${HOST_GID}` and the image's `USER` is not root. E: n/a (the runner has no Docker access). **Manual** `id` (T18).
- [ ] No private keys in the container: only the agent socket and `*.pub` host keys are mounted (`docker inspect` mounts).
  - U: n/a (no code). I: T8 deploy-config check: `hostbud`'s mounts are exactly the data volume, the agent socket and `*.pub` files. E: n/a (the runner has no Docker access).
- [ ] `make logs` at info level contains no tokens, user paths or command strings.
  - U: T12 captured info-level logs from create/rename/kill requests contain no path or command. I: n/a (covered by U and E). E: T12 *Logs clean*: after the run, the `hostbud-e2e-app` info logs contain none of the scenarios' paths, commands or markers.

## Definition of done
Process checks; they apply to the whole milestone, not to individual tests.

- [ ] `make lint test` green (Go unit + integration against `test/sshd`, frontend lint/type-check/Vitest), run with only Docker installed.
- [ ] `make e2e` green (both projects), and every E2E item above was added by the task it's tagged with (no end-of-milestone catch-up).
- [ ] Every functional and security criterion's U / I / E tests exist and pass. Each n/a has its reason, and "manual" is used only where allowed.
- [ ] `make gitleaks` clean; pre-commit hook installed via `make hooks` and blocking a planted fake secret.
- [ ] No real hostnames, domains, IPs, usernames or home paths in tracked files (`git grep` for the values in `.env`).
- [ ] README documents M1 usage; `.env.example` lists every new variable; ARCHITECTURE matches what was built.
- [ ] README documents PostgreSQL owner access, whitelist SQL, credential location, and the loopback-only uncommon-port requirement without containing real credentials.
- [ ] Summary delivered: what changed, env vars the owner must set, manual host steps.
