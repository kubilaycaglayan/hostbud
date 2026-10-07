import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

test('(M8 T37) Terminal font size controls affect only the active session and reset on reload', async ({ page, ui, target }) => {
  const first = uniqueName('font-size')
  const second = uniqueName('font-other')
  await target.tmux('new-session', '-d', '-s', first, '-c', '/home/dev')
  await target.tmux('new-session', '-d', '-s', second, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(first)

  const firstHeader = page.getByRole('region', { name: `Terminal: ${first}`, exact: true })
  const titleBar = firstHeader.locator('[data-terminal-header]')
  await expect(titleBar.locator('[data-terminal-font-controls]')).toBeVisible()
  expect(await titleBar.evaluate((header) => {
    const controls = header.querySelector('[data-terminal-font-controls]')
    const actions = header.querySelector('button[aria-label="Terminal actions"]')
    return !!controls && !!actions && !!(controls.compareDocumentPosition(actions) & Node.DOCUMENT_POSITION_FOLLOWING)
  })).toBe(true)
  const firstTerminal = firstHeader.locator('[data-testid="terminal"] .xterm')
  const originalSize = Number.parseFloat(await firstTerminal.evaluate((el) => getComputedStyle(el).fontSize))
  await firstHeader.getByRole('button', { name: 'Increase terminal font size' }).click()
  await expect.poll(async () => Number.parseFloat(await firstTerminal.evaluate((el) => getComputedStyle(el).fontSize))).toBe(originalSize + 1)

  await ui.openTerminal(second)
  const secondHeader = page.getByRole('region', { name: `Terminal: ${second}`, exact: true })
  const secondTerminal = secondHeader.locator('[data-testid="terminal"] .xterm')
  await expect.poll(async () => Number.parseFloat(await secondTerminal.evaluate((el) => getComputedStyle(el).fontSize))).toBe(originalSize)

  await page.reload()
  await ui.openTerminal(first)
  const reloadedHeader = page.getByRole('region', { name: `Terminal: ${first}`, exact: true })
  const reloadedTerminal = reloadedHeader.locator('[data-testid="terminal"] .xterm')
  await expect.poll(async () => Number.parseFloat(await reloadedTerminal.evaluate((el) => getComputedStyle(el).fontSize))).toBe(originalSize)
})
