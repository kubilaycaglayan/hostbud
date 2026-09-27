import { TS_LOOPBACK_URL } from '../helpers/api.ts'
import { STORAGE_STATE } from '../helpers/auth.ts'
import { ctl } from '../helpers/ctl.ts'
import { expect, test } from '../helpers/fixtures.ts'

test.use({ baseURL: TS_LOOPBACK_URL, storageState: STORAGE_STATE })

test('(T8) Tailscale identity allowlist leaves the loopback path exempt', async ({ page, request }) => {
  try {
    await ctl.tsMap('stranger')
    await ctl.restartTSApp()
    expect((await request.get('/api/auth/me')).status()).toBe(200)
    expect((await request.get('/api/machines/host/sessions')).status()).toBe(200)
    await page.goto('/')
    await expect(page.getByRole('complementary', { name: 'Sessions' })).toBeVisible()
  } finally {
    await ctl.tsMap('allowed')
    await ctl.restartTSApp()
  }
})
