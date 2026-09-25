# AGENTS.md — instructions for coding agents

You are building **hostbud**. Read `docs/ARCHITECTURE.md` (design, source of truth) and `docs/ROADMAP.md` (milestones) before starting. Work milestone by milestone, in order. Don't start a milestone until the previous one meets its acceptance criteria.

## Hard rules — public repository
This repo is **public**. Never commit:
- secrets, tokens, API keys, private keys, certificates;
- real hostnames, domains, IPs (including Tailscale 100.x addresses), usernames, home paths, SSH aliases, or file names from the owner's machines;
- `.env`, `data/`, `backups/`, databases, logs.

All such values come from environment variables. **If you need a config value that doesn't exist yet, add it to `.env.example` with a placeholder and a comment, read it from env in `internal/config`, and mention it in your summary so the owner can fill it in.** Use `example.com`, `server-a`, `/home/dev` in docs, tests and fixtures. Run `gitleaks` before every commit (`make gitleaks`; the pre-commit hook runs it via Docker).

## Scope (v1)
- **Single target: the host machine** hostbud runs on, reached over SSH from the container. Multi-machine support is deferred (ROADMAP *Later*), but keep `machine_id` in the schema, API and `sshx` so it can return without a rewrite.
- **Access paths:** (1) SSH port forward to `127.0.0.1:${HOSTBUD_LOCAL_PORT}` (plain HTTP) and (2) `https://${HOSTBUD_DOMAIN}` on the Tailscale IP. No app login: the host has a single user and the tailnet is trusted.
- **v2 is design-only.** Don't implement it; just keep the v1 obligations in ARCHITECTURE §10.
- No CI and no git remote for now; the repo will be published to GitHub later.

## Environment
- The dev machine **is** the deploy host. There is no separate staging environment. The app is deployed straight to the host with `make deploy` (`docker compose up -d --build`). The app holds no critical state (tmux sessions live on the targets), but:
  - never run destructive tmux commands (kill-session, kill-server) except on explicit user action behind a confirmation dialog;
  - never change users' tmux configs, shell configs, or `~/.ssh` files; `~/.ssh` is mounted read-only;
  - migrations are append-only and must not drop user data.
- **Dockerize everything we can.** Build, test, lint and gitleaks all run in containers via `make`; don't assume Go, gitleaks or golangci-lint are installed on the host.
- Local verification: `make test` (unit + integration against the `test/sshd` container), then `make deploy` and open `http://localhost:${HOSTBUD_LOCAL_PORT}`.

## Stack (don't substitute without updating docs/ARCHITECTURE.md)
- Backend: Go (latest stable), `log/slog`, `creack/pty`, `pkg/sftp`, `kevinburke/ssh_config` (display only; later, multi-machine), `modernc.org/sqlite`, embedded migrations, a WebSocket library (`coder/websocket` preferred).
- **Use the system `ssh` binary** with the generated config (`-F /data/ssh/config`). Do not use a Go SSH client library for connections.
- Frontend: Vue 3 + Vite + TypeScript, Pinia, Tailwind, Reka UI, `@xterm/xterm` + addons, `splitpanes`, `vue-draggable-plus`. No runtime CDN assets.
- Deploy: Docker Compose (hostbud + Caddy with `caddy-dns/cloudflare`), linux/amd64.

## Security checklist (must stay true)
- [ ] Caddy publishes ports only on `${TAILSCALE_IP}` (TLS) and `127.0.0.1:${HOSTBUD_LOCAL_PORT}` (plain HTTP, port-forward access), never `0.0.0.0`; hostbud publishes no ports.
- [ ] All remote commands are built from shell-quoted args via the single `sshx` helper; session names validated.
- [ ] `StrictHostKeyChecking yes`; the host's key is pinned from the read-only mounted `/etc/ssh/ssh_host_*_key.pub` (never trust-on-first-use); `BatchMode yes` for non-interactive calls. (Multi-machine, later: UI fingerprint confirmation.)
- [ ] WebSocket and state-changing requests check `Origin` against the allowlist: `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
- [ ] Destructive actions require UI confirmation.
- [ ] Container runs as non-root `${HOST_UID}:${HOST_GID}`; private keys are never mounted (agent socket only).
- [ ] No secrets or user paths in logs at info level.
- [ ] (v2) Hook endpoints use per-run tokens (stored hashed).

## Code conventions
- `internal/` packages as in ARCHITECTURE §14; keep `store` the only place with SQL.
- All state changes publish typed events on the `events` bus; the UI updates from events, not polling.
- Session creation goes through a single service function accepting `{machine, name, path, env, startCommand}`.
- Context-aware, timeout-bounded exec everywhere.
- Errors shown to the user are actionable (e.g. "tmux not found on the host — install with `sudo apt install tmux`").
- **Naming:** use `hostbud` as the application name and as the prefix for everything we name: Compose project (`name: hostbud`), services/containers (`hostbud`, `hostbud-caddy`, `hostbud-test-sshd`), images, volumes (`hostbud-data`), networks, SSH aliases (`hostbud-host`), env vars (`HOSTBUD_*`), the Go module/binary, and the frontend package. Don't use abbreviations like `hb`.
- Commits: conventional commits (`feat:`, `fix:`, `chore:` …), small and focused.

## Definition of done (per milestone)
1. Acceptance criteria in docs/ROADMAP.md pass.
2. `make lint test` green; gitleaks clean.
3. Docs updated (README usage, ARCHITECTURE if design changed, `.env.example` for new vars).
4. A short summary listing: what changed, any new env vars the owner must set, and manual steps on the host.
