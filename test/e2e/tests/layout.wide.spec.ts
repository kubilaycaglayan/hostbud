import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

async function createAccount(ui: import('../helpers/ui.ts').UI) {
  const account = newAccount('e2e-layout-wide')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)
}

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
  await expect(page.getByRole('button', { name: 'Hide sidebar' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Scroll history' })).toHaveCount(0)
})

test('(T13) Left bar toggle and toolbar', async ({ page, target, ui, isMobile }) => {
  test.skip(isMobile, 'desktop layout only')
  await createAccount(ui)
  const name = uniqueName('e2e-t13-toolbar')
  await target.resetTmux()
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await expect(page.getByText('Projects & sessions')).toHaveCount(0)
  await expect(page.getByText('Projects', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Hide sidebar' }).click()
  await expect(page.getByRole('complementary', { name: 'Sessions' })).toBeHidden()
  await expect(page.getByRole('button', { name: 'Show sidebar' })).toHaveAttribute('aria-expanded', 'false')
  await page.reload()
  await expect(page.getByRole('button', { name: 'Show sidebar' })).toBeVisible()
  await expect(page.getByRole('complementary', { name: 'Sessions' })).toBeHidden()
  await page.getByRole('button', { name: 'Show sidebar' }).click()
  const sidebar = page.getByRole('complementary', { name: 'Sessions' })
  await expect(sidebar).toBeVisible()
  await page.getByRole('banner').getByRole('button', { name: 'New session', exact: true }).click()
  const create = page.getByRole('dialog', { name: 'New session' })
  const created = uniqueName('e2e-t13-created')
  await create.getByLabel('Name').fill(created)
  await create.getByRole('button', { name: 'Create session' }).click()
  await ui.waitForTerminal(created)
  await page.getByRole('banner').getByRole('button', { name: 'Browse files', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Browse files' })).toBeVisible()
})
