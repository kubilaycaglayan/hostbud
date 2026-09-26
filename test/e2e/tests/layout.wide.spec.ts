import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

test('(T2) Wide layout unchanged', async ({ page, target, isMobile, ui }) => {
  test.skip(isMobile, 'wide layout runs on desktop Chromium')
  const name = uniqueName('e2e-wide-layout')
  await target.resetTmux()
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await expect(page.getByRole('complementary', { name: 'Sessions' })).toBeVisible()
  await ui.openTerminal(name)
  await expect(page.getByRole('tablist', { name: 'Open terminals' })).toBeVisible()
  await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
  await expect(page.getByRole('dialog', { name: 'Project tree' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Show project tree' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Scroll history' })).toHaveCount(0)
})
