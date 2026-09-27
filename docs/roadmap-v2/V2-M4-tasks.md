# V2-M4 — Completion gates: tasks

Goal, scope, switch and the base task list: [ROADMAP.md](ROADMAP.md#v2-m4--completion-gates-opt-in-per-item) (cited as **R T*n***). Design: [ARCHITECTURE.md](ARCHITECTURE.md) (**v2 §N**), v1 docs as **v1 ARCHITECTURE §N**. Checklist: [V2-M4-acceptance.md](V2-M4-acceptance.md). Pattern and rules as in [V2-M1-tasks.md](V2-M1-tasks.md) and [AGENTS.md](../../AGENTS.md).

This file does **not** repeat those documents. Each task points to its R T*n* entry and adds only the decisions it leaves open.

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Schema, item API and gate edits | Not started |
| T2 Verify runner | Not started |
| T3 Approval | Not started |
| T4 Owner actions | Not started |
| T5 Panel | Not started |
| T6 Docs | Not started |
| T7 Milestone acceptance | Not started |
| T8 Safe Docker cleanup | Not started |

**Precondition:** V2-M3 is done (its checklist ticked, open owner items excepted). Record the check in the Progress note.

## Rules for this milestone

The v2 ROADMAP *Rules*, AGENTS.md and the V2-M1 milestone rules (fast checks per commit, production untouched until the acceptance task, design changes into v2 docs in the same commit, bugs as failing test then fix, explicit staging) apply in full. e2e is written per task and **runs only in T7**; T8 is the safe Docker cleanup. This milestone adds:

- **No gates = V2-M3.** An item with `verify_command` NULL and `requires_approval` false goes `achieved` ⇒ `done` in the same transaction as before: same events, notifications, slot release and hand-off. The V2-M1–V2-M3 tests stay unchanged and green.
- **Gates are item states; the run stays `achieved`.** `achieved` ⇒ `verifying` (if set) ⇒ `awaiting_approval` (if set) ⇒ `done`. The run's token is revoked at `achieved` as before, and no stale timer runs on a gated item. A late achieved after stale enters the gates too; the queue stays paused (V2-M1 R10).
- **Queue:** while an item is `verifying` or `awaiting_approval` the queue stays `running` (waiting, not paused) and its next item doesn't start. Pausing then works as in V2-M1: the gate finishes, nothing new starts.
- **Slots (V2-M2):** `verifying` **holds** its queue's slot (hostbud is running work on the host); `awaiting_approval` **frees** it (nothing runs). `waiting_since` is set when the item leaves its gates, not at `achieved`. With `HOSTBUD_PARALLEL_QUEUES` off, slots aren't consulted.
- **Notifications (V2-M3):** `done` notifies when the item becomes `done`, never at `achieved`. `awaiting_approval` and a failed verify (exit ≠ 0, timeout, missing directory, SSH failure, restart) notify as **needs attention** under the `on_attention` choice, with outcomes `awaiting_approval` and `verify_failed`. Keys: `run:<id>:approval` and `run:<id>:verify:<attempt>`. The payload allowlist is unchanged: never the command or its output.
- **Guarded transitions:** every gate result and owner action is one `UPDATE … WHERE status = <expected>` in the store. The loser gets 409 naming the current state, so two devices can't apply an action twice.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T1 | `make lint test`, `scripts/compose-config.sh`, e2e `tsc` | Not run |
| CP2 | T2–T4 | `make lint test` **three times in a row** (timeouts, races, restart), e2e `tsc` | Not run |
| CP3 | T5–T6 | `make lint test`, `vue-tsc`, e2e `tsc`, `make gitleaks`, docs check | Not run |
| CP4 | T7 | `make lint test`, e2e `tsc`, `make gitleaks`, `make deploy` (no `make e2e`: on demand only) | Not run |

**Progress note:** (precondition and checkpoint results go here.)

---

## T1 — Schema, item API and gate edits

Scope: R T1. Additions:
- **One migration**, append-only: add the two columns, then widen `queue_items.status` (+ `verifying`, `awaiting_approval`) and `run_events.source` (+ `verify`). Each CHECK is replaced in **one** `ALTER TABLE … DROP CONSTRAINT <name>, ADD CONSTRAINT <name> CHECK (…)` statement; the names come from the V2-M1 migration. The new sets are strict supersets, so every existing row stays valid. This `DROP CONSTRAINT` is the only `DROP` the migration test allows; no `UPDATE`/`DELETE`.
- **Item API:** items take and return `verifyCommand` (string, empty = NULL, ≤ 4 KiB, splits cleanly with the flags splitter, no NUL or newline) and `requiresApproval`. Status and `run.changed`/`queue.changed` carry the two new states.
- **Gate edits:** `queued` items edit as before. On `needs_attention` items **only** the two gate fields can be edited (to fix a bad verify command before Re-run verify, T4); the other fields stay refused (V2-M1). `running`, `verifying` and `awaiting_approval` refuse any edit with 409 ("gates are fixed while the item is <state>; they apply as they were when the run started"). Deleting a queue with a `verifying` item is refused like one with an active run.
- **Tests:**
  - U: validation (unbalanced quote, NUL, too long, empty ⇒ NULL); the edit matrix per state; the widened CHECKs.
  - I: seed a V2-M3 database, migrate: row counts and checksums equal, every row passes the new CHECKs, a second apply is a no-op, the file contains only the allowed `DROP CONSTRAINT`.
- **E2E:** `gates.api.spec.ts`: *Gate fields* (create with gates, read back; invalid command → 400; edit refused while `running`, allowed on `needs_attention`).

## T2 — Verify runner

Scope: R T2. Additions:
- **Argv, not shell:** the command is split by the flags splitter and every word is quoted by `sshx`; `;`, `&&`, `|`, `$(…)` and backticks are literal arguments. Pipelines need an explicit `sh -c '…'`, documented in T6. It runs as its own `sshx` call: first `test -d -- <path>`, then `cd -- <path> && exec timeout -k 10s <secs> <argv…>`. Never through tmux, `send-keys` or the run's session.
- **Claim before run:** entering `verifying` and inserting `run_events(source='verify', kind='verify_started', attempt=n)` commit in one transaction before the call; `verify_result` records exit code, duration, `truncated` and the tail.
- **Outcomes** (all but exit 0 ⇒ `needs_attention` with an actionable `detail`, queue paused):
  - exit 0 ⇒ approval gate or `done`;
  - non-zero ⇒ "verify failed (exit N)";
  - timeout (`HOSTBUD_VERIFY_TIMEOUT`, Go duration 10s–2h, default `10m`; local context deadline plus remote `timeout`, exit 124/137) ⇒ "verify timed out after 10m";
  - missing directory ⇒ "project directory is missing on the host — restore it, then Re-run verify";
  - SSH failure ⇒ the `sshx` actionable message, "verify didn't run";
  - `timeout` not on the host ⇒ "`timeout` not found on the host — install coreutils".
- **Output:** stdout and stderr combined, streamed through a 16 KiB ring buffer (never held whole); invalid UTF-8 replaced, ANSI and control bytes except `\n`/`\t` stripped. Info logs carry item id, attempt, exit code and duration only; command and output never.
- **Restart:** an item in `verifying` whose last attempt has a `verify_started` but no `verify_result` isn't re-run: it goes to `needs_attention` ("hostbud restarted during verify — Re-run verify"). One without a started attempt starts it once.
- **Tests:**
  - U: the splitter and quoting (the metacharacters above, quotes, unicode, leading `-`); outcome mapping; ring buffer with 50 MiB and binary input; log hygiene; restart cases; the slot and notification rules (fake clock, fake `sshx`).
  - I: against `test/sshd`: runs in the project dir (`pwd` in output); `touch 'x; touch pwned'` creates only `x; touch pwned`; timeout leaves no remote process; missing dir; SSH failure (stopped sshd); no tmux call in the `sshx` log.
- **E2E:** a new ctl action `/target/files` creates, removes and reads files under the e2e project directory only. `gates.api.spec.ts`: *Verify fails then passes* (R, then Resume advances), *Verify timeout*, *Verify quoting* (`pwned` absent via ctl), *Verify output capped* (`head -c 5000000 /dev/urandom`: tail ≤ 16 KiB, `truncated`), *Missing project directory*, *Restart during verify* (`sh -c 'echo x >> count; sleep 3'`, `/app/restart`: needs attention, `count` has one line). `gates-multi.api.spec.ts`: *Verify holds a slot* (cap 1: B waits while A verifies). `gates-notify.api.spec.ts`: *Failed verify notifies* (pushfake: attention, `verify_failed`, no command or output).

## T3 — Approval

Scope: R T3. Additions:
- `POST /api/items/{id}/approve` and `…/reject`: authenticated, Origin-checked, in `routes.json`; only from `awaiting_approval`, else 409. Reject ⇒ `needs_attention` ("rejected by <account>"), queue paused. `run_events` records the account id, never the email at info.
- **Restart:** `awaiting_approval` is stored state; nothing re-runs and no timer arms.
- **Tests:** U: the transitions; Approve/Reject raced 50 times → exactly one 200 and one `run_events` row; the slot freed; the approval notification once. I: n/a (no host interaction; the store race runs in U against the test database).
- **E2E:** `gates.api.spec.ts`: *Approval* (R), *Approve and reject race* (parallel requests: one 200, one 409), *Restart while awaiting approval* (still awaiting, Approve advances). `gates-multi.api.spec.ts`: *Awaiting approval frees the slot*. `gates-notify.api.spec.ts`: *Approval notifies* (attention once; `done` only after Approve).

## T4 — Owner actions

Scope: R T4. Additions:
- **Re-run verify** (`POST …/reverify`): only from `needs_attention` when the latest run is `achieved` and a verify command is set (else 409 with the reason); a new attempt, then the approval gate if set. Like every owner action, the queue stays paused until Resume.
- **Retry, Skip, Mark done** keep V2-M1's rules (`needs_attention` only). From `verifying` or `awaiting_approval` they return 409 ("reject first" / "wait for verify, at most <timeout>"). Mark done skips remaining gates (owner override).
- **Tests:** U: each action raced against each other (Re-run verify, Retry, Skip, Mark done) → one wins, one 409; Retry re-applies the gates on the new run. I: Re-run verify against `test/sshd` without starting a new session.
- **E2E:** `gates.api.spec.ts`: *Owner actions race* (Re-run verify vs Skip in parallel: one applies), *Retry re-applies gates*.

## T5 — Panel

Scope: R T5. Additions:
- The item form has a verify field (monospace, autocomplete off, the argv hint) and an approval toggle; fields lock per T1's matrix. The item view shows the gate state, attempt, exit code, duration and the output tail as text with a "truncated" note. Approve/Reject (Reject confirmed) and Re-run verify; a 409 refetches and says who acted.
- **Tests:** U (Vitest): the field locking, output rendering (no HTML), buttons per state, live updates from events.
- **E2E:** `gates.spec.ts` (desktop) and `gates.phone.spec.ts`: *Gates panel* (set gates, see verifying then awaiting, Approve advances live), *Two devices* (desktop approves; the phone's buttons vanish without reload; a stale click shows the 409 message).

## T6 — Docs

Scope: R T6. Also: v2 §5.4 (the item states, gates, slots and notifications above), v2 §6 (columns, CHECKs), the v2 ROADMAP status line linking these files, `.env.example` (`HOSTBUD_VERIFY_TIMEOUT`), README (argv, `sh -c` for pipelines, runs as the host user in the project directory), and the docs check covering the var and routes.
- **Tests:** the docs check. **E2E:** n/a (documents).

## T7 — Milestone acceptance

Scope: R T7, done as V2-M1 T13 (the deploy checks; e2e fixing rules apply to on-demand runs; plus: existing items have no gates and V2-M3 queues still advance). Tick the checklist with dates and commits (E items as written until an on-demand run passes them); write the summary with the new env var and the open owner items.
- **E2E:** no new scenarios; no run (on demand only; a full suite on every e2e app runs when the owner asks).

## T8 — Safe Docker cleanup

As in V2-M1 T14. No new e2e services. Never close a tmux session.
- **Tests/E2E:** n/a (operations; the health check verifies).

---

## New configuration

`HOSTBUD_VERIFY_TIMEOUT` (T2): optional, default `10m`. The e2e app sets `5s`.
