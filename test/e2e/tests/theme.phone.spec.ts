import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

test.describe('theme on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  test('(T7) Pick Dark and Light and follow System with an attached terminal', async ({ page, ui, target }) => {
    const fresh = newAccount('e2e-theme-phone')
    forbidInLogs(fresh.email, fresh.password)
    await owner.allow(fresh.email)
    await ui.createAccount(fresh)
    const session = uniqueName('theme-phone-terminal')
    await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
    await page.reload()
    await ui.openTerminal(session)
    const pids = (await target.tmux('list-clients', '-t', `=${session}`, '-F', '#{client_pid}')).trim()
    const choose = async (mode: 'Dark' | 'Light' | 'System') => {
      const saved = page.waitForResponse((response) => response.request().method() === 'PUT' && new URL(response.url()).pathname === '/api/ui-state/theme')
      await ui.openAccountMenu()
      await page.getByRole('radio', { name: mode, exact: true }).check()
      await saved
    }
    for (const mode of ['Light', 'Dark'] as const) {
      await choose(mode)
      const value = mode === 'Light' ? 'light' : 'dark'
      await expect(page.locator('html')).toHaveAttribute('data-theme', value)
      await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', mode === 'Light' ? '#ffffff' : '#0f1115')
      await expect.poll(() => page.evaluate((name) => window.__hostbud?.termTheme(name).background, session)).toBe(mode === 'Light' ? '#ffffff' : '#0f1115')
    }
    await choose('System')
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    expect((await target.tmux('list-clients', '-t', `=${session}`, '-F', '#{client_pid}')).trim()).toBe(pids)
  })
})
