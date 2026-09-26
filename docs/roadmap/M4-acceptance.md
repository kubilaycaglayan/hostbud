# M4 — Projects and file browser: acceptance checklist

M4 is done when every criterion below is satisfied. Tasks: [M4-tasks.md](M4-tasks.md).

M4 adds an SFTP-backed directory picker and persistent project metadata for the single built-in host. It does not add multi-machine behavior, file editing, delete/rename operations in the remote filesystem, or a new session lifecycle. Existing authentication, Origin checks, `sshx`, session service, and event-driven UI rules still apply.

## Test coverage rule

Each criterion names **U** (unit), **I** (integration against `test/sshd`, PostgreSQL, or rendered deploy config) and **E** (e2e) coverage, including the task that writes it. A layer marked n/a has a reason. E2E scenarios are written with the behavior and type-checked, but `make e2e` is not run until M7's final task. A checked box means required U/I tests pass and the e2e scenario exists and type-checks; e2e pass is recorded at M7.

## File browser and SFTP

- [x] The authenticated file browser opens at the target user's SFTP home directory, displays a breadcrumb and current path, and can navigate into and back out of directories. Paths containing spaces, quotes, Unicode and shell metacharacters remain literal paths.
  - U: T1 path normalization/validation and SFTP entry mapping; `sshx` arguments remain separated and safely quoted.
  - I: T1 against `test/sshd`: home, list, nested navigation and hostile-but-valid path names through the production SFTP client.
  - E: T1 *Browse home and navigate* API scenario through Caddy; T4 exercises the responsive browser UI (desktop and iPhone 13 Pro).
- [x] Directory listings show directories before files and are stable-sorted by name; hidden entries are omitted by default and appear when the hidden toggle is enabled. Symlink metadata is represented safely, and resolution is lazy with loops/errors surfaced as actionable row state.
  - U: T1 sorting, hidden filtering, file metadata and symlink policy cases.
  - I: T1 against `test/sshd`: visible/hidden files, directory ordering, valid symlink and broken symlink; no traversal outside the target filesystem is introduced by path handling.
  - E: T1 *Hidden entries and symlinks* API scenario through Caddy; T4 *Hidden toggle and lazy symlink status in the browser* (desktop).
- [x] Path input supports autocomplete from the current directory, keyboard selection/navigation and submitting a typed path. Invalid, missing and non-directory paths give actionable errors without losing the current browser location.
  - U: T4 autocomplete filtering, keyboard state and path input/error states.
  - I: T1 SFTP stat/list error mapping for missing path and non-directory path.
  - E: T4 *Path autocomplete and invalid paths* (desktop and iPhone 13 Pro).
- [x] The user can create a child directory. Empty names, `.`/`..`, separators, NUL and names that escape the selected directory are rejected; a successful creation refreshes the listing and selects or reveals the new directory.
  - U: T1 mkdir name validation and containment; T4 form behavior.
  - I: T1 against `test/sshd`: mkdir succeeds, the new path is visible, and rejected names do not create anything.
  - E: T4 *Create folder* (desktop and iPhone 13 Pro).
- [x] The browser supports listing, stat and mkdir only. There are no delete or remote rename controls or endpoints in M4.
  - U: T1 route/method table and service surface excludes destructive filesystem operations.
  - I: T1 unsupported methods return the standard not-found/method response and leave target contents unchanged.
  - E: T1 *No destructive file actions* (API-level through Caddy; assert no delete/rename action is exposed).
- [ ] Browse files opens in a modal dialog from an icon-only FolderPlus button outside the left gutter; the browser is not squeezed into the project/session sidebar. Directory project actions use related FolderPlus/FolderOpen icons with accessible names and tooltips.
  - U: T4 Vitest covers dialog open/close, icon button accessible names and existing/new project action state.
  - I: n/a — dialog and icon presentation are frontend-only; T1 integration covers the filesystem API behavior.
  - E: T4 *File browser dialog and icon actions* (desktop and iPhone 13 Pro): open/close the dialog from the header control and use the icon-only directory project action.

## Projects and session placement

- [x] “Open as project” persists the selected directory as a project; reopening an existing project path selects the existing project instead of creating a duplicate. Project paths are absolute, normalized SFTP paths; project names default to the final path component and may be edited through the existing project API contract.
  - U: T2 normalization, uniqueness behavior, default name and repository validation.
  - I: T2 PostgreSQL migration/repository round-trip, duplicate path handling and append-only migration check.
  - E: T3 *Project API access control and events* covers project rename through the API; T4 *Open as project and persist* covers path defaulting, duplicate selection, and persistence (desktop and iPhone 13 Pro).
- [x] Projects are listed in the tree and survive sign-out/sign-in, page reload and app-container restart. Project list updates are driven by typed events after a successful state change; the UI does not poll projects.
  - U: T3 typed event publication; T4 project store applies project events to the browser; T5 tree store handles duplicate/replayed updates idempotently.
  - I: T2 PostgreSQL persistence across repository/process reconnect; T3 event publication from project mutations.
  - E: T4 *Project persists and updates live* (desktop; create from a second page and verify event-driven update without reload, then sign out/in and verify persistence after app restart).
- [x] A new session created from a project uses that project's exact directory as its working directory and is created through the existing single session service. The session appears under that project after the session list/event update.
  - U: T3 project action passes `{machine, name, path, env, startCommand}` to the session service and handles failure without a phantom tree node.
  - I: T3 against `test/sshd`: session `#{session_path}` equals the chosen project path and list diff associates the session.
  - E: T4 *New session here* (desktop and iPhone 13 Pro): create from a project and assert the exact target session path; T5 verifies project tree placement.
- [x] A session belongs to the project with the longest path-component prefix on the same machine. `/work/app` matches `/work/app`; `/work/application` does not. Matching is independent of project display name and does not use a raw string-prefix boundary error.
  - U: T3 path-component matching table, including root, trailing slash, sibling prefixes and unmatched paths.
  - I: T3 against `test/sshd` plus PostgreSQL fixtures: live sessions map correctly to persisted project paths.
  - E: T5 *Longest-prefix project mapping* (desktop; create sessions in nested, sibling-prefix and unrelated directories using the real terminal).
- [x] A `session_links` association made when creating a session from a project takes precedence over path matching while that session name/link is valid. Renames update the link through the existing session rename path; ended sessions do not leave stale links that can assign a later unrelated session.
  - U: T2 repository link upsert, rename and stale-link rules; T3 placement precedence.
  - I: T2 PostgreSQL constraints/updates and T3 create, rename, end/recreate with same name against `test/sshd`.
  - E: T5 *Linked session rename and cleanup* (desktop).
- [x] Unmatched live sessions appear under “Other sessions”. “Save as project” for such a session persists its `session_path` as a project and moves the session under it without changing or restarting the tmux session.
  - U: T3 unmatched grouping and save action state; assert it calls project creation only.
  - I: T3 PostgreSQL project insert and session cache remains unchanged; target `#{session_id}` and `#{session_path}` are unchanged.
  - E: T5 *Other sessions and Save as project* (desktop and iPhone 13 Pro).
- [x] Project names and saved user order are rendered from persisted metadata; projects with equal or nested paths remain distinct entries.
  - U: T3 tree projection and deterministic ordering for equal names/paths.
  - I: T2 project list repository order and uniqueness constraints.
  - E: T5 *Distinct project tree entries* (desktop).

## Left bar and account controls

- [x] The left bar's project and session rows use a user-controlled order. Users can reorder projects and sessions within their project or Other sessions group; the order is persisted per account and restored after reload, sign-out/sign-in and app restart. New rows are added without automatically re-sorting existing rows. No alphabetical, activity, or recency auto-sort overrides the saved order.
  - U: T5 tree ordering operations, insertion of newly observed projects/sessions, serialization and validation; invalid or duplicate order entries are repaired deterministically.
  - I: T5 per-account project and session order round-trip through the PostgreSQL-backed `tree` UI-state key; order for one account does not affect another.
  - E: T5 *Left bar custom order* (desktop and iPhone 13 Pro): reorder project and session rows, create a new session/project and verify existing order stays put, reload/restart and verify order persists.
- [x] For each session row, the close/kill ×, three-dot actions menu and pencil rename control sit in a compact action group immediately to the right of the session title in the left bar. The × still opens the existing destructive-action confirmation; the menu and rename behavior remain available. This placement applies only to the left bar, not terminal tabs, panes or other views.
  - U: T5 `SessionList` action order, accessible names, and callbacks; kill confirmation remains wired.
  - I: n/a — control placement and callbacks are frontend behavior; the existing kill/rename endpoint integration coverage remains authoritative for effects.
  - E: T5 *Left bar session actions* (desktop and iPhone 13 Pro): verify the action group follows the row title, rename works, and × requires confirmation before killing.
- [x] The signed-in account email and Sign out action appear at the top-right of the app header, rather than the bottom of the left bar. Sign out continues to revoke the session and clear live/layout state. On narrow screens, the email and Sign out remain reachable without opening the left bar.
  - U: T5 header placement/accessibility and sign-out callback; existing auth-store tests cover session clearing.
  - I: n/a — placement is frontend behavior; M1 authentication integration covers session revocation.
  - E: T5 *Account controls in app header* (desktop and iPhone 13 Pro): verify email/Sign out at top right with the left bar both visible and closed; sign out returns to sign-in and stops live updates.
- [x] Session rows do not show tmux window counts.
  - U: T5 `SessionList` does not render a window-count label for zero, one or multiple windows.
  - I: n/a — presentation of the existing session `windows` field is frontend-only.
  - E: T5 *No window counts* (desktop): sessions with one and multiple windows both render without a count label.

## Recent start commands

- [x] Starting a session from a project offers the most recently used start commands for that project, with most recently used first; choosing one starts the session in the project path and updates recency. A newly entered command may be remembered as a recent command for that project.
  - U: T2 repository recency ordering/upsert and bounded input validation; T6 command suggestion order, project session submission behavior, and recording only after a successful start.
  - I: T2 PostgreSQL recency round-trip/upsert; T6 created tmux session receives the selected start command and history remains project-scoped.
  - E: T6 *Recent start command* (desktop and iPhone 13 Pro): start with a command, create another session from the project and verify the command is offered and works.
- [x] Recent commands are scoped to a project, not shared across projects, and are treated as command text passed through the established session creation quoting path. No command is auto-executed merely by opening the picker.
  - U: T2 project scoping and validation; T6 project-specific suggestion loading, no implicit submission, and exact command text in the shared service call.
  - I: T2 two-project isolation; T6 hostile command text is passed as one command value without shell argument injection.
  - E: T6 *Recent commands are project-scoped and require selection* (desktop).

## Security and compatibility

- [x] All new filesystem routes require an authenticated account and enforce the existing Origin allowlist for mkdir. Unauthenticated calls fail before remote SFTP work is opened.
  - U: T1 filesystem handler auth/Origin/method tests.
  - I: T1 route tests against the running app and `test/sshd`; unauthenticated calls do not open remote SFTP.
  - E: T1 *Filesystem API access control* (API-level through Caddy): signed-out request gets 401; foreign-Origin mkdir gets 403; allowed Origin works.
- [x] Project routes require authentication, state-changing requests enforce the existing Origin allowlist, and successful mutations publish typed events consumed by connected clients. `/ws/events` retains its existing auth and Origin checks.
  - U: T3 project handler auth/Origin/status and typed event tests.
  - I: T3 PostgreSQL-backed mutation and event publication test.
  - E: T3 *Project API access control and events* (API-level through Caddy): signed-out request gets 401; foreign-Origin mutation gets 403; allowed-origin create is visible through the project API and connected event stream.
- [x] All remote filesystem work uses `sshx` and the configured system `ssh`/SFTP path; filesystem paths and names are never interpolated into a shell command. Requests are context-aware and timeout-bounded, and SFTP errors are actionable without logging user paths at info level.
  - U: T1 cancellation, timeout and error mapping; architecture check confirms `fsbrowse` does not invoke a second command runner.
  - I: T1 timeout/disconnect against `test/sshd`; verify the SFTP client is closed and the app remains responsive.
  - E: T1 *SFTP unavailable recovery* (desktop): stop target sshd, observe an actionable error, restore it and browse successfully.
- [ ] PostgreSQL schema changes are append-only and preserve existing users, authentication state, UI layouts and machine records. `machine_id` remains part of project and session association keys even though v1 has only the host machine.
  - U: T2 migration ordering and repository key tests.
  - I: T2 migrate a database populated with M1–M3 records; verify those records and the seeded host machine remain unchanged and repositories scope by `machine_id`.
  - E: n/a — schema preservation and machine scoping are verified directly in the PostgreSQL integration layer; e2e would not observe the database constraint more accurately.

## E2E scenarios (`make e2e`, simulated user)

Projects remain `desktop-chromium` and `iphone-13-pro` against the throwaway target only. Each scenario is tagged to the task that writes it. Scenarios must not access the real host's filesystem or tmux. While M4 is being built, only the e2e suite type-check runs; execution is reserved for M7's final full-suite run.

- [ ] **(T1) Browse home and navigate:** authenticated API returns the target home and navigates nested paths containing spaces, quotes, Unicode and shell metacharacters through Caddy. T4 adds the desktop and phone UI navigation.
- [ ] **(T1) Hidden entries and symlinks:** API hidden toggle and directory-first ordering work; symlinks remain unresolved in listings and explicit stat reports resolved, broken or looping state (desktop API).
- [ ] **(T1) No destructive file actions:** API attempts cannot mutate or remove target entries; there is no delete or remote rename route.
- [ ] **(T4) File browser dialog and icon actions:** open/close the modal from the FolderPlus header button outside the left gutter; project actions use icon buttons with accessible labels (desktop and iPhone 13 Pro).
- [x] **(T1) Filesystem API access control:** auth and Origin enforcement through Caddy; unauthorized requests do not initiate SFTP.
- [x] **(T3) Project API access control and events:** auth and Origin enforcement through Caddy; successful create is returned by the API and published to the event stream.
- [x] **(T1) SFTP unavailable recovery:** failure is actionable and browsing works after target recovery.
- [ ] **(T4) Path autocomplete and invalid paths:** choose an autocomplete result, submit an existing typed path, and recover from missing/non-directory paths (desktop and phone).
- [ ] **(T4) Hidden toggle and lazy symlink status in the browser:** hidden entries appear only when requested, and checking a symlink lazily shows its broken/resolved/loop status (desktop).
- [ ] **(T4) Create folder:** create a nested folder, see it in the listing, reject invalid names (desktop and phone).
- [ ] **(T4) Open as project and persist:** create from a chosen folder, observe its tree entry; reload and app restart retain it (desktop and phone).
- [ ] **(T4) Project persists and updates live:** another authenticated browser creates a project and the first view updates without reload (desktop).
- [ ] **(T4) New session here:** create from a project, verify exact `session_path`, type into it and see it grouped under its project (desktop and phone).
- [x] **(T5) Longest-prefix project mapping:** sessions at nested, sibling-prefix and unrelated paths land in the correct project or Other sessions (desktop).
- [x] **(T5) Linked session rename and cleanup:** project-created session link follows UI rename and is removed/invalidated after session end (desktop).
- [x] **(T5) Other sessions and Save as project:** unmatched session is grouped separately; save it, see it move under a project, and verify tmux session id is unchanged (desktop and phone).
- [x] **(T5) Distinct project tree entries:** nested paths and similar names remain separate, deterministic entries (desktop).
- [x] **(T5) Left bar custom order:** reorder projects and sessions, add new rows, and verify existing custom order persists through reload, sign-out/sign-in and restart (desktop and phone).
- [x] **(T5) Left bar session actions:** the ×, ⋯ and pencil sit directly to the right of the title; rename works; × prompts before kill (desktop and phone).
- [x] **(T5) Account controls in app header:** email and Sign out appear top-right whether the left bar is open or closed; sign out returns to sign-in and stops live updates (desktop and phone; auth revocation is also covered by the T8B auth scenario).
- [x] **(T5) No window counts:** one-window and multi-window rows both omit the count label (desktop).
- [x] **(T6) Recent start command:** choose a recent command on a second project session; it runs in the correct project directory (desktop and phone).
- [x] **(T6) Recent commands are project-scoped and require selection:** another project's command is absent and opening the picker runs nothing (desktop).

## Definition of done

- [ ] Every functional and security criterion is satisfied; every U/I test passes and each criterion's E2E scenario exists and type-checks.
- [ ] M4 scenarios are included in the M7 full e2e run; no M4 e2e execution occurs before M7's final task.
- [ ] `make lint test` and `make gitleaks` are green; no secrets or owner-specific host data are tracked.
- [ ] README documents browsing, project creation, session placement, recent commands and filesystem limitations; ARCHITECTURE matches implemented behavior; `.env.example` is updated only if a new env variable is actually required.
- [ ] Migrations preserve all existing data and are append-only.
- [ ] Summary delivered with changes, any new env vars and manual host steps.
