import { TS_DOMAIN_URL, forbidInLogs, upgradeStatus } from '../helpers/api.ts'
import { TS_DOMAIN_STORAGE_STATE } from '../helpers/auth.ts'
import { ctl } from '../helpers/ctl.ts'
import { expect, test } from '../helpers/fixtures.ts'

test.use({
  baseURL: TS_DOMAIN_URL,
  ignoreHTTPSErrors: true,
  storageState: TS_DOMAIN_STORAGE_STATE,
  // A refused tailnet user gets the app shell with a 403 (the scenario's point).
  allowedBrowserErrors:
    /^HTTP 403: GET https:\/\/[^/]+\/(api\/auth\/me)?$|^console error: Failed to load resource: the server responded with a status of 403 .*@ https:\/\/[^/]+\/(api\/auth\/me)?$/,
})
test.setTimeout(60_000)

test('(T8) Tailscale allowlist guards the domain path and recovers after the negative cache', async ({ page, request }) => {
  try {
    await ctl.tsMap('stranger')
    await ctl.restartTSApp()
    const stranger = await request.get('/api/auth/me')
    expect(stranger.status()).toBe(403)
    expect(await stranger.json()).toMatchObject({ error: "This tailnet user isn't allowed" })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: "This tailnet user isn't allowed" })).toBeVisible()
    const cookie = `hostbud_session=${(await request.storageState()).cookies.find((c) => c.name === 'hostbud_session')?.value ?? ''}`
    forbidInLogs(cookie)
    expect(await upgradeStatus('/ws/events', TS_DOMAIN_URL, cookie, TS_DOMAIN_URL)).toBe(403)
    expect((await request.get('/api/health')).status()).toBe(200)

    await ctl.tsMap('allowed')
    await page.waitForTimeout(10_200)
    expect((await request.get('/api/auth/me')).status()).toBe(200)

    for (const mapping of ['unknown', 'error'] as const) {
      await ctl.tsMap(mapping)
      await ctl.restartTSApp()
      const denied = await request.get('/api/auth/me')
      expect(denied.status()).toBe(403)
      expect(await denied.json()).toMatchObject({ error: "Your Tailscale identity couldn't be verified" })
      expect((await request.get('/api/health')).status()).toBe(200)
    }
  } finally {
    await ctl.tsMap('allowed')
    await ctl.restartTSApp()
  }
})
