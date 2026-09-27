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

/** A one-finger vertical swipe over the terminal (dy > 0: finger moves down). */
async function swipe(page: import('@playwright/test').Page, dy: number) {
  await page.getByTestId('terminal').evaluate((host, dy) => {
    const target = host.querySelector('.xterm-screen') ?? host
    const box = target.getBoundingClientRect()
    const x = box.left + box.width / 2
    const y = box.top + box.height / 2 - dy / 2
    const fire = (type: string, points: number[]) => {
      const event = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'touches', { value: points.map((py) => ({ clientX: x, clientY: py })) })
      target.dispatchEvent(event)
    }
    fire('touchstart', [y])
    for (let step = 1; step <= 10; step++) fire('touchmove', [y + (dy * step) / 10])
    fire('touchend', [])
  }, dy)
}

test('(T9) Touch swipe scrolls the full tmux history', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-touch-history')
  await ui.type('seq 1 400', true)
  await expect.poll(() => target.capture(name)).toContain('400')
  await expect(page.locator('.terminal-touch .xterm-scrollable-element > .scrollbar')).toBeHidden()
  await swipe(page, 300)
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('1')
  await expect.poll(async () => Number(await target.display(name, '#{scroll_position}'))).toBeGreaterThan(5)
  await expect(page.getByTestId('scroll-bar')).toBeVisible()
  await swipe(page, -2000)
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0')
})

test('(T9) Touch swipe scrolls a mouse-aware full-screen app', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-touch-app')
  await ui.type("printf '\\033[?1049h\\033[?1000h\\033[?1006h'; stty -icanon -echo; cat -v", true)
  await expect.poll(() => target.display(name, '#{mouse_any_flag}')).toBe('1')
  await swipe(page, 200)
  await expect.poll(() => target.capture(name)).toContain('^[[<64;')
  expect(await target.display(name, '#{pane_in_mode}')).toBe('0')
})
