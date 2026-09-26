import { expect, test } from '../helpers/fixtures.ts'
import { openShell, promptLine } from '../helpers/shell.ts'

// Mac editing keys (M3 T1): Option is Alt, Cmd is Meta. A hardware-keyboard
// feature, so desktop only. Results are checked in the real shell on the
// target (capture-pane).

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(isMobile, 'hardware keyboard shortcuts')
  await target.resetTmux()
})

// Delete word and line (T1)
test('delete word and line: Option+Backspace and Cmd+Backspace', async ({ ui, target }) => {
  const name = await openShell(ui, target, 'e2e-del')
  const keys = ui.page.keyboard

  await ui.type('echo alpha beta gamma')
  await expect.poll(() => promptLine(target, name)).toMatch(/echo alpha beta gamma$/)
  await keys.press('Alt+Backspace')
  await expect.poll(() => promptLine(target, name)).toMatch(/\$ echo alpha beta$/)

  await keys.press('Meta+Backspace')
  await expect.poll(() => promptLine(target, name)).toMatch(/\$$/)

  // The line is really empty: a new command runs on its own.
  await ui.type('echo done-$((2+3))', true)
  await expect.poll(() => target.capture(name)).toMatch(/^done-5$/m)
})

// Move by word and line (T1)
test('move by word and line: Option+←/→ and Cmd+←/→', async ({ ui, target }) => {
  const name = await openShell(ui, target, 'e2e-move')
  const keys = ui.page.keyboard

  await ui.type('echo one three')
  await keys.press('Alt+ArrowLeft')
  await ui.type('two ', true)
  await expect.poll(() => target.capture(name)).toMatch(/^one two three$/m)

  await ui.type('cho X-$((1+1))')
  await keys.press('Meta+ArrowLeft')
  await ui.type('e')
  await keys.press('Meta+ArrowRight')
  await ui.type('Y', true)
  await expect.poll(() => target.capture(name)).toMatch(/^X-2Y$/m)

  await ui.type('echo a c')
  await keys.press('Meta+ArrowLeft')
  await keys.press('Alt+ArrowRight') // after "echo"
  await keys.press('Alt+ArrowRight') // after "a"
  await ui.type(' b', true)
  await expect.poll(() => target.capture(name)).toMatch(/^a b c$/m)
})
