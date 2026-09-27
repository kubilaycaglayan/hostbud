import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { uniqueName } from '../helpers/target.ts'

async function account(ui: import('../helpers/ui.ts').UI, label = 'e2e-theme') {
  const fresh = newAccount(label)
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
  return fresh
}

async function chooseTheme(page: import('@playwright/test').Page, mode: 'Dark' | 'Light' | 'System') {
  const save = page.waitForResponse((response) => response.request().method() === 'PUT' && new URL(response.url()).pathname === '/api/ui-state/theme')
  const menu = page.getByRole('button', { name: 'Account' })
  if (!(await menu.evaluate((el) => el.parentElement instanceof HTMLDetailsElement && el.parentElement.open))) await menu.click()
  await page.getByRole('radio', { name: mode, exact: true }).check()
  await save
}

test('(T7) Pick Dark and Light updates UI and mounted terminal', async ({ page, ui, target }, testInfo) => {
  await account(ui)
  const session = uniqueName('theme-terminal')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await page.reload()
  await ui.openTerminal(session)
  const clientPids = (await target.tmux('list-clients', '-t', `=${session}`, '-F', '#{client_pid}')).trim()
  for (const mode of ['Light', 'Dark'] as const) {
    await chooseTheme(page, mode)
    const expected = mode === 'Light' ? 'light' : 'dark'
    await expect(page.locator('html')).toHaveAttribute('data-theme', expected)
    await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor)).toBe(mode === 'Light' ? 'rgb(255, 255, 255)' : 'rgb(15, 17, 21)')
    await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', mode === 'Light' ? '#ffffff' : '#0f1115')
    await expect.poll(() => ui.page.evaluate((name) => window.__hostbud?.termTheme(name).background, session)).toBe(mode === 'Light' ? '#ffffff' : '#0f1115')
  }
  expect((await target.tmux('list-clients', '-t', `=${session}`, '-F', '#{client_pid}')).trim()).toBe(clientPids)
  await chooseTheme(page, 'Light')
  await testInfo.attach('theme-light-tree.png', { body: await page.screenshot(), contentType: 'image/png' })
  await ui.headerAction('New session')
  await testInfo.attach('theme-light-dialog.png', { body: await page.screenshot(), contentType: 'image/png' })
  await page.keyboard.press('Escape')
  await ui.openTerminal(session)
  await testInfo.attach('theme-light-terminal.png', { body: await page.screenshot(), contentType: 'image/png' })
})

test('(T7) Theme persists per account across reload and restart', async ({ page, browser, ui, request }) => {
  await account(ui, 'e2e-theme-persist')
  await chooseTheme(page, 'Light')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await ctl.restartApp()
  await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  const context = await browser.newContext()
  const other = await context.newPage()
  const otherUI = new (await import('../helpers/ui.ts')).UI(other)
  const second = newAccount('e2e-theme-account-b')
  forbidInLogs(second.email, second.password)
  await owner.allow(second.email)
  await otherUI.createAccount(second)
  await expect(other.locator('html')).toHaveAttribute('data-theme', /dark|light/)
  await otherUI.openAccountMenu()
  await expect(other.getByRole('radio', { name: 'System', exact: true })).toBeChecked()
  await other.close()
  await context.close()
  const response = await page.request.put('/api/ui-state/theme', { data: { version: 1, mode: 'dark' }, headers: { Origin: 'http://evil.example.com' } })
  expect(response.status()).toBe(403)
})

test('(T7) System follows the OS and saved Light avoids a first-paint flash', async ({ page, ui }) => {
  await account(ui, 'e2e-theme-system')
  await chooseTheme(page, 'System')
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await page.emulateMedia({ colorScheme: 'dark' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await chooseTheme(page, 'Dark')
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.emulateMedia({ colorScheme: 'dark' })
  await chooseTheme(page, 'Light')
  await page.addInitScript(() => {
    requestAnimationFrame(() => { (window as Window & { __firstThemeBackground?: string }).__firstThemeBackground = getComputedStyle(document.documentElement).backgroundColor })
  })
  await page.reload()
  await expect.poll(() => page.evaluate(() => (window as Window & { __firstThemeBackground?: string }).__firstThemeBackground)).toBe('rgb(255, 255, 255)')
})
