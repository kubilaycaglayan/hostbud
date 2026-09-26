# M4 — Projects and file browser: acceptance checklist

M4 is done when every criterion below is satisfied. Tasks: [M4-tasks.md](M4-tasks.md).

M4 adds an SFTP-backed directory picker and persistent project metadata for the single built-in host. It does not add multi-machine behavior, file editing, delete/rename operations in the remote filesystem, or a new session lifecycle. Existing authentication, Origin checks, `sshx`, session service, and event-driven UI rules still apply.

## Test coverage rule

Each criterion names **U** (unit), **I** (integration against `test/sshd`, PostgreSQL, or rendered deploy config) and **E** (e2e) coverage, including the task that writes it. A layer marked n/a has a reason. E2E scenarios are written with the behavior and type-checked, but `make e2e` is not run until M7's final task. A checked box means required U/I tests pass and the e2e scenario exists and type-checks; e2e pass is recorded at M7.

## File browser and SFTP

- [ ] The authenticated file browser opens at the target user's SFTP home directory, displays a breadcrumb and current path, and can navigate into and back out of directories. Paths containing spaces, quotes, Unicode and shell metacharacters remain literal paths.
  - U: T1 path normalization/validation and SFTP entry mapping; `sshx` arguments remain separated and safely quoted.
  - I: T1 against `test/sshd`: home, list, nested navigation and hostile-but-valid path names through the production SFTP client.
  - E: T3 *Browse home and navigate* (desktop and iPhone 13 Pro).
- [ ] Directory listings show directories before files and are stable-sorted by name; hidden entries are omitted by default and appear when the hidden toggle is enabled. Symlink metadata is represented safely, and resolution is lazy with loops/errors surfaced as actionable row state.
  - U: T1 sorting, hidden filtering, file metadata and symlink policy cases.
  - I: T1 against `test/sshd`: visible/hidden files, directory ordering, valid symlink and broken symlink; no traversal outside the target filesystem is introduced by path handling.
  - E: T3 *Hidden entries and symlinks* (desktop).
- [ ] Path input supports autocomplete from the current directory, keyboard selection/navigation and submitting a typed path. Invalid, missing and non-directory paths give actionable errors without losing the current browser location.
  - U: T4 autocomplete filtering, keyboard state and path input/error states.
  - I: T1 SFTP stat/list error mapping for missing path and non-directory path.
  - E: T4 *Path autocomplete and invalid paths* (desktop and iPhone 13 Pro).
- [ ] The user can create a child directory. Empty names, `.`/`..`, separators, NUL and names that escape the selected directory are rejected; a successful creation refreshes the listing and selects or reveals the new directory.
  - U: T1 mkdir name validation and containment; T4 form behavior.
  - I: T1 against `test/sshd`: mkdir succeeds, the new path is visible, and rejected names do not create anything.
  - E: T4 *Create folder* (desktop and iPhone 13 Pro).
- [ ] The browser supports listing, stat and mkdir only. There are no delete or remote rename controls or endpoints in M4.
  - U: T1 route/method table and service surface excludes destructive filesystem operations.
  - I: T1 unsupported methods return the standard not-found/method response and leave target contents unchanged.
  - E: T3 *No destructive file actions* (API-level through Caddy; assert no delete/rename action is exposed).

## Projects and session placement

- [ ] “Open as project” persists the selected directory as a project; reopening an existing project path selects the existing project instead of creating a duplicate. Project paths are absolute, normalized SFTP paths; project names default to the final path component and may be edited through the existing project API contract.
  - U: T2 normalization, uniqueness behavior, default name and repository validation.
  - I: T2 PostgreSQL migration/repository round-trip, duplicate path handling and append-only migration check.
  - E: T4 *Open as project and persist* (desktop and iPhone 13 Pro).
- [ ] Projects are listed in the tree and survive sign-out/sign-in, page reload and app-container restart. Project list updates are driven by typed events after a successful state change; the UI does not poll projects.
  - U: T3 typed event publication; T5 tree store applies project events and handles duplicate/replayed updates idempotently.
  - I: T2 PostgreSQL persistence across repository/process reconnect; T3 event publication from project mutations.
  - E: T4 *Project persists and updates live* (desktop; create from a second page and verify event-driven update without reload, then reload/restart persistence).
- [ ] A new session created from a project uses that project's exact directory as its working directory and is created through the existing single session service. The session appears under that project after the session list/event update.
  - U: T3 project action passes `{machine, name, path, env, startCommand}` to the session service and handles failure without a phantom tree node.
  - I: T3 against `test/sshd`: session `#{session_path}` equals the chosen project path and list diff associates the session.
  - E: T4 *New session here* (desktop and iPhone 13 Pro): create from a project, type a marker, and assert target session path and tree placement.
- [ ] A session belongs to the project with the longest path-component prefix on the same machine. `/work/app` matches `/work/app`; `/work/application` does not. Matching is independent of project display name and does not use a raw string-prefix boundary error.
  - U: T3 path-component matching table, including root, trailing slash, sibling prefixes and unmatched paths.
  - I: T3 against `test/sshd` plus PostgreSQL fixtures: live sessions map correctly to persisted project paths.
  - E: T5 *Longest-prefix project mapping* (desktop; create sessions in nested, sibling-prefix and unrelated directories using the real terminal).
- [ ] A `session_links` association made when creating a session from a project takes precedence over path matching while that session name/link is valid. Renames update the link through the existing session rename path; ended sessions do not leave stale links that can assign a later unrelated session.
  - U: T2 repository link upsert, rename and stale-link rules; T3 placement precedence.
  - I: T2 PostgreSQL constraints/updates and T3 create, rename, end/recreate with same name against `test/sshd`.
  - E: T5 *Linked session rename and cleanup* (desktop).
- [ ] Unmatched live sessions appear under “Other sessions”. “Save as project” for such a session persists its `session_path` as a project and moves the session under it without changing or restarting the tmux session.
  - U: T3 unmatched grouping and save action state; assert it calls project creation only.
  - I: T3 PostgreSQL project insert and session cache remains unchanged; target `#{session_id}` and `#{session_path}` are unchanged.
  - E: T5 *Other sessions and Save as project* (desktop and iPhone 13 Pro).
- [ ] Project names and ordering are rendered from persisted metadata; projects with equal or nested paths remain distinct entries. Existing M6 tree customization is not pulled forward into M4.
  - U: T3 tree projection and deterministic ordering for equal names/paths.
  - I: T2 project list repository order and uniqueness constraints.
  - E: T5 *Distinct project tree entries* (desktop).

## Recent start commands

- [ ] Starting a session from a project offers the most recently used start commands for that project, with most recently used first; choosing one starts the session in the project path and updates recency. A newly entered command may be remembered as a recent command for that project.
  - U: T2 recency ordering/upsert and bounded input validation; T3 selection and submission behavior.
  - I: T2 PostgreSQL recency round-trip/upsert; T3 created tmux session receives the selected start command.
  - E: T6 *Recent start command* (desktop and iPhone 13 Pro): start with a command, create another session from the project and verify the command is offered and works.
- [ ] Recent commands are scoped to a project, not shared across projects, and are treated as command text passed through the established session creation quoting path. No command is auto-executed merely by opening the picker.
  - U: T2 project scoping and validation; T3 no implicit submission and safe service call.
  - I: T2 two-project isolation; T3 hostile command text is passed as one command value without shell argument injection.
  - E: T6 *Recent commands are project-scoped and require selection* (desktop).

## Security and compatibility

- [ ] All new filesystem routes require an authenticated account and enforce the existing Origin allowlist for mkdir. Unauthenticated calls fail before remote SFTP work is opened.
  - U: T1 filesystem handler auth/Origin/method tests.
  - I: T1 route tests against the running app and `test/sshd`; unauthenticated calls do not open remote SFTP.
  - E: T3 *Filesystem API access control* (API-level through Caddy): signed-out request gets 401; foreign-Origin mkdir gets 403; allowed Origin works.
- [ ] Project routes require authentication, state-changing requests enforce the existing Origin allowlist, and successful mutations publish typed events consumed by connected clients. `/ws/events` retains its existing auth and Origin checks.
  - U: T3 project handler auth/Origin/status and typed event tests.
  - I: T3 PostgreSQL-backed mutation and event publication test.
  - E: T3 *Project API access control and events* (API-level through Caddy): signed-out request gets 401; foreign-Origin mutation gets 403; allowed-origin create is visible through the project API and connected event stream.
- [ ] All remote filesystem work uses `sshx` and the configured system `ssh`/SFTP path; filesystem paths and names are never interpolated into a shell command. Requests are context-aware and timeout-bounded, and SFTP errors are actionable without logging user paths at info level.
  - U: T1 cancellation, timeout and error mapping; architecture check confirms `fsbrowse` does not invoke a second command runner.
  - I: T1 timeout/disconnect against `test/sshd`; verify the SFTP client is closed and the app remains responsive.
  - E: T3 *SFTP unavailable recovery* (desktop): stop target sshd, observe an actionable error, restore it and browse successfully.
- [ ] PostgreSQL schema changes are append-only and preserve existing users, authentication state, UI layouts and machine records. `machine_id` remains part of project and session association keys even though v1 has only the host machine.
  - U: T2 migration ordering and repository key tests.
  - I: T2 migrate a database populated with M1–M3 records; verify those records and the seeded host machine remain unchanged and repositories scope by `machine_id`.
  - E: n/a — schema preservation and machine scoping are verified directly in the PostgreSQL integration layer; e2e would not observe the database constraint more accurately.

## E2E scenarios (`make e2e`, simulated user)

Projects remain `desktop-chromium` and `iphone-13-pro` against the throwaway target only. Each scenario is tagged to the task that writes it. Scenarios must not access the real host's filesystem or tmux. While M4 is being built, only the e2e suite type-check runs; execution is reserved for M7's final full-suite run.

- [ ] **(T3) Browse home and navigate:** open the browser, see the target home, enter nested folders and navigate back with breadcrumbs (desktop and phone).
- [ ] **(T3) Hidden entries and symlinks:** hidden files toggle on/off; directories sort before files; symlink row state is stable and broken-link errors are recoverable (desktop).
- [ ] **(T3) No destructive file actions:** browser UI has no delete/remote rename controls; API attempts cannot mutate or remove target entries.
- [ ] **(T3) Filesystem API access control:** auth and Origin enforcement through Caddy; unauthorized requests do not initiate SFTP.
- [ ] **(T3) Project API access control and events:** auth and Origin enforcement through Caddy; successful create is returned by the API and published to the event stream.
- [ ] **(T3) SFTP unavailable recovery:** failure is actionable and browsing works after target recovery.
- [ ] **(T4) Path autocomplete and invalid paths:** choose an autocomplete result, submit an existing typed path, and recover from missing/non-directory paths (desktop and phone).
- [ ] **(T4) Create folder:** create a nested folder, see it in the listing, reject invalid names (desktop and phone).
- [ ] **(T4) Open as project and persist:** create from a chosen folder, observe its tree entry; reload and app restart retain it (desktop and phone).
- [ ] **(T4) Project persists and updates live:** another authenticated browser creates a project and the first view updates without reload (desktop).
- [ ] **(T4) New session here:** create from a project, verify exact `session_path`, type into it and see it grouped under its project (desktop and phone).
- [ ] **(T5) Longest-prefix project mapping:** sessions at nested, sibling-prefix and unrelated paths land in the correct project or Other sessions (desktop).
- [ ] **(T5) Linked session rename and cleanup:** project-created session link follows UI rename and is removed/invalidated after session end (desktop).
- [ ] **(T5) Other sessions and Save as project:** unmatched session is grouped separately; save it, see it move under a project, and verify tmux session id is unchanged (desktop and phone).
- [ ] **(T5) Distinct project tree entries:** nested paths and similar names remain separate, deterministic entries (desktop).
- [ ] **(T6) Recent start command:** choose a recent command on a second project session; it runs in the correct project directory (desktop and phone).
- [ ] **(T6) Recent commands are project-scoped and require selection:** another project's command is absent and opening the picker runs nothing (desktop).

## Definition of done

- [ ] Every functional and security criterion is satisfied; every U/I test passes and each criterion's E2E scenario exists and type-checks.
- [ ] M4 scenarios are included in the M7 full e2e run; no M4 e2e execution occurs before M7's final task.
- [ ] `make lint test` and `make gitleaks` are green; no secrets or owner-specific host data are tracked.
- [ ] README documents browsing, project creation, session placement, recent commands and filesystem limitations; ARCHITECTURE matches implemented behavior; `.env.example` is updated only if a new env variable is actually required.
- [ ] Migrations preserve all existing data and are append-only.
- [ ] Summary delivered with changes, any new env vars and manual host steps.
