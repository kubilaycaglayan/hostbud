# M1 — acceptance checklist

M1 is done when every box is ticked. Tasks: [M1-tasks.md](M1-tasks.md).

Setup for the manual checks: `make deploy` on the host, then from another machine `ssh -L 9055:localhost:9055 <host>` and open `http://localhost:9055`. Keep a real terminal on the host open alongside (`ssh <host>`).

## Functional

### Session list
- [ ] The UI lists every tmux session on the host (name, attached/detached, window count).
- [ ] With no tmux server running, the list is empty — no error.
- [ ] `tmux new -d -s acc-a` in the real terminal → `acc-a` appears within one poll interval (default 3s), without reloading.
- [ ] `tmux kill-session -t acc-a` in the real terminal → it disappears within one poll interval.
- [ ] `tmux attach -t acc-b` / detach in the real terminal → the attached dot updates.

### Create / rename / kill
- [ ] Create with only a path (default `~`) → session named after the directory, started in that directory.
- [ ] Create with a name, a path like `~/some/dir`, and a start command (e.g. `htop`) → session runs the command in that directory; `tmux ls` in the real terminal shows it.
- [ ] Invalid name (e.g. `a.b`, `a:b`, a space) is rejected in the form and by the API.
- [ ] Duplicate name or a non-existent path → clear, actionable error.
- [ ] Rename from the UI → `tmux ls` shows the new name; the UI updates.
- [ ] Kill from the UI asks for confirmation; Cancel leaves the session alive; Confirm kills it.

### Terminal
- [ ] Clicking a session attaches in a full terminal; typing works.
- [ ] Claude Code renders and behaves correctly (input, scrolling output, colors).
- [ ] vim works (insert mode, `:q`, arrow keys, colors).
- [ ] htop renders correctly and responds to keys; mouse clicks work if tmux `mouse` is on.
- [ ] Resizing the browser window resizes the tmux window (`tmux display -p '#{window_width}x#{window_height}'`).
- [ ] Closing the tab ends only the attach: the session keeps running (`tmux ls`).
- [ ] Detaching (`prefix d`) or the session exiting shows an exit state with a Reconnect button.

### Robustness
- [ ] `docker compose restart hostbud` → the UI comes back and sessions are still listed; attached terminals can reconnect.
- [ ] Stopping sshd on the host (or breaking the agent socket) shows an "unreachable" banner with a hint; restoring it recovers without restarting hostbud.
- [ ] With tmux missing (test sshd image without tmux), the UI shows "tmux not found — install with …".

## E2E (`make e2e`, simulated user)
Every scenario passes in **both** Playwright projects (`desktop-chromium`, `iphone-13-pro`) against the throwaway target — never the real host. UI actions go through what a user sees; outcomes are checked in the UI **and** on the target (`tmux ls` / `capture-pane`).

- [ ] **Open the app:** `http://localhost:9055` loads the shell; the session list is empty on a fresh target, with no error.
- [ ] **Real-terminal create/kill:** a session created with `tmux new -d` on the target appears within one poll interval; `tmux kill-session` makes it disappear.
- [ ] **Attached state:** attaching from a second client on the target flips the attached indicator.
- [ ] **Create with defaults:** user opens the create dialog, gives only a path → session appears, named after the directory, and `tmux display -p '#{session_path}'` matches.
- [ ] **Create with start command:** name + path + `htop` → the session's pane is running htop (`#{pane_current_command}`), visible in the terminal.
- [ ] **Invalid input:** names like `a.b`, `a:b`, `a b` are rejected in the form; a duplicate name and a missing path show the actionable error text.
- [ ] **Attach and type:** user clicks a session, types `echo e2e-$RANDOM` + Enter → the marker is in `capture-pane` and in the browser terminal.
- [ ] **Full-screen apps:** vim (insert text, `:wq` writes the file on the target) and htop (renders, `q` quits) behave correctly.
- [ ] **Resize:** changing the viewport changes `#{window_width}x#{window_height}` on the target.
- [ ] **Leave without killing:** closing the page ends the attach; the session is still in `tmux ls`.
- [ ] **Exit state:** detaching (`prefix d`) or the program exiting shows the exit state; Reconnect re-attaches.
- [ ] **Rename:** rename via the UI → new name in `tmux ls` and the list.
- [ ] **Kill:** Cancel keeps the session; Confirm removes it from `tmux ls` and the list.
- [ ] **App restart:** restarting `hostbud-e2e-app` → the UI recovers and lists the same sessions; the terminal can reconnect.
- [ ] **Host unreachable:** stopping sshd on the target shows the unreachable banner with its hint; starting it again recovers without reloading.
- [ ] **tmux missing:** against the tmux-less target, the UI shows the install hint.
- [ ] **Origin:** a request/WebSocket with a foreign `Origin` is rejected.
- [ ] **Stable:** two consecutive full runs pass (no flakes); failures leave traces/screenshots/videos in `test/e2e/results/`.

## Security (AGENTS.md checklist, M1 scope)
- [ ] `ss -ltn` on the host: 9055 bound on `127.0.0.1` only; nothing on `0.0.0.0` from hostbud/Caddy; hostbud container publishes no ports.
- [ ] A request with a foreign `Origin` (e.g. `curl -H 'Origin: http://evil.example.com' -X POST …`) and a WebSocket upgrade with a foreign Origin are rejected.
- [ ] Remote commands only go through the `sshx` builder with quoted args (code review); session names validated server-side.
- [ ] `/data/ssh/config` has `StrictHostKeyChecking yes` and `BatchMode yes`; `/data/ssh/known_hosts` contains only the pinned host keys from `/run/host-keys`.
- [ ] Swapping in a wrong pinned key → connection refused with a host-key mismatch error (integration test).
- [ ] Kill requires confirmation in the UI.
- [ ] `docker compose exec hostbud id` shows `${HOST_UID}:${HOST_GID}`, not root.
- [ ] No private keys in the container: only the agent socket and `*.pub` host keys are mounted (`docker inspect` mounts).
- [ ] `make logs` at info level contains no tokens, user paths or command strings.

## Definition of done
- [ ] `make lint test` green (Go unit + integration against `test/sshd`, frontend lint/type-check/Vitest), run with only Docker installed.
- [ ] `make e2e` green (both projects).
- [ ] `make gitleaks` clean; pre-commit hook installed via `make hooks` and blocking a planted fake secret.
- [ ] No real hostnames, domains, IPs, usernames or home paths in tracked files (`git grep` for the values in `.env`).
- [ ] README documents M1 usage; `.env.example` lists every new variable; ARCHITECTURE matches what was built.
- [ ] Summary delivered: what changed, env vars the owner must set, manual host steps.
