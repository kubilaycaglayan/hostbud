# hostbud

Manage the tmux sessions on your server from a web UI: browse directories, organize them as projects, and attach to sessions in a full browser terminal. Built for terminal-first and agentic-coding workflows. Self-hosted; reachable only via SSH port forward or your Tailscale tailnet. (Multi-machine support is planned.)

> Status: early development. See [docs/ROADMAP.md](docs/ROADMAP.md).

## How it works
- Runs in Docker on one host, behind Caddy: plain HTTP on `127.0.0.1:9055` for SSH port forwarding, and HTTPS on your domain bound to the host's Tailscale IP.
- Reaches the host's tmux over SSH using your ssh-agent (keys never enter the container).
- Stores only metadata (projects, layout); tmux sessions live on the host.

## Quick start
1. Prerequisites on the host: Docker + Compose (nothing else — build, tests and lint run in containers), Tailscale, `sshd` running (hostbud reaches the host over SSH too), a stable ssh-agent socket with your keys loaded, and your own public key in `~/.ssh/authorized_keys`.
2. Cloudflare: create an `A` record for your subdomain → the host's Tailscale IP, **DNS only (grey cloud)**. Create an API token with `Zone:DNS:Edit` for that zone.
3. `cp .env.example .env` and fill it in.
4. `make deploy` (the host's `/etc/ssh/ssh_host_{ed25519,ecdsa,rsa}_key.pub` must exist; drop the mount in `docker-compose.yml` for any key type your sshd doesn't have).
5. Open `https://<your subdomain>` from a device on your tailnet, or from any machine with SSH access: `ssh -L 9055:localhost:9055 <host>` → `http://localhost:9055`.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

## Development
Everything runs in containers; the host only needs Docker and `make` (`make help` lists targets). Tools run in long-lived `hostbud-tools-*` containers that `make` execs into (created on first use; `make tools-down` removes them).
- `make build` — build the Vue app (`web/dist`) and the Go binary with it embedded (`bin/hostbud`). `make go-build` alone embeds whatever is in `web/dist` and serves a placeholder page if the frontend was never built.
- `make lint test` — golangci-lint, eslint, vue-tsc; Go tests and Vitest.
- `make e2e` — simulated-user tests (Playwright, desktop Chromium + iPhone 13 Pro/WebKit) against a throwaway target in a separate `hostbud-e2e` Compose project, never the real host. It starts a fresh stack, runs, and tears it all down; failure traces, screenshots and videos land in `test/e2e/results/`. For a fast edit/test loop, `make e2e-up` keeps the stack running, `make e2e-run` runs the suite against it (`ARGS="-g smoke"` filters), and `make e2e-down` removes it. Images rebuild only when their inputs change.
- `make hooks` — install the gitleaks pre-commit hook.
- `make deploy` — build the image and (re)start `hostbud` + `hostbud-caddy` (`docker compose up -d --build`); `make logs` follows their logs. Check with `curl http://localhost:9055/api/health`.
- `make backup` — copy the running app's SQLite database to `./backups/` (gitignored).

## License
MIT
