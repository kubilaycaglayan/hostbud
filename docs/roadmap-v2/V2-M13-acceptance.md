# V2-M13 — Servers (other SSH targets): acceptance checklist

## Criteria

- [x] **1 Servers are stored and pinned.** A server row keeps nickname, host, port, user and the confirmed host keys across restarts; the generated ssh config and known_hosts include it; an unconfirmed or changed key is refused. — U: T1 (sshx config/known_hosts, store) · I: T1 (store migration, sshx scan + pinned exec + wrong key) · E: T2 (servers.api.spec.ts).
- [x] **2 Add, rename and remove through the API.** Scan returns fingerprints; add validates every field and starts tracking; removal is custom-only, refused while projects use the server, never touches tmux. — U: T2 (api validation, registry) · I: T2 (API against the test target) · E: T2 (servers.api.spec.ts).
- [x] **3 Servers dialog.** The header *Add server* button opens a dialog that adds a server after the owner confirms its fingerprints, lists servers with status, and removes after confirmation. — U: T3 (ServersDialog.spec.ts) · I: n/a (frontend only; API in criterion 2) · E: T3 (servers.spec.ts).
- [x] **4 Multi-server tree.** Sessions of every server appear; project rows and unplaced sessions on another server show its nickname chip; opening, renaming and killing a remote session act on that server. — U: T4 (tree.servers.spec.ts, stores/tree.servers.spec.ts, SessionTree.servers.spec.ts) · I: n/a (frontend only) · E: T4 (servers.spec.ts).
- [x] **5 Target picker.** New session and Browse files ask for the server when there is more than one; the session/project is created there. — U: T5 (ServerPicker.spec.ts) · I: T2 (session create on the added server) · E: T5 (servers.spec.ts).
- [x] **7 Queues on servers (T7, follow-up).** A server's project takes queues; their runs start on that server with `HOSTBUD_URL` = `HOSTBUD_SERVER_HOOK_BASE_URL` or `https://${HOSTBUD_DOMAIN}` (neither: the run fails with a hint); slots, recovery, session links and session-ended checks work per machine; the Queue panel acts on the queue's machine and shows its nickname. — U: T7 (config, store, service, starter, dispatcher, QueuePanel.spec.ts) · I: T7 (`TestIntegrationQueueOnAServer`, deploytest) · E: T7 (servers.api.spec.ts, servers.spec.ts).
- [x] **6 Docs and deploy.** ARCHITECTURE/README updated; lint, tests, e2e type-check, gitleaks, deploy and health pass. — U/I/E: n/a (documentation/operations; T6 records the checks).

## E2E scenarios

- [x] (T2) API *servers.api.spec.ts* — written and type-checked; run pending on demand.
- [x] (T3, T4, T5) Desktop *servers.spec.ts* — written and type-checked; run pending on demand.
- [x] (T7) API *servers.api.spec.ts* *a server project's queue runs its item on the server* and desktop *servers.spec.ts* *Create queue on a server session* — written and type-checked; run pending on demand.
- [ ] (on demand) Full suite green on both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Add a real server from the deployed UI (its `authorized_keys` must accept the agent's key) and open a session on it (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.
- [ ] (T7) Run a queue item on a real server: it must reach `https://${HOSTBUD_DOMAIN}` (tailnet) or set `HOSTBUD_SERVER_HOOK_BASE_URL` (open).

## Definition of done

- [x] Unit/integration tests pass; E2E scenarios written and type-checked.
- [x] `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and health check pass.
- [x] Docker cleanup was safely skipped because hostbud toolbox containers and warm test targets were active; production volumes were not touched.
- [x] Summary includes owner checks and pending on-demand E2E run.
