# V2-M3 — Notifications: acceptance checklist

V2-M3 is done when every box is ticked. The exception is *Manual checks (owner)*: those are the owner's backlog and never block ([AGENTS.md](../../AGENTS.md)).

Links: tasks in [V2-M3-tasks.md](V2-M3-tasks.md); criteria 1–7 and the owner check in [ROADMAP.md](ROADMAP.md#v2-m3--notifications-opt-in); design in [ARCHITECTURE.md](ARCHITECTURE.md).

Each box is one roadmap criterion. Its coverage line names the task that writes each U/I/E test; sub-bullets are the details the tasks decided. The test rules are V2-M1's ([V2-M1-acceptance.md](V2-M1-acceptance.md)): stubs only, E items written per task and type-checked. `make e2e` runs **only on demand** (v2 ROADMAP *Rules*, owner's decision 2026-09-27): a box is ticked when its U/I tests pass and its E scenarios are written; the full run stays open until the owner asks for it.

## Criteria

- [x] **1 Off by default; no permission prompt until the owner opts in; denied and revoked permission handled.**
  - No `notification_prefs` row = off. `requestPermission` is called only from the Settings toggle.
  - Denied: the toggle stays off with how to allow it in the browser. Revoked later: this device's subscription is deleted and Settings says so; other devices keep theirs.
  - U: T0, T4 · I: T0 (a fresh account reads off) · E: T4 *Notification settings*, *Permission denied*, *Permission revoked*.
- [x] **2 The three events notify in-app and through push, with no sensitive content.**
  - Only done, needs attention and queue finished; intermediate states never notify. A click opens the item (same-origin paths only).
  - One builder, one allowlisted payload `{v, kind, key, project, position, outcome, url}` ≤ 1 KiB: no instruction, flags, paths, session names, `detail`, pane output, tokens or emails on the lock screen.
  - Push is RFC 8291/8292; endpoints are `https`, port 443, DNS hosts, no redirects; the SW cache rules and the CSP are unchanged.
  - U: T1, T2 · I: T2 (pushfake decrypts) · E: T1 *In-app notification*, T2 *Push subscription*, *Lock-screen payload*, T4 *Test notification*.
- [x] **3 Expired subscriptions (`404`/`410`) are removed.**
  - Deleted at once with their pending deliveries, never retried. 429/5xx retry at most 3 times; other 4xx are dropped. Info logs never carry the endpoint.
  - U: T3 · I: T3 (pushfake 410, 500 then 201) · E: T3 *Expired subscription*.
- [x] **4 With every account off, behavior is exactly V2-M2's.**
  - No outbox rows, no outbound request, no `notification` field on events, no prompt; the V2-M1 and V2-M2 tests run unchanged.
  - U: T1, T2 · I: T0 (V2-M2 data migrates with equal checksums; append-only) · E: T1 *Switch off*, and the V2-M1/V2-M2 suites run in T6.
- [x] **5 Missing or invalid VAPID keys turn push off with an actionable message, not a startup error.**
  - None, some, or an invalid key: the app starts, one warn log names the vars (no values), Settings shows the reason with `make vapid-keys`; in-app notifications still work.
  - U: T0 · I: T0 (`deploytest`: the vars reach only `hostbud`; `HOSTBUD_PUSH_TEST_ENDPOINT` absent in production) · E: T0 *Push unavailable*.
- [x] **6 Each device and account gets its own notifications, never twice.**
  - Subscriptions and event choices are per account; an endpoint moves to the account that subscribes it last; sign-out removes only that device.
  - Dedupe key per event (`Topic` and `tag`), unique per account in the outbox, written in the transition's transaction; a delivery is claimed before its POST and never re-sent after a restart. A device with push gets no extra in-app copy.
  - U: T2, T3 · I: T2 (two accounts, three subscriptions), T3 (restart with pending and claimed rows) · E: T2 *Devices and accounts*, T3 *No duplicates*.
- [x] **7 The docs are aligned** as listed in R T5 and T5.
  - U/I: T5 docs check · E: n/a (documents).

Status (2026-09-28): criteria 1–7 are ticked with their U/I tests passing (CP1–CP4; CP2 three times in a row) and their E items written and type-checked. Commits: T0 `aa8bacd`, T1 `f074c12`, T2 `2f4ffbd`, T3 `b20c1f2`, T4 `477e4cf`, T5 `0f45ba2`.

## E2E scenarios

Written in T0–T4 and type-checked, on desktop and `iphone-13-pro` (permission flows where the profile can grant it). They run only on demand.

- [x] (T0) Settings API — written
- [x] (T0) Push unavailable — written
- [x] (T1) In-app notification — written
- [x] (T1) Switch off — written
- [x] (T2) Push subscription — written
- [x] (T2) Lock-screen payload — written
- [x] (T2) Devices and accounts — written
- [x] (T3) Expired subscription — written
- [x] (T3) No duplicates — written
- [x] (T4) Notification settings (desktop and phone) — written
- [x] (T4) Permission denied — written
- [x] (T4) Permission revoked — written
- [x] (T4) Test notification — written
- [ ] (on demand) Full suite green on every e2e app: every v1, V2-M1, V2-M2 and V2-M3 scenario, both profiles, no skips or weakened assertions — run only when the owner asks; open until then, not a blocker

## Manual checks (owner; backlog, not blockers)

- [ ] Push on the installed iPhone PWA and on a desktop browser with hostbud closed (ROADMAP).
- [ ] Generate VAPID keys with `make vapid-keys` and set them in `.env` (safe default: unset, push off with the message).
- [ ] Turn notifications on for your own account (safe default: off).

## Definition of done

- [x] Every criterion is ticked (E items as written and type-checked; `make e2e` runs only on demand and a pending run is listed as open).
- [x] `make lint test` is green, `make gitleaks` is clean, and nothing host-specific is tracked (no VAPID private key, no endpoints). (CP4, 2026-09-28)
- [x] *(host)* `make deploy` succeeded: the stack is healthy, V2-M2 queues are intact, every account is off unless the owner turned it on, and push is available or shows the reason. (2026-09-28: health ok; migration 8 (already applied by an earlier deploy from this tree, a no-op now); queue and item counts and checksums equal before and after; no `notification_prefs` row enabled, no subscription, no outbox row; no VAPID keys set, so the app logs one warning naming the three vars and Settings shows the reason. Backup taken afterwards.)
- [x] T7 Docker cleanup is done (including `hostbud-e2e-pushfake`), or skipped with the reason recorded; production, volumes and backups are intact, and the reclaimed space is reported. (2026-09-28: five toolbox containers, about 4 MB.)
- [x] The summary is delivered: changes, the new env vars, host steps (outbound HTTPS, VAPID keys), e2e results and open owner items. (2026-09-28)
