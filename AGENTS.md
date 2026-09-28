# AGENTS.md — instructions for coding agents

You are building **hostbud**. Read `docs/ARCHITECTURE.md` (design, source of truth) and `docs/ROADMAP.md` (milestones) before starting. Work milestone by milestone, in order. Don't start a milestone until the previous one meets its acceptance criteria (open owner items don't count; see below).

Regularly commit your changes during a goal and after you complete a goal.

## Owner items never block agents
The owner works through their own backlog on their own schedule. Nothing that waits on the owner (manual checks on their devices, approvals, decisions, go-aheads, answers to a question) may stop an agent's work.
- Never stop, pause or wait for the owner. Take the safe default the docs name (usually: don't do the risky action), record the item as **open** in the milestone's acceptance checklist (*Manual checks (owner)*) and in the summary, and continue with the next step or task.
- Open owner items don't block a task's **Done**, a checkpoint, a milestone's definition of done, or starting the next milestone. They stay listed as open until the owner ticks them.
- The safety rules still hold: a safe default never means doing the destructive or host-changing action without the owner. It means skipping that action and recording it.
- Only a direct instruction from the user in the current session overrides this (for example, "stop after this task").

## Hard rules — public repository
This repo is **public**. Never commit:
- secrets, tokens, API keys, private keys, certificates;
- real hostnames, domains, IPs (including Tailscale 100.x addresses), usernames, home paths, SSH aliases, or file names from the owner's machines;
- `.env`, `data/`, `backups/`, databases, logs.

All such values come from environment variables. **If you need a config value that doesn't exist yet, add it to `.env.example` with a placeholder and a comment, read it from env in `internal/config`, and mention it in your summary so the owner can fill it in.** Use `example.com`, `server-a`, `/home/dev` in docs, tests and fixtures. Run `gitleaks` before every commit (`make gitleaks`; the pre-commit hook runs it via Docker).

## Scope (v1)
- **Single target: the host machine** hostbud runs on, reached over SSH from the container. Multi-machine support is deferred (ROADMAP *Later*), but keep `machine_id` in the schema, API and `sshx` so it can return without a rewrite.
- **Access paths:** (1) SSH port forward to `127.0.0.1:${HOSTBUD_LOCAL_PORT}` (plain HTTP) and (2) `https://${HOSTBUD_DOMAIN}` on the Tailscale IP. Reachability alone isn't enough: the web app requires an account (whitelisted email + password, login throttling; ARCHITECTURE §8).
- **v2** follows its own roadmap: [docs/roadmap-v2/ROADMAP.md](docs/roadmap-v2/ROADMAP.md) (V2-M1 implemented; later milestones are opt-in). Keep the v1 obligations in ARCHITECTURE §10.
- No CI and no git remote for now; the repo will be published to GitHub later.

## Environment
- The dev machine **is** the deploy host. There is no separate staging environment. The app is deployed straight to the host with `make deploy` (`docker compose up -d --build`). The app holds no critical state (tmux sessions live on the targets), but:
  - never run destructive tmux commands (kill-session, kill-server) except on explicit user action behind a confirmation dialog;
  - never change users' tmux configs, shell configs, or `~/.ssh` files; `~/.ssh` is mounted read-only;
  - migrations are append-only and must not drop user data.
- **Dockerize everything we can.** Build, test, lint and gitleaks all run in containers via `make`; don't assume Go, gitleaks or golangci-lint are installed on the host.
- Local verification: `make test` (unit + integration against the `test/sshd` container), then `make deploy` and open `http://localhost:${HOSTBUD_LOCAL_PORT}`. `make e2e` (simulated user against a throwaway target) runs only on demand; see below.
- **E2E tests** (`make e2e`, docs/ARCHITECTURE.md §13.1) simulate a real user in the browser (desktop Chromium + iPhone 13 Pro/WebKit) against the throwaway `hostbud-e2e-target` — **never the real host's tmux**. Any new implementation or fix whose behavior e2e can cover must add or update its scenario **in the same commit**.
  - **E2E runs only on demand** (owner's decision, 2026-09-27; replaces the earlier "paused until M7" and v2 "once per milestone" rules). When implementing a feature, fixing a bug or working through a task, checkpoint or milestone, agents **never** run the full suite (`make e2e`, or `e2e-up`/`e2e-run` without a filter). The only e2e check per commit is that the suite type-checks (`tsc`). A full run happens only when the user asks for one in the current session. Focused scenarios (`make e2e-run ARGS="-g ..."`) also run only when the user asks, or while fixing failures from a run the user asked for.
    - A milestone's or task's definition of done never waits on an e2e run: E items count as **written** (type-checked) until an on-demand run passes them. Record the pending run as open in the acceptance checklist and summary, like an owner item.
    - After **every full `make e2e` run**, create its own dated report under `docs/e2e-triage/`, update the relevant triage index, and commit those report changes before another full run. Include the command/commit, passed/failed/skipped counts, elapsed time, every failed test title and profile, concise error, classification, and known or suspected shared cause. The runner clears `test/e2e/results` at the next run, so preserve and commit the report first. Fix the whole failure inventory as a batch, using focused scenarios and lower test layers, before another full run, and only if the user asks for one.
  - **v2 only: every milestone ends with a safe Docker cleanup** (its last task, after the deploy): `make docker-clean` without `CACHE=1`, only when nothing is in use; never prune globally or touch production containers, volumes, `backups/`, other projects or tmux sessions. Procedure: `docs/roadmap-v2/ROADMAP.md` (*Rules*) and v1 M7 T15.
  - **Never defer writing e2e** to a later task or to the end of a milestone (only running is on demand). If the behavior is reachable through the UI or the HTTP/WebSocket API through Caddy, it gets a scenario now (API-level before the UI exists).
  - Every task you plan or implement has an **E2E:** line (scenarios added, or why nothing is reachable), and every E2E item in `docs/roadmap/*-acceptance.md` is tagged with the task that adds it. When you split, add or reorder tasks, keep both true.
  - If a done task's E2E item is missing, fix that first, before starting the next task.
- **Three test layers per acceptance criterion.** Every criterion in `docs/roadmap/*-acceptance.md` has a coverage line with **U** (unit), **I** (integration, against `test/sshd` or the real deploy config) and **E** (e2e) tests, each naming the task that writes it. n/a needs a one-line reason, and "manual" is only for what automation can't observe.
  - Before starting a task, read which U/I/E tests the acceptance file assigns to it (and its **Tests:** / **E2E:** lines). Write them in the same commit(s) as the behavior.
  - When you plan tasks or a milestone, or change what a task builds, write or update those coverage lines. Never add a criterion without them.

## Stack (don't substitute without updating docs/ARCHITECTURE.md)
- Backend: Go (latest stable), `log/slog`, `creack/pty`, `pkg/sftp`, `kevinburke/ssh_config` (display only; later, multi-machine), PostgreSQL via `jackc/pgx` (embedded, append-only migrations), a WebSocket library (`coder/websocket` preferred).
- **Use the system `ssh` binary** with the generated config (`-F /data/ssh/config`). Do not use a Go SSH client library for connections.
- Frontend: Vue 3 + Vite + TypeScript, Pinia, Tailwind, Reka UI, `@xterm/xterm` + addons, `splitpanes`, `vue-draggable-plus`. No runtime CDN assets.
- Deploy: Docker Compose (hostbud + `hostbud-postgres` + Caddy with `caddy-dns/cloudflare`), linux/amd64.

## Security checklist (must stay true)
- [ ] Caddy publishes ports only on `${TAILSCALE_IP}` (TLS) and `127.0.0.1:${HOSTBUD_LOCAL_PORT}` (plain HTTP, port-forward access), never `0.0.0.0`; hostbud publishes no ports.
- [ ] All remote commands are built from shell-quoted args via the single `sshx` helper; session names validated.
- [ ] `StrictHostKeyChecking yes`; the host's key is pinned from the read-only mounted `/etc/ssh/ssh_host_*_key.pub` (never trust-on-first-use); `BatchMode yes` for non-interactive calls. (Multi-machine, later: UI fingerprint confirmation.)
- [ ] WebSocket and state-changing requests check `Origin` against the allowlist: `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
- [ ] Destructive actions require UI confirmation.
- [ ] Container runs as non-root `${HOST_UID}:${HOST_GID}`; private keys are never mounted (agent socket only).
- [ ] No secrets or user paths in logs at info level.
- [x] (v2) Hook endpoints use per-run tokens (stored hashed). Active since V2-M1 T3: `POST /api/hooks/{run}/{event}` only, 32-byte tokens stored as SHA-256, revoked when the run ends.

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
2. `make lint test` green; gitleaks clean; every acceptance criterion's unit, integration and e2e tests (per its coverage line) exist, added task by task, not at the end. Unit and integration tests pass; e2e tests type-check (an E item counts as written until an on-demand run passes it; no milestone requires `make e2e` green).
3. Docs updated (README usage, ARCHITECTURE if design changed, `.env.example` for new vars).
4. A short summary listing: what changed, any new env vars the owner must set, manual steps on the host, and the open owner items (backlog, not blockers).
