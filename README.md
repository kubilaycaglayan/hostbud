# hostbud

Manage the tmux sessions on your server from a web UI: browse directories, organize them as projects, and attach to sessions in a full browser terminal. Built for terminal-first and agentic-coding workflows. Self-hosted; reachable only via SSH port forward or your Tailscale tailnet. (Multi-machine support is planned.)

> Status: early development. See [ROADMAP.md](ROADMAP.md).

## How it works
- Runs in Docker on one host, behind Caddy: plain HTTP on `127.0.0.1:9055` for SSH port forwarding, and HTTPS on your domain bound to the host's Tailscale IP.
- Reaches the host's tmux over SSH using your ssh-agent (keys never enter the container).
- Stores only metadata (projects, layout); tmux sessions live on the host.

## Quick start
1. Prerequisites on the host: Docker + Compose (nothing else — build, tests and lint run in containers), Tailscale, `sshd` running (hostbud reaches the host over SSH too), a stable ssh-agent socket with your keys loaded, and your own public key in `~/.ssh/authorized_keys`.
2. Cloudflare: create an `A` record for your subdomain → the host's Tailscale IP, **DNS only (grey cloud)**. Create an API token with `Zone:DNS:Edit` for that zone.
3. `cp .env.example .env` and fill it in.
4. `make deploy`
5. Open `https://<your subdomain>` from a device on your tailnet, or from any machine with SSH access: `ssh -L 9055:localhost:9055 <host>` → `http://localhost:9055`.

See [ARCHITECTURE.md](ARCHITECTURE.md) for details.

## License
MIT
