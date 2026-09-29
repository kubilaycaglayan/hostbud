import { request as playwrightRequest } from '@playwright/test'
import { ORIGIN, forbidInLogs, mutate } from '../helpers/api.ts'
import { newAccount } from '../helpers/auth.ts'
import { notifications, owner } from '../helpers/db.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { putNotificationSettings } from '../helpers/notifications.ts'
import { newDevice, pushfake, received, subscribe, unsubscribe, type Device } from '../helpers/push.ts'
import { addItem, control, createQueue, getQueue, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'
import { ctl } from '../helpers/ctl.ts'

// V2-M3 T2/T3: Web Push to hostbud-e2e-pushfake. Devices are test-held key
// pairs subscribed through the API (the browser's push service can't be
// used in e2e); every body is decrypted here as the browser would.

const stubs = new Stubs()
const ALLOWED = ['body', 'key', 'kind', 'outcome', 'position', 'project', 'title', 'url', 'v']

test.describe('Web Push (API)', () => {
  test.describe.configure({ timeout: 120_000 })

  test.beforeEach(async ({ target }) => {
    await target.resetTmux()
    await stubs.reset()
    await pushfake.reset()
  })

  /** One item that behaves as told, started; resolves when the queue is
   * paused or finished. */
  async function runItem(request: import('@playwright/test').APIRequestContext, target: import('../helpers/target.ts').Target, condition: string, behavior: 'achieve:1' | 'fail', opts: { flags?: string; prefix?: string; extra?: string } = {}) {
    const project = await newProject(request, target, opts.prefix ?? 'e2e-push')
    await stubs.setBehavior(condition, behavior, 0.5, opts.extra)
    const queue = await createQueue(request, project.id)
    await addItem(request, queue.id, { instruction: `/goal ${condition}`, flags: opts.flags })
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const want = behavior === 'fail' ? 'paused' : 'finished'
    await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 60_000 }).toBe(want)
    return { project, queue: await getQueue(request, queue.id) }
  }

  async function waitFor(d: Device, n: number) {
    await expect.poll(async () => (await pushfake.posts(d.path)).length, { timeout: 15_000 }).toBe(n)
  }

  test('(V2-M3 T2) Push subscription', async ({ request, target }) => {
    const d = newDevice()
    expect((await subscribe(request, d)).status()).toBe(204)
    expect((await notifications.subscriptions()).map((s) => s.endpoint)).toContain(d.endpoint)
    await putNotificationSettings(request, { enabled: true })

    const { project, queue } = await runItem(request, target, 'e2e push subscription', 'achieve:1')
    const run = queue.items[0].run!
    await waitFor(d, 2) // item done, queue finished
    const posts = await pushfake.posts(d.path)
    for (const p of posts) {
      expect(p.headers['content-encoding']).toBe('aes128gcm')
      expect(p.headers.authorization).toMatch(/^vapid t=[^,]+, k=/)
      expect(p.headers.ttl).toBe('86400')
    }
    const payloads = await received(d)
    expect(payloads.find((p) => p.key === `run:${run.id}:done`)).toMatchObject({ v: 1, kind: 'done', title: `${project.name}: item 1 done`, url: `/queues/${queue.id}?item=${queue.items[0].id}` })
    expect(payloads.find((p) => p.key === `queue:${queue.id}:finished:${run.id}`)).toMatchObject({ kind: 'finished', title: `${project.name}: queue finished` })
  })

  test('(V2-M3 T2) Lock-screen payload', async ({ request, target }) => {
    const d = newDevice()
    expect((await subscribe(request, d)).status()).toBe(204)
    await putNotificationSettings(request, { enabled: true })
    const marker = `lockmark${Date.now()}`
    forbidInLogs(marker)
    const { project, queue } = await runItem(request, target, `${marker} instruction`, 'fail', { flags: `--append-system-prompt ${marker}-flag`, prefix: 'e2e-push-lock' })
    const run = queue.items[0].run!
    await waitFor(d, 1)
    const [payload] = await received(d)
    expect(Object.keys(payload).sort()).toEqual(ALLOWED)
    expect(payload).toMatchObject({ kind: 'attention', outcome: 'failed', position: 1, key: `run:${run.id}:attention` })
    const text = JSON.stringify(payload)
    expect(text).not.toContain(marker)
    expect(text).not.toContain(project.path)
    expect(text).not.toContain(run.sessionName)
    expect(text).not.toContain('@example.com')
    expect(text).not.toContain('/home/dev')
    // Needs attention is urgent.
    expect((await pushfake.posts(d.path))[0].headers.urgency).toBe('high')
  })

  test('(V2-M3 T2) Devices and accounts', async ({ request, target, baseURL }) => {
    // This account: two devices, every event. Another: one device, done off.
    const [a1, a2, b1] = [newDevice(), newDevice(), newDevice()]
    expect((await subscribe(request, a1)).status()).toBe(204)
    expect((await subscribe(request, a2)).status()).toBe(204)
    await putNotificationSettings(request, { enabled: true })
    const account = newAccount('push')
    forbidInLogs(account.email, account.password)
    await owner.allow(account.email)
    const other = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    try {
      expect((await other.post('/api/auth/register', { data: account, headers: { Origin: ORIGIN } })).status()).toBe(201)
      expect((await other.post('/api/auth/login', { data: account, headers: { Origin: ORIGIN } })).status()).toBe(200)
      expect((await subscribe(other, b1, ORIGIN)).status()).toBe(204)
      expect((await mutate(other, 'PUT', '/api/notifications/settings', { enabled: true, onDone: false })).status()).toBe(200)

      const { queue } = await runItem(request, target, 'e2e push devices', 'achieve:1')
      const run = queue.items[0].run!
      await waitFor(a1, 2)
      await waitFor(a2, 2)
      await waitFor(b1, 1)
      expect((await received(b1)).map((p) => p.key)).toEqual([`queue:${queue.id}:finished:${run.id}`])
      for (const d of [a1, a2]) expect((await received(d)).map((p) => p.key).sort()).toEqual([`queue:${queue.id}:finished:${run.id}`, `run:${run.id}:done`].sort())

      // A device moves to the account that subscribes it last.
      expect((await subscribe(other, a2, ORIGIN)).status()).toBe(204)
      const subs = await notifications.subscriptions()
      expect(subs.find((s) => s.endpoint === a2.endpoint)?.email).toBe(account.email)

      // Sign-out removes that device only (the app deletes it first).
      expect((await unsubscribe(other, b1)).status()).toBe(204)
      expect((await other.post('/api/auth/logout', { headers: { Origin: ORIGIN } })).status()).toBe(204)
      const left = (await notifications.subscriptions()).map((s) => s.endpoint)
      expect(left).not.toContain(b1.endpoint)
      expect(left).toContain(a1.endpoint)
      expect(left).toContain(a2.endpoint)
    } finally {
      await other.dispose()
    }
  })

  test('(V2-M3 T2) Endpoint rules', async ({ request }) => {
    const d = newDevice()
    for (const endpoint of ['http://push.example.com/x', 'https://push.example.com:8443/x', 'https://127.0.0.1/x', 'https://hostbud-e2e-postgres/x']) {
      const res = await mutate(request, 'POST', '/api/notifications/subscriptions', { endpoint, keys: { p256dh: d.p256dh, auth: d.auth } })
      expect(res.status(), endpoint).toBe(400)
    }
  })

  test('(V2-M3 T3) Expired subscription', async ({ request, target }) => {
    const [gone, missing, ok] = [newDevice(), newDevice(), newDevice()]
    for (const d of [gone, missing, ok]) expect((await subscribe(request, d)).status()).toBe(204)
    await pushfake.answer(gone.path, [410])
    await pushfake.answer(missing.path, [404])
    await putNotificationSettings(request, { enabled: true })

    await runItem(request, target, 'e2e push expired', 'achieve:1')
    await waitFor(ok, 2)
    // Removed at once, never retried.
    await expect.poll(async () => (await notifications.subscriptions()).map((s) => s.endpoint), { timeout: 15_000 }).not.toContain(gone.endpoint)
    const left = (await notifications.subscriptions()).map((s) => s.endpoint)
    expect(left).not.toContain(missing.endpoint)
    expect(left).toContain(ok.endpoint)
    // Deliveries run a few at a time, so the run's second event may already
    // be in flight to a device when its first 404/410 lands: at most one
    // post per event, never a retry.
    for (const d of [gone, missing]) expect((await pushfake.posts(d.path)).length).toBeLessThanOrEqual(2)

    // The next event doesn't reach them.
    const { queue: again } = await runItem(request, target, 'e2e push expired again', 'achieve:1')
    await waitFor(ok, 4)
    for (const d of [gone, missing]) {
      const keys = (await received(d)).map((p) => p.key)
      expect(keys.length).toBeLessThanOrEqual(2)
      expect(keys.join(' ')).not.toContain(again.id)
      expect(keys.join(' ')).not.toContain(again.items[0].run!.id)
    }
  })

  test('(V2-M3 T3) No duplicates', async ({ request, target }) => {
    const d = newDevice()
    expect((await subscribe(request, d)).status()).toBe(204)
    await putNotificationSettings(request, { enabled: true })
    // The stub fires its last Stop hook twice; then the app restarts.
    const { queue } = await runItem(request, target, 'e2e push no duplicates', 'achieve:1', { extra: 'stops=2' })
    const run = queue.items[0].run!
    await waitFor(d, 2)
    await ctl.appRestart()
    await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 30_000 }).toBe(200)
    // Give a replay after the restart the time a delivery takes.
    await expect.poll(async () => (await pushfake.posts(d.path)).length, { timeout: 5_000, intervals: [1_000] }).toBe(2)
    await new Promise((r) => setTimeout(r, 2_000))
    const keys = (await received(d)).map((p) => p.key)
    expect(keys.sort()).toEqual([`queue:${queue.id}:finished:${run.id}`, `run:${run.id}:done`].sort())
    expect((await notifications.outbox()).filter((o) => o.key.includes(run.id))).toHaveLength(2)
  })
})
