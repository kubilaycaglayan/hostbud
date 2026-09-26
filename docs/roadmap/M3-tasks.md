# M3 — Terminal workspace: tasks

Scope and acceptance: [../ROADMAP.md](../ROADMAP.md#m3--terminal-workspace) · checklist: [M3-acceptance.md](M3-acceptance.md).

## Progress
Update this table in the same commit that finishes a task.

| Task | Status |
|---|---|
| T1 Mac editing keys | ✅ done |
| T2 Copy and paste | next |
| Tabs, splits, auto-reconnect, links, search | to be broken into tasks after T2 |

The owner asked for T1 and T2 first (after M2), so they come before the rest of M3. Same rules as M1/M2: each task ends with a green `make lint test` **and `make e2e`**, a clean `make gitleaks`, and its own conventional commit(s); its **Tests:** and **E2E:** lines land in the same commit as the behavior, tagged in [M3-acceptance.md](M3-acceptance.md).

---

### T1 — Mac editing keys
- `web/src/lib/terminalKeys.ts`: a pure mapping from a key event to the bytes a macOS terminal sends with "natural text editing" (iTerm2's preset):
  - Option+Backspace → `ESC DEL` (readline/zsh: delete the previous word);
  - Cmd+Backspace → `Ctrl-U` (delete to the start of the line);
  - Option+← / → → `ESC b` / `ESC f` (word back / forward);
  - Cmd+← / → → `Ctrl-A` / `Ctrl-E` (start / end of line).
  Only these exact modifier combinations match (Shift, Ctrl or a second modifier ⇒ unmapped, left to xterm or the browser). The mapping is the same on every OS (Alt is Option; `metaKey` is Cmd, or the Windows/Super key elsewhere).
- `TerminalView`: `attachCustomKeyEventHandler` sends a mapped sequence through xterm's input path on `keydown`, prevents the browser default (e.g. Cmd+← as "Back"), and swallows the matching `keypress`/`keyup`.

**Tests:** U (Vitest): the mapping table (each key, unmapped plain keys and extra modifiers); `TerminalView` sends the sequence once on keydown, prevents the default and ignores keyup. I: n/a (bytes pass straight through to the PTY; M1 T13 covers the bridge).

**E2E (desktop-chromium; a hardware-keyboard feature):**
- **Delete word and line (T1):** at a bash prompt, `echo alpha beta gamma` then Option+Backspace leaves `echo alpha beta `; Cmd+Backspace clears the line (checked with `capture-pane`), and a new command still runs.
- **Move by word and line (T1):** `echo one three`, Option+← then typing `two ` and Enter prints `one two three`; `cho X`, Cmd+←, `e`, Cmd+→, Enter prints `X`; Option+→ moves forward a word.

### T2 — Copy and paste
Scope: the *Copy and paste* bullet of [ROADMAP M3](../ROADMAP.md#m3--terminal-workspace). Planned in detail when T1 is done.
