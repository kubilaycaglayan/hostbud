# V2-M3 — Notifications: tasks

Goal, scope, switch and the base task list: [ROADMAP.md](ROADMAP.md#v2-m3--notifications-opt-in) (cited as **R T*n***). Design: [ARCHITECTURE.md](ARCHITECTURE.md) (**v2 §N**), v1 docs as **v1 ARCHITECTURE §N**. Checklist: [V2-M3-acceptance.md](V2-M3-acceptance.md). Pattern and rules as in [V2-M1-tasks.md](V2-M1-tasks.md) and [AGENTS.md](../../AGENTS.md).

This file does **not** repeat those documents. Each task points to its R T*n* entry and adds only the decisions it leaves open. T0 is new: the schema and switch that R T1–T4 all need.

## Progress

Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T0 Schema, switch and VAPID config | Not started |
| T1 In-app notifications | Not started |
| T2 Web Push | Not started |
| T3 Outbound delivery | Not started |
| T4 Settings UI | Not started |
| T5 Docs | Not started |
| T6 Milestone acceptance | Not started |
| T7 Safe Docker cleanup | Not started |

**Precondition:** V2-M2 is done (its checklist ticked, open owner items excepted). Record the check in the Progress note.

## Rules for this milestone

The v2 ROADMAP *Rules*, AGENTS.md and the V2-M1 milestone rules (fast checks per commit, production untouched until the acceptance task, design changes into v2 docs in the same commit, bugs as failing test then fix, explicit staging) apply in full. e2e is written per task and **runs only in T6**; T7 is the safe Docker cleanup. This milestone adds:

- **The switch** is per account, stored server-side (T0); no row = off. With every account off, nothing new runs: no permission prompt, no service worker `push` subscription, no outbox rows, no outbound request. The V2-M1 and V2-M2 tests stay unchanged and green.
- **Payload allowlist.** A notification carries only `{v, kind, key, project, position, outcome, url}`. `url` is an app path (`/queues/<id>?item=<id>`). Never instruction text, flags, paths, session names, `detail`, pane output, tokens or emails. One builder (`internal/notify`) makes every title and body, for in-app and push alike; it caps the payload at 1 KiB.
- **One notification per event and device.** The dedupe key is `run:<id>:done`, `run:<id>:attention` or `queue:<id>:finished:<run id>`. It is the push `Topic` (hashed) and the `Notification` `tag`, so a device that sees it twice replaces it instead of stacking it.
- **Push test endpoint:** subscription endpoints must be `https`, on port 443, with a DNS host (no IP literal); delivery follows no redirects. The e2e-only var `HOSTBUD_PUSH_TEST_ENDPOINT` (a URL prefix, empty in production) lets `hostbud-e2e-pushfake` through.

| Checkpoint | After | Runs | Status |
|---|---|---|---|
| CP1 | T0–T1 | `make lint test`, `scripts/compose-config.sh`, e2e `tsc` | Not run |
| CP2 | T2–T3 | `make lint test` **three times in a row** (retries, timers, restart), e2e `tsc` | Not run |
| CP3 | T4–T5 | `make lint test`, `vue-tsc`, e2e `tsc`, `make gitleaks`, docs check | Not run |
| CP4 | T6 | `make lint test`, e2e `tsc`, `make gitleaks`, `make deploy` (no `make e2e`: on demand only) | Not run |

**Progress note:** (precondition and checkpoint results go here.)

---

## T0 — Schema, switch and VAPID config

Scope: R T2 (table, env vars). Additions:
- **One migration**, append-only: `notification_prefs(user_id PK, enabled, on_done, on_attention, on_finished)` (`enabled` defaults to `false`, the three event choices to `true`), `push_subscriptions(id, user_id, endpoint UNIQUE, p256dh, auth, created_at)`, and `notification_outbox(id, user_id, dedupe_key, payload_json, created_at, UNIQUE(user_id, dedupe_key))` with `notification_deliveries(outbox_id, subscription_id, claimed_at, status, PK(outbox_id, subscription_id))` (T3).
- **VAPID config:** all three vars set and valid ⇒ push available. None, some, or an invalid key ⇒ push **off**, never a startup error: one warn log naming the missing or invalid vars (never values), and the settings API reports `push: {available: false, reason: "Push is off: set HOSTBUD_VAPID_PUBLIC_KEY, HOSTBUD_VAPID_PRIVATE_KEY and HOSTBUD_VAPID_SUBJECT (make vapid-keys)"}`. In-app notifications still work. `make vapid-keys` prints a pair from a container and writes nothing.
- **API:** `GET`/`PUT /api/notifications/settings` (the caller's account only), authenticated, Origin-checked, in `routes.json`. It returns the prefs, `push`, and the VAPID public key when available.
- **Tests:**
  - U: config cases (none, partial, invalid, valid); a fresh account reads off; one account's PUT never changes another's.
  - I: seed a V2-M2 database, migrate: row counts and checksums equal, second apply a no-op, no `DROP`/`UPDATE`/`DELETE` in the file; `deploytest`: the VAPID vars reach only `hostbud`, `HOSTBUD_PUSH_TEST_ENDPOINT` is absent from production.
- **E2E:** `notifications.api.spec.ts`: *Settings API* (fresh account off, enable, bad Origin → 403). *Push unavailable* on `hostbud-e2e-app-multi`, which gets no VAPID keys: the reason names the vars and the app is healthy.

## T1 — In-app notifications

Scope: R T1. Additions:
- `run.changed` / `queue.changed` for the three transitions carry a `notification` object built by `internal/notify` (the allowlisted payload), so in-app and push wording match. The field is attached only while at least one account is on. The client never builds text from other event fields.
- The client notifies only when the account is on, the event is chosen, `Notification.permission === "granted"`, and this device has **no** push subscription (then push shows it). It never calls `requestPermission` itself.
- Reconnect resync never replays: only live events after page load notify, and seen keys are kept for the page's life.
- E2E builds wrap `Notification` in a spy (`window.__notifications`).
- **Tests:** U (Vitest): the three events, intermediate states ignored, switch off, event off, permission `default`/`denied`, push-subscribed device skipped, resync replay ignored, text has no instruction or path. I: n/a (browser only).
- **E2E:** `notifications.spec.ts` (Chromium, permission granted, account on via API): *In-app notification* (a stub item achieves; one notification with the expected title and tag; click opens the item). *Switch off*: the same run with the account off shows nothing.

## T2 — Web Push

Scope: R T2. Additions:
- **Subscribe:** `POST /api/notifications/subscriptions {endpoint, keys}` and `DELETE …` by endpoint, for the caller's account. The same endpoint subscribed by another account moves to it (a shared device follows who is signed in). Sign-out deletes the device's subscription first.
- **Encryption:** RFC 8291 `aes128gcm` with the VAPID JWT (RFC 8292), through a maintained Go library; `TTL` 24 h, `Urgency: high` for needs attention.
- **Fan-out:** a run or queue transition writes one outbox row per opted-in account whose event choice matches, **in the same store transaction** as the transition. Duplicate signals don't transition twice (V2-M1), and the unique `(user_id, dedupe_key)` catches the rest.
- **Service worker:** `push` shows the payload (title, body, `tag` = key); `notificationclick` focuses an open client or opens `url`, only if it is a same-origin path. Precache and bypass rules unchanged.
- **CSP:** reviewed; no change expected (push is server-side, `worker-src 'self'` stays).
- **`hostbud-e2e-pushfake`:** records each POST (headers and body), answers per endpoint as set by a ctl action (201, 404, 410, 500, slow). The e2e app's VAPID pair is generated at `e2e-up`, never committed.
- **Tests:**
  - U: encryption round-trip; payload allowlist (an item whose instruction, flags and path hold marker strings, and a token in `detail`, yields a payload with none of them); the endpoint rules; the account move.
  - I: against pushfake, a transition sends one decryptable POST; two accounts, one with two subscriptions, get three POSTs with each account's own event choice.
- **E2E:** `push.api.spec.ts` (subscriptions with test-held keys at pushfake endpoints): *Push subscription* (R); *Lock-screen payload* (decrypted: only allowlisted fields, no marker, path or token); *Devices and accounts* (as in I, plus sign-out removes that device only).

## T3 — Outbound delivery

Scope: R T3. Additions:
- **Sender:** one goroutine drains pending deliveries. Timeout 10 s per POST; retries on network errors, 429 and 5xx, at most 3 attempts (1 s, 5 s, 25 s, `Retry-After` honored up to 60 s); other 4xx are dropped. Info logs carry the subscription id and status, never the endpoint.
- **Expired:** 404 or 410 deletes the subscription (and its pending deliveries) at once.
- **At most once:** a delivery is claimed (`claimed_at`) in a committed transaction before the POST and never re-sent after a claim. After a restart, unclaimed rows are sent; claimed ones aren't. A crash mid-POST loses at most that one notification.
- **Pruning:** outbox and delivery rows older than 7 days are deleted (operational rows, not user data).
- **Tests:**
  - U: retry schedule, `Retry-After`, 404/410 deletion, other 4xx, claim-before-send, restart replay, logs without endpoints.
  - I: pushfake answers 500 then 201 (one notification), 410 (row gone, no retry); the app restarts with pending and claimed rows (each key reaches pushfake once).
- **E2E:** `push.api.spec.ts`: *Expired subscription* (410 and 404: removed from the list, the next event doesn't reach it); *No duplicates* (the stub sends its `Stop` hook twice, then the app restarts via `/app/restart`; pushfake saw each key once).

## T4 — Settings UI

Scope: R T4. Additions:
- The toggle asks for permission **only on click**. `denied` keeps it off and explains how to allow notifications in the browser's site settings. On iOS outside an installed PWA, it says to add hostbud to the home screen first.
- **Revoked later:** on app start, if the account is on but this device's permission isn't `granted`, the device's subscription is deleted and Settings shows "Notifications are blocked on this device". Other devices are unaffected.
- The push-unavailable reason from T0 is shown as is. "Send test notification" goes to this device only, rate-limited like M7's writes.
- **Tests:** U (Vitest): off by default, no `requestPermission` before the click, denied, revoked, iOS hint, unavailable reason, test button. I: route table and Origin check for the test route.
- **E2E:** `notifications.spec.ts` and `notifications.phone.spec.ts`: *Notification settings* (off by default, no prompt before the toggle, per-event choice saved); *Permission denied*; *Permission revoked* (clear permissions, reload: message shown, subscription gone); *Test notification* (reaches pushfake). Where a profile can't grant permission, it checks the denied state.

## T5 — Docs

Scope: R T5. Also: v2 §4 and §9 (notifier, outbox, endpoint rules), the v2 ROADMAP status line linking these files, `.env.example` (VAPID vars, `HOSTBUD_PUSH_TEST_ENDPOINT` marked e2e-only), README (outbound HTTPS, `make vapid-keys`), and the docs check covering the new vars and routes.
- **Tests:** the docs check. **E2E:** n/a (documents).

## T6 — Milestone acceptance

Scope: R T6, done as V2-M1 T13 (the deploy checks; e2e fixing rules apply to on-demand runs; plus: every account is off unless the owner turned it on, and a missing VAPID set shows the reason instead of failing). Tick the checklist with dates and commits (E items as written until an on-demand run passes them); write the summary with the new env vars and the open owner items.
- **E2E:** no new scenarios; no run (on demand only; a full suite on every e2e app runs when the owner asks).

## T7 — Safe Docker cleanup

As in V2-M1 T14. Extend `docker-clean` to remove `hostbud-e2e-pushfake` (R T7). Never close a tmux session.
- **Tests/E2E:** n/a (operations; the health check verifies).

---

## New configuration

`HOSTBUD_VAPID_PUBLIC_KEY`, `HOSTBUD_VAPID_PRIVATE_KEY`, `HOSTBUD_VAPID_SUBJECT` (T0): optional; unset keeps push off with a message. `HOSTBUD_PUSH_TEST_ENDPOINT` (T2): e2e only, empty in production.
