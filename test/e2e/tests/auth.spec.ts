import type { APIRequestContext } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { IP_MAX_FAILURES, LOGIN_MAX_FAILURES, newAccount } from '../helpers/auth.ts'
import { forbidInLogs, ORIGIN, upgradeStatus } from '../helpers/api.ts'
import { owner } from '../helpers/db.ts'

// Signed out: these scenarios start without the saved session.
test.use({
  storageState: { cookies: [], origins: [] },
  // Expected refusals (401 on /api/auth/me before sign-in, 400/401/429 from
  // the forms) show up as failed responses and console messages.
  allowedBrowserErrors:
    /^HTTP (400|401|429): (GET|POST) http:\/\/localhost:9055\/api\/auth\/|^console error: Failed to load resource: the server responded with a status of (400|401|429)/,
})

test.beforeEach(async () => {
  // Each scenario starts with fresh throttling (the owner can reset it too).
  await owner.clearRateLimits()
})
// ...and leaves none behind: later specs register accounts too.
test.afterEach(async () => {
  await owner.clearRateLimits()
})

async function createAccount(request: APIRequestContext) {
  const account = newAccount()
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  const res = await request.post('/api/auth/register', { data: account, headers: { Origin: ORIGIN } })
  expect(res.status(), await res.text()).toBe(201)
  return account
}

function loginRequest(request: APIRequestContext, email: string, password: string) {
  return request.post('/api/auth/login', { data: { email, password }, headers: { Origin: ORIGIN } })
}

// Whitelist-gated registration (T8B)
test('whitelist-gated registration: refused until the owner allows the address', async ({ page, ui }) => {
  const account = newAccount()
  forbidInLogs(account.email, account.password)
  await page.goto('/')
  const form = ui.authForm()
  await form.tab('Create account').click()
  await form.email.fill(account.email)
  await form.password.fill(account.password)
  await form.submit('Create account').click()
  await expect(form.alert).toContainText("Registration isn't possible with this email address")
  await expect(form.alert).toContainText('owner has approved')
  expect(await owner.userExists(account.email)).toBe(false)

  await owner.allow(account.email) // the owner's SQL
  await form.submit('Create account').click()
  await ui.expectAccountEmail(account.email)
  expect(await owner.userExists(account.email)).toBe(true)
})

// Registration/sign-in/logout (T8B)
test('sign in, reach the app, sign out: protected routes refuse afterwards', async ({ page, ui, context }) => {
  const account = await createAccount(page.request)
  await page.goto('/')
  await expect(ui.authForm().tab('Sign in')).toBeVisible()
  expect((await page.request.get('/api/machines')).status()).toBe(401)

  await ui.signIn(account.email, account.password)
  await expect(ui.tree()).toBeVisible()
  await ui.expectAccountEmail(account.email)
  expect((await page.request.get('/api/machines')).status()).toBe(200)
  const cookie = (await context.cookies()).find((c) => c.name === 'hostbud_session')!
  expect(cookie.httpOnly).toBe(true)
  expect(cookie.sameSite).toBe('Lax')
  forbidInLogs(cookie.value)
  const cookieHeader = `${cookie.name}=${cookie.value}`
  expect(await upgradeStatus('/ws/events', ORIGIN, cookieHeader)).toBe(101)

  await ui.signOut()
  await expect(ui.authForm().tab('Sign in')).toBeVisible()
  expect((await page.request.get('/api/machines')).status()).toBe(401)
  // The old cookie was revoked server-side, not just deleted in the browser.
  const replay = await page.request.get('/api/auth/me', { headers: { Cookie: cookieHeader } })
  expect(replay.status()).toBe(401)
  expect(await upgradeStatus('/ws/events', ORIGIN, cookieHeader)).toBe(401)
  expect(await upgradeStatus('/ws/term?machine=host&session=x', ORIGIN, cookieHeader)).toBe(401)
  expect(await upgradeStatus('/ws/events', ORIGIN)).toBe(401)
})

// Whitelist-gated login (T8B)
test('whitelist-gated login: disabling the address blocks new sign-ins, the account stays', async ({ page, ui, browser }) => {
  const account = await createAccount(page.request)
  await page.goto('/')
  await ui.signIn(account.email, account.password)
  await expect(ui.tree()).toBeVisible()

  await owner.disable(account.email) // the owner's SQL
  const other = await browser.newContext({ storageState: { cookies: [], origins: [] } })
  const res = await loginRequest(other.request, account.email, account.password)
  expect(res.status()).toBe(401)
  expect((await res.json()).error).toBe('invalid email or password') // same as a wrong password
  await other.close()
  expect(await owner.userExists(account.email)).toBe(true)

  // In the UI: the same generic message.
  await ui.signOut()
  await ui.signIn(account.email, account.password)
  await expect(ui.authForm().alert).toHaveText('Invalid email or password.')
})

function retryAfter(res: { headers(): Record<string, string> }): number {
  return Number(res.headers()['retry-after'])
}

// Escalating login rate limit (T8B)
test('escalating login rate limit: 429 with growing Retry-After, also across emails', async ({ page, ui }) => {
  const account = await createAccount(page.request)
  const bad = () => loginRequest(page.request, account.email, 'wrong-password-123')

  for (let i = 0; i < LOGIN_MAX_FAILURES; i++) expect((await bad()).status()).toBe(401)
  const waits: number[] = []
  for (let i = 0; i < 3; i++) {
    const res = await bad()
    expect(res.status()).toBe(429)
    expect((await res.json()).error).toBe('too many attempts')
    waits.push(retryAfter(res))
  }
  expect(waits[1]).toBeGreaterThan(waits[0])
  expect(waits[2]).toBeGreaterThan(waits[1])
  // Blocked means blocked: even the right password waits.
  expect((await loginRequest(page.request, account.email, account.password)).status()).toBe(429)

  // Changing only the email doesn't escape the IP-wide bucket.
  await owner.clearRateLimits()
  let res
  for (let i = 0; i < IP_MAX_FAILURES; i++) {
    res = await loginRequest(page.request, newAccount('e2e-rot').email, 'wrong-password-123')
    expect(res.status()).toBe(401)
  }
  res = await loginRequest(page.request, newAccount('e2e-rot').email, 'wrong-password-123')
  expect(res.status()).toBe(429)
  const first = retryAfter(res)
  res = await loginRequest(page.request, newAccount('e2e-rot').email, 'wrong-password-123')
  expect(res.status()).toBe(429)
  expect(retryAfter(res)).toBeGreaterThan(first)

  // The UI tells the user how long to wait.
  await page.goto('/')
  await ui.signIn(account.email, account.password)
  await expect(ui.authForm().alert).toContainText(/Too many attempts\. Try again in \d+ seconds\./)
})
