import type { Page } from '@playwright/test'
import { DOMAIN_STORAGE_STATE, newAccount, sessionCookieHeader } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'
import {
  DOMAIN,
  DOMAIN_URL,
  FOREIGN_ORIGIN,
  MACHINE,
  ORIGIN,
  forbidInLogs,
  listSessions,
  mutate,
  upgradeStatus,
} from '../helpers/api.ts'

// The domain path (T2): https://hostbud.example.test, served by the e2e
// Caddy with the production proxy config and an internal-CA certificate.
test.use({ baseURL: DOMAIN_URL, ignoreHTTPSErrors: true })

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

interface SocketWindow {
  e2eSocket: { messages: unknown[]; out: string; open: boolean; ws: WebSocket }
}

// Opens a WebSocket on the page's own origin (wss:// here), collecting JSON
// text messages and decoded binary output.
async function openSocket(page: Page, path: string) {
  await page.evaluate((path) => {
    const w = window as unknown as SocketWindow
    const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}${path}`)
    ws.binaryType = 'arraybuffer'
    w.e2eSocket = { messages: [], out: '', open: false, ws }
    const dec = new TextDecoder()
    ws.onopen = () => (w.e2eSocket.open = true)
    ws.onmessage = (m) => {
      if (typeof m.data === 'string') w.e2eSocket.messages.push(JSON.parse(m.data))
      else w.e2eSocket.out += dec.decode(m.data, { stream: true })
    }
  }, path)
  await expect
    .poll(() => page.evaluate(() => (window as unknown as SocketWindow).e2eSocket.open), { timeout: 10_000 })
    .toBe(true)
}
const socket = (page: Page) => page.evaluate(() => (window as unknown as SocketWindow).e2eSocket)

test.describe('fresh sign-in', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  // HTTPS domain path (T2)
  test('HTTPS domain path: Secure cookie, sessions API, events and terminal over WSS', async ({ page, target }) => {
    const account = newAccount('e2e-domain')
    forbidInLogs(account.email, account.password)
    await owner.allow(account.email)
    const headers = { Origin: DOMAIN_URL }
    expect((await page.request.post('/api/auth/register', { data: account, headers })).status()).toBe(201)
    expect((await page.request.post('/api/auth/login', { data: account, headers })).status()).toBe(200)

    const cookies = await page.context().cookies(DOMAIN_URL)
    const session = cookies.find((c) => c.name === 'hostbud_session')
    expect(session, 'session cookie on the domain').toBeDefined()
    forbidInLogs(session!.value)
    expect(session).toMatchObject({ domain: DOMAIN, secure: true, httpOnly: true, sameSite: 'Lax' })

    const name = uniqueName('e2e-dom')
    await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
    await expect.poll(async () => (await listSessions(page.request)).map((s) => s.name)).toContain(name)

    await page.goto('/')
    expect(page.url()).toBe(`${DOMAIN_URL}/`)
    await openSocket(page, '/ws/events')
    await expect.poll(async () => ((await socket(page)).messages[0] as { type?: string })?.type).toBe('snapshot')

    const marker = uniqueName('e2e-mark')
    forbidInLogs(marker)
    await openSocket(page, `/ws/term?machine=${MACHINE}&session=${name}&cols=90&rows=30`)
    await expect.poll(() => target.display(name, '#{session_attached}'), { timeout: 10_000 }).toBe('1')
    await page.evaluate(
      (t) => (window as unknown as SocketWindow).e2eSocket.ws.send(new TextEncoder().encode(t)),
      `echo ${marker}\r`,
    )
    await expect.poll(() => target.capture(name), { timeout: 10_000 }).toContain(marker)
    await expect.poll(async () => (await socket(page)).out).toContain(marker)
  })
})

// Origin on both paths (T2)
test('origin on both paths: the domain origin is allowed, http and foreign origins are not', async ({
  request,
  target,
}) => {
  const cookie = sessionCookieHeader(DOMAIN_STORAGE_STATE)
  const name = uniqueName('e2e-orig')
  const create = (origin: string) =>
    request.fetch(`/api/machines/${MACHINE}/sessions`, {
      method: 'POST',
      data: { name, path: '/home/dev' },
      headers: { Origin: origin, Cookie: cookie },
    })

  for (const origin of [`http://${DOMAIN}`, `${DOMAIN_URL}:8443`, FOREIGN_ORIGIN]) {
    expect((await create(origin)).status(), origin).toBe(403)
    expect(await upgradeStatus('/ws/events', origin, cookie, DOMAIN_URL), origin).toBe(403)
  }
  expect(await target.sessions()).not.toContain(name)

  expect((await create(DOMAIN_URL)).status()).toBe(201)
  await expect.poll(() => target.sessions()).toContain(name)
  expect(await upgradeStatus('/ws/events', DOMAIN_URL, cookie, DOMAIN_URL)).toBe(101)
  expect(await upgradeStatus(`/ws/term?machine=${MACHINE}&session=${name}&cols=80&rows=24`, DOMAIN_URL, cookie, DOMAIN_URL)).toBe(101)
})

// Origin on both paths (T2): the port-forward path keeps working next to it.
test.describe('loopback path', () => {
  test.use({ baseURL: ORIGIN })

  test('origin on both paths: http://localhost keeps working on the loopback site', async ({ request, target }) => {
    const cookie = sessionCookieHeader()
    const name = uniqueName('e2e-loop')
    const res = await mutate(request, 'POST', `/api/machines/${MACHINE}/sessions`, { name, path: '/home/dev' })
    expect(res.status()).toBe(201)
    await expect.poll(() => target.sessions()).toContain(name)
    expect(await upgradeStatus('/ws/events', ORIGIN, cookie)).toBe(101)
    expect(await upgradeStatus('/ws/events', FOREIGN_ORIGIN, cookie)).toBe(403)
  })
})
