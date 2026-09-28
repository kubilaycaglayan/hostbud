# V2-M5 — LLM stale-run supervisor: acceptance checklist

V2-M5 is done when every box is ticked. The exception is *Manual checks (owner)*: those are the owner's backlog and never block ([AGENTS.md](../../AGENTS.md)).

Links: tasks in [V2-M5-tasks.md](V2-M5-tasks.md); criteria 1–10 and the owner checks in [ROADMAP.md](ROADMAP.md#v2-m5--llm-stale-run-supervisor-opt-in); design in [ARCHITECTURE.md](ARCHITECTURE.md).

Each box is one roadmap criterion. Its coverage line names the task that writes each U/I/E test; sub-bullets are the details the tasks decided. The test rules are V2-M1's ([V2-M1-acceptance.md](V2-M1-acceptance.md)): stubs and `hostbud-e2e-llmfake` only, E items written per task and **first run in T7**. A box is ticked only when its U/I tests pass and its E scenario passed in T7.

## Criteria

- [ ] **1 Off unless configured; with no provider, V2-M4 behavior is unchanged.**
  - No timer, `capture-pane`, outbound request, `llm` row, `flag` field or notification; the V2-M1–V2-M4 tests run unchanged.
  - U: T1, T2 · I: T1, T2 (no `capture-pane` in the `sshx` log) · E: T1 *No provider, no calls*; the V2-M1–V2-M4 suites in T7.
- [ ] **2 A missing, partial or invalid provider config turns the supervisor off with an actionable message.**
  - Never a startup error; one warn log naming vars, never values; `GET /api/supervisor` gives the reason and never the key.
  - U: T1 · I: T1 · E: T1 *Supervisor status*.
- [ ] **3 Quiet and stale runs are classified from `capture-pane` and flagged, within budget.**
  - Due after the quiet window or on `stale`; claims count against the hourly budget.
  - U: T2 · I: T2 (against `test/sshd` and a fake provider) · E: T2 *Quiet run is flagged*, *Stale run is classified*, *Budget respected*.
- [ ] **4 `capture-pane` is safe.**
  - `sshx` argv, validated exact target `=<name>`, 256 KiB read cap and 200 lines; gone or renamed ⇒ skipped, no call, never a prefix match.
  - U: T2 · I: T2 · E: T2 *Session gone or renamed*.
- [ ] **5 Only eligible runs are classified; budget and timer survive a restart.**
  - Only `running` and `stale` runs; gated items and ended runs never. A restart recomputes from stored rows and makes no extra call; an interrupted claim ends `unknown`.
  - U: T2 · I: T2 · E: T2 *Gated items not classified*, *Restart keeps budget and timer*.
- [ ] **6 A classification never advances the queue or changes an item, run, gate or slot.**
  - Including `completed`; a result racing achieved, exit or an owner action is discarded without a flag.
  - U: T3 · I: T3 · E: T3 *Completed label never advances*, *Completed races achieved*, *Run ends mid-classification*, *Owner action during classification*.
- [ ] **7 Provider failures and hostile pane text never change a status.**
  - Timeout, 429/5xx (≤ 3 attempts, ≤ 60 s), other 4xx, malformed or out-of-schema answers ⇒ `unknown`; injected instructions stay data.
  - U: T1, T3 · I: T1 (fake provider over HTTP) · E: T3 *Provider errors*, *Prompt injection*.
- [ ] **8 No key or pane text in logs; pane text scrubbed, capped, never stored or shown beyond label and reason.**
  - Scrub on by default, run token always removed, 8 KiB cap after scrub.
  - U: T1, T4 · I: T1 log-hygiene · E: T4 *Scrubbed before sending*, *Pane text not exposed* (logs themselves: n/a, not observable in the browser).
- [ ] **9 Flags show on desktop and phone and notify once per run and label.**
  - "needs your input" under `on_attention`, key `run:<id>:llm:<label>`, V2-M3 allowlist (no reason, no pane text).
  - U: T5 · I: n/a (UI and notify builder; delivery covered by V2-M3) · E: T5 *Flag badge*, *Flag notifies once*.
- [ ] **10 The docs are aligned** as listed in R T6 and T6.
  - U/I: T6 docs check · E: n/a (documents).

## E2E scenarios

Written in T1–T5, first run in T7, on desktop and `iphone-13-pro`.

- [x] (T1) Supervisor status — written and type-checked; run pending
- [x] (T1) No provider, no calls — written and type-checked; run pending
- [x] (T2) Quiet run is flagged — written and type-checked; run pending
- [x] (T2) Stale run is classified — written and type-checked; run pending
- [x] (T2) Budget respected — written and type-checked; run pending
- [ ] (T2) Session gone or renamed
- [x] (T2) Gated items not classified — written and type-checked; run pending
- [ ] (T2) Restart keeps budget and timer
- [x] (T3) Completed label never advances — written and type-checked; run pending
- [ ] (T3) Completed races achieved
- [ ] (T3) Run ends mid-classification
- [x] (T3) Owner action during classification — skip race written and type-checked; run pending
- [x] (T3) Provider errors — 503 exhaustion written and type-checked; run pending
- [ ] (T3) Prompt injection
- [x] (T4) Scrubbed before sending — written and type-checked; run pending
- [ ] (T4) Pane text not exposed
- [x] (T5) Flag badge (desktop and phone) — written and type-checked; run pending
- [x] (T5) Flag notifies once — written and type-checked; run pending
- [ ] (on demand) Full suite green on every e2e app: every v1 and V2-M1–V2-M5 scenario, both profiles, no skips or weakened assertions — run only when the owner asks; open until then, not a blocker

## Manual checks (owner; backlog, not blockers)

- [ ] One real OpenAI classification of a quiet real agent session (ROADMAP; safe default: no provider in production `.env`).
- [ ] Review cost settings (quiet window, budget, model).

## Definition of done

- [ ] Every criterion is ticked (E items as written and type-checked; `make e2e` runs only on demand and a pending run is listed as open).
- [ ] `make lint test` is green, `make gitleaks` is clean, and nothing host-specific or key-like is tracked.
- [x] *(host)* `make deploy` succeeded and the stack is healthy; `HOSTBUD_LLM_PROVIDER` is empty in the deployed container. The authenticated supervisor status check and V2-M4 queue check remain unverified.
- [x] T8 Docker cleanup skipped with reason recorded: `make docker-clean` removes toolbox containers, including the active command container. Production, volumes and backups were left intact; no space was reclaimed.
- [ ] The summary is delivered: changes, the `HOSTBUD_LLM_*` and `OPENAI_API_KEY` vars, e2e results and open owner items.
