import { request as playwrightRequest } from '@playwright/test'
import { newAccount } from '../helpers/auth.ts'
import { FOREIGN_ORIGIN, ORIGIN, forbidInLogs } from '../helpers/api.ts'
import { owner } from '../helpers/db.ts'
import { expect, test } from '../helpers/fixtures.ts'

// UI state API (M3 T6), through Caddy like the app uses it.
const put = (request: import('@playwright/test').APIRequestContext, key: string, data: unknown, origin = ORIGIN) =>
  request.put(`/api/ui-state/${key}`, { data, headers: { Origin: origin } })

test('UI state API: per-account round trip, key allowlist, Origin check and size limit', async ({ request, baseURL }) => {
  const layout = { version: 1, tabs: [{ id: 't1' }], activeTab: 't1', marker: Math.random() }
  const saved = await put(request, 'layout', layout)
  expect(saved.status(), await saved.text()).toBe(204)
  const got = await request.get('/api/ui-state/layout')
  expect(got.status()).toBe(200)
  expect(await got.json()).toEqual(layout)

  // A second account doesn't see the first one's layout.
  const account = newAccount()
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  const other = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  try {
    expect((await other.post('/api/auth/register', { data: account, headers: { Origin: ORIGIN } })).status()).toBe(201)
    expect((await other.post('/api/auth/login', { data: account, headers: { Origin: ORIGIN } })).status()).toBe(200)
    expect((await other.get('/api/ui-state/layout')).status()).toBe(404)
  } finally {
    await other.dispose()
  }

  // Refusals: a foreign Origin, an unknown key, more than 64 KiB.
  expect((await put(request, 'layout', { evil: true }, FOREIGN_ORIGIN)).status()).toBe(403)
  expect((await request.get('/api/ui-state/unknown')).status()).toBe(404)
  expect((await put(request, 'unknown', {})).status()).toBe(404)
  expect((await put(request, 'layout', { large: 'x'.repeat(65 * 1024) })).status()).toBe(413)
  // Nothing of that got stored.
  expect(await (await request.get('/api/ui-state/layout')).json()).toEqual(layout)
})
