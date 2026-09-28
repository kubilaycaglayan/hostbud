# V2-M2 — Multiple queues and parallel runs: tasks

Goal, scope, switch and the base task list: [ROADMAP.md](ROADMAP.md#v2-m2--multiple-queues-and-parallel-runs-opt-in) (cited as **R T*n***). Design: [ARCHITECTURE.md](ARCHITECTURE.md) (**v2 §N**). Checklist: [V2-M2-acceptance.md](V2-M2-acceptance.md). Pattern and rules as in [V2-M1-tasks.md](V2-M1-tasks.md).

This file does **not** repeat those documents. Each task points to its R T*n* entry and adds only the decisions it leaves open.

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Schema | Done |
| T2 Several queues and the switch | Done |
| T3 Session naming | Done |
| T4 Dispatcher with slots | Done |
| T5 API and panel | Not started |
| T6 Docs | Not started |
| T7 Milestone acceptance | Not started |
| T8 Safe Docker cleanup | Not started |

**Precondition:** V2-M1 is done (its checklist ticked, open owner items excepted). Record the check in the Progress note.

## Rules for this milestone

The v2 ROADMAP *Rules*, AGENTS.md and the V2-M1 milestone rules (fast checks per commit, production untouched until the acceptance task, design changes into v2 docs in the same commit, bugs as failing test then fix, explicit staging) apply in full. e2e is written per task and **runs only in T7**; T8 is the safe Docker cleanup. This milestone adds:

- **The switch** is the new env var `HOSTBUD_PARALLEL_QUEUES` (boolean, default `false`, T2). It goes to `.env.example`, `internal/config` (validated) and the `hostbud` service's `environment:` only. With it off, every V2-M1 path behaves exactly as before, so the V2-M1 tests stay unchanged and green.
- **A second e2e app** (T2): `hostbud-e2e-app-multi` with the switch on, its own database and its own loopback Caddy site, sharing the throwaway target (the `hostbud-e2e-app-notmux` pattern). The V2-M1 suite keeps running against the switch-off app. Add ctl actions to restart it.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T1–T3 | `make lint test`, `scripts/compose-config.sh`, e2e `tsc` | Green (2026-09-28) |
| CP2 | T4 | `make lint test` **three times in a row** (concurrent signals, timers), e2e `tsc` | Green (2026-09-28, 3/3) |
| CP3 | T5–T6 | `make lint test`, `vue-tsc`, e2e `tsc`, `make gitleaks`, docs check | Not run |
| CP4 | T7 | `make lint test`, e2e `tsc`, `make gitleaks`, `make deploy` (no `make e2e`: on demand only) | Not run |

**Progress note:**
- **Precondition** (2026-09-28): V2-M1 is done: every criterion ticked; only its on-demand e2e run and owner checks are open (allowed).
- **T1** (2026-09-28): `0006_parallel_queues.sql` adds `machine_capacity`, `queues.waiting_since` and `queues_project_name`; store gets capacity get/set, the active-run count and the waiting order. The e2e `seedRun` now names its queues uniquely (the new index).
- **T2** (2026-09-28): `HOSTBUD_PARALLEL_QUEUES` (config, compose `hostbud` only, `.env.example`); switch-on create with unique names (an unnamed queue takes the first free "Queue n"); the switch-off 409 while another queue is running or has an active run; `shared_directory` warnings on start/resume responses, `GET /api/queues` and the peers' `queue.changed`. e2e: `hostbud-e2e-app-multi` (own database `hostbud_multi` via the idempotent `hostbud-e2e-multidb`, Caddy `:9058`, ctl `/multi/restart`, the `multi` fixture and `multiDb` helpers, logs-clean check); `queues-multi.api.spec.ts` adds *Two queues in parallel*, *Same-directory warning* and a switch-off leftover-queue scenario on the V2-M1 app.
- **T3** (2026-09-28): `RunSessionName(project, queue, pos)`: the project's oldest queue (by `created_at, id`; queue ids are random, so "smallest id" means the oldest row) keeps `<project>-q<pos>`, others get `<project>-<queue>-q<pos>`; the session service's bounded duplicate retry covers simultaneous creation. v2 §5.1 updated. e2e *Name collision* (with a leftover session and a rename).
- **CP1** (2026-09-28): `make lint test` green (includes `compose-config.sh` and the e2e `tsc`).
- **T4** (2026-09-28): `dispatch` on the dispatcher goroutine hands out slots FIFO by `waiting_since` (a queue whose run ended goes behind everyone in line, even at an equal timestamp); `store.CreateRunInSlot` re-checks the cap under a per-machine advisory lock; `failed`/`exited`/cancelled runs free their slot at once, stale ones hold it; cap changes go through `Service.SetCapacity` → `CapacityChanged`; `waitingForSlot` is derived in the view and published on change; with the switch off the V2-M1 `advance` path runs unchanged. v2 §5.5/§6 added. e2e `queue-slots.api.spec.ts`: *Cap of one*, *Slots in start order*, *Stale holds a slot*, *Cap after restart* (the cap is seeded in the multi database until T5's API).
- **CP2** (2026-09-28): `make lint test` green three times in a row (a first try failed only on gosec's weak-random warning in a test; fixed with deterministic picks).

---

## T1 — Schema

Scope: R T1, v2 §6. Additions:
- **One migration**, the next free number. It only adds; nothing is dropped, renamed or rewritten:
  - `machine_capacity(machine_id PK, max_concurrent_runs INT NULL CHECK (max_concurrent_runs BETWEEN 1 AND 32))`, NULL = no cap; no row = no cap;
  - `queues.waiting_since TIMESTAMPTZ NULL` (slot order, T4);
  - a unique index on `queues(project_id, lower(name))` (V2-M1 allowed one queue, so no existing rows can conflict).
- **Store:** get/set capacity; count active runs (`starting`, `running`, `stale`) per machine; list waiting queues ordered by `waiting_since, id`.
- **Tests:**
  - U: capacity bounds; the active-run count includes `stale`; waiting order.
  - I: seed a V2-M1 database (queues, items, runs, events), migrate: row counts and checksums per table are equal, a second apply is a no-op, and the migration file contains no `DROP`/`ALTER … DROP`/`UPDATE`/`DELETE`.
- **E2E:** none reachable. Add `helpers/db.ts` seeds for capacity and a stale run (used by T4).

## T2 — Several queues and the switch

Scope: R T2. Additions:
- **Switch off** (default): the V2-M1 one-queue rule and message stay byte-for-byte. If queues were created while it was on, they stay listed, editable and deletable, but starting or resuming one is refused with 409 while another queue has an active run: "Parallel queues are off — pause queue <name> and wait for its run to end, or set `HOSTBUD_PARALLEL_QUEUES=true`". Active runs are never cancelled by switching off.
- **Switch on:** `POST /api/queues` accepts more queues per project. Names are unique per project (case-insensitive) → 409 with a hint.
- **Same-directory warning:** when a queue starts or resumes and another queue whose project resolves to the **same path** is `running` (or `paused` with an active run), the response carries `warnings: [{code: "shared_directory", queues: [...]}]` and `GET /api/queues` marks both queues. It never blocks. Paths are compared cleaned; two projects with one path count.
- **Tests:**
  - U: switch-off create/start/resume paths match V2-M1 responses exactly; the switch-off 409; the warning for same project, for two projects sharing a path, and not for different paths.
  - I: `deploytest` checks the var reaches only `hostbud`; the V2-M1 queue integration tests run unchanged with the switch off.
- **E2E:** add `hostbud-e2e-app-multi` and its helpers. `queues-multi.api.spec.ts`: *Two queues in parallel* (API level; both first items run at once, each queue stays sequential) and *Same-directory warning*. The V2-M1 specs (including "a second queue is refused") stay on the switch-off app, unchanged.

## T3 — Session naming

Scope: R T3. Additions:
- **First queue** = the project's oldest existing queue (smallest id). It keeps `<project>-q<pos>`; others use `<project>-<queue>-q<pos>`, the queue name sanitized as a tmux name.
- **Collisions** (a leftover session, two sanitized names that match, or another project's name) use v1's suffix. Two runs created at the same moment must not both pick one name: if `new-session` reports a duplicate, retry with the next suffix, bounded.
- The name is stored on the run, so renaming a queue never changes an existing run's session.
- **Tests:**
  - U: the first queue's name equals V2-M1's; names for queues with spaces, unicode, and names that sanitize alike.
  - I: two runs on one project created concurrently against `test/sshd` get distinct sessions.
- **E2E:** *Name collision* in `queues-multi.api.spec.ts`: two queues on one project run at once, the first queue's session is `<project>-q1`, the second's `<project>-<queue>-q1`.

## T4 — Dispatcher with slots

Scope: R T4. Additions:
- **One machine-wide decision point.** Slot checks and run creation happen in one dispatcher goroutine per machine, and the store re-checks in the same transaction (lock the `machine_capacity` row, or an advisory lock per machine, then count active runs). Concurrent hook signals, owner actions and the timer can never exceed the cap.
- **Stale holds a slot**, because its session is alive. It frees the slot only on a late achieved, or an owner action that cancels the run (Retry, Skip, Mark done). `failed`/`exited` runs free it at once.
- **Order:** `waiting_since` is set when a queue starts or resumes, and again when its previous run ends. A free slot goes to the waiting queue with the oldest `waiting_since` (ties by id). A queue that just finished a run goes to the back, so with a cap of 1 queues take turns and none starves.
- **Waiting reason:** derived, not stored. The head item of a running queue without a run while the cap is reached is shown as `waitingForSlot` ("waiting for a free slot"), with `queue.changed` published whenever that changes.
- **Cap changes:** lowering the cap below the active count stops nothing; new runs wait. Raising it or clearing it dispatches at once.
- **Restart:** recovery reloads active runs (V2-M1 R16) **before** any dispatch, so the count after a restart includes them; the waiting order survives through `waiting_since`.
- **Switch off:** slot logic isn't consulted; V2-M1's one active run per queue plus T2's rule apply.
- **Tests:**
  - U: fake clock and session layer: cap 1 with three queues takes turns (A, B, C, A…); stale holds, owner action frees; lowering and raising the cap; 50 concurrent signals never exceed the cap.
  - I: against `test/sshd`, two queues finishing at once with cap 1 start exactly one next run; a restart with runs active keeps the count.
- **E2E:** `queue-slots.api.spec.ts` on the multi app:
  - *Cap of one:* queue B waits with "waiting for a free slot" and starts when queue A's run achieves;
  - *Slots in start order:* three queues, cap 1, each with two `achieve:1` items; the start order interleaves, no queue waits twice in a row;
  - *Stale holds a slot:* A's `silent` run goes stale, B keeps waiting; Skip on A frees the slot and B starts;
  - *Cap after restart:* restart the multi app while A runs; B still waits, then starts after A achieves.

## T5 — API and panel

Scope: R T5. Additions:
- **API:** `GET`/`PUT /api/machines/{id}/capacity` with `{maxConcurrentRuns: int|null}`; Origin-checked, authenticated, in `routes.json` under M7's limits. It publishes `queue.changed` (no new event type). `GET /api/queues` adds `waitingForSlot`, the `warnings`, and `parallelQueues` (the switch).
- **Panel:** a queue switcher (a list on desktop, a select on the phone), create, rename, delete (with confirmation). The cap lives in Settings. With the switch off, the switcher shows only what exists and the create button explains the switch. The warning is a banner on both queues. A waiting item shows "waiting for a free slot".
- **Tests:** U (Vitest): switcher, cap form validation, waiting and warning rendering, the switch-off state. I: route table and Origin checks for the capacity route.
- **E2E:** *Queues panel* in `queues.spec.ts` (desktop) and `queues.phone.spec.ts`: create two queues on one project (warning shown), run both, set the cap to 1 in Settings and see the waiting state clear live, rename and delete (confirmed). *Capacity API* in `queues-multi.api.spec.ts`: capacity validation (0, 33, non-integer → 400) and a bad Origin → 403.

## T6 — Docs

Scope: R T6. Also: v2 §5.1 and §6 (slots, `waiting_since`, naming), the v2 ROADMAP status line linking these files, `.env.example`, and the docs check covering the new var and route.
- **Tests:** the docs check. **E2E:** n/a (documents).

## T7 — Milestone acceptance

Scope: R T7, done as V2-M1 T13 (the deploy checks; e2e fixing rules apply to on-demand runs; plus: the switch is off in production unless the owner set it, and the V2-M1 queue still works). Tick the checklist with dates and commits (E items as written until an on-demand run passes them), and write the summary with the new env var and the open owner items.
- **E2E:** no new scenarios; no run (on demand only; a full suite on both e2e apps runs when the owner asks).

## T8 — Safe Docker cleanup

As in V2-M1 T14. Extend `docker-clean` to cover `hostbud-e2e-app-multi` and its database. Never close a tmux session.
- **Tests/E2E:** n/a (operations; the health check verifies).

---

## New configuration

`HOSTBUD_PARALLEL_QUEUES` (T2): `true` enables several queues and parallel runs; default `false` keeps V2-M1 behavior. The cap is set in Settings, not in `.env`.
