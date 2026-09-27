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
  await expect(page.getByTestId('key-bar')).toBeVisible()
  return name
}

test('(T4) Ctrl-C interrupts', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-ctrlc')
  await ui.type('sleep 1000', true)
  await page.getByRole('button', { name: 'Control' }).tap()
  await page.keyboard.insertText('c')
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('bash')
  await expect.poll(() => target.capture(name)).toContain('^C')
})

test('(T4) Esc leaves vim insert mode', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-vim')
  await ui.type('vim', true)
  await ui.type('iabc')
  await page.getByRole('button', { name: 'Escape' }).tap()
  await ui.type('dd')
  expect(await target.capture(name)).not.toContain('abc')
  await ui.type(':q!', true)
})

test('(T4) Arrows recall history', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-arrows')
  await ui.type('echo one', true)
  await page.getByRole('button', { name: 'Up arrow' }).tap()
  await page.keyboard.press('Enter')
  await expect.poll(async () => (await target.capture(name)).match(/\bone\b/g)?.length ?? 0).toBeGreaterThanOrEqual(2)
  await ui.type('echo abc')
  await page.getByRole('button', { name: 'Left arrow' }).tap()
  await ui.type('X', true)
  await expect.poll(() => target.capture(name)).toContain('abXc')
})

test('(T4) Slash is pinned at the right edge of the key bar', async ({ page, target, ui }) => {
  await session(page, target, ui, 'e2e-slash')
  const slash = page.getByRole('button', { name: 'Slash', exact: true })
  const bar = await page.getByTestId('key-bar').boundingBox()
  const box = await slash.boundingBox()
  expect(bar && box).toBeTruthy()
  // Visible without scrolling the key bar, as its rightmost key.
  expect(box!.x + box!.width).toBeLessThanOrEqual(bar!.x + bar!.width)
  expect(bar!.x + bar!.width - (box!.x + box!.width)).toBeLessThan(16)
})

test('(T4) Tab, Alt and symbols', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-keybar')
  await ui.type('ech')
  await page.getByRole('button', { name: 'Tab', exact: true }).tap()
  await ui.type('tab-complete', true)
  await expect.poll(() => target.capture(name)).toContain('tab-complete')

  await ui.type("printf '%s\\n' '")
  for (const key of ['Pipe', 'Tilde', 'Slash', 'Hyphen']) await page.getByRole('button', { name: key, exact: true }).tap()
  await ui.type("'", true)
  await expect.poll(() => target.capture(name)).toContain('|~/-')

  await ui.type('echo word')
  await page.getByRole('button', { name: 'Alt', exact: true }).tap()
  await page.keyboard.insertText('b')
  await ui.type('X', true)
  await expect.poll(() => target.capture(name)).toContain('Xword')
})

test('(T4) Key bar keeps the keyboard', async ({ page, target, ui }) => {
  await session(page, target, ui, 'e2e-keyfocus')
  const terminalInput = page.getByRole('textbox', { name: 'Terminal input' })
  await expect(terminalInput).toBeFocused()
  for (const key of ['Escape', 'Control', 'Alt', 'Left arrow', 'Up arrow']) {
    await page.getByRole('button', { name: key, exact: true }).tap()
    await expect(terminalInput).toBeFocused()
  }
})

test('(T4) Application cursor keys', async ({ page, target, ui }) => {
  const name = await session(page, target, ui, 'e2e-cursor-mode')
  const path = `/home/dev/${uniqueName('e2e-less')}.txt`
  await target.run(`seq 1 200 > ${path}`)
  await ui.type(`less ${path}`, true)
  const before = await target.capture(name)
  await page.getByRole('button', { name: 'Down arrow' }).tap()
  await expect.poll(() => target.capture(name)).not.toBe(before)
  await ui.type('q')
})
