# V2-M2 — Multiple queues and parallel runs: acceptance checklist

V2-M2 is done when every box is ticked. The exception is *Manual checks (owner)*: those are the owner's backlog and never block (AGENTS.md).

Links: tasks in [V2-M2-tasks.md](V2-M2-tasks.md); criteria 1–8 and the owner check in [ROADMAP.md](ROADMAP.md#v2-m2--multiple-queues-and-parallel-runs-opt-in); design in [ARCHITECTURE.md](ARCHITECTURE.md).

Each box is one roadmap criterion. Its coverage line names the task that writes each U/I/E test; sub-bullets are the details the tasks decided. The test rules are V2-M1's ([V2-M1-acceptance.md](V2-M1-acceptance.md)): stubs only, E items written per task and **first run in T7**. A box is ticked only when its U/I tests pass and its E scenario passed in T7.

## Criteria

- [ ] **1 Two queues advance independently, each sequential** (switch on).
  - Both first items run at once; within a queue the next item starts only after its own achieved record.
  - U: T4 · I: T4 · E: T2 *Two queues in parallel*, T5 *Queues panel*.
- [ ] **2 The cap is respected and slots are fair.**
  - A `stale` run holds its slot; only a late achieved or an owner action (Retry, Skip, Mark done) frees it. `failed`/`exited` free it at once.
  - Order is FIFO by `waiting_since` (start, resume, or previous run ending), so with a cap of 1 queues take turns and none starves.
  - One machine-wide decision point plus a locked re-count in the store: concurrent signals never exceed the cap.
  - After a restart, active runs are reloaded before any dispatch, so the cap and the order still hold.
  - Lowering the cap stops nothing; raising or clearing it dispatches at once.
  - U: T4 · I: T4 (simultaneous finishes, restart) · E: T4 *Cap of one*, *Slots in start order*, *Stale holds a slot*, *Cap after restart*.
- [ ] **3 With the switch off, V2-M1 behavior is unchanged.**
  - `HOSTBUD_PARALLEL_QUEUES` defaults to `false`, is validated, and reaches only `hostbud`.
  - The one-queue rule and message are identical; the V2-M1 unit, integration and e2e tests run unchanged.
  - Queues left over from switch-on stay listed; starting one while another has an active run → 409 naming the switch. No run is cancelled.
  - U: T2 · I: T2 (V2-M1 queue tests with the switch off, `deploytest`) · E: the V2-M1 suite on the switch-off app, run in T7.
- [ ] **4 Session names never collide; the first queue keeps the V2-M1 name.**
  - First queue (oldest) → `<project>-q<pos>`; others → `<project>-<queue>-q<pos>`; suffixes as in v1, with a bounded retry on a duplicate at creation.
  - U: T3 · I: T3 (concurrent creation on `test/sshd`) · E: T3 *Name collision*.
- [ ] **5 The migration is append-only and keeps V2-M1 data.**
  - Adds `machine_capacity`, `queues.waiting_since` and the per-project name index only; no drop, rename, update or delete.
  - U: T1 · I: T1 (a seeded V2-M1 copy keeps its checksums; a second apply is a no-op) · E: n/a (not user-visible; indirect through T4).
- [ ] **6 Two running queues on one project directory show a warning.**
  - Same project or two projects with the same cleaned path; shown on both queues; never blocks.
  - U: T2, T5 · I: n/a (pure logic over stored paths) · E: T2 *Same-directory warning*, T5 *Queues panel*.
- [ ] **7 Queues and the cap are manageable from desktop and phone.**
  - Switcher, create, rename, delete (confirmed); the cap in Settings; "waiting for a free slot" shown; updates come from `queue.changed` only.
  - The capacity route is authenticated, Origin-checked and in `routes.json`; out-of-range values → 400.
  - U: T5 · I: T5 (route table, Origin) · E: T5 *Queues panel* (desktop and phone), *Capacity API*.
- [ ] **8 The docs are aligned** as listed in R T6 and T6.
  - U/I: T6 docs check · E: n/a (documents).

## E2E scenarios

Written in T2–T5, first run in T7, on desktop and `iphone-13-pro`.

- [ ] (T2) Two queues in parallel
- [ ] (T2) Same-directory warning
- [ ] (T3) Name collision
- [ ] (T4) Cap of one
- [ ] (T4) Slots in start order
- [ ] (T4) Stale holds a slot
- [ ] (T4) Cap after restart
- [ ] (T5) Queues panel (desktop and phone)
- [ ] (T5) Capacity API
- [ ] (on demand) Full suite green on both e2e apps: every v1, V2-M1 and V2-M2 scenario, both profiles, no skips or weakened assertions — run only when the owner asks; open until then, not a blocker

## Manual checks (owner; backlog, not blockers)

- [ ] Two real agents in parallel on separate projects (ROADMAP).
- [ ] Decide whether to set `HOSTBUD_PARALLEL_QUEUES=true` in production (safe default: off).

## Definition of done

- [ ] Every criterion is ticked (E items as written and type-checked; `make e2e` runs only on demand and a pending run is listed as open).
- [ ] `make lint test` is green, `make gitleaks` is clean, and nothing host-specific is tracked.
- [ ] *(host)* `make deploy` succeeded: the stack is healthy, V2-M1 queues are intact, and the switch is as the owner set it.
- [ ] T8 Docker cleanup is done, or skipped with the reason recorded; production, volumes and backups are intact, and the reclaimed space is reported.
- [ ] The summary is delivered: changes, the new env var, host steps, e2e results and open owner items.
