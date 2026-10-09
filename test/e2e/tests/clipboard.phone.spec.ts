import { expect, test } from '../helpers/fixtures.ts'
import { openShell, textRect, termSelection } from '../helpers/shell.ts'
import { uniqueName } from '../helpers/target.ts'

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

test('(T2) Touch long press selects terminal text for copying', async ({ page, ui, target }) => {
  const session = await openShell(ui, target, 'e2e-touch-copy')
  const marker = uniqueName('touch-copy')
  await target.tmux('send-keys', '-t', `=${session}:`, '-l', `seq 1 400; printf '%s\\n' ${marker}`)
  await target.tmux('send-keys', '-t', `=${session}:`, 'Enter')
  // The marker comes last so that it is still on screen after the history.
  await expect.poll(() => ui.termText(session)).toMatch(new RegExp(`^${marker}\\s*$`, 'm'))
  const rect = await textRect(page, marker)
  await page.getByTestId('terminal').dispatchEvent('pointerdown', {
    pointerType: 'touch',
    clientX: rect.x + rect.width / 2,
    clientY: rect.y + rect.height / 2,
  })
  await page.waitForTimeout(500)
  await expect.poll(() => termSelection(page)).toBe(marker)
  // The long press opens its own menu over the selection (Escape would go to
  // the terminal, which clears the selection): copy from there.
  const copy = page.getByRole('menu').getByRole('menuitem', { name: 'Copy', exact: true })
  await expect(copy).toBeEnabled()
  await copy.tap()
  await expect(page.getByRole('menu')).toHaveCount(0)
  expect(await termSelection(page)).toBe(marker)
})

test('(T42) Touch long press selects a complete hard-wrapped URL for copying', async ({ page, ui, target }) => {
  const session = await openShell(ui, target, 'e2e-touch-url', (name) =>
    target.tmux('set', '-t', name, 'mouse', 'on'),
  )
  const url = `https://example.com/oauth?client_id=${'a'.repeat(160)}&state=${'b'.repeat(160)}`
  const cols = await page.evaluate(() => window.__hostbud!.termSize().cols)
  const wrapped = Array.from({ length: Math.ceil(url.length / cols) }, (_, i) => url.slice(i * cols, (i + 1) * cols))
  const printCommand = `printf '%s\\n' ${wrapped.map((part) => `'${part}'`).join(' ')}; stty echo`
  await ui.type('stty -echo', true)
  await ui.type(printCommand, true)
  await expect.poll(async () => (await ui.termText(session)).replace(/\n/g, '')).toContain(url)

  const rect = await textRect(page, url.slice(0, 24))
  await page.getByTestId('terminal').dispatchEvent('pointerdown', {
    pointerType: 'touch',
    clientX: rect.x + rect.width / 2,
    clientY: rect.y + rect.height / 2,
  })
  await page.waitForTimeout(500)
  await expect.poll(async () => (await termSelection(page)).replace(/[\r\n]/g, '')).toBe(url)
  await expect(page.getByRole('menu', { name: 'Terminal menu' }).getByRole('menuitem', { name: 'Copy', exact: true })).toBeEnabled()
})
