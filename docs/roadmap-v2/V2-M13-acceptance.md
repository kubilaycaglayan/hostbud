# V2-M13 — Servers (other SSH targets): acceptance checklist

## Criteria

- [ ] **1 Servers are stored and pinned.** A server row keeps nickname, host, port, user and the confirmed host keys across restarts; the generated ssh config and known_hosts include it; an unconfirmed or changed key is refused. — U: T1 (sshx config/known_hosts, store) · I: T1 (store migration, sshx scan + pinned exec + wrong key) · E: T2 (servers.api.spec.ts).
- [ ] **2 Add, rename and remove through the API.** Scan returns fingerprints; add validates every field and starts tracking; removal is custom-only, refused while projects use the server, never touches tmux. — U: T2 (api validation, registry) · I: T2 (API against the test target) · E: T2 (servers.api.spec.ts).
- [ ] **3 Servers dialog.** The header *Add server* button opens a dialog that adds a server after the owner confirms its fingerprints, lists servers with status, and removes after confirmation. — U: T3 (ServersDialog.spec.ts) · I: n/a (frontend only; API in criterion 2) · E: T3 (servers.spec.ts).
- [ ] **4 Multi-server tree.** Sessions of every server appear; project rows and unplaced sessions on another server show its nickname chip; opening, renaming and killing a remote session act on that server. — U: T4 (tree.spec.ts, tree store, ProjectTreeRow/SessionList) · I: n/a (frontend only) · E: T4 (servers.spec.ts).
- [ ] **5 Target picker.** New session and Browse files ask for the server when there is more than one; the session/project is created there. — U: T5 (SessionDialogs.spec.ts, FileBrowser.spec.ts) · I: T2 (session create on the added server) · E: T5 (servers.spec.ts).
- [ ] **6 Docs and deploy.** ARCHITECTURE/README updated; lint, tests, e2e type-check, gitleaks, deploy and health pass. — U/I/E: n/a (documentation/operations; T6 records the checks).

## E2E scenarios

- [ ] (T2) API *servers.api.spec.ts* — written and type-checked; run pending on demand.
- [ ] (T3, T4, T5) Desktop *servers.spec.ts* — written and type-checked; run pending on demand.
- [ ] (on demand) Full suite green on both profiles — open until requested, not a blocker.

## Manual checks (owner; backlog, not blockers)

- [ ] Add a real server from the deployed UI (its `authorized_keys` must accept the agent's key) and open a session on it (open).
- [ ] E2E scenarios have not been run; on-demand full run remains open.

## Definition of done

- [ ] Unit/integration tests pass; E2E scenarios written and type-checked.
- [ ] `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and health check pass.
- [ ] Docker cleanup done or safely skipped.
- [ ] Summary includes owner checks and pending on-demand E2E run.
