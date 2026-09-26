# M2 — acceptance checklist

M2 is done when every box is ticked. Tasks: [M2-tasks.md](M2-tasks.md).

Setup for the manual checks: `.env` has the real `HOSTBUD_DOMAIN`, `TAILSCALE_IP` and `CLOUDFLARE_API_TOKEN`; `make deploy` on the host. Use a phone with Tailscale connected, and the same phone (or another device) with Tailscale off.

## Test coverage rule
Same as M1 ([M1-acceptance.md](M1-acceptance.md#test-coverage-rule)): every criterion names its **U** (unit), **I** (integration: `test/sshd` or the rendered deploy config) and **E** (e2e) tests and the task that writes each; n/a needs a one-line reason; "manual" only where automation can't observe it (a real certificate from Let's Encrypt, the tailnet, a real phone's keyboard). A ticked box means its automated tests exist and pass.

## Functional

### Domain and TLS
- [ ] Caddy is a custom build that includes the Cloudflare DNS provider, and the e2e stack uses the same image.
  - U: n/a (no code; a build file). I: T1 deploy-config check: built from `deploy/caddy/Dockerfile`; `caddy list-modules` lists `dns.providers.cloudflare`. E: T1 *Custom Caddy image*.
- [ ] `https://${HOSTBUD_DOMAIN}` is served with a certificate obtained via ACME DNS-01 (Cloudflare); the token is read from the environment at runtime and certificates persist across restarts.
  - U: n/a (Caddy config, no hostbud code). I: T2 `caddy adapt` of the production Caddyfile: ACME issuer with the Cloudflare DNS provider and `{env.CLOUDFLARE_API_TOKEN}`, no token value in the config, adapts with `ACME_EMAIL` empty · T1 certificate volumes. E: T2 *HTTPS domain path* (same proxy config on an internal-CA certificate). **Manual (T4):** the real certificate is valid on the phone (issuer Let's Encrypt, name matches).
- [ ] The HTTPS site is published only on `${TAILSCALE_IP}` (`:443`, `:80`), the loopback site only on `127.0.0.1:${HOSTBUD_LOCAL_PORT}`; nothing on `0.0.0.0`.
  - U: n/a (no code). I: T2 deploy-config check: exactly those three Caddy ports; no other service publishes except PostgreSQL's loopback port. E: n/a (the e2e stack publishes nothing by design). **Manual (T4):** `ss -ltn` on the host.
- [ ] From a phone on the tailnet, `https://${HOSTBUD_DOMAIN}` loads, signs in, attaches and types.
  - U: T3 viewport and terminal-input tests (Vitest). I: n/a (browser path; the PTY side is M1 T13). E: T2 *Domain UI* · T3 *Phone attach and type* in `iphone-13-pro-domain`. **Manual (T4):** a real phone on the tailnet.
- [ ] From outside the tailnet the domain is unreachable (the name resolves to a Tailscale address that only routes inside the tailnet; the DNS record is DNS-only, not proxied).
  - U: n/a (network, no code). I: T2 deploy-config check (Caddy binds only loopback and `${TAILSCALE_IP}`). E: n/a (e2e has no tailnet). **Manual (T4):** phone with Tailscale off can't connect; Cloudflare record shows "DNS only".
- [ ] The `localhost` port-forward path still works (`ssh -L 9055:localhost:9055 <host>` → `http://localhost:9055`).
  - U: M1 T12 Origin tests keep `http://localhost:<port>` · T2 `AllowedOrigins`. I: T2 `caddy adapt`: the loopback site is plain HTTP and proxies to `hostbud:8080`. E: the whole M1 suite (`desktop-chromium`, `iphone-13-pro` on `http://localhost:9055`) · T2 *Origin on both paths*. **Manual (T4):** port forward from another machine.

### Origin allowlist
- [ ] State-changing requests and WebSocket upgrades are accepted from `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}` only.
  - U: T2 `AllowedOrigins` and Origin middleware (both allowed, `http://<domain>`, other port, foreign; POST and upgrade). I: T2 deploy-config check: the app receives `HOSTBUD_DOMAIN`. E: T2 *Origin on both paths*.
- [ ] Over HTTPS the session cookie is `Secure` (and still `HttpOnly`, `SameSite=Lax`); over the loopback path it works without `Secure`.
  - U: M1 T8B cookie flags behind a trusted HTTPS proxy. I: n/a (header logic; covered by U and E). E: T2 *HTTPS domain path* (cookie flags) · M1 T8B *Registration/sign-in/logout* (loopback).

### Phone usability
- [ ] The terminal fits the viewport on a phone in portrait and landscape (no horizontal scroll, nothing cut off), and rotating resizes the tmux window.
  - U: T3 `--app-height` from `visualViewport` (Vitest) · M1 T17 ResizeObserver → `resize` frame. I: M1 T13 resize changes the window size on test sshd. E: T3 *Fits the viewport* · T3 *Rotate*.
- [ ] On-screen keyboard input works: tapping the terminal (or **Keyboard**) focuses it without zooming, typed text and Enter reach the session, and the keyboard doesn't cover the terminal.
  - U: T3 helper textarea attributes and 16px font · Keyboard button focuses the input · `--app-height` follows the visual viewport (Vitest). I: n/a (browser input; byte passthrough is M1 T13). E: T3 *Phone attach and type* (text input without key events, as the on-screen keyboard sends it). **Manual (T4):** a real iPhone/Android keyboard (Playwright's WebKit is not iOS Safari).
- [ ] Switching sessions on the phone works: back to the list, open another session, type there.
  - U: M1 T15 selection opens the terminal (Vitest). I: n/a (frontend). E: T3 *Switch sessions*.

## E2E (`make e2e`, simulated user)
Projects: `desktop-chromium` and `iphone-13-pro` on `http://localhost:9055` (the port-forward path, as in M1), plus `iphone-13-pro-domain` on `https://hostbud.example.test` (the domain path with an internal-CA certificate). API-level items run in `desktop-chromium` only. Each item is tagged with the task that adds it, in the same commit as the behavior.

- [ ] **(T1) Custom Caddy image:** the e2e Caddy is built from `deploy/caddy/Dockerfile`, and the suite runs through it.
- [ ] **(T2) HTTPS domain path:** over `https://hostbud.example.test`, sign-in sets a `Secure`, `HttpOnly` cookie; the sessions API lists a real-terminal session; `/ws/events` snapshots and `/ws/term` attaches over WSS (a marker reaches `capture-pane`).
- [ ] **(T2) Origin on both paths:** `Origin: https://hostbud.example.test` is accepted on the domain site for a POST and a WebSocket upgrade; `http://hostbud.example.test` and a foreign origin are rejected; `http://localhost:9055` keeps working.
- [ ] **(T2) Domain UI:** in `iphone-13-pro-domain` the app loads over HTTPS with no console errors, the user signs in through the form and sees the session list.
- [ ] **(T3) Phone attach and type:** tap a session, tap the terminal, type a command like the on-screen keyboard (text input, then Enter) → the output is in the browser terminal and in `capture-pane` (both phone projects).
- [ ] **(T3) Switch sessions:** back to the list, open a second session, type → the marker is in the second session; the first is detached (both phone projects).
- [ ] **(T3) Rotate:** portrait → landscape → portrait changes `#{window_width}x#{window_height}` each time, wider in landscape (both phone projects).
- [ ] **(T3) Fits the viewport:** in both orientations the terminal is inside the viewport, there's no horizontal page scroll, and the terminal input's font is at least 16px (both phone projects).
- [ ] **(every task) Kept green:** each task's commit ran `make e2e` green, and the M1 suite still passes.
- [ ] **(T4) Stable:** two consecutive full runs pass from a clean checkout.

## Security (AGENTS.md checklist, M2 scope)
- [ ] Caddy publishes only on `${TAILSCALE_IP}` (TLS) and `127.0.0.1:${HOSTBUD_LOCAL_PORT}`; hostbud publishes no ports.
  - U: n/a (no code). I: T2 deploy-config check. E: n/a (the e2e stack publishes nothing). **Manual (T4):** `ss -ltn`.
- [ ] WebSocket and state-changing requests check `Origin` against exactly `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}`.
  - U: T2 Origin middleware. I: T2 deploy-config check (`HOSTBUD_DOMAIN` reaches the app). E: T2 *Origin on both paths*.
- [ ] The Cloudflare token is never committed, logged or written into the adapted Caddy config; Caddy gets it as an env var and the app never sees it.
  - U: n/a (no hostbud code handles it). I: T2 deploy-config check (only `hostbud-caddy` has `CLOUDFLARE_API_TOKEN`) · T2 `caddy adapt` has no token value. E: n/a (e2e has no token). **Manual (T4):** `make gitleaks`; `make logs` shows no token.
- [ ] The DNS record is DNS-only (never proxied, never a Cloudflare Tunnel), so nothing is exposed publicly.
  - U: n/a. I: n/a (external service). E: n/a. **Manual (T4):** Cloudflare dashboard; README says so.

## Definition of done
- [ ] `make lint test` green; `make e2e` green in all three projects; every E2E item above added by the task it's tagged with.
- [ ] Every criterion's U / I / E tests exist and pass; each n/a has its reason, and "manual" is used only where allowed.
- [ ] `make gitleaks` clean; no real domain, Tailscale IP, token or username in tracked files (`git grep` for the `.env` values).
- [ ] README has the deployment guide; `.env.example` documents every variable; ARCHITECTURE matches what was built.
- [ ] Summary delivered: what changed, env vars the owner must set, manual host steps.
