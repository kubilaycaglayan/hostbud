# V2-M5 — LLM stale-run supervisor: tasks

Goal, scope, switch and the base task list: [ROADMAP.md](ROADMAP.md#v2-m5--llm-stale-run-supervisor-opt-in) (cited as **R T*n***). Design: [ARCHITECTURE.md](ARCHITECTURE.md) (**v2 §N**), v1 docs as **v1 ARCHITECTURE §N**. Checklist: [V2-M5-acceptance.md](V2-M5-acceptance.md). Pattern and rules as in [V2-M1-tasks.md](V2-M1-tasks.md) and [AGENTS.md](../../AGENTS.md).

This file does **not** repeat those documents. Each task points to its R T*n* entry and adds only the decisions it leaves open.

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Provider, config and status | Implemented; e2e scenarios written and type-checked |
| T2 Trigger and capture | Partial; due/budget/stale/restart/gone/rename covered; exact-prefix and large-pane integration coverage remain |
| T3 Result handling | Partial; guarded result, races, injection and most provider failures covered; provider-timeout e2e remains |
| T4 Privacy | Implemented; e2e privacy assertions written and type-checked |
| T5 Panel and notifications | Implemented; repeat-label dedupe and new-label notification scenarios written and type-checked |
| T6 Docs | Implemented; source docs/env updated and docs check passed |
| T7 Milestone acceptance | Partial; tests and deploy passed, scenario coverage remains incomplete |
| T8 Safe Docker cleanup | Skipped; active toolbox and unrelated project containers make cleanup unsafe |

**Precondition:** V2-M4 is done (its checklist ticked, open owner items excepted). Record the check in the Progress note.

## Rules for this milestone

The v2 ROADMAP *Rules*, AGENTS.md and the V2-M1 milestone rules (fast checks per commit, production untouched until the acceptance task, design changes into v2 docs in the same commit, bugs as failing test then fix, explicit staging) apply in full. e2e is written per task and **runs only in T7**; T8 is the safe Docker cleanup. This milestone adds:

- **Off = V2-M4.** With `HOSTBUD_LLM_PROVIDER` empty the supervisor isn't constructed: no timer, no `capture-pane`, no outbound request, no `source='llm'` rows, no `flag` field on events, no new notifications. The V2-M1–V2-M4 tests stay unchanged and green.
- **Flag only.** The supervisor's only write is inserting `run_events(source='llm')` rows through one narrow store method; it has no access to item, run, queue, gate or slot updates. No migration: `llm` is already in the `source` CHECK (v2 §6), and the budget and timer are derived from `runs.last_signal_at` and those rows.
- **Eligible runs:** a run in `running` (item `running`) or `stale` (item `needs_attention`, still its latest run). Never `starting`, never an ended run (`achieved`, `failed`, `exited`, `cancelled`), so never a gated item (`verifying`, `awaiting_approval`, whose run is `achieved`).
- **Visible result:** only the label and a short reason (≤ 200 chars, one line, control bytes stripped, scrubbed). `running` and `unknown` are recorded but show no badge. Pane text, prompt and raw answer are held in memory for the call only: never stored, logged (any level), put on events or shown.
- **Test fixtures:** e2e apps: `hostbud-e2e-app` keeps no provider; new `hostbud-e2e-app-llm` points at `hostbud-e2e-llmfake` (short quiet window, budget 3 for repeated-label/new-label notification coverage, VAPID keys, pushfake allowed); `hostbud-e2e-app-multi` gets a partial config (provider set, no key). The key in e2e config is an obvious placeholder.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T1 | `make lint test`, `scripts/compose-config.sh`, e2e `tsc` | `make lint test`, Compose config, e2e `tsc` passed |
| CP2 | T2–T4 | `make lint test` **three times in a row** (timers, retries, races, restart), e2e `tsc` | Three consecutive `make lint test` passes; e2e `tsc` passed; scenario gaps remain |
| CP3 | T5–T6 | `make lint test`, `vue-tsc`, e2e `tsc`, `make gitleaks`, docs check | lint/test, e2e `tsc`, gitleaks and docs check passed once |
| CP4 | T7 | `make lint test`, e2e `tsc`, `make gitleaks`, `make deploy` (no `make e2e`: on demand only) | Checks passed; deploy healthy with provider unset; milestone scenario gaps remain |

**Progress note:** V2-M4 acceptance was checked before starting; it is complete with owner checks and on-demand e2e still open. Current branch has the provider and supervisor implementation commits. Three consecutive `make lint test` runs passed; Compose config, e2e `tsc`, docs check and `make gitleaks` passed. `make deploy` succeeded; health is OK and `HOSTBUD_LLM_PROVIDER` is empty in the deployed container. The protected status endpoint returned 401 without a session. `make docker-clean` was skipped because it removes active toolbox containers (including this session's tool container); unrelated project containers are also running. The full e2e suite has not been run (on-demand only), and uncovered scenario gaps remain.

---

## T1 — Provider, config and status

Scope: R T1. Additions:
- **Config:** provider `openai` with model and key set and every value valid ⇒ on. Unknown provider, missing model or key, a key with whitespace, a bad `HOSTBUD_LLM_BASE_URL`, or an invalid quiet window, budget or scrub value ⇒ supervisor **off**, never a startup error: one warn log naming the vars (never values), and `GET /api/supervisor` reports `{enabled: false, reason}`, e.g. "LLM supervisor is off: set OPENAI_API_KEY (HOSTBUD_LLM_PROVIDER=openai)". On: `{enabled, provider, model, scrub, quietAfter, maxPerRunHour}`, never the key. Authenticated, in `routes.json`. No provider call at startup.
- **Base URL:** default is the provider's; override must be `https`, except `http` to a single-label host (a Compose service name, for the fake). No redirects; the key goes only to that origin.
- **Client:** structured output (JSON schema with the six labels and `reason`); 20 s per attempt; timeout, 429 and 5xx retried at most twice with backoff (`Retry-After` honored, capped at 30 s), ≤ 60 s per classification. Any other 4xx, malformed JSON, extra fields or a label outside the enum ⇒ `unknown` with an actionable reason (401 ⇒ "provider rejected the API key — check OPENAI_API_KEY").
- **Tests:**
  - U: the config matrix (each var missing or invalid ⇒ off + reason, key never in the reason or logs); request shape; each failure ⇒ `unknown` or retry, attempts ≤ 3; redirects refused.
  - I: log hygiene: a run with a marker key and marker pane text at info and debug, logs contain neither; a local fake provider over the real HTTP stack.
- **E2E:** `supervisor.api.spec.ts`: *Supervisor status* (on for `app-llm`, no key in the body; off with reason on `app`; off naming `OPENAI_API_KEY` on `app-multi`, which stays healthy). *No provider, no calls* (`app`: a `silent` stub run past the window has no flag and no `llm` events; llmfake saw no request from it).

## T2 — Trigger and capture

Scope: R T2. Additions:
- **Due:** eligible and `now ≥ max(last_signal_at or started_at, last claim) + HOSTBUD_LLM_QUIET_AFTER`, or on the transition to `stale`; and fewer than `HOSTBUD_LLM_MAX_PER_RUN_HOUR` claims in the last 60 min. At most one in flight per run, two overall.
- **Claim before call:** an `llm_started` row commits before `capture-pane`; claims count against the budget whatever their outcome. On restart a claim without a result gets `llm_result` `unknown` ("hostbud restarted during classification") and isn't re-run; timers are recomputed from stored rows, so a restart makes no extra call.
- **Capture:** `sshx` argv `tmux capture-pane -p -J -t =<name>: -S -200`, name checked by the v1 session-name validator first. Read at most 256 KiB of stdout, then keep the last 200 lines. "can't find session" (gone or renamed) ⇒ the claim ends `skipped`, no provider call, not counted; never a prefix match (`run-1` gone never captures `run-10`).
- **Tests:**
  - U: due and budget with a fake clock (quiet, stale, window edges, restart with fresh and old claims); eligibility for every run and item state; argv and validation.
  - I: against `test/sshd`: capture of a real session; the exact target with a prefix-named neighbour; gone and renamed sessions; a 5 MiB pane capped; the `sshx` log has only `capture-pane`.
- **E2E:** `supervisor.api.spec.ts` (`app-llm`, new stub behavior `quiet-print` prints ctl-set text then sends no hooks; ctl `/llmfake/script` and `/llmfake/requests`): *Quiet run is flagged* (R), *Stale run is classified*, *Budget respected* (R), *Session gone or renamed*, *Gated items not classified* (a long verify and an awaiting-approval item past the window: no request), *Restart keeps budget and timer* (`/app/restart` after two claims: no third request).

## T3 — Result handling

Scope: R T3. Additions:
- **Guarded result:** `llm_result` (label, reason) is inserted with the flag only if, in the same statement, the run is still eligible and `last_signal_at` equals its value at claim time. Otherwise it's recorded `discarded`: no event, no badge, no notification. So a run that achieves, exits or gets an owner action (Mark done, Skip, Retry) mid-classification gets no flag.
- **Flag lifetime:** the latest flagging result is shown until a newer hook signal or the run ends. `run.changed` carries `flag: {label, reason, at}` or `null` (field only while on).
- **`completed`:** "looks finished — check and Mark done". Mark done stays where V2-M1 allows it (`needs_attention`, e.g. stale); on a running item the badge links to the session. The supervisor adds no owner action.
- **Injection:** the fixed system prompt treats the pane as untrusted data; the pane goes as one JSON string field in the user message. A label from injected text can only produce a flag.
- **Tests:**
  - U: every label; the guard against achieved, exit, stale ⇒ achieved, Mark done, Skip, Retry and a new signal (each raced 50 times); queue, item, run and slot rows unchanged after every result, including `completed`; injection text stays inside the data field.
  - I: a fake provider with delay while the stub achieves: item `done`, queue advances as in V2-M4, no flag.
- **E2E:** `supervisor.api.spec.ts`: *Completed label never advances* (R), *Completed races achieved*, *Run ends mid-classification*, *Owner action during classification*, *Provider errors* (503 then 200 ⇒ one flag; 429 ×3, timeout, malformed, out-of-schema ⇒ `unknown`, no badge, item unchanged), *Prompt injection* (pane asks to answer `completed` and mark done: nothing changes).

## T4 — Privacy

Scope: R T4. Additions:
- **Pipeline:** strip ANSI and control bytes ⇒ scrub ⇒ cap to the last 8 KiB. `HOSTBUD_LLM_SCRUB` default `true`; only `false` turns it off. The run's own `HOSTBUD_RUN_TOKEN` value is always removed, even with scrubbing off.
- **Patterns:** `NAME=value` / `NAME: value` where the name contains KEY, TOKEN, SECRET, PASS or AUTH; `Bearer …`; known prefixes (`sk-`, `ghp_`, `github_pat_`, `xox`, `AKIA`); JWTs; PEM blocks; URL userinfo; runs of ≥ 32 base64/hex chars. Replaced with `[redacted]`.
- **Tests:** U: each pattern, near-misses kept, cap after scrub, token removed with scrub off, reason re-scrubbed. I: n/a (pure text; T1's log-hygiene test covers the logs).
- **E2E:** `supervisor.api.spec.ts`: *Scrubbed before sending* (secrets and the run token printed by `quiet-print` are absent from the request llmfake recorded and from the reason), *Pane text not exposed* (run API and events carry only label and reason).

## T5 — Panel and notifications

Scope: R T5. Additions:
- **Panel:** badge (label, time) on the item, reason as text in the item view, updated live from `run.changed`; supervisor status and the privacy note in settings.
- **Notifications (V2-M3):** a new flag notifies under `on_attention` as "needs your input", key `run:<id>:llm:<label>`, kind `flag`, outcome = label; the allowlist is unchanged, so no reason or pane text. `running` and `unknown` never notify.
- **Tests:** U (Vitest): badge per label, reason as text (no HTML), live clear. U (Go): the payload per label, once per key.
- **E2E:** `supervisor.spec.ts` (desktop) and `supervisor.phone.spec.ts`: *Flag badge*. `supervisor-notify.api.spec.ts`: *Flag notifies once* (pushfake: one per run and label; same label again ⇒ none; new label ⇒ one; payload allowlisted).

## T6 — Docs

Scope: R T6. Also: v2 §4/§5 (supervisor, eligibility, flag lifetime), the v2 ROADMAP status line, `.env.example` (all vars below, placeholders only), README (what leaves the host, scrubbing, cost bound), and the docs check covering the vars and routes.
- **Tests:** the docs check. **E2E:** n/a (documents).

## T7 — Milestone acceptance

Scope: R T7, done as V2-M1 T13. Deploy checks: with no provider in production `.env`, nothing calls out and existing queues advance. Tick the checklist; write the summary with the new vars and the open owner items.
- **E2E:** no new scenarios; no run (on demand only; a full suite on every e2e app runs when the owner asks).

## T8 — Safe Docker cleanup

As in V2-M1 T14; the `docker-clean` recipe also removes the `hostbud-e2e-llmfake` image and container. Never close a tmux session.
- **Tests/E2E:** n/a (operations; the health check verifies).

---

## New configuration

All optional; empty provider = off. `HOSTBUD_LLM_PROVIDER` (`openai`), `HOSTBUD_LLM_MODEL`, `OPENAI_API_KEY`, `HOSTBUD_LLM_BASE_URL` (T1); `HOSTBUD_LLM_QUIET_AFTER` (default `20m`, same range as `HOSTBUD_RUN_STALE_AFTER`), `HOSTBUD_LLM_MAX_PER_RUN_HOUR` (default 2, 1–20) (T2); `HOSTBUD_LLM_SCRUB` (default `true`) (T4).
