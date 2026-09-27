import { readFileSync } from 'node:fs'
import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { MACHINE, mutate, ORIGIN, upgradeSocket } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'
import { sessionCookieHeader } from '../helpers/auth.ts'

test('(T2) stalled tmux returns a bounded timeout and inventory recovers', async ({ request }) => {
  const name = uniqueName('e2e-timeout')
  await ctl.stallTmux()
  try {
    const res = await mutate(request, 'POST', `/api/machines/${MACHINE}/sessions`, {
      name, path: '/home/dev', env: {}, startCommand: '',
    }, ORIGIN)
    const body = await res.text()
    expect(res.status(), body).toBe(504)
    expect(body).toContain("The host didn't answer within 10s")
  } finally {
    await ctl.unstallTmux()
  }
  await expect.poll(async () => {
    const response = await request.get('/api/machines')
    const { machines } = await response.json() as { machines: { status: string }[] }
    return machines[0]?.status
  }, { timeout: 20_000 }).toBe('ok')
  const recovered = await mutate(request, 'POST', `/api/machines/${MACHINE}/sessions`, {
    name, path: '/home/dev', env: {}, startCommand: '',
  }, ORIGIN)
  expect(recovered.status(), await recovered.text()).toBe(201)
})

test('(T3) stalled SFTP returns a bounded timeout and recovers', async ({ request }) => {
  await ctl.stallSftp(60)
  const start = Date.now()
  try {
    const res = await request.get(`/api/machines/${MACHINE}/fs?path=~`, { timeout: 20_000 })
    const body = await res.json()
    expect(res.status(), JSON.stringify(body)).toBe(504)
    expect(body).toMatchObject({ error: "The host's file service didn't answer within 10s" })
    expect(Date.now() - start).toBeLessThan(15_000)
  } finally {
    await ctl.unstallSftp()
  }
  await expect.poll(async () => (await request.get(`/api/machines/${MACHINE}/fs?path=~`)).status(), { timeout: 15_000 }).toBe(200)
})

test('(T4) terminal cap returns an actionable 429 before starting another attach', async ({ target }) => {
  const name = uniqueName('e2e-term-cap')
  await target.run(`tmux new-session -d -s '${name}' -c /home/dev`)
  const cookie = sessionCookieHeader()
  const path = `/ws/term?machine=${MACHINE}&session=${name}&cols=80&rows=24`
  const sockets: import('node:stream').Duplex[] = []
  try {
    const opened = await Promise.all(Array.from({ length: 32 }, () => upgradeSocket(path, ORIGIN, cookie)))
    for (const item of opened) {
      expect(item.status).toBe(101)
      if (item.socket) sockets.push(item.socket)
    }
    const rejected = await upgradeSocket(path, ORIGIN, cookie)
    expect(rejected.status).toBe(429)
    expect(JSON.parse(rejected.body ?? '')).toMatchObject({
      error: 'Too many open terminals (32)',
      hint: 'Close some tabs or panes; each open terminal keeps an ssh process on the host.',
    })
    await expect.poll(() => target.tmux('list-clients', '-t', `=${name}`, '-F', '#{client_pid}').then((s) => s.trim().split('\n').filter(Boolean).length)).toBe(32)
  } finally {
    for (const socket of sockets) socket.destroy()
  }
})

test('(T4) stalled terminal client is dropped while the tmux session survives', async ({ target }) => {
  test.setTimeout(60_000)
  const name = uniqueName('e2e-slow-term')
  await target.run(`tmux new-session -d -s '${name}' -c /home/dev`)
  const cookie = sessionCookieHeader()
  const path = `/ws/term?machine=${MACHINE}&session=${name}&cols=80&rows=24`
  let stalled: import('node:stream').Duplex | undefined
  let recovered: import('node:stream').Duplex | undefined
  try {
    const opened = await upgradeSocket(path, ORIGIN, cookie)
    expect(opened.status).toBe(101)
    stalled = opened.socket
    stalled?.pause()
    await expect.poll(() => target.tmux('list-clients', '-t', `=${name}`, '-F', '#{client_pid}').then((s) => s.trim().split('\n').filter(Boolean).length)).toBe(1)
    // tmux sends only screen updates: the flood must last until the socket
    // buffers between hostbud and the paused client are full (bounded).
    await target.tmux('send-keys', '-t', `=${name}:`, 'timeout 60 yes', 'Enter')
    await expect.poll(() => target.tmux('list-clients', '-t', `=${name}`, '-F', '#{client_pid}').then((s) => s.trim().split('\n').filter(Boolean).length), { timeout: 30_000 }).toBe(0)
    expect((await target.sessions())).toContain(name)
    stalled?.destroy()
    const next = await upgradeSocket(path, ORIGIN, cookie)
    expect(next.status).toBe(101)
    recovered = next.socket
    await expect.poll(() => target.tmux('list-clients', '-t', `=${name}`, '-F', '#{client_pid}').then((s) => s.trim().split('\n').filter(Boolean).length)).toBe(1)
  } finally {
    await target.tmux('send-keys', '-t', `=${name}:`, 'C-c').catch(() => undefined)
    stalled?.destroy()
    recovered?.destroy()
  }
})

test('(T5) request limits and no-store headers pass through Caddy', async ({ request }) => {
  const routes = JSON.parse(readFileSync('routes.json', 'utf8')) as {
    method: string; path: string; stateChanging: boolean; authRequired: boolean; websocket: boolean; jsonBody: boolean
  }[]
  const body = JSON.stringify({ value: 'x'.repeat(65 << 10) })
  const jsonRoutes = routes.filter((route) => route.stateChanging && route.jsonBody)
  for (const route of jsonRoutes) {
    const path = route.path.replace('{machine}', MACHINE).replace('{name}', 'request-limit').replace('{id}', 'missing').replace('{key}', 'layout')
    for (const [contentType, expected] of [['application/json', 413], ['text/plain', 415]] as const) {
      const response = await request.fetch(path, {
        method: route.method,
        data: body,
        headers: { Origin: ORIGIN, 'Content-Type': contentType },
      })
      expect(response.status(), `${route.method} ${path} ${contentType}`).toBe(expected)
    }
  }
  for (const route of jsonRoutes.filter((item) => !item.path.startsWith('/api/ui-state/'))) {
    const path = route.path.replace('{machine}', MACHINE).replace('{name}', 'request-limit').replace('{id}', 'missing').replace('{key}', 'layout')
    const response = await request.fetch(path, {
      method: route.method,
      data: JSON.stringify({ unexpectedField: true }),
      headers: { Origin: ORIGIN, 'Content-Type': 'application/json' },
    })
    expect(response.status(), `${route.method} ${path} unknown field`).toBe(400)
  }
  for (const path of ['/api/health', '/api/does-not-exist']) {
    const response = await request.get(path)
    expect(response.headers()['cache-control']).toBe('no-store')
  }
  const health = await request.get('/api/health')
  expect(health.status()).toBe(200)
  expect(await health.json()).toEqual({ status: 'ok' })
  const wideHeader = await request.get('/api/health', { headers: { 'X-Hostbud-Large': 'x'.repeat(40 << 10) } })
  expect(wideHeader.status()).toBeGreaterThanOrEqual(400)
  expect(wideHeader.status()).toBeLessThan(500)
})
