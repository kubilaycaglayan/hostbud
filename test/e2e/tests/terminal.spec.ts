import { expect, test } from '../helpers/fixtures.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import { UI } from '../helpers/ui.ts'

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

async function newSession(target: Target, prefix: string) {
  const name = uniqueName(prefix)
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  return name
}

const attached = (target: Target, name: string) => target.display(name, '#{session_attached}')
const command = (target: Target, name: string) => target.display(name, '#{pane_current_command}')

// Attach and type (T17)
test('attach and type: the marker is on the target and in the browser terminal', async ({ ui, target }) => {
  const name = await newSession(target, 'e2e-type')
  const marker = uniqueName('e2e-mark')
  forbidInLogs(marker)
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('1')

  await ui.type(`echo ${marker}`, true)
  await expect.poll(() => target.capture(name)).toContain(marker)
  await expect.poll(() => ui.termText()).toContain(marker)
})

test('connection diagnostics: input probes report PTY acknowledgment without exposing text', async ({ ui, target }) => {
  const name = await newSession(target, 'e2e-diagnostics')
  await ui.open()
  await ui.openTerminal(name)
  await ui.terminalAction(name, 'Connection diagnostics')
  const dialog = ui.page.getByRole('dialog', { name: 'Connection diagnostics' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('WebSocket round trip')).toBeVisible()
  await ui.type('x')
  await expect.poll(async () => (await dialog.locator('dd').nth(1).textContent()) ?? '').toMatch(/\d+\.\d ms/)
  await expect(dialog.getByText('Server PTY write')).toBeVisible()
  await expect(dialog.getByText(/Typed text and terminal output are never recorded/)).toBeVisible()
  await dialog.getByRole('button', { name: 'Close diagnostics' }).click()
})

// Full-screen apps (T17)
test('full-screen apps: vim writes a file, htop renders and quits', async ({ ui, target }) => {
  const name = await newSession(target, 'e2e-tui')
  const file = `${uniqueName('e2e-vim')}.txt`
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('1')

  await ui.type(`vim ~/${file}`, true)
  await expect.poll(() => command(target, name)).toBe('vim')
  await ui.type('i')
  await expect.poll(() => target.capture(name)).toContain('-- INSERT --')
  await ui.type('hello from vim')
  await ui.page.keyboard.press('Escape')
  await ui.type(':wq', true)
  await expect.poll(() => command(target, name)).toBe('bash')
  expect(await target.run(`cat ~/${file}`)).toBe('hello from vim\n')

  await ui.type('htop', true)
  await expect.poll(() => command(target, name)).toBe('htop')
  await expect.poll(() => ui.termText()).toMatch(/Mem|Tasks|Load|F1/)
  await ui.type('q')
  await expect.poll(() => command(target, name)).toBe('bash')
})

// htop mouse (T18: automates the "mouse clicks work if tmux mouse is on"
// check). mouse is set on this throwaway session only.
test('htop takes mouse clicks when tmux mouse is on', { tag: '@desktop' }, async ({ ui, target }) => {
  // Taps on a phone aren't mouse clicks, and htop's key bar needs ~80 columns.
  const name = uniqueName('e2e-mouse')
  await target.tmux('new-session', '-d', '-s', name, 'htop')
  await target.tmux('set-option', '-t', `=${name}:`, 'mouse', 'on')
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => command(target, name)).toBe('htop')
  await expect.poll(() => ui.termText()).toContain('Quit')

  // Click htop's own "F10 Quit" label in the bottom row, like a user.
  const box = (await ui.page.getByTestId('terminal').locator('.xterm-screen').boundingBox())!
  const { cols, rows } = await ui.page.evaluate(() => window.__hostbud!.termSize())
  const lines = (await ui.termText()).split('\n')
  const row = lines.findLastIndex((l) => l.includes('Quit'))
  const col = lines[row].indexOf('Quit')
  await ui.page.mouse.click(box.x + ((col + 1.5) * box.width) / cols, box.y + ((row + 0.5) * box.height) / rows)
  // htop was the session's program: quitting it ends the session.
  await expect.poll(() => target.sessions()).not.toContain(name)
})

// Resize (T17)
test('resize: a viewport change resizes the tmux window', async ({ page, ui, target }) => {
  const name = await newSession(target, 'e2e-size')
  const viewport = page.viewportSize()!
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('1')
  const size = () => target.run(`tmux list-clients -t =${name} -F '#{client_width}x#{client_height}'`)
  const window = () => target.display(name, '#{window_width}x#{window_height}')
  await expect.poll(size).not.toBe('')
  const before = await size()
  const windowBefore = await window()

  await page.setViewportSize({ width: viewport.width + 300, height: viewport.height + 150 })
  await expect.poll(size).not.toBe(before)
  await expect.poll(window).not.toBe(windowBefore)
  const [w] = (await size()).trim().split('x')
  expect(Number(w)).toBeGreaterThan(Number(before.split('x')[0]))
})

// Background browser windows must not compete to resize a shared tmux session.
test('background window detaches its terminal until it is visible', async ({ page, ui, target }) => {
  const name = await newSession(target, 'e2e-background-size')
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('1')
  const firstSize = () => page.evaluate(() => window.__hostbud!.termSize())

  const secondPage = await page.context().newPage()
  const secondUI = new UI(secondPage)
  await secondUI.open()
  await secondUI.waitForTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('2')

  await secondPage.bringToFront()
  await expect.poll(() => page.evaluate(() => document.visibilityState)).toBe('hidden')
  await expect.poll(() => attached(target, name)).toBe('1')
  const before = await firstSize()
  const viewport = page.viewportSize()!
  await page.setViewportSize({ width: Math.max(480, viewport.width - 300), height: Math.max(400, viewport.height - 200) })
  await page.waitForTimeout(300)
  expect(await firstSize()).toEqual(before)

  await page.bringToFront()
  await expect.poll(async () => firstSize()).not.toEqual(before)
  await expect.poll(() => attached(target, name)).toBe('2')
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await secondPage.close()

  // A fresh page load restores the saved layout and focuses its active cursor.
  await page.reload()
  await ui.waitForTerminal(name)
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
})

// Leave without killing (T17)
test('leave without killing: closing the page ends only the attach', async ({ page, ui, target }) => {
  const name = await newSession(target, 'e2e-leave')
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('1')
  await page.close()
  await expect.poll(() => attached(target, name)).toBe('0')
  expect(await target.sessions()).toContain(name)
})

// Exit state (T17)
test('exit state: prefix d and a program exiting show it; Reconnect re-attaches', async ({ ui, target }) => {
  const name = await newSession(target, 'e2e-exit')
  await ui.open()
  await ui.openTerminal(name)
  await expect.poll(() => attached(target, name)).toBe('1')

  await ui.page.getByRole('textbox', { name: 'Terminal input' }).focus()
  await ui.page.keyboard.press('Control+b')
  await ui.page.keyboard.press('d')
  await expect(ui.termStatus()).toContainText('Session detached or ended.')
  await expect.poll(() => attached(target, name)).toBe('0')

  await ui.termStatus().getByRole('button', { name: 'Reconnect' }).click()
  await expect(ui.termStatus()).toHaveCount(0)
  await expect.poll(() => attached(target, name)).toBe('1')

  // The session's program exits: the session ends, and the UI says so.
  const short = uniqueName('e2e-short')
  await target.tmux('new-session', '-d', '-s', short, 'sleep 3')
  await ui.openTerminal(short)
  // Since M3 T7 an ended session's tab closes with a notice (the exit banner
  // may show first, briefly).
  await expect(ui.toast(`Session ${short} ended`)).toBeVisible({ timeout: 10_000 })
  await expect(ui.pane(short)).toHaveCount(0)
})

// Terminal after restart (T17): now automatic, see reconnect.spec.ts
// (M3 T3 *App restart re-attach*).

// Create with start command (T17 part): the program is visible in the terminal.
test('create with start command: htop is visible in the terminal', async ({ ui, target }) => {
  const name = uniqueName('e2e-htop-ui')
  await ui.open()
  await ui.createSession({ name, startCommand: 'htop' })
  await ui.waitForTerminal(name)
  await expect.poll(() => command(target, name)).toBe('htop')
  await expect.poll(() => ui.termText()).toMatch(/Mem|Tasks|Load|F1/)
})
