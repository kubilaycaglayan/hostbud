import { request as playwrightRequest } from '@playwright/test'
import { FOREIGN_ORIGIN, ORIGIN, forbidInLogs, mutate } from '../helpers/api.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { test as multiTest } from '../helpers/multi.ts'
import { getNotificationSettings, putNotificationSettings } from '../helpers/notifications.ts'

// V2-M3 T0: the per-account notification settings, through Caddy.

const VAPID_VARS = /HOSTBUD_VAPID_PUBLIC_KEY, HOSTBUD_VAPID_PRIVATE_KEY and HOSTBUD_VAPID_SUBJECT \(make vapid-keys\)/

test('Settings API: a fresh account is off, enabling changes only that account, a foreign Origin is refused', async ({ request, baseURL }) => {
  const fresh = await getNotificationSettings(request)
  expect(fresh).toMatchObject({ enabled: false, onDone: true, onAttention: true, onFinished: true })

  const saved = await putNotificationSettings(request, { enabled: true, onFinished: false })
  expect(saved).toMatchObject({ enabled: true, onDone: true, onAttention: true, onFinished: false })
  expect(await getNotificationSettings(request)).toMatchObject({ enabled: true, onFinished: false })

  // Another account still reads off.
  const account = newAccount('notify')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  const other = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  try {
    expect((await other.post('/api/auth/register', { data: account, headers: { Origin: ORIGIN } })).status()).toBe(201)
    expect((await other.post('/api/auth/login', { data: account, headers: { Origin: ORIGIN } })).status()).toBe(200)
    expect(await getNotificationSettings(other)).toMatchObject({ enabled: false, onDone: true, onAttention: true, onFinished: true })
  } finally {
    await other.dispose()
  }

  // A foreign Origin changes nothing; signed out is 401.
  expect((await mutate(request, 'PUT', '/api/notifications/settings', { enabled: false }, FOREIGN_ORIGIN)).status()).toBe(403)
  expect(await getNotificationSettings(request)).toMatchObject({ enabled: true })
  const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  try {
    expect((await anonymous.get('/api/notifications/settings')).status()).toBe(401)
  } finally {
    await anonymous.dispose()
  }
})

multiTest('Push unavailable: without VAPID keys the reason names the vars and the app stays healthy', async ({ multi }) => {
  const settings = await getNotificationSettings(multi)
  expect(settings.push.available).toBe(false)
  expect(settings.push.reason).toMatch(VAPID_VARS)
  expect(settings.vapidPublicKey).toBeUndefined()
  // In-app notifications still work: the switch saves.
  expect((await putNotificationSettings(multi, { enabled: true })).enabled).toBe(true)
  const health = await multi.get('/api/health')
  expect(health.status()).toBe(200)
  expect(await health.json()).toEqual({ status: 'ok' })
})
