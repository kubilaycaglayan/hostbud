import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { ORIGIN } from '../helpers/api.ts'
import { owner } from '../helpers/db.ts'

test.use({
  // The browser also logs each refused /api/auth/me as a console error.
  allowedBrowserErrors:
    /^request failed: GET .*\/api\/auth\/me|^HTTP 502: GET .*\/api\/auth\/me|^console error: Failed to load resource: .* @ https?:\/\/[^/]+\/api\/auth\/me$/,
})

// T6: the shell must wait for auth rather than presenting stale sessions or
// incorrectly treating a network failure as a signed-out account.
test('(T6) Unreachable at start-up recovers through Try again', async ({ page, ui }, info) => {
  test.skip(!['desktop-chromium', 'iphone-13-pro'].includes(info.project.name))
  await page.route('**/api/auth/me', (route) => route.abort())
  await page.goto('/')
  await expect(page.getByRole('heading', { name: "Can't reach hostbud" })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Sign in' })).toHaveCount(0)
  await expect(page.getByText(/session/i)).toHaveCount(0)
  await page.unroute('**/api/auth/me')
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(ui.tree()).toBeVisible()
})

test('(T6) Unreachable 502 at start-up recovers through Try again', async ({ page, ui }, info) => {
  test.skip(!['desktop-chromium', 'iphone-13-pro'].includes(info.project.name))
  await page.route('**/api/auth/me', (route) => route.fulfill({ status: 502, body: 'upstream unavailable' }))
  await page.goto('/')
  await expect(page.getByRole('heading', { name: "Can't reach hostbud" })).toBeVisible()
  await page.unroute('**/api/auth/me')
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(ui.tree()).toBeVisible()
})

test.describe('offline sign-in', () => {
  test.use({
    storageState: { cookies: [], origins: [] },
    allowedBrowserErrors:
      /^request failed: POST .*\/api\/auth\/login|^HTTP 401: GET .*\/api\/auth\/me|^console error: Failed to load resource: .* @ https?:\/\/[^/]+\/api\/auth\/(me|login)$/,
  })
  test('(T6) Offline sign-in error is not counted as a failed login', async ({ page, ui }, info) => {
    test.skip(info.project.name !== 'desktop-chromium')
    const account = newAccount('e2e-offline')
    await owner.allow(account.email)
    await page.request.post('/api/auth/register', { data: account, headers: { Origin: ORIGIN } })
    await page.goto('/')
    await page.route('**/api/auth/login', (route) => route.abort())
    await ui.signIn(account.email, account.password)
    await expect(ui.authForm().alert).toContainText("Can't reach hostbud, check your connection")
    await page.unroute('**/api/auth/login')
    await ui.signIn(account.email, account.password)
    await expect(ui.tree()).toBeVisible()
  })
})
