# M4 — Projects and file browser: tasks

Goal: browse the host's filesystem over SFTP, save directories as projects, create sessions in project directories, group live sessions under the best matching project, and offer project-scoped recent start commands.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m4--projects-and-file-browser) · checklist: [M4-acceptance.md](M4-acceptance.md).

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 SFTP service and API | Done |
| T2 Project and recent-command persistence | Done |
| T3 Project API, events and session placement service | Done |
| T4 File browser and project actions | Done |
| T5 Project tree and unmatched sessions | Done |
| T6 Recent start commands | Done |
| T7 Documentation, audit and release | Planned |

Tasks proceed in order. Before implementation, re-check M3's acceptance gates and the M4 acceptance coverage below. Every behavior-changing task writes its unit, integration and e2e coverage in the same commit. Run `make lint test` and `make gitleaks` per repository rules; from M4 onward do not run `make e2e`, `e2e-up` or `e2e-run` before M7. Each task that changes e2e files only type-checks the suite with `tsc` as part of lint. E2E scenarios are authored now and run once with the full suite in M7.

## T1 — SFTP service and API

- Add `internal/fsbrowse` operations for home, list, stat and mkdir over `pkg/sftp` via the established `sshx` system-ssh configuration. Reuse one lazy client for the active host with an idle timeout; honor request cancellation and bounded operations.
- Preserve path bytes/characters through SFTP without constructing remote shell commands. Normalize paths as target absolute paths; use path-component containment and reject invalid child directory names. Do not add remote delete or rename.
- List entries with directory/type metadata; sort directories before other entries and each group by stable name ordering. Return hidden entries only when requested. Resolve symlink targets only when needed and report loops, broken targets and permission errors as row-level states.
- Wire authenticated endpoints from ARCHITECTURE §9: `GET /api/machines/:id/fs/home`, `GET /api/machines/:id/fs?path=&hidden=`, `GET /api/machines/:id/fs/stat?path=`, `POST /api/machines/:id/fs/mkdir`. Validate the built-in machine id, response limits, query/body shape and Origin on mkdir. Keep errors actionable and redact paths from info logs.
- No SQL or direct process execution outside the approved packages.

**Tests:** U (Go): path normalization and hostile-but-valid names, containment, sort order, hidden filtering, symlink handling, cancellation and error mapping; API auth/Origin/validation/method tests. I (`test/sshd`): home/list/stat/mkdir with spaces, Unicode, hidden file, symlink and missing paths; timeout/disconnect cleanup; unauthorized calls do not open remote SFTP.

**E2E:** add API-level **(T1) Browse home and navigate**, **Hidden entries and symlinks**, **No destructive file actions**, **Filesystem API access control**, and **SFTP unavailable recovery** in [M4-acceptance.md](M4-acceptance.md#e2e-scenarios-make-e2e-simulated-user); type-check only, do not run.

**Done:** SFTP API works against `test/sshd`, is authenticated/Origin checked, creates only child directories, and the e2e scenarios compile.

## T2 — Project and recent-command persistence

- Inspect the existing project/session repository surface and migration history before adding migration SQL. Add append-only schema or columns/indexes only where required by the architecture contract: `projects` are unique by `(machine_id, path)`; `session_links` remain keyed by `(machine_id, session_name)`; `recent_commands` are scoped through project id.
- Implement project and link repository operations and recent-command list/upsert operations in `internal/store`; all SQL stays in `store`. Preserve `machine_id` in keys and queries despite the single-host v1 UI.
- Define path/name normalization at the repository boundary; duplicate paths resolve to the existing project. Keep ordering deterministic. Bound recent command history (document the chosen small maximum in ARCHITECTURE and tests) and validate empty/oversize command values.
- Ensure renaming a linked session can update its session link atomically, and provide cleanup when a session is observed ended. Migration is additive and safe with M1–M3 data.

**Tests:** U (Go): repository validation and ordering; migration sequence/no destructive statements; link upsert, rename, cleanup and project scoping. I (PostgreSQL): migrate a populated M1–M3 database; project CRUD/list round-trip, duplicate paths, machine scoping, link updates/cleanup and recent command upsert/order/isolation.

**E2E:** no direct endpoint/UI behavior is exposed until T3–T6. Update the E2E PostgreSQL fixture only if schema startup requires it; add no scenario here because persistence is exercised through T4–T6 scenarios. If a project or recent-command route becomes reachable in this task, add its API scenario here instead of deferring it.

**Done:** migrations are append-only, existing rows survive, repositories pass PostgreSQL integration tests, and no SQL was added outside `store`.

## T3 — Project API, events and session placement service

- Implement authenticated project routes matching ARCHITECTURE §9 (list/get/create/rename). Keep project delete unsupported until tree references and the roadmap define its behavior; unsupported methods return the documented API response.
- Publish typed project change events on the existing event bus only after successful persistence. Browsers consume these through `/ws/events`; no polling endpoint or timer is added.
- Add the project placement service: `session_links` hint first, then longest path-component prefix on the same machine, otherwise Other sessions. UI-created sessions call the single existing session creation service with the project's path. For links, handle rename and ended-session cleanup without binding a later unrelated same-name session.
- Keep `machine_id` on all service and event structures so multi-machine support can return without schema/API rewrite.

**Tests:** U (Go): project handler auth/Origin/body/status behavior; typed event payloads; longest-prefix cases (nested, root, sibling prefix, trailing slash, unmatched); link precedence, rename/end behavior; session service receives exact machine/name/path/env/startCommand. I (PostgreSQL + `test/sshd`): project mutation persists and publishes; real sessions map to project paths; create/rename/end/recreate flow preserves target session semantics.

**E2E:** add **(T3) Project API access control and events** in the acceptance checklist. Exercise the project API and authenticated event stream independently of the UI; type-check only, do not run.

**Done:** project API is protected, mutations publish typed events, and service-level mapping and session creation pass integration coverage.

## T4 — File browser and project actions

- Build the browser view with breadcrumb navigation, current-path input/autocomplete, directory-first listing, hidden toggle, keyboard navigation and actionable loading/empty/error states.
- Add Create folder with validation and immediate refreshed listing. No delete or remote rename UI.
- Add Open as project, using the existing project when the path is already saved. Add New session here for the selected project, wired to the single session service and existing session creation UI conventions.
- On phone widths, make directory navigation, folder creation, project selection and new session actions usable with touch targets and the app's existing list/terminal navigation model.
- Update README and ARCHITECTURE if the browser/API or user-facing semantics differ from existing design contracts.

**Tests:** U (Vitest): breadcrumb/path input, autocomplete and keyboard behavior, error recovery, hidden toggle, folder form validation, duplicate project selection, project action exact path, session action calls service and handles errors without phantom projects. I: T1/T2/T3 integration contracts cover remote path and persistence; no additional I test unless UI introduces a new server behavior.

**E2E:** add **(T4) Path autocomplete and invalid paths**, **Create folder**, **Open as project and persist**, **Project persists and updates live**, and **New session here** to [M4-acceptance.md](M4-acceptance.md#e2e-scenarios-make-e2e-simulated-user), desktop and phone where specified. Type-check only.

**Done:** browse, mkdir, save project and create-in-project flows work in both responsive profiles; scenarios compile.

## T5 — Project tree and unmatched sessions

- Replace the flat session grouping with Project → Session and an Other sessions group, preserving the tree's existing event-driven session updates and M3 layout behavior.
- Recompute placement from current projects and live session state using the T3 placement service. Project-created links take precedence; otherwise use longest path-component match. Do not use project display name for matching.
- Add Save as project for unmatched sessions. It creates/chooses the project for that session path and only changes metadata/grouping; it must not rename, detach, restart or kill the tmux session.
- Keep similar project names and nested paths distinct. Pinning and collapse customization remain in M6.
- Implement the left bar custom-order interaction for project rows and session rows within each group. Persist the account's explicit order, append new rows without moving existing rows, and never auto-sort by name, activity or recency. This moves drag-to-sort into M4 from M6; M6 must not re-add it.
- Persist both project and session/group order per account with a new `tree` key in `/api/ui-state/:key`; extend the key allowlist in M4 (M6 later extends it with `theme`). Keep the stored format versioned and validate it before applying. Do not use the global `projects.sort_order` field for this account-specific presentation state.
- Move each session row's kill ×, more-actions ⋯ and rename pencil into a compact cluster immediately to the right of its title in the left bar only. Preserve the existing kill confirmation and action semantics.
- Move the signed-in email and Sign out control from the sidebar footer to the top-right of the app header; keep them reachable on narrow screens when the sidebar is closed.
- Remove tmux window-count labels from session rows entirely.

**Tests:** U (Vitest): tree projection for nested and similar paths, unmatched grouping, typed event updates, duplicate/replayed events, save-as-project state and no session mutation; custom reorder/persistence and append-without-resort; left-bar action order/handlers and kill confirmation wiring; account header placement and sign-out wiring; no window-count rendering. Go unit coverage from T3 remains authoritative for path matching. I: T3 integration covers session ids/paths; add integration case for out-of-band-created sessions if absent; per-user tree UI-state round-trip covers account-scoped project and session order.

**E2E:** add **(T5) Longest-prefix project mapping**, **Linked session rename and cleanup**, **Other sessions and Save as project**, **Distinct project tree entries**, **Left bar custom order**, **Left bar session actions**, **Account controls in app header**, and **No window counts** to the acceptance checklist. Use real-terminal-created sessions on the throwaway target; type-check only.

**Done:** live sessions appear in the correct project or Other sessions group; save-as-project moves metadata only; scenarios compile.

## T6 — Recent start commands

- Add project-scoped recent command suggestions to the project session-creation flow. Choosing one starts in the project's path and records/updates recency; allow a newly entered command to become a recent command according to the agreed history limit.
- Do not run a command merely because the picker opened or a suggestion was rendered. Pass command text through the existing session service and `sshx` argument quoting path; do not introduce a second remote execution route.
- Document the recent-command limit, retention semantics and clear/reset behavior (if any) in ARCHITECTURE and README. Do not add a destructive history action unless explicitly designed.

**Tests:** U (Go): bounded history and input validation; U (Vitest): suggestion ordering, project scoping, picker opening without submission, selection submits once, error handling. I (PostgreSQL + `test/sshd`): command history isolation/upsert and selected command reaches a session started at the project path without argument injection.

**E2E:** add **(T6) Recent start command** and **(T6) Recent commands are project-scoped and require selection** to the acceptance checklist for desktop and phone as specified; type-check only.

**Done:** recent commands persist and are isolated per project; selecting one launches in the intended directory; scenarios compile.

## T7 — Documentation, audit and release

- Update README usage for browsing, creating projects, starting a session in a project, Other sessions/Save as project, recent commands, hidden files, SFTP requirements and the absence of remote delete/rename. Include the left-bar custom order and account-control locations if they need explanation.
- Reconcile ARCHITECTURE §§7–9, 11 and 13 with actual behavior, including endpoint shapes, event types, path matching, symlink policy, recent command history limit and tests. Update ROADMAP only if the scope changes.
- *(host)* `make deploy`; verify browser and project persistence with the real host only after automated checks pass. Never touch or stop user tmux sessions except through explicitly authorized UI actions; filesystem browsing remains read/list/stat/mkdir only.
- Audit every acceptance criterion's U/I/E line and task attribution, docs, `.env.example`, `make lint test`, `make gitleaks`, and E2E TypeScript compilation. Do not run the e2e stack or suite during M4. Report manual host checks and any env vars in the summary.

**Tests:** none new beyond any regression coverage required by the audit. T7 confirms `make lint test`, `make gitleaks`, and e2e TypeScript check; full `make e2e` is reserved for M7.

**E2E:** audit that all T3–T6 scenarios are present, correctly tagged and type-check; do not run. M4 scenarios join the M7 full suite.

**Done:** all M4 acceptance items are satisfied, docs match behavior, host smoke checks are recorded, and no M4 e2e run has occurred.
