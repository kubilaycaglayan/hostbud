# V2-M13 — Servers (other SSH targets): tasks

Goal: besides the built-in host, the owner can add other SSH servers ("targets") from the UI, give each a nickname, and pick the target when creating a session or adding a project from Browse files. Projects on another server show a chip with its nickname in their tree row. Everything (servers, their pinned host keys, projects) is stored on disk (PostgreSQL volume + the generated `/data/ssh` files) and survives restarts.

Design (v1 ARCHITECTURE §4.2–4.3, *Later* multi-machine, narrowed):
- A server is a `machines` row with `source = 'custom'`: id `s-<hex>`, ssh alias `hostbud-custom-<id>`, label (nickname), host name, port, user, and the host keys the owner confirmed. Append-only migration adds the connection columns.
- Host-key trust is never TOFU: the UI scans the host keys (`ssh-keyscan`), shows their SHA256 fingerprints, and only the keys the owner confirms are pinned (`known_hosts` under `HostKeyAlias hostbud-custom-<id>`).
- Authentication stays agent-only (the mounted agent socket); no passwords or private keys. A server that rejects the agent's key shows as unreachable with an actionable hint.
- The generated ssh config gains one `Host hostbud-custom-<id>` block per server, written before the `Host *` defaults, rewritten atomically on every add/remove.
- A runtime machine registry owns one inventory poller (and SFTP browser) per machine; adding a server starts its poller, removing it stops the poller, closes its masters and publishes `machine.removed`. Removing never touches tmux on the server, and is refused while projects still use the server.
- The v2 queue stays host-only (v2 ROADMAP *Later*: multi-machine runs need the target to reach `HOSTBUD_URL`); queue creation for another server's project is refused with a hint. *Superseded by the follow-up task T7 below: server projects take queues.*
- Frontend: an *Add server* header button opens the Servers dialog (list, add with fingerprint confirmation, remove with confirmation). New session and Browse files show a *Server* picker when more than one server exists. The tree shows every machine's sessions; a session's tree identity is its name on the host and `machine/name` elsewhere, so saved tree state stays valid. Project rows (and unplaced sessions) of another server carry a nickname chip.

## Progress

| Task | Status |
|---|---|
| T1 Store and sshx: server rows, dynamic ssh config, host-key scan | Done |
| T2 Machine registry and server API | Done |
| T3 Servers dialog (add / remove) | Done |
| T4 Multi-server tree and nickname chips | Done |
| T5 Server picker in New session and Browse files | Done |
| T6 Docs, verification and deploy | Done: `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and `/api/health` passed |
| T7 Safe Docker cleanup | Done: skipped because hostbud toolbox containers and warm test targets were active |

## Tasks

### T1 — Store and sshx: server rows, dynamic ssh config, host-key scan
Migration `0021_custom_machines.sql` adds `host_name`, `port`, `ssh_user`, `host_keys` to `machines`. Store: `CreateMachine`, `DeleteMachine` (custom only; refused with `ErrInUse` while projects reference it; removes its session links and capacity row), `RenameMachine`. sshx: `Client.SetTargets` rewrites config and known_hosts atomically; `Alias` resolves custom ids; ControlMaster recovery and `Close` work per alias; `Scan(ctx, host, port)` runs `ssh-keyscan` (argv only, validated host/port) and returns key type, key and SHA256 fingerprint.

Tests: U: config rendering with servers, known_hosts lines, host/user/port validation, fingerprint formatting, store create/delete/in-use. I: store migration against Postgres; sshx scans `hostbud-test-sshd-notmux`, pins it as a server, and runs a command through the new alias; a wrong pinned key fails with the host-key error. E2E: n/a (no route yet; T2 covers it through the API).

### T2 — Machine registry and server API
New `internal/machines` registry: one inventory + SFTP browser per machine, start/stop at runtime, snapshots in display order. `session.Service`, the API (`Machines`, file browser per machine) and the terminal handler (tmux version per machine) read it. Routes: `POST /api/machines/scan {host, port}`, `POST /api/machines {label, host, port, user, hostKeys}`, `PATCH /api/machines/{machine} {label}`, `DELETE /api/machines/{machine}` (custom only, 409 while projects use it). Event `machine.removed`. Queue creation for a non-host project → 400 with a hint.

Tests: U: registry add/remove/order, API validation (bad host, user, port, key, label; host can't be removed), routes.json. I: API against `hostbud-test-sshd-notmux` added as a server (scan → add → status → sessions route → remove). E2E: `servers.api.spec.ts` — scan, add the e2e second target, list it, create a session on it, refuse removal while a project uses it, remove. Adds `hostbud-e2e-target2` to the e2e compose.

### T3 — Servers dialog (add / edit nickname / remove)
Header *Add server* button → Servers dialog: list servers with status; add form (nickname, host, user, port) → *Check host key* shows fingerprints → *Trust and add*; edit a configured server's nickname inline and save through the rename API; remove with confirmation. Connection endpoint changes remain unavailable because they require scanning and confirming replacement host keys. machines store handles `machine.removed`; API client methods.

Tests: U: ServersDialog.spec.ts (scan, fingerprint confirm, add, nickname edit, error, remove confirm), machines store removal. I: n/a (frontend only; API covered by T2). E2E: `servers.spec.ts` desktop *Add a server from the header, edit its nickname and remove it*.

### T4 — Multi-server tree and nickname chips
Sessions of every machine appear in the tree. Session refs (`name` on the host, `machine/name` elsewhere) flow through select/kill/rename/split/hide/windows; the saved tree state accepts both forms. Projects place only same-machine sessions. Project rows and unplaced sessions of another server show a nickname chip. Opening, renaming and killing a remote session use its machine.

Tests: U: lib/tree.servers.spec.ts (refs, placement per machine, saved order), stores/tree.servers.spec.ts, SessionTree.servers.spec.ts (project and loose-session chips, ref events, rename on the server). I: n/a (frontend only). E2E: `servers.spec.ts` *A project on another server shows its nickname chip and opens its session*.

### T5 — Server picker in New session and Browse files
New session and Browse files show a *Server* select when more than one machine exists (host first, default host). The file browser lists the chosen server's files and adds projects there; the projects store loads every machine's projects.

Tests: U: ServerPicker.spec.ts (New session and Browse files pickers; host-only hides the picker; the server's projects only), projects store multi-machine load (tree.spec.ts, FileBrowserDialog.spec.ts). I: n/a (frontend only). E2E: `servers.spec.ts` *New session and Browse files ask for the server*.

### T6 — Docs, verification and deploy
ARCHITECTURE §4 and §9 (servers, routes), README usage, this checklist. `make lint test`, e2e type-check, `make gitleaks`, `make deploy`, `/api/health`. Browser e2e stays on demand.

### T7 — Safe Docker cleanup
v2 Rules and v1 M7 T15: `make docker-clean` (no `CACHE=1`) only when nothing is in use; otherwise skip and record. Tests/E2E: n/a (operations only).

## T7 (follow-up, 2026-10-04) — Queues on servers

A server's project takes queues. The service lists and warns across machines (shared directories only on the same machine); the dispatcher dispatches, recovers and releases session links per machine, and checks each machine's inventory snapshot only against that machine's runs (before, a server's snapshot could end host runs). The starter sets `HOSTBUD_URL` for server runs to `HOSTBUD_SERVER_HOOK_BASE_URL`, else `https://${HOSTBUD_DOMAIN}`; with neither the run fails with that hint. The Queue panel uses the queue's machine for sessions, kill, open and photo upload, and shows the server's nickname; *Create queue* on a server session saves the project on that server.

Tests: U: config `TestLoadServerHookBaseURL`, store `TestQueuesOnServers`, queue `TestServiceQueuesOnServers`, `TestStartOnAServerUsesTheServerHookURL`, `TestServerQueueRunsOnTheServer`, `TestExistingSessionIdle` (per machine), QueuePanel.spec.ts *runs a server queue on its server*. I: `TestIntegrationQueueOnAServer` (API against PostgreSQL), deploytest (`HOSTBUD_SERVER_HOOK_BASE_URL` only reaches hostbud). E2E: `servers.api.spec.ts` *a server project's queue runs its item on the server* (and the creation in *server API*), `servers.spec.ts` *Create queue on a server session*.
