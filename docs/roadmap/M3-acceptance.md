# M3 — acceptance checklist

M3 is done when every box is ticked. Tasks: [M3-tasks.md](M3-tasks.md).

Setup for the manual checks: `make deploy` on the host; a Mac with a desktop browser (port forward or the domain); a phone on the tailnet; Claude Code and vim in a tmux session on the host.

## Test coverage rule
Same as M1 ([M1-acceptance.md](M1-acceptance.md#test-coverage-rule)): every criterion names its **U** (unit), **I** (integration: `test/sshd`, PostgreSQL or the rendered deploy config) and **E** (e2e) tests and the task that writes each; n/a needs a one-line reason; "manual" only where automation can't observe it (a real Mac keyboard, iOS, Claude Code). A ticked box means its automated tests exist and pass.

## Functional

### Mac editing keys (owner request)
- [ ] Option+Backspace deletes the previous word and Cmd+Backspace deletes to the start of the line in the shell.
  - U: T1 key mapping · `TerminalView` sends it on keydown (Vitest). I: n/a (byte passthrough; M1 T13). E: T1 *Delete word and line*. **Manual (T9):** the owner in a Mac browser, and in Claude Code's prompt.
- [x] Option+←/→ move by word and Cmd+←/→ jump to the start/end of the line.
  - U: T1 key mapping (Vitest). I: n/a (byte passthrough). E: T1 *Move by word and line*.
- [x] Other keys are unchanged: plain Backspace and arrows, Ctrl combinations and Shift-modified combinations reach the program as before.
  - U: T1 unmapped keys and extra modifiers (Vitest). I: n/a. E: M1 *Attach and type*, *Full-screen apps* (vim arrows, Escape) stay green.

### Copy and paste (owner request)
- [x] Text selected with the mouse is copied with Ctrl+Shift+C / Cmd+Shift+C (and Cmd+C with a selection); Ctrl+C still interrupts the program.
  - U: T2 `clipboardKey` table · `TerminalView` copy writes the selection and sends no bytes · failed write → toast (Vitest). I: n/a (browser clipboard; nothing reaches the host). E: T2 *Copy selection* · T2 *Ctrl+C still interrupts*.
- [x] The terminal's context menu offers Copy, Paste and Select all; Shift+right-click (Option on macOS) opens it even when the program captures the mouse.
  - U: T2 `TerminalMenu` (Vitest). I: n/a (browser only). E: T2 *Copy selection* (menu Copy) · T2 *Bracketed paste* (menu Paste).
- [ ] Shift+drag (Option+drag on macOS) selects text even when the program or tmux `mouse on` captures the mouse.
  - U: T2 terminal options include `macOptionClickForcesSelection` (Vitest). I: n/a (xterm selection is client-side). E: T2 *Forced selection*. **Manual (T9):** Option+drag on a Mac.
- [ ] Ctrl+Shift+V / Cmd+Shift+V / Cmd+V paste as bracketed paste when the program enables it: a multi-line paste into bash doesn't run line by line.
  - U: T2 paste combinations send no bytes and keep the browser default · menu Paste calls `term.paste` (Vitest). I: n/a (the bracket markers are bytes through the M1 T13 bridge). E: T2 *Bracketed paste*. **Manual (T9):** a multi-line paste into Claude Code.
- [ ] OSC 52 writes reach the browser clipboard: a tmux copy-mode yank works with tmux's defaults, and programs inside tmux (vim, Claude Code) work with `set-clipboard on`. OSC 52 clipboard *reads* are refused.
  - U: T2 OSC 52 provider (writes decoded, reads refused, oversize ignored) (Vitest). I: T2 tmux copy-mode `copy-selection` emits `ESC]52` through the term bridge on `test/sshd`. E: T2 *OSC 52 yank* · T2 *OSC 52 read refused*. **Manual (T9):** a yank in vim / Claude Code on the host with `set-clipboard on`.

### Auto-reconnect
- [ ] When a terminal's connection drops (network cut, app restart), it re-attaches by itself with backoff; the tmux session survives, the screen is restored and input works again, with no click needed.
  - U: T3 backoff schedule, jitter, immediate retry on `online`/visible, input dropped while disconnected (Vitest, fake timers). I: T3 a vanished client is dropped by ping timeout, its ssh process exits and the tmux session remains (`test/sshd`). E: T3 *Network cut re-attach* (desktop and phone) · T3 *App restart re-attach*. **Manual (T9):** toggle Wi-Fi on the phone.
- [x] A hung connection (no close, no data) is detected within 30 s, for the terminal and for the live session list.
  - U: T3 term client ping + 25 s silence ⇒ `disconnected` · `LiveConnection` 40 s silence ⇒ reconnect (Vitest) · Go events handler sends `heartbeat` (fake ticker). I: T3 client `ping` → `pong` through the real bridge. E: T3 *Network cut re-attach* (the cut hangs TCP; a session created during the cut appears afterwards).
- [x] No automatic re-attach after a detach or when the session ended; a signed-out user gets the sign-in form instead of a retry loop.
  - U: T3 no retry after `exit`, after `close()`, or for an unlisted session · `401` stops and signs out (Vitest). I: n/a (client decision). E: T3 *Detach doesn't loop*.

### Links
- [x] URLs printed in the terminal open in a new tab on click (tap on phones); OSC 8 hyperlinks work and show their real target on hover; only `http`/`https` open.
  - U: T4 scheme allowlist, `window.open` with `noopener,noreferrer`, OSC 8 hover (Vitest). I: n/a (browser only). E: T4 *Click a URL* (desktop and phone) · T4 *OSC 8 link*.

### Search
- [x] Ctrl+Shift+F / Cmd+F or the 🔍 button opens a search bar that finds text on screen and in the scrollback received since attaching, with match count, next/previous, case and regex options; Escape closes it and returns focus to the terminal.
  - U: T5 key mapping (plain Ctrl+F unmapped) · `TerminalSearch` (options, count, invalid regex, Escape, selection pre-fill) · tmux output reaches the scrollback (`scrollback.spec.ts`, real xterm) (Vitest). I: n/a (client-side buffer). E: T5 *Search scrollback* · T5 *Search options* · T5 *Search on the phone*.

### Tabs and splits
- [x] Several sessions can be open at once in tabs; picking an already open session focuses its tab; each tab's input reaches its own session.
  - U: T7 layout store open/activate/close, the 16-terminal limit; tab bar roles and keys (Vitest). I: n/a (each pane is M1 T13's attach). E: T7 *Tabs*.
- [x] Closing a tab or pane only detaches the view (the tmux session keeps running; no confirmation).
  - U: T7 close · T8 close with collapse (Vitest). I: n/a (closing a socket ends only the attach; M1 T13). E: T7 *Close tab detaches* · T8 *Close pane*.
- [x] Renaming a session in the UI updates its tabs/panes; a session that disappears (killed anywhere) closes its tabs/panes with a notice.
  - U: T7 rename / drop-missing · T8 remove-missing with rebalance (Vitest). I: n/a (list events are M1's). E: T7 *Tabs follow rename and kill*.
- [x] A tab can be split horizontally and vertically (nested, up to 4 panes); typing goes to the focused pane; dragging a divider resizes each pane's tmux window.
  - U: T8 tree operations and limit · `LayoutNodeView` focus (Vitest). I: n/a (resize → tmux is M1 T13's integration test). E: T8 *Split and type* · T8 *Resize split*.
- [x] The tab/split layout (order, active tab, splits, divider positions, focused pane) survives reload and container restart; panes whose session no longer exists are dropped.
  - U: T7 serialize/validate/debounced save · T8 split validation (Vitest) · T6 handler (Go). I: T6 store round-trip (PostgreSQL). E: T7 *Tabs survive reload* · T8 *Splits survive reload*.
- [ ] On narrow screens (phones) tabs work through a compact tab bar, and a split tab shows one pane at a time with a pane switcher.
  - U: T8 pane cycler (Vitest). I: n/a (frontend). E: T7 *Tabs on the phone* · T8 *Split on the phone*. **Manual (T9):** a real phone.

### UI state API
- [x] `GET|PUT /api/ui-state/{key}` stores JSON per account, only for allowlisted keys, up to 64 KiB.
  - U: T6 handler (allowlist, 400/404/413, namespacing, auth, Origin) (Go) · client helpers (Vitest). I: T6 store round-trip for two users (PostgreSQL). E: T6 *UI state API*.

## E2E (`make e2e`, simulated user)
Projects as in M2: `desktop-chromium` and `iphone-13-pro` on `http://localhost:9055`, `iphone-13-pro-domain` on `https://hostbud.example.test`. Clipboard scenarios run in `desktop-chromium` only (Playwright grants clipboard permissions only in Chromium). Each item is tagged with the task that adds it, in the same commit as the behavior.

- [x] **(T1) Delete word and line:** Option+Backspace and Cmd+Backspace edit a bash command line (desktop).
- [x] **(T1) Move by word and line:** Option+←/→ and Cmd+←/→ move the cursor so typed text lands in the right place (desktop).
- [x] **(T2) Copy selection:** a drag-selected line is in the clipboard after Ctrl+Shift+C and after the menu's Copy.
- [x] **(T2) Ctrl+C still interrupts:** with a selection present, Ctrl+C interrupts `sleep` and leaves the clipboard alone.
- [x] **(T2) Bracketed paste:** a two-line paste (keys and menu) sits in the command line until Enter, then each command runs once.
- [x] **(T2) Forced selection:** with tmux `mouse on`, a plain drag makes no selection, and Shift+drag does and copies.
- [x] **(T2) OSC 52 yank:** a tmux copy-mode yank lands in the browser clipboard.
- [x] **(T2) OSC 52 read refused:** an OSC 52 clipboard query gets no reply.
- [x] **(T3) Network cut re-attach:** cut and restore the app's network → the terminal re-attaches on its own, the old output is still on screen, input works, the same tmux session has one client, and the list shows a session created during the cut (desktop and `iphone-13-pro`).
- [x] **(T3) App restart re-attach:** after restarting the app the terminal re-attaches by itself and input works.
- [x] **(T3) Detach doesn't loop:** after `detach-client` the banner shows and nothing re-attaches for 5 s; **Reconnect** attaches.
- [x] **(T4) Click a URL:** clicking (tapping) a printed URL opens exactly it in a new page (desktop and `iphone-13-pro`).
- [x] **(T4) OSC 8 link:** an OSC 8 label shows its target on hover and opens it on click; a `javascript:` target opens nothing.
- [x] **(T5) Search scrollback:** a marker scrolled off screen is found (1 of 1) and scrolled into view; Escape returns input to the shell.
- [x] **(T5) Search options:** match case and regex change the counts; an invalid regex says so.
- [x] **(T5) Search on the phone:** the 🔍 button opens search and finds a marker (`iphone-13-pro`).
- [x] **(T6) UI state API:** put/get round-trip; another account gets 404; foreign Origin 403; unknown key 404; oversize 413 (API level).
- [x] **(T7) Tabs:** three sessions in three tabs; input lands in each; re-picking an open session focuses its tab.
- [x] **(T7) Close tab detaches:** the session stays listed with no client attached; no dialog.
- [x] **(T7) Tabs follow rename and kill:** a UI rename relabels the tab; an out-of-band kill closes it with a notice.
- [x] **(T7) Tabs survive reload:** the same tabs, order and active tab after reload and after an app restart; a missing session's tab is dropped.
- [x] **(T7) Tabs on the phone:** two tabs switched and typed into from the compact tab bar (`iphone-13-pro`).
- [x] **(T8) Split and type:** a nested split of three panes; each pane's input reaches its session.
- [x] **(T8) Resize split:** dragging a divider changes both panes' tmux `#{window_width}`.
- [x] **(T8) Close pane:** panes close down to one; closed sessions stay listed, detached.
- [x] **(T8) Splits survive reload:** tree, divider position and focus survive reload and an app restart.
- [x] **(T8) Split on the phone:** a split tab shows one pane, and the pane switcher changes which one (`iphone-13-pro`).
- [ ] **(every task) Kept green:** T1–T5 each ran `make e2e` green in their commit; from T6 on, each test checkpoint (CP1–CP3 in [M3-tasks.md](M3-tasks.md#progress)) ran it green, and the M1 and M2 suites still pass.
- [ ] **(T9) Stable:** two consecutive full runs pass from a clean checkout.

## Security (AGENTS.md checklist, M3 scope)
- [x] OSC 52 is write-only: programs on the host can't read the browser clipboard.
  - U: T2 provider refuses reads (Vitest). I: n/a (client decision). E: T2 *OSC 52 read refused*.
- [x] Terminal links open only `http`/`https`, with `noopener,noreferrer`.
  - U: T4 allowlist (Vitest). I: n/a. E: T4 *OSC 8 link* (`javascript:` target).
- [x] `PUT /api/ui-state/{key}` requires a signed-in session and an allowed `Origin`; values are size-limited and scoped to the account.
  - U: T6 handler (Go). I: T6 store (PostgreSQL). E: T6 *UI state API*.
- [x] The new e2e ctl actions (network cut/restore) are fixed commands on the throwaway stack only, like the existing ones.
  - U: n/a (a fixed table in `ctl/server.mjs`). I: n/a. E: T3 *Network cut re-attach* uses them; reviewed in T9's audit.

## Definition of done
- [ ] `make lint test` green; `make e2e` green in all three projects; every E2E item above added by the task it's tagged with.
- [ ] Every criterion's U / I / E tests exist and pass; each n/a has its reason, and "manual" is used only where allowed.
- [ ] `make gitleaks` clean; no real hostnames, paths or usernames in tracked files.
- [x] README has the *Terminal* section (T9); ARCHITECTURE matches what was built; `.env.example` unchanged or documents any new variable.
- [ ] Owner's manual checks done (T9) and recorded here.
- [ ] Summary delivered: what changed, env vars the owner must set, manual host steps.
