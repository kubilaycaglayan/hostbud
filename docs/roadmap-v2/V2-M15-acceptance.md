# V2-M15 — Fixed-time window nudge: acceptance checklist

## Criteria

- [x] **1. One nudge configuration is durable and opt-in.** One saved row targets exactly one registered machine and absolute directory, chooses Claude or Codex and one of five schedule presets, defaults disabled, and retains only bounded operational status. — U: T1/T2 (validation and serialization); I: T1/T2 (PostgreSQL/API round trip); E: T2 (API configuration scenario).
- [x] **2. Daily schedule uses the fixed UTC+03:00 slots and skips missed occurrences.** The choices are `01,06,11,16,21`; `02,07,12,17,22`; `03,08,13,18,23`; `04,09,14,19,00`; `05,10,15,20,00`. Activation waits for the next strictly future slot. Downtime and clock jumps skip elapsed slots; a unique transactional claim prevents duplicate work across app instances and claimed work is never replayed after restart. — U: T1; I: T1; E: n/a (server clock transitions are deterministically tested at U/I layers; E2E checks the returned next slot in T2).
- [x] **3. Every claimed slot replaces only the exact nudge session and sends the provider prompt.** On the chosen machine and directory, hostbud ends only `window-nudge-chore` if present, creates exactly that name running the chosen CLI, then sends `hi, please kill this tmux session window-nudge-chore`. A collision or failure is recorded without a late retry; other tmux sessions survive. — U: T3; I: T3 (`test/sshd`, stub Codex; exact-name collision and recreation in session integration tests); E: n/a (the timer side effect is verified against the throwaway SSH target at I; arming and API-visible state are exercised in T2/T4).
- [x] **4. The top-bar control configures and clearly arms the feature.** The dialog selects machine, directory, provider and preset. A confirmation explains recurring exact-session termination. The button is filled orange while enabled; enabling waits for the next slot, disabling stops future slots without killing the current session, and the next time/last outcome are visible. Desktop and phone can configure it. — U: T4; I: T2/T3 (API persistence and target behavior); E: T4 desktop/phone scenarios (written and type-checked).
- [x] **5. Standard protections and delivery checks hold.** API requires auth and Origin for state changes, SQL stays in store, remote commands use the shared session service/sshx quoting, no new env variables or host timezone changes are needed, docs are aligned, `make lint test`, E2E type-check, gitleaks and deployed health pass. — U: T2/T3; I: T2/T3/T5; E: T2/T4 (Origin/config), T5 type-check.

## Manual checks (owner; backlog, not blockers)

- [ ] Start the schedule with a real Claude or Codex installation and verify the nudge reaches the intended client (open; never run by automated tests).
- [ ] Review the exact-session replacement confirmation and chosen daily UTC+03:00 schedule on the deployed app (open).
- [ ] Host timezone check: the host reports UTC+00:00, not UTC+03:00. The schedule uses fixed UTC+03:00 and needs no host timezone change; owner review remains open.
- [ ] E2E scenarios are written/type-checked; on-demand browser run remains open.

## Definition of done

- [x] All five criteria pass; migrations are append-only; no real tmux session was touched during implementation.
- [x] `make lint test`, E2E TypeScript check, `make gitleaks`, deploy and `/api/health` pass.
- [x] Safe Docker cleanup skipped because hostbud test targets and toolbox containers are active; production volumes and containers remain intact.
- [x] Summary lists changes, new env vars (none expected), manual host steps and open owner items.
