import { DOMAIN_URL, forbidInLogs } from '../helpers/api.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

// Runs in the iphone-13-pro-domain project only (https://hostbud.example.test).
test.use({
  storageState: { cookies: [], origins: [] },
  // Signed out, the app's first /api/auth/me answers 401.
  allowedBrowserErrors:
    /^HTTP 401: GET https:\/\/hostbud\.example\.test\/api\/auth\/me|^console error: Failed to load resource: the server responded with a status of 401/,
})

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await owner.clearRateLimits()
})

// Domain UI (T2)
test('domain UI: the app loads over HTTPS, the user signs in and sees the sessions', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-dui')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  const account = newAccount('e2e-dui')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)

  await page.goto('/')
  expect(page.url()).toBe(`${DOMAIN_URL}/`)
  expect(await page.evaluate(() => window.isSecureContext)).toBe(true)
  const form = ui.authForm()
  await form.tab('Create account').click()
  await form.email.fill(account.email)
  await form.password.fill(account.password)
  await form.submit('Create account').click()

  await ui.expectAccountEmail(account.email)
  await expect(ui.session(name)).toBeVisible()
})
