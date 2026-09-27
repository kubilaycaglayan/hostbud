import { expect, test } from '../helpers/fixtures.ts'
import { openShell, textRect, termSelection } from '../helpers/shell.ts'
import { uniqueName } from '../helpers/target.ts'

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
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
  await expect(page.getByRole('button', { name: 'Copy selected text' })).toBeVisible()
  await page.getByRole('button', { name: 'Copy selected text' }).tap()
})
