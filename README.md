# hostbud

<p align="center"><img src="web/public/favicon.svg" alt="hostbud" width="128"></p>

Manage the tmux sessions on your server from a web UI: browse directories, organize them as projects, and attach to sessions in a full browser terminal. Built for terminal-first and agentic-coding workflows. Self-hosted; reachable only via SSH port forward or your Tailscale tailnet. (Multi-machine support is planned.)

> Status: **v1** — hostbud is a single-host, self-hosted tmux web client. See [docs/ROADMAP.md](docs/ROADMAP.md).

Choose **Dark**, **Light** or **System** from the Account menu. System follows the device appearance, and the selected theme applies to the interface and open terminals.

## How it works
- Runs in Docker on one host, behind Caddy: plain HTTP on `127.0.0.1:9055` for SSH port forwarding, and HTTPS on your domain bound to the host's Tailscale IP (a Let's Encrypt certificate via Cloudflare DNS-01).
- Reaches the host's tmux over SSH using a dedicated key in your ssh-agent (private keys never enter the container) and pins the host's own SSH host keys.
- Accounts are email + password, limited to addresses you allow with plain SQL; sign-in is throttled.
- Stores accounts and UI metadata in PostgreSQL; tmux sessions live on the host.

## Quick start

Follow these steps on a fresh Debian or Ubuntu host. Replace only the example values shown; don't put real host details in tracked files.

1. **Install the host prerequisites.** Install [Docker Engine and Compose v2 on Debian](https://docs.docker.com/engine/install/debian/) or [Ubuntu](https://docs.docker.com/engine/install/ubuntu/), then install `make`, OpenSSH server/client, and tmux with `sudo apt update && sudo apt install -y make openssh-server openssh-client tmux`. Install Tailscale using its [Linux instructions](https://tailscale.com/docs/install/linux), run `sudo tailscale up`, and sign this host and the devices that will use hostbud into the same tailnet. Confirm `docker compose version`, `make --version`, `systemctl is-active ssh`, `tmux -V`, and `tailscale ip -4` work. Ensure `/etc/ssh/ssh_host_ed25519_key.pub`, `/etc/ssh/ssh_host_ecdsa_key.pub`, and `/etc/ssh/ssh_host_rsa_key.pub` exist; hostbud pins these public keys.
2. **Create a dedicated SSH key**, restricted to the Docker subnet and without forwarding:
   ```sh
   ssh-keygen -t ed25519 -N '' -C hostbud -f ~/.ssh/hostbud_ed25519
   echo "from=\"172.16.0.0/12\",no-agent-forwarding,no-port-forwarding,no-X11-forwarding $(cat ~/.ssh/hostbud_ed25519.pub)" >> ~/.ssh/authorized_keys
   ```
   The default Compose subnet is `172.29.55.0/24`, inside the restricted range. If you change `HOSTBUD_SUBNET`, keep it inside `172.16.0.0/12` and update this restriction to the same range.
3. **Load the key into a stable agent socket at boot.** With systemd's user `ssh-agent.socket`, the socket is `/run/user/<uid>/openssh_agent`. Create `~/.config/systemd/user/hostbud-ssh-add.service`:
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
   ```sh
   systemctl --user daemon-reload
   systemctl --user enable --now ssh-agent.socket hostbud-ssh-add.service
   sudo loginctl enable-linger "$USER"
   SSH_AUTH_SOCK="/run/user/$(id -u)/openssh_agent" ssh-add -l
   ```
   The final command should list the dedicated key. Set `HOST_SSH_AUTH_SOCK` to this socket in `.env`.
4. **Configure tailnet DNS.** Create a DNS-only A record in Cloudflare for the chosen subdomain, pointing to the host's Tailscale IPv4 (`tailscale ip -4`). The record must resolve to that address on your clients. See [Domain access over Tailscale](#domain-access-over-tailscale) for certificate and split DNS details.
5. **Configure hostbud.** From the repository, run `cp .env.example .env`. Set `HOST_UID` and `HOST_GID` to `id -u` / `id -g`, `HOST_HOME`, `HOST_SSH_USER`, `HOST_SSH_AUTH_SOCK`, `HOSTBUD_DOMAIN`, `TAILSCALE_IP`, `CLOUDFLARE_API_TOKEN`, and a new random `HOSTBUD_DB_PASSWORD`. Check that `HOSTBUD_LOCAL_PORT` and `HOSTBUD_DB_LOCAL_PORT` are unused. Protect the file and check the setup:
   ```sh
   chmod 600 .env
   make doctor
   ```
   The doctor prints check names and fixes, never secret values. Resolve failed checks before deploying.
6. **Deploy and verify health.** `make deploy` builds and starts hostbud, PostgreSQL, and Caddy. With the default port, run `curl http://localhost:9055/api/health`; it should return `{"status":"ok"}`. Check `make logs` if the containers don't become healthy.
7. **Allow the first account** (there is deliberately no web admin):
   ```sh
   docker compose exec hostbud-postgres psql -U hostbud -d hostbud \
     -c "INSERT INTO email_allowlist (email_normalized) VALUES ('you@example.com');"
   ```
8. **Verify both access paths.** From a client with SSH access, run `ssh -L 9055:localhost:9055 server-a` and open `http://localhost:9055` (use `localhost`, not `127.0.0.1`). Choose **Create account** with the allowlisted address. From another tailnet device, open `https://hostbud.example.com` and sign in. Check that the terminal can attach to a session.
9. **Optional identity gate.** If you want Tailscale identity allowlisting, follow [the optional allowlist setup](#tailscale-identity-allowlist-optional), restart Compose, and verify your login still works on the domain.
10. **Take the first backup.** Run `make backup`, confirm it reports a dump file, and copy that file to storage outside this host.

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
- *`make doctor` says the SSH agent has no key:* check `systemctl --user status hostbud-ssh-add.service`, confirm `HOST_SSH_AUTH_SOCK` names the live socket, then run `SSH_AUTH_SOCK=<socket> ssh-add ~/.ssh/hostbud_ed25519` and `make doctor` again.
- *The host's SSH key changed:* verify the host's key rotation before updating anything. If it was expected, run `make deploy` to repin the mounted public keys; if it was unexpected, investigate the host before reconnecting.
- *"host didn't answer" or SFTP timeout:* check that sshd is running and reachable, tmux is installed, and the agent has the hostbud key. The app retries terminal attachments; use **Retry now** after fixing the host.
- *Sign-in says "too many attempts":* wait for the displayed block interval before retrying. Repeated failures increase the delay; don't keep submitting passwords.
- *"Too many open terminals":* close unused tabs or split panes. Each attached terminal uses an SSH process on the host.
- *Tailscale returns 403:* confirm the device's Tailscale login or `tag:…` value is in `HOSTBUD_ALLOWED_TS_USERS` and that the LocalAPI socket is mounted when the optional gate is enabled.
- *The name doesn't resolve on a device:* check that device's DNS (`dig +short <domain> @1.1.1.1` returns the Tailscale IP). With Tailscale's MagicDNS on, a broken MagicDNS resolver on that device also breaks public names.
- *The name doesn't resolve on tailnet devices, but public DNS answers:* check Tailscale's admin console → DNS for a Split DNS (custom nameserver) entry for the domain and delete it. Such an entry names a DNS server to ask, not an address, and nothing on the host answers DNS. The Cloudflare record is all hostbud needs.
- *"request origin not allowed":* open hostbud exactly as `https://<HOSTBUD_DOMAIN>` or `http://localhost:<HOSTBUD_LOCAL_PORT>`.

### Tailscale identity allowlist (optional)

The tailnet already limits reachability. You can add a second gate for the domain site by setting `HOSTBUD_ALLOWED_TS_USERS` to a comma-separated list of Tailscale login names (or `tag:…` values for tagged nodes). Then uncomment `COMPOSE_FILE=docker-compose.yml:deploy/compose.tailscale.yml` in `.env` and set `TAILSCALED_SOCKET` to the host's tailscaled LocalAPI socket. Restart Compose after changing the list. With the override enabled, a missing or unreadable socket prevents startup; identity lookup failures return 403. The gate applies to the HTTPS domain path; SSH port-forward access through `localhost` remains exempt.

To find a login, run `tailscale whois <device-ip>` on a tailnet device and use the login shown for that node. The allowlist is an additional account gate: users still need a hostbud account.

## Using hostbud
- **Install on a phone:** open `https://${HOSTBUD_DOMAIN}` in Safari on the iPhone, choose **Share → Add to Home Screen**, then launch hostbud from its icon. The installed iOS app has a separate cookie store from Safari, so sign in once there. Android and desktop Chromium can install hostbud from the browser's install prompt.
- **Browse files:** use **Browse files** in the app bar, right after the host name, to open the file browser dialog. Navigate the host over SFTP, show or hide dotfiles, create a folder, save the current directory as a project, or start a session there. The path bar's FolderPlus adds the directory being shown; it changes to FolderOpen when that path is already a project. Directory rows also use folder-plus to add a project and folder-open for an existing project. The browser requires the host's SSH/SFTP access. It can list, inspect and create folders; it has no remote delete or rename action.
- The sidebar groups host sessions under saved projects or **Other sessions** (● attached / ○ detached) and follows changes made anywhere (e.g. `tmux new -d -s x` in a real terminal) within one poll interval (`HOSTBUD_POLL_INTERVAL`, default 3s). Drag projects and sessions to set an order saved to your account. Use **Save as project** for an unmatched session to save its directory without changing the running tmux session.
- **New session**: a directory (default `~`, `~/…` works), an optional name (default: the directory's name; if the chosen name is taken, hostbud opens the first free `name-1`, `name-2`, … and shows an info toast) and an optional start command such as `htop` or `claude`. Renaming to a taken name remains an error.
- ✎ renames, ✕ kills (after a confirmation); the ⋯ menu opens a session in a split.
- **Hide vs. remove:** Hide is only your account's tree preference. **Remove project…** deletes the shared project entry and its recent-command history for every account; the folder and its tmux sessions stay untouched, and sessions move to the next matching project or **Other sessions**. Removal asks for confirmation.
- Starting a session in a project offers that project's recent commands. Choose a suggestion or enter a command, then press **Create**; opening the picker or selecting a suggestion never runs it. Each project keeps its 20 newest distinct commands; using one again moves it to the front. There is no manual history-clear action.
- A banner explains host problems (sshd unreachable, tmux missing) with the fix; hostbud recovers by itself once they're fixed.

### Customizing the tree

Drag projects and sessions, or focus a row and use Alt+↑/↓, to change their order. New rows append; hostbud does not sort by name or recent activity. Collapse a project or **Other sessions** with its chevron. Expand a session to see its tmux windows, then expand split windows to see panes; selecting a window or pane switches the attached terminal.

Rename from the pencil, row menu, F2, or the command palette. Renaming a session from a real terminal looks like the old session ended and a new one appeared. **Hide** and **Show hidden** change only your account's tree. Pin projects to keep them in the **Pinned** section; drag or use Alt+↑/↓ to order projects within their section. Order, pins, hidden rows, collapsed groups and expanded windows are saved to your account and survive reloads and hostbud restarts.

### Command palette

Press ⌘K (Mac) or Ctrl+Shift+K to search sessions, loaded windows, projects and actions. Ctrl+K opens the palette outside a terminal; in a terminal, it stays with the shell. Results follow tree order when scores tie. Hidden sessions are marked. The palette can rename, hide or pin tree rows, change the theme, create sessions and run app actions; killing a session still asks for confirmation. Use the **Command palette** header button on touch screens.

### Keyboard shortcuts

Press ⌘/ (Mac), Ctrl+Shift+/ or `?` to open **Keyboard shortcuts**. ⌘⇧E / Ctrl+Shift+E moves focus between the tree and terminal. Ctrl+Shift+] / [ switches hostbud tabs; Ctrl+Shift+D toggles between the two most recently selected hostbud tabs. Tree-specific keys work only when a tree row has focus. Ctrl+K inside the terminal remains the shell's kill-to-end-of-line shortcut.

### Theme

Choose **Dark**, **Light** or **System** from the Account menu or command palette. The choice applies to the interface and open terminals and is saved to your account. Programs that picked their own colors for the old theme (Codex's prompt box, for example) keep them until restarted; hostbud still keeps their text readable by enforcing a minimum contrast, and restarting the program (for Codex, quit and `codex resume`) or using a fixed Dark/Light theme gives it matching colors. Before sign-in, the browser uses its own last theme mirror to avoid a flash; a new account starts in System mode.

### Terminal
- **Tabs:** click a session to open it in a tab (clicking it again brings its tab back); **New session** opens its session in a new tab. Tabs stay attached in the background, so switching is instant. Close a tab with ×, a middle click, or Delete on the focused tab: that only detaches the view, and the session keeps running. Drag a tab to change the order (on a phone, touch and hold it first); the order is saved with your tabs, and new tabs still open at the end. Renaming a session relabels its tabs; a session killed anywhere closes its tabs with a notice. Up to 16 terminals can be open at once, and each holds its own connection to the host.
- **Splits:** a pane's **Split right** / **Split down** (or a list row's ⋯ → **Open in split right/down**) opens a session, or a new one, beside it: up to 4 panes per tab, nested as you like. Click a pane to type into it; drag a divider to resize (the tmux windows follow). × in a pane's header closes just that pane.
- Your tabs, splits, divider positions and focused pane are saved to your account and come back after a reload or a hostbud restart; tabs of sessions that ended meanwhile are dropped.
- **Reconnect:** if the connection drops (Wi-Fi, sleep, a hostbud restart), the terminal re-attaches by itself ("Reconnecting…", with **Retry now**); keys typed meanwhile are dropped. After a detach (`prefix d`) or the program exiting, use **Reconnect**.
- **Mac editing keys:** Option+Backspace deletes a word, Cmd+Backspace deletes to the start of the line, Option+←/→ move by word, Cmd+←/→ jump to the start/end of the line.
- **Option-click** (Alt-click elsewhere) in the terminal moves the program's caret to the clicked character, also across the lines of a multiline prompt such as Codex's or Claude Code's. hostbud does it with left/right arrow keys, so it works wherever those keys move the caret.
- **Copy and paste:** select with the mouse, then Ctrl+Shift+C (Cmd+Shift+C, or Cmd+C on a Mac) or right-click → **Copy**. Ctrl+C still interrupts the program. Paste with Ctrl+Shift+V (Cmd+V on a Mac) or right-click → **Paste**; a multi-line paste into bash, zsh, vim or Claude Code arrives as one bracketed paste and doesn't run line by line.
  - When a program captures the mouse (tmux `set -g mouse on`, vim, htop), hold **Shift** while dragging (**Option** on a Mac) to select anyway, and Shift/Option+right-click for the menu.
  - Text copied inside the terminal reaches your browser clipboard over OSC 52: tmux copy-mode yanks work with tmux's defaults; for vim, Claude Code and other programs *inside* tmux, add `set -g set-clipboard on` to your `~/.tmux.conf` (hostbud never changes it). Programs can't read your clipboard: OSC 52 queries are ignored.
  - The browser allows clipboard access only on `http://localhost:…` (the port forward) and the HTTPS domain, not on a plain-HTTP LAN address.
- **Search:** Ctrl+Shift+F (Cmd+F on a Mac) or 🔍 searches the terminal's output since you attached, with Match case and Regex; Enter/Shift+Enter jump between matches, Escape closes. For older tmux history use copy mode (`prefix [`, then `?`). The mouse wheel scrolls back through the same output when tmux's `mouse` is off.
- **Links:** click (tap) a URL in the terminal to open it in a new tab; only `http`/`https` links open. OSC 8 hyperlinks (`ls --hyperlink`, Claude Code) show their real target on hover; inside tmux they need `set -as terminal-features ',xterm*:hyperlinks'` in your `~/.tmux.conf`.
- **On a phone,** the project tree is the home screen and opens as a drawer over the terminal from the **Show sidebar** icon. **New session** and **Browse files** are icon buttons in the app bar, after the host name, on phones and desktop alike. On desktop, the header's **Hide sidebar** / **Show sidebar** icon toggles it, and its state survives reload. A compact tab bar switches tabs; a split tab shows one pane at a time, with a "Pane n of m" button to switch. Tap the terminal (or ⌨) to bring up the keyboard; the terminal shrinks to stay above it, and rotating the phone resizes the tmux window. The key bar supplies Esc, Tab, Ctrl, Alt, arrows and common symbols; **Scroll history** opens tmux scrollback controls.

### Using hostbud on a phone

Open **Show sidebar** to switch projects or sessions without detaching the terminal. On the key bar, tap Ctrl or Alt before a key or typed character; double-tap to lock a modifier. Arrow keys follow the running program's cursor mode. Use the terminal scroll controls to browse shared pane history with tmux copy mode (tmux 2.4 or newer). Done, Bottom or typing exits copy mode. Long-press a word in terminal output to select it, then tap **Copy** in the terminal header.

To install on iPhone, open `https://${HOSTBUD_DOMAIN}` in Safari and choose **Share → Add to Home Screen**. Sign in once in the installed app; iOS keeps its cookies separate from Safari. Android and desktop Chromium can install from the browser prompt. Updates take over on the next app launch. If hostbud is offline at startup, the app shell shows **Can't reach hostbud** and retries. Installation and offline launch require the HTTPS domain or `localhost`; plain-HTTP LAN addresses are not secure contexts.

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

## Backup and restore

Run `make backup` to create `backups/hostbud-<UTC timestamp>.dump`, a private PostgreSQL custom-format dump. The command creates `backups/` with mode 700, sets the dump to mode 600, and prints its filename and size. It uses the runtime `pg_dump`; if that client is older than the database server, backup stops with an install hint.

Check a dump without touching the configured database:

```sh
make restore-check FILE=backups/hostbud-20260927T120000000000000Z.dump
```

The check restores to a temporary database, verifies the migration version and counts of users, allowlist entries, projects and UI state, then drops the temporary database. For a real restore, use `make restore FILE=…`; it validates the dump, shows the target database and requires typing its name. Non-interactive use must pass `CONFIRM=<database>`. Restore takes a safety backup first, stops only the app while applying the dump in one transaction, starts it again, and waits for health.

A database dump does not include `hostbud-data` (regenerated SSH configuration and ControlMaster sockets plus `auth-key` used for rate-limit hashing), Caddy certificates (`hostbud-caddy-data`), or `.env`. Losing `auth-key` resets throttling buckets; Caddy can reissue certificates, subject to Let's Encrypt rate limits. Keep `.env` in a password manager and copy `backups/` off the host.

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
