# hostbud

Manage the tmux sessions on your server from a web UI: browse directories, organize them as projects, and attach to sessions in a full browser terminal. Built for terminal-first and agentic-coding workflows. Self-hosted; reachable only via SSH port forward or your Tailscale tailnet. (Multi-machine support is planned.)

> Status: **M1** — the tmux manager works over an SSH port forward (`http://localhost:9055`). Access on your own domain over Tailscale (HTTPS) arrives in M2. See [docs/ROADMAP.md](docs/ROADMAP.md).

## How it works
- Runs in Docker on one host, behind Caddy: plain HTTP on `127.0.0.1:9055` for SSH port forwarding (and, from M2, HTTPS on your domain bound to the host's Tailscale IP).
- Reaches the host's tmux over SSH using a dedicated key in your ssh-agent (private keys never enter the container) and pins the host's own SSH host keys.
- Accounts are email + password, limited to addresses you allow with plain SQL; sign-in is throttled.
- Stores accounts and UI metadata in PostgreSQL; tmux sessions live on the host.

## Quick start (M1)
1. **Prerequisites on the host:** Docker + Compose (nothing else — build, tests and lint run in containers), `sshd` running (hostbud reaches the host over SSH), tmux, and a systemd user session.
2. **A dedicated SSH key for hostbud**, allowed only from Docker networks and without forwarding:
   ```sh
   ssh-keygen -t ed25519 -N '' -C hostbud -f ~/.ssh/hostbud_ed25519
   echo "from=\"172.16.0.0/12\",no-agent-forwarding,no-port-forwarding,no-X11-forwarding $(cat ~/.ssh/hostbud_ed25519.pub)" >> ~/.ssh/authorized_keys
   ```
   The Compose network uses a fixed subnet inside `172.16.0.0/12` (`HOSTBUD_SUBNET`), so the `from=` restriction matches only hostbud.
3. **Load the key into a stable agent socket at boot.** With systemd's user `ssh-agent.socket` the socket is `/run/user/<uid>/openssh_agent`. Create `~/.config/systemd/user/hostbud-ssh-add.service`:
   ```ini
   [Unit]
   Description=Load the hostbud SSH key into the ssh-agent
   Requires=ssh-agent.socket
   After=ssh-agent.socket

   [Service]
   Type=oneshot
   RemainAfterExit=yes
   Environment=SSH_AUTH_SOCK=%t/openssh_agent
   ExecStart=/usr/bin/ssh-add %h/.ssh/hostbud_ed25519

   [Install]
   WantedBy=default.target
   ```
   then `systemctl --user daemon-reload && systemctl --user enable --now ssh-agent.socket hostbud-ssh-add.service`, and `sudo loginctl enable-linger "$USER"` so both start at boot without a login.
4. **Configure:** `cp .env.example .env` and set at least `HOST_UID` / `HOST_GID` (`id -u` / `id -g`), `HOST_SSH_USER`, `HOST_SSH_AUTH_SOCK` (the socket above) and a new random `HOSTBUD_DB_PASSWORD`. Check that `HOSTBUD_LOCAL_PORT` (9055) and the uncommon `HOSTBUD_DB_LOCAL_PORT` are free with `ss -ltn`; both bind to loopback only. The domain, Cloudflare and Tailscale settings are for M2 and can stay as placeholders.
5. **Deploy:** `make deploy`. The host's `/etc/ssh/ssh_host_{ed25519,ecdsa,rsa}_key.pub` must exist (drop the mount in `docker-compose.yml` for a key type your sshd doesn't have). `curl http://localhost:9055/api/health` answers `{"status":"ok"}`.
6. **Allow your address** (there is deliberately no web admin for this):
   ```sh
   docker compose exec hostbud-postgres psql -U hostbud -d hostbud \
     -c "INSERT INTO email_allowlist (email_normalized) VALUES ('you@example.com');"
   ```
7. **Open it** from any machine with SSH access to the host: `ssh -L 9055:localhost:9055 <host>`, then browse to `http://localhost:9055` (use `localhost`, not `127.0.0.1`: requests are checked against that origin), choose **Create account**, and you're signed in.

## Using hostbud
- The sidebar lists the host's tmux sessions (● attached / ○ detached, window count) and follows changes made anywhere (e.g. `tmux new -d -s x` in a real terminal) within one poll interval (`HOSTBUD_POLL_INTERVAL`, default 3s).
- **New session**: a directory (default `~`, `~/…` works), an optional name (default: the directory's name; `name-1`, `name-2`, … if taken) and an optional start command such as `htop` or `claude`.
- ✎ renames, ✕ kills (after a confirmation). Click a session to attach in the terminal; closing the tab only detaches — the session keeps running. After a detach (`prefix d`), the program exiting or a restart, use **Reconnect**.
- A banner explains host problems (sshd unreachable, tmux missing) with the fix; hostbud recovers by itself once they're fixed.
- On a phone, the list and the terminal take turns (← goes back to the list).

PostgreSQL credentials are supplied through the local, gitignored `.env` using the documented `HOSTBUD_DB_*` variables. They are not copied into tracked files, images or logs. For owner maintenance, use `docker compose exec hostbud-postgres psql ...` or the optional loopback-only maintenance port. Choose an uncommon `HOSTBUD_DB_LOCAL_PORT`, verify it is unused with `ss -ltn`, and never expose it on `0.0.0.0`, the Tailscale address or the public domain.

PostgreSQL is initialized as a fresh application database. The previous provisional SQLite database is not migrated because this deployment has not been used; its file, if present in the existing app data volume, is left untouched and ignored.

Example whitelist maintenance SQL (replace the placeholder address; never commit real addresses):

```sql
INSERT INTO email_allowlist (email_normalized, enabled, note, created_at, updated_at)
VALUES ('owner@example.com', TRUE, 'owner', now(), now())
ON CONFLICT (email_normalized) DO UPDATE
SET enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at;

-- Stop an address from signing in again (the account is kept):
UPDATE email_allowlist SET enabled = FALSE, updated_at = now() WHERE email_normalized = 'owner@example.com';

-- Lift sign-in throttling (e.g. after locking yourself out):
DELETE FROM login_rate_limits;
```

Run it with `docker compose exec hostbud-postgres psql -U hostbud -d hostbud` (the values of `HOSTBUD_DB_USER` / `HOSTBUD_DB_NAME`). Addresses are stored lowercased; sign-in throttling settings are in `.env.example`.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

## Development
Everything runs in containers; the host only needs Docker and `make` (`make help` lists targets). Tools run in long-lived `hostbud-tools-*` containers that `make` execs into (created on first use; `make tools-down` removes them).
- `make build` — build the Vue app (`web/dist`) and the Go binary with it embedded (`bin/hostbud`). `make go-build` alone embeds whatever is in `web/dist` and serves a placeholder page if the frontend was never built.
- `make lint test` — golangci-lint, eslint, vue-tsc; Go unit + integration tests and Vitest. Integration tests run against throwaway sshd containers (`hostbud-test-sshd`, `hostbud-test-sshd-notmux`) that `make test` starts and keeps running between runs; `make test-down` removes them. `make go-unit` runs only the Go unit tests.
- `make e2e` — simulated-user tests (Playwright, desktop Chromium + iPhone 13 Pro/WebKit) against a throwaway target in a separate `hostbud-e2e` Compose project, never the real host. It starts a fresh stack, runs, and tears it all down; failure traces, screenshots and videos land in `test/e2e/results/`. For a fast edit/test loop, `make e2e-up` keeps the stack running, `make e2e-run` runs the suite against it (`ARGS="-g smoke"` filters), and `make e2e-down` removes it. Images rebuild only when their inputs change.
- `make hooks` — install the gitleaks pre-commit hook.
- `make deploy` — build the image and (re)start `hostbud` + `hostbud-caddy` (`docker compose up -d --build`); `make logs` follows their logs. Check with `curl http://localhost:9055/api/health`.
- `make backup` — create a PostgreSQL backup in `./backups/` (gitignored).

## License
MIT
