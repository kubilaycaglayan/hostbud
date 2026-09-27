import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
  await target.resetTmux()
})

async function session(page: import('@playwright/test').Page, target: import('../helpers/target.ts').Target, ui: import('../helpers/ui.ts').UI, prefix: string) {
  const name = uniqueName(prefix)
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(name)
  return name
}

test('(T5) Scroll into history', async ({ page, target, ui }) => {
  const name = uniqueName('e2e-scroll-history')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await target.tmux('send-keys', '-t', name, 'seq 1 400', 'Enter')
  await expect.poll(() => target.capture(name)).toContain('400')
  await ui.open()
  await ui.openTerminal(name)
  const before = await ui.termText(name)
  expect(before).not.toContain('\n1\n')
  await page.getByRole('button', { name: 'Scroll history' }).tap()
  await expect(page.getByTestId('scroll-bar')).toBeVisible()
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('1')
  await expect.poll(async () => Number(await target.display(name, '#{scroll_position}'))).toBeGreaterThan(0)
  await expect.poll(() => ui.termText(name)).toMatch(/(?:^|\n)(?:[1-9]\d?|[12]\d{2}|3[0-5]\d)(?:\n|$)/)
  const position = Number(await target.display(name, '#{scroll_position}'))
  await page.getByRole('button', { name: 'Page up' }).tap()
  await expect.poll(async () => Number(await target.display(name, '#{scroll_position}'))).toBeGreaterThan(position)
})

test('(T6) Touch swipe scrolls history directly', async ({ page, target, ui }) => {
  const name = uniqueName('e2e-touch-scroll')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(name)
  await target.tmux('send-keys', '-t', name, 'seq 1 400', 'Enter')
  await expect.poll(() => ui.termText(name)).toContain('400')

  const terminal = page.getByTestId('terminal')
  const viewport = terminal.locator('.xterm-viewport')
  const bottom = await viewport.evaluate((element) => element.scrollTop)
  await terminal.dispatchEvent('pointerdown', { pointerType: 'touch', pointerId: 1, clientX: 180, clientY: 520 })
  await terminal.dispatchEvent('pointermove', { pointerType: 'touch', pointerId: 1, clientX: 180, clientY: 200 })
  await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBeLessThan(bottom)
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0')
  const top = await viewport.evaluate((element) => element.scrollTop)
  await terminal.dispatchEvent('pointerup', { pointerType: 'touch', pointerId: 1, clientX: 180, clientY: 200 })

  await terminal.dispatchEvent('pointerdown', { pointerType: 'touch', pointerId: 2, clientX: 180, clientY: 200 })
  await terminal.dispatchEvent('pointermove', { pointerType: 'touch', pointerId: 2, clientX: 180, clientY: 360 })
  await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBeGreaterThan(top)
  await terminal.dispatchEvent('pointerup', { pointerType: 'touch', pointerId: 2, clientX: 180, clientY: 360 })
})

test('(T5) Leave scroll mode', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-scroll-exit')
  await page.getByRole('button', { name: 'Scroll history' }).tap()
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('1')
  await page.getByRole('button', { name: 'Done' }).tap()
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0')
  await expect(page.getByTestId('key-bar')).toBeVisible()

  await page.getByRole('button', { name: 'Scroll history' }).tap()
  const terminalInput = page.getByRole('textbox', { name: 'Terminal input' })
  await expect(terminalInput).toBeFocused()
  await page.keyboard.insertText('echo scroll-typed-input')
  await page.keyboard.press('Enter')
  await expect.poll(() => target.capture(name)).toContain('scroll-typed-input')
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0')
  await expect(page.getByTestId('key-bar')).toBeVisible()

  await page.getByRole('button', { name: 'Scroll history' }).tap()
  await page.getByRole('button', { name: 'Bottom' }).tap()
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0')
  await expect(page.getByTestId('key-bar')).toBeVisible()
})

test('(T5) Scroll works with a full-screen program', async ({ page, target, ui }) => {
  const name = uniqueName('e2e-scroll-htop')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev', 'htop')
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('htop')
  await ui.open()
  await ui.openTerminal(name)
  await page.getByRole('button', { name: 'Scroll history' }).tap()
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('1')
  await page.getByRole('button', { name: 'Done' }).tap()
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0')
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('htop')
})
