import { sessionCookieHeader } from '../helpers/auth.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'
import {
  FOREIGN_ORIGIN,
  MACHINE,
  POLL_INTERVAL_MS,
  forbidInLogs,
  listSessions,
  mutate,
  upgradeStatus,
} from '../helpers/api.ts'

const sessionsPath = `/api/machines/${MACHINE}/sessions`

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

// API list (T12)
test('API list: a session made in a real terminal shows up in GET sessions', async ({ target, request }) => {
  const name = uniqueName('e2e-list')
  await target.tmux('new-session', '-d', '-s', name)
  await expect
    .poll(async () => (await listSessions(request)).map((s) => s.name), { timeout: 3 * POLL_INTERVAL_MS })
    .toContain(name)
  const s = (await listSessions(request)).find((x) => x.name === name)!
  expect(s.windows).toBe(1)
  expect(s.attached).toBe(0)
})

// API mutations (T12)
test('API mutations: create, rename and kill are reflected in tmux ls', async ({ target, request }) => {
  const dir = uniqueName('e2e-apidir')
  const marker = uniqueName('e2e-cmd')
  await target.run(`mkdir -p ~/${dir}`)
  forbidInLogs(dir, marker)

  const name = uniqueName('e2e-api')
  const created = await mutate(request, 'POST', sessionsPath, {
    name,
    path: `~/${dir}`,
    startCommand: `sh -c 'exec sleep 3600' ${marker}`,
  })
  expect(created.status(), await created.text()).toBe(201)
  expect(await created.json()).toEqual({ name })
  expect(await target.sessions()).toContain(name)
  expect(await target.display(name, '#{session_path}')).toBe(`/home/dev/${dir}`)
  // The mutation refreshed the inventory: the list is current right away.
  expect((await listSessions(request)).map((s) => s.name)).toContain(name)

  const renamed = `${name}-r`
  const patch = await mutate(request, 'PATCH', `${sessionsPath}/${name}`, { name: renamed })
  expect(patch.status(), await patch.text()).toBe(200)
  expect(await target.sessions()).toEqual(expect.arrayContaining([renamed]))
  expect(await target.sessions()).not.toContain(name)

  const del = await mutate(request, 'DELETE', `${sessionsPath}/${renamed}`)
  expect(del.status()).toBe(204)
  expect(await target.sessions()).not.toContain(renamed)
  expect((await listSessions(request)).map((s) => s.name)).not.toContain(renamed)
})

// API validation (T12)
test('API validation: invalid names return 400 {error, hint}', async ({ target, request }) => {
  for (const bad of ['a.b', 'a:b', 'a b']) {
    const res = await mutate(request, 'POST', sessionsPath, { name: bad })
    expect(res.status()).toBe(400)
    const body = await res.json()
    expect(body.error).toBeTruthy()
    expect(body.hint).toBeTruthy()
  }
  expect(await target.sessions()).toEqual([])
})

// Events (T12)
test('events: /ws/events sends a snapshot, then sessions.changed after a real-terminal create', async ({
  page,
  target,
}) => {
  await page.goto('/')
  // Open the socket from the page, so the browser sends its real Origin.
  await page.evaluate(() => {
    const w = window as unknown as { e2eEvents: unknown[]; e2eOpen: boolean }
    w.e2eEvents = []
    const ws = new WebSocket(`ws://${location.host}/ws/events`)
    ws.onopen = () => (w.e2eOpen = true)
    ws.onmessage = (m) => w.e2eEvents.push(JSON.parse(m.data))
  })
  const events = () => page.evaluate(() => (window as unknown as { e2eEvents: { type: string }[] }).e2eEvents)
  await expect.poll(async () => (await events())[0]?.type).toBe('snapshot')
  const snapshot = (await events())[0] as unknown as { machines: { id: string; status: string }[] }
  expect(snapshot.machines.map((m) => m.id)).toEqual([MACHINE])
  expect(snapshot.machines[0].status).toBe('ok')

  const name = uniqueName('e2e-ev')
  await target.tmux('new-session', '-d', '-s', name)
  const start = Date.now()
  await expect
    .poll(
      async () =>
        (await events()).some(
          (e) =>
            e.type === 'sessions.changed' &&
            (e as unknown as { payload: { sessions: { name: string }[] } }).payload.sessions.some(
              (s) => s.name === name,
            ),
        ),
      // One poll interval, plus slack for the ssh round trip and the browser.
      { timeout: POLL_INTERVAL_MS + 1_500, intervals: [100] },
    )
    .toBe(true)
  test.info().annotations.push({ type: 'latency', description: `${Date.now() - start}ms` })
})

// Origin (T12)
test('origin: foreign-Origin POST and /ws/events upgrade are rejected', async ({ target, request }) => {
  const name = uniqueName('e2e-evil')
  const res = await mutate(request, 'POST', sessionsPath, { name }, FOREIGN_ORIGIN)
  expect(res.status()).toBe(403)
  expect(await target.sessions()).not.toContain(name)

  expect(await upgradeStatus('/ws/events', FOREIGN_ORIGIN, sessionCookieHeader())).toBe(403)
  expect(await upgradeStatus('/ws/events', 'http://localhost:9055', sessionCookieHeader())).toBe(101)
})
