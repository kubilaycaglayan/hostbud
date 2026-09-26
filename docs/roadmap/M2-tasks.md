# M2 — Deploy on the domain: tasks

Goal: from a phone on the tailnet, `https://${HOSTBUD_DOMAIN}` loads with a valid certificate and can attach and type; the domain is unreachable from outside the tailnet; the `localhost` port-forward path still works.

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m2--deploy-on-the-domain) · checklist: [M2-acceptance.md](M2-acceptance.md).

## Progress
Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Custom Caddy image | ✅ done |
| T2 TLS site on the Tailscale IP | ✅ done |
| T3 Phone usability | ✅ done |
| T4 Deployment guide and release | ✅ done |

Same rules as M1 ([M1-tasks.md](M1-tasks.md)): work top to bottom; each task ends with a green `make lint test` **and `make e2e`**, a clean `make gitleaks`, and its own conventional commit(s). Every task has a **Tests:** line (unit and integration tests it owes) and an **E2E:** line (scenarios it adds, tagged with the task in [M2-acceptance.md](M2-acceptance.md#e2e-make-e2e-simulated-user)), all landing in the same commit as the behavior. Tasks marked *(host)* need the real host, domain and tailnet to verify.

**What e2e can and can't reach.** The e2e stack has no Cloudflare token, public DNS or tailnet. It serves the *same* proxy config on an HTTPS site for a test domain (`hostbud.example.test`) with a certificate from Caddy's internal CA, so the browser path over HTTPS/WSS (Origin, Secure cookie, terminal) is simulated for real. The real certificate (DNS-01 against Cloudflare) and tailnet-only reachability stay manual checks, backed by integration checks on the rendered production config.

---

### T1 — Custom Caddy image
- `deploy/caddy/Dockerfile`: `caddy:<ver>-builder` + `xcaddy build --with github.com/caddy-dns/cloudflare@<pinned>` → `caddy:<ver>`, linux/amd64.
- `docker-compose.yml`: `hostbud-caddy` builds it (`image: hostbud-caddy:local`) and keeps certificates in named volumes `hostbud-caddy-data` (`/data`) and `hostbud-caddy-config` (`/config`), so restarts don't re-issue (Let's Encrypt rate limits).
- The e2e stack's Caddy is built from the same Dockerfile (rebuilt only when `deploy/caddy/Dockerfile` changes).
- `scripts/caddy-config.sh` (run by `make test-env`): builds the image when its inputs changed and writes `caddy list-modules` into `.cache/` for the deploy-config tests. T2 extends it with `caddy adapt`.

**Tests:** U: n/a (no Go code; a build file). I: deploy-config check: `hostbud-caddy` is built from `deploy/caddy/Dockerfile`; the built image lists `dns.providers.cloudflare`; its mounts are only the Caddyfile (read-only) and the two named volumes.

**E2E:** *Custom Caddy image (T1)*: the e2e Caddy is built from `deploy/caddy/Dockerfile`, and the whole suite (starting with *Open the app*) runs through it.

**Done:** `make test` passes the new deploy-config checks; `make e2e` green through the custom image.

### T2 — TLS site on the Tailscale IP
- Split the Caddy config so production and e2e share the proxy:
  - `deploy/caddy/hostbud.caddy`: a `(hostbud)` snippet (`reverse_proxy hostbud:8080`) and the loopback site `http://:{$HOSTBUD_LOCAL_PORT}`.
  - `deploy/caddy/Caddyfile`: global options (`admin off`; HTTP/1.1 and HTTP/2 only, since UDP isn't published), `import hostbud.caddy`, and the site `{$HOSTBUD_DOMAIN}` with `tls {$ACME_EMAIL} { dns cloudflare {env.CLOUDFLARE_API_TOKEN}; resolvers 1.1.1.1 1.0.0.1; propagation_delay 20s; propagation_timeout -1 }` (public resolvers for the zone lookup, since Docker's DNS may SERVFAIL on SOA; a fixed wait instead of the propagation check, which can sit on a cached NXDOMAIN). The token is read at runtime (`{env.…}`), never written into the adapted config.
- Compose publishes `${TAILSCALE_IP}:443:443`, `${TAILSCALE_IP}:80:80` and `127.0.0.1:${HOSTBUD_LOCAL_PORT}` — nothing on `0.0.0.0`. `HOSTBUD_DOMAIN`, `TAILSCALE_IP` and `CLOUDFLARE_API_TOKEN` become required (`:?` with a hint); `ACME_EMAIL` stays optional. Caddy gets only the variables it uses; the app gets `HOSTBUD_DOMAIN` (Origin allowlist).
- Origin allowlist: `https://${HOSTBUD_DOMAIN}` and `http://localhost:${HOSTBUD_LOCAL_PORT}` (already built in M1 T12; now wired and tested over both paths).
- e2e: the e2e Caddyfile imports `hostbud.caddy` unchanged and adds `https://hostbud.example.test` with `tls internal` (plus the `:9056` tmux-less site). The e2e app gets `HOSTBUD_DOMAIN=hostbud.example.test`; the name resolves to Caddy for the runner (`extra_hosts`). A Playwright project `iphone-13-pro-domain` runs the phone scenarios over HTTPS (`ignoreHTTPSErrors`, internal CA).

**Tests:** U: `AllowedOrigins` lists exactly the two origins (domain unset ⇒ localhost only); the Origin middleware accepts `https://<domain>` and `http://localhost:<port>` and rejects `http://<domain>`, `https://<domain>:8443` and foreign origins on POST and WebSocket upgrades. I: deploy-config check: `hostbud-caddy` publishes exactly `127.0.0.1:<local>`, `<TAILSCALE_IP>:443` and `<TAILSCALE_IP>:80`; the app receives `HOSTBUD_DOMAIN`; Caddy receives the token only as an env var. `caddy adapt` of the production Caddyfile (placeholder values): admin disabled; the domain site uses the ACME issuer with the Cloudflare DNS provider and `{env.CLOUDFLARE_API_TOKEN}`; the loopback site is plain HTTP; both proxy to `hostbud:8080`; the placeholder token value appears nowhere in the adapted JSON; with `ACME_EMAIL` empty the config still adapts.

**E2E:**
- **HTTPS domain path (T2, desktop, API level):** over `https://hostbud.example.test`: sign in; the session cookie is `Secure` and `HttpOnly`; `GET …/sessions` lists a real-terminal session; `/ws/events` sends a snapshot and `/ws/term` attaches over WSS (a marker reaches `capture-pane`).
- **Origin on both paths (T2, desktop, API level):** a POST and a WebSocket upgrade with `Origin: https://hostbud.example.test` are accepted on the domain site; `http://hostbud.example.test` and a foreign origin are rejected; `http://localhost:9055` keeps working on the loopback site.
- **Domain UI (T2, `iphone-13-pro-domain`):** the app loads over HTTPS with no console errors, signs in through the form and lists sessions.

**Done:** `make test` passes the Caddy/compose checks; `make e2e` green in all projects.

### T3 — Phone usability
- The app's height follows the *visual* viewport (`window.visualViewport`, `--app-height`, fallback `100dvh`) so the on-screen keyboard shrinks the terminal instead of covering its bottom rows (iOS and Android: both shrink the visual viewport; WebKit rejects the `interactive-widget` viewport key, so it isn't used).
- The terminal's hidden input (xterm's helper textarea) uses a 16px font (iOS doesn't zoom on focus) with autocorrect, autocapitalize and spellcheck off; `touch-action: manipulation` on the terminal (no double-tap zoom).
- A **Show keyboard** (⌨) button in the terminal header on touch screens focuses the terminal input (brings the on-screen keyboard back after it was dismissed); tapping the terminal does the same.
- The terminal fits the viewport in portrait and landscape: no horizontal page scroll, and the resize reaches tmux (M1's ResizeObserver path).

**Tests:** U (Vitest): `--app-height` follows a fake `visualViewport` (initial, resize, cleanup); the helper textarea gets the attributes and the 16px class; the Keyboard button focuses the terminal input. I: n/a (frontend only; the PTY → tmux resize is M1 T13's integration test).

**E2E (`iphone-13-pro` and `iphone-13-pro-domain`):**
- **Phone attach and type (T3):** open the app, tap a session, tap the terminal, type a command as the on-screen keyboard does (text input without key events, then Enter) → its output is in the browser terminal and in `capture-pane`.
- **Switch sessions (T3):** back to the list, open a second session, type → the marker lands in the second session; the first is detached.
- **Rotate (T3):** portrait → landscape → portrait: each rotation changes `#{window_width}x#{window_height}` on the target to match the new orientation (landscape wider than portrait).
- **Fits the viewport (T3):** in both orientations the terminal lies inside the viewport, the page doesn't scroll horizontally, and the terminal input's font size is at least 16px.

**Done:** Vitest and `make e2e` green; *(host)* manual pass on a real phone in [M2-acceptance.md](M2-acceptance.md).

### T4 — Deployment guide and release
- README: deployment guide — Tailscale on the host and the phone; Cloudflare `A` record `${HOSTBUD_DOMAIN}` → Tailscale IP, **DNS only**; API token scoped to `Zone:DNS:Edit` for that zone only; `.env` values; boot ordering (Docker publishing on the Tailscale IP needs it to exist: `After=tailscaled.service` drop-in or `net.ipv4.ip_nonlocal_bind=1`); stable ssh-agent socket; sshd on the host; the port-forward path; troubleshooting certificate issuance with `make logs`.
- ARCHITECTURE updated where the design moved (Caddy config split, e2e HTTPS site, HTTP/3 off, Caddy volumes); `.env.example` comments.
- *(host)* `make deploy`; the real certificate is issued; `ss -ltn` shows only the loopback port and the Tailscale IP; phone on the tailnet; unreachable off the tailnet; port forward still works.
- E2E and coverage audit as in M1 T18; summary to the owner.

**Tests:** none new beyond the audit (docs task).

**E2E:** audit only: every E2E item in [M2-acceptance.md](M2-acceptance.md) exists and passes in its projects, twice in a row from a clean checkout.
