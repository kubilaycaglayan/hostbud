# hostbud

Manage the tmux sessions on your server from a web UI: browse directories, organize them as projects, and attach to sessions in a full browser terminal. Built for terminal-first and agentic-coding workflows. Self-hosted; reachable only via SSH port forward or your Tailscale tailnet. (Multi-machine support is planned.)

> Status: **M5** — hostbud includes phone controls and can be installed as a home-screen app. See [docs/ROADMAP.md](docs/ROADMAP.md).

## How it works
- Runs in Docker on one host, behind Caddy: plain HTTP on `127.0.0.1:9055` for SSH port forwarding, and HTTPS on your domain bound to the host's Tailscale IP (a Let's Encrypt certificate via Cloudflare DNS-01).
- Reaches the host's tmux over SSH using a dedicated key in your ssh-agent (private keys never enter the container) and pins the host's own SSH host keys.
- Accounts are email + password, limited to addresses you allow with plain SQL; sign-in is throttled.
- Stores accounts and UI metadata in PostgreSQL; tmux sessions live on the host.

## Quick start
1. **Prerequisites on the host:** Docker + Compose (nothing else — build, tests and lint run in containers), `sshd` running (hostbud reaches the host over SSH: `sudo apt install openssh-server`), tmux, a systemd user session, and [Tailscale](#domain-access-over-tailscale) for the domain.
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
4. **Configure:** `cp .env.example .env` and set at least `HOST_UID` / `HOST_GID` (`id -u` / `id -g`), `HOST_SSH_USER`, `HOST_SSH_AUTH_SOCK` (the socket above) a new random `HOSTBUD_DB_PASSWORD`, and the domain settings `HOSTBUD_DOMAIN`, `TAILSCALE_IP` and `CLOUDFLARE_API_TOKEN` ([below](#domain-access-over-tailscale)). Check that `HOSTBUD_LOCAL_PORT` (9055) and the uncommon `HOSTBUD_DB_LOCAL_PORT` are free with `ss -ltn`; both bind to loopback only. Keep `.env` private: `chmod 600 .env`.
5. **Deploy:** `make deploy`. The host's `/etc/ssh/ssh_host_{ed25519,ecdsa,rsa}_key.pub` must exist (drop the mount in `docker-compose.yml` for a key type your sshd doesn't have). `curl http://localhost:9055/api/health` answers `{"status":"ok"}`; within a minute Caddy has the domain's certificate (`make logs` shows `certificate obtained successfully`).
6. **Allow your address** (there is deliberately no web admin for this):
   ```sh
   docker compose exec hostbud-postgres psql -U hostbud -d hostbud \
     -c "INSERT INTO email_allowlist (email_normalized) VALUES ('you@example.com');"
   ```
7. **Open it** from any machine with SSH access to the host: `ssh -L 9055:localhost:9055 <host>`, then browse to `http://localhost:9055` (use `localhost`, not `127.0.0.1`: requests are checked against that origin), choose **Create account**, and you're signed in. Or, from any device on your tailnet, open `https://<HOSTBUD_DOMAIN>`.

## Domain access over Tailscale
The domain works only inside your tailnet: its DNS record points at the host's Tailscale address, and Caddy listens for it on that address only. Nothing is exposed to the internet.

1. **Tailscale** on the host and on each device (phone, laptop), signed in to the same tailnet. `tailscale ip -4` on the host gives `TAILSCALE_IP` (100.x.y.z).
2. **DNS record** in Cloudflare for a subdomain, e.g. `hostbud.example.com`: type `A`, content = `TAILSCALE_IP`, proxy status **DNS only** (grey cloud). Never proxied (orange) and never a Cloudflare Tunnel: both would publish the app on the internet. The name resolves publicly, but the address only routes inside the tailnet.
3. **API token** for the certificate (ACME DNS-01; the address isn't reachable for HTTP challenges): Cloudflare dashboard → My Profile → API Tokens → Create Token → *Edit zone DNS* template, permission **Zone → DNS → Edit**, zone resources **only the domain's zone**. Put it in `.env` as `CLOUDFLARE_API_TOKEN`; only the Caddy container receives it. `ACME_EMAIL` is optional.
4. **Boot order:** Docker can only publish on `TAILSCALE_IP` once that address exists. If `hostbud-caddy` fails to start after a reboot (`cannot assign requested address`), either let the kernel bind addresses before they exist:
   ```sh
   echo 'net.ipv4.ip_nonlocal_bind = 1' | sudo tee /etc/sysctl.d/60-hostbud.conf && sudo sysctl --system
   ```
   or start Docker after Tailscale (`sudo systemctl edit docker.service` → `[Unit]` `After=tailscaled.service` `Wants=tailscaled.service`).
5. `make deploy`, then open `https://<HOSTBUD_DOMAIN>` on a tailnet device and sign in.

**Troubleshooting**
- *No certificate:* `make logs`. `could not determine zone` or `403` errors point at the token (permission or zone); certificates are kept in the `hostbud-caddy-data` volume, so restarts don't re-issue.
- *The name doesn't resolve on a device:* check that device's DNS (`dig +short <domain> @1.1.1.1` returns the Tailscale IP). With Tailscale's MagicDNS on, a broken MagicDNS resolver on that device also breaks public names.
- *The name doesn't resolve on tailnet devices, but public DNS answers:* check Tailscale's admin console → DNS for a Split DNS (custom nameserver) entry for the domain and delete it. Such an entry names a DNS server to ask, not an address, and nothing on the host answers DNS. The Cloudflare record is all hostbud needs.
- *"request origin not allowed":* open hostbud exactly as `https://<HOSTBUD_DOMAIN>` or `http://localhost:<HOSTBUD_LOCAL_PORT>`.

## Using hostbud
- **Install on a phone:** open `https://${HOSTBUD_DOMAIN}` in Safari on the iPhone, choose **Share → Add to Home Screen**, then launch hostbud from its icon. The installed iOS app has a separate cookie store from Safari, so sign in once there. Android and desktop Chromium can install hostbud from the browser's install prompt.
- **Browse files:** use the folder-plus button in the app header to open the file browser dialog. Navigate the host over SFTP, show or hide dotfiles, create a folder, save the current directory as a project, or start a session there. Directory rows use folder-plus to add a project and folder-open for an existing project. The browser requires the host's SSH/SFTP access. It can list, inspect and create folders; it has no remote delete or rename action.
- The sidebar groups host sessions under saved projects or **Other sessions** (● attached / ○ detached) and follows changes made anywhere (e.g. `tmux new -d -s x` in a real terminal) within one poll interval (`HOSTBUD_POLL_INTERVAL`, default 3s). Drag projects and sessions to set an order saved to your account. Use **Save as project** for an unmatched session to save its directory without changing the running tmux session.
- **New session**: a directory (default `~`, `~/…` works), an optional name (default: the directory's name; `name-1`, `name-2`, … if taken) and an optional start command such as `htop` or `claude`.
- ✎ renames, ✕ kills (after a confirmation); the ⋯ menu opens a session in a split.
- Starting a session in a project offers that project's recent commands. Choose a suggestion or enter a command, then press **Create**; opening the picker or selecting a suggestion never runs it. Each project keeps its 20 newest distinct commands; using one again moves it to the front. There is no manual history-clear action.
- A banner explains host problems (sshd unreachable, tmux missing) with the fix; hostbud recovers by itself once they're fixed.

### Terminal
- **Tabs:** click a session to open it in a tab (clicking it again brings its tab back); **New session** opens its session in a new tab. Tabs stay attached in the background, so switching is instant. Close a tab with ×, a middle click, or Delete on the focused tab: that only detaches the view, and the session keeps running. Renaming a session relabels its tabs; a session killed anywhere closes its tabs with a notice. Up to 16 terminals can be open at once, and each holds its own connection to the host.
- **Splits:** a pane's **Split right** / **Split down** (or a list row's ⋯ → **Open in split right/down**) opens a session, or a new one, beside it: up to 4 panes per tab, nested as you like. Click a pane to type into it; drag a divider to resize (the tmux windows follow). × in a pane's header closes just that pane.
- Your tabs, splits, divider positions and focused pane are saved to your account and come back after a reload or a hostbud restart; tabs of sessions that ended meanwhile are dropped.
- **Reconnect:** if the connection drops (Wi-Fi, sleep, a hostbud restart), the terminal re-attaches by itself ("Reconnecting…", with **Retry now**); keys typed meanwhile are dropped. After a detach (`prefix d`) or the program exiting, use **Reconnect**.
- **Mac editing keys:** Option+Backspace deletes a word, Cmd+Backspace deletes to the start of the line, Option+←/→ move by word, Cmd+←/→ jump to the start/end of the line.
- **Copy and paste:** select with the mouse, then Ctrl+Shift+C (Cmd+Shift+C, or Cmd+C on a Mac) or right-click → **Copy**. Ctrl+C still interrupts the program. Paste with Ctrl+Shift+V (Cmd+V on a Mac) or right-click → **Paste**; a multi-line paste into bash, zsh, vim or Claude Code arrives as one bracketed paste and doesn't run line by line.
  - When a program captures the mouse (tmux `set -g mouse on`, vim, htop), hold **Shift** while dragging (**Option** on a Mac) to select anyway, and Shift/Option+right-click for the menu.
  - Text copied inside the terminal reaches your browser clipboard over OSC 52: tmux copy-mode yanks work with tmux's defaults; for vim, Claude Code and other programs *inside* tmux, add `set -g set-clipboard on` to your `~/.tmux.conf` (hostbud never changes it). Programs can't read your clipboard: OSC 52 queries are ignored.
  - The browser allows clipboard access only on `http://localhost:…` (the port forward) and the HTTPS domain, not on a plain-HTTP LAN address.
- **Search:** Ctrl+Shift+F (Cmd+F on a Mac) or 🔍 searches the terminal's output since you attached, with Match case and Regex; Enter/Shift+Enter jump between matches, Escape closes. For older tmux history use copy mode (`prefix [`, then `?`). The mouse wheel scrolls back through the same output when tmux's `mouse` is off.
- **Links:** click (tap) a URL in the terminal to open it in a new tab; only `http`/`https` links open. OSC 8 hyperlinks (`ls --hyperlink`, Claude Code) show their real target on hover; inside tmux they need `set -as terminal-features ',xterm*:hyperlinks'` in your `~/.tmux.conf`.
- **On a phone,** the project tree is the home screen and opens as a drawer over the terminal from ☰. A compact tab bar switches tabs; a split tab shows one pane at a time, with a "Pane n of m" button to switch. Tap the terminal (or ⌨) to bring up the keyboard; the terminal shrinks to stay above it, and rotating the phone resizes the tmux window. The key bar supplies Esc, Tab, Ctrl, Alt, arrows and common symbols; **Scroll history** opens tmux scrollback controls.

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
- `make docker-clean` — free hostbud's Docker disk: untagged images, the e2e stack and its images, the toolbox containers (`CACHE=1` also prunes BuildKit cache older than 72h, for every project on the host). Builds keep Go/pnpm caches in BuildKit cache mounts, so a deploy adds only a few MB; `make deploy` and the test scripts drop the images they replace.
- `make backup` — create a PostgreSQL backup in `./backups/` (gitignored).

## License
MIT
