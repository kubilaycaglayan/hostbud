# M3 — acceptance checklist

M3 is done when every box is ticked. Tasks: [M3-tasks.md](M3-tasks.md). Coverage rule as in [M1-acceptance.md](M1-acceptance.md#test-coverage-rule): every criterion names its U / I / E tests and the task that writes each; n/a needs a reason; "manual" only where automation can't observe it.

Criteria for tabs, splits, auto-reconnect, links and search are added when that part of M3 is broken into tasks.

## Functional

### Mac editing keys (owner request)
- [ ] Option+Backspace deletes the previous word and Cmd+Backspace deletes to the start of the line in the shell.
  - U: T1 key mapping · `TerminalView` sends it on keydown (Vitest). I: n/a (byte passthrough; M1 T13). E: T1 *Delete word and line*. **Manual:** the owner in a Mac browser, and in Claude Code's prompt.
- [x] Option+←/→ move by word and Cmd+←/→ jump to the start/end of the line.
  - U: T1 key mapping (Vitest). I: n/a (byte passthrough). E: T1 *Move by word and line*.
- [x] Other keys are unchanged: plain Backspace and arrows, Ctrl combinations and Shift-modified combinations reach the program as before.
  - U: T1 unmapped keys and extra modifiers (Vitest). I: n/a. E: M1 *Attach and type*, *Full-screen apps* (vim arrows, Escape) stay green.

### Copy and paste (owner request)
Criteria written with T2.

## E2E (`make e2e`)
- [x] **(T1) Delete word and line:** Option+Backspace and Cmd+Backspace edit a bash command line (desktop).
- [x] **(T1) Move by word and line:** Option+←/→ and Cmd+←/→ move the cursor so typed text lands in the right place (desktop).
