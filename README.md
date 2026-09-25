# hostbud

Manage tmux sessions across all your machines from one web UI: discover SSH hosts, browse directories, organize them as projects, and attach to sessions in a full browser terminal. Built for terminal-first and agentic-coding workflows. Self-hosted and tailnet-only.

> Status: early development. See [ROADMAP.md](ROADMAP.md).

## How it works
- Runs in Docker on one host, behind Caddy, reachable only on your Tailscale tailnet.
- Uses your existing `~/.ssh/config` and ssh-agent (keys never enter the container).
- Reads tmux state from each active machine; stores only metadata (connections, projects, layout).

## Quick start
1. Prerequisites on the host: Docker + Compose, Tailscale, `sshd` running (hostbud reaches the host over SSH too), a stable ssh-agent socket with your keys loaded, and your own public key in `~/.ssh/authorized_keys`.
2. Cloudflare: create an `A` record for your subdomain → the host's Tailscale IP, **DNS only (grey cloud)**. Create an API token with `Zone:DNS:Edit` for that zone.
3. `cp .env.example .env` and fill it in.
4. `make deploy`
5. Open `https://<your subdomain>` from a device on your tailnet.

See [ARCHITECTURE.md](ARCHITECTURE.md) for details.

## License
MIT
