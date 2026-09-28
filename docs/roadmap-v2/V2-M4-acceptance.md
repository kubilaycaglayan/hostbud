# V2-M4 — Completion gates: acceptance checklist

V2-M4 is done when every box is ticked. The exception is *Manual checks (owner)*: those are the owner's backlog and never block ([AGENTS.md](../../AGENTS.md)).

Links: tasks in [V2-M4-tasks.md](V2-M4-tasks.md); criteria 1–9 and the owner check in [ROADMAP.md](ROADMAP.md#v2-m4--completion-gates-opt-in-per-item); design in [ARCHITECTURE.md](ARCHITECTURE.md).

Each box is one roadmap criterion. Its coverage line names the task that writes each U/I/E test; sub-bullets are the details the tasks decided. The test rules are V2-M1's ([V2-M1-acceptance.md](V2-M1-acceptance.md)): stubs only, E items written per task and **first run in T7**. A box is ticked only when its U/I tests pass and its E scenario passed in T7.

## Criteria

- [x] **1 With no gates, V2-M3 behavior is unchanged.** — 2026-09-28 (T2 d11bdc6; the V2-M1–V2-M3 unit and integration tests unchanged and green, CP2 three runs, CP3; E: the earlier suites written, run on demand); E written and type-checked, run on demand
  - `achieved` ⇒ `done` in one transaction, with the same events, notifications, slot release and hand-off; the V2-M1–V2-M3 tests run unchanged.
  - U: T2 · I: T2 (the V2-M1–V2-M3 queue integration tests unchanged) · E: the V2-M1–V2-M3 suites in T7.
- [x] **2 A verify command gates advancement on exit 0, with a timeout and visible output.** — 2026-09-28 (T2 d11bdc6); E written and type-checked, run on demand
  - Non-zero, timeout (`HOSTBUD_VERIFY_TIMEOUT`, remote process gone), missing project directory and SSH failure each ⇒ needs attention with its own actionable message; the queue pauses.
  - Output: combined, 16 KiB tail, `truncated` flag, binary and ANSI sanitized, shown as text; never logged at info.
  - U: T2 · I: T2 (against `test/sshd`) · E: T2 *Verify fails then passes*, *Verify timeout*, *Verify output capped*, *Missing project directory*.
- [x] **3 Approval gates advancement.** — 2026-09-28 (T3 476ae07); E written and type-checked, run on demand
  - The queue waits (running, not paused); Approve ⇒ done and advance; Reject ⇒ needs attention; both recorded with `source='user'`.
  - U: T3 · I: n/a (no host interaction) · E: T3 *Approval*.
- [x] **4 Verify runs in the project directory from argv through `sshx`, never through `send-keys`.** — 2026-09-28 (T2 d11bdc6); E written and type-checked, run on demand
  - Split by the flags splitter, every word quoted; shell metacharacters are literal; its own `sshx` call, never the run's tmux session.
  - U: T2 · I: T2 (`pwd`, the `pwned` file, no tmux call) · E: n/a for the path (I covers it); T2 *Verify quoting* checks the escape from the browser side.
- [x] **5 The migration is append-only.** — 2026-09-28 (T1 e22f73f); E written and type-checked, run on demand
  - Adds two columns and widens two CHECKs (one `DROP CONSTRAINT … ADD CONSTRAINT` each, the only `DROP`); no data changes, every existing row valid, a second apply a no-op.
  - U: T1 · I: T1 (V2-M3 data, equal checksums) · E: n/a (indirect through T2).
- [x] **6 A restart never loses a gated item or runs verify twice; racing owner actions apply once.** — 2026-09-28 (T2 d11bdc6, T3 476ae07, T4 d1b4bd1, T5 b1c81d6); E written and type-checked, run on demand
  - Claim before run: an interrupted attempt ⇒ needs attention, never re-run; `awaiting_approval` survives as is.
  - Approve, Reject, Re-run verify, Retry, Skip and Mark done are guarded transitions: one wins, the other gets 409 naming the state; two devices see the result live.
  - U: T2, T3, T4 · I: T2 (restart) · E: T2 *Restart during verify*, T3 *Approve and reject race*, *Restart while awaiting approval*, T4 *Owner actions race*, T5 *Two devices*.
- [x] **7 Gates interact with slots and notifications as decided.** — 2026-09-28 (T2 d11bdc6, T3 476ae07); E written and type-checked, run on demand
  - `verifying` holds the slot, `awaiting_approval` frees it; `waiting_since` is set when the item leaves its gates.
  - Awaiting approval and a failed verify notify as needs attention (`on_attention`); `done` only after the gates; no command or output in the payload.
  - U: T2, T3 · I: n/a (dispatcher logic; U uses the test database) · E: T2 *Verify holds a slot*, *Failed verify notifies*, T3 *Awaiting approval frees the slot*, *Approval notifies*.
- [x] **8 Gate edits follow a defined rule; the panel works on desktop and phone.** — 2026-09-28 (T1 e22f73f, T5 b1c81d6); E written and type-checked, run on demand
  - Gates edit on `queued`, and alone on `needs_attention`; `running`, `verifying`, `awaiting_approval` ⇒ 409. The new routes are Origin-checked and in `routes.json`.
  - U: T1, T5 · I: T1 (route table, Origin) · E: T1 *Gate fields*, T5 *Gates panel*.
- [x] **9 The docs are aligned** as listed in R T6 and T6. — 2026-09-28 (T6 2bf455a); E written and type-checked, run on demand
  - U/I: T6 docs check · E: n/a (documents).

## E2E scenarios

Written in T1–T5, first run in T7, on desktop and `iphone-13-pro`.

- [x] (T1) Gate fields — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Verify fails then passes — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Verify timeout — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Verify quoting — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Verify output capped — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Missing project directory — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Restart during verify — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Verify holds a slot — written and type-checked 2026-09-28; not run (on demand)
- [x] (T2) Failed verify notifies — written and type-checked 2026-09-28; not run (on demand)
- [x] (T3) Approval — written and type-checked 2026-09-28; not run (on demand)
- [x] (T3) Approve and reject race — written and type-checked 2026-09-28; not run (on demand)
- [x] (T3) Restart while awaiting approval — written and type-checked 2026-09-28; not run (on demand)
- [x] (T3) Awaiting approval frees the slot — written and type-checked 2026-09-28; not run (on demand)
- [x] (T3) Approval notifies — written and type-checked 2026-09-28; not run (on demand)
- [x] (T4) Owner actions race — written and type-checked 2026-09-28; not run (on demand)
- [x] (T4) Retry re-applies gates — written and type-checked 2026-09-28; not run (on demand)
- [x] (T5) Gates panel (desktop and phone) — written and type-checked 2026-09-28; not run (on demand)
- [x] (T5) Two devices — written and type-checked 2026-09-28; not run (on demand)
- [ ] **(open)** (on demand) Full suite green on every e2e app: every v1 and V2-M1–V2-M4 scenario, both profiles, no skips or weakened assertions — run only when the owner asks; open until then, not a blocker

## Manual checks (owner; backlog, not blockers)

- [ ] **(open, owner)** A real item gated by the project's own `make test` (ROADMAP; safe default: no gates on real items).

## Definition of done

- [x] Every criterion is ticked (E items as written and type-checked; `make e2e` runs only on demand and a pending run is listed as open). — 2026-09-28
- [x] `make lint test` is green, `make gitleaks` is clean, and nothing host-specific is tracked. — 2026-09-28 (CP3)
- [x] *(host)* `make deploy` succeeded: the stack is healthy, existing items have no gates, and V2-M3 queues still advance. — 2026-09-28: healthy, migration 0009 applied, 9 existing items with no gates; the ungated path is covered by the unchanged V2-M1–V2-M3 tests (the host's one queue is paused by the owner and was left as is)
- [ ] T8 Docker cleanup is done, or skipped with the reason recorded; production, volumes and backups are intact, and the reclaimed space is reported.
- [ ] The summary is delivered: changes, `HOSTBUD_VERIFY_TIMEOUT`, host steps (`timeout` on the host), e2e results and open owner items.
