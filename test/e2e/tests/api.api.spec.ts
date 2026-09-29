import { sessionCookieHeader } from '../helpers/auth.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { shq, uniqueName } from '../helpers/target.ts'
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

test('API kill many: one request kills several sessions and reports the missing one', async ({ target, request }) => {
  const a = uniqueName('e2e-killmany-a')
  const b = uniqueName('e2e-killmany-b')
  const keep = uniqueName('e2e-killmany-keep')
  const gone = uniqueName('e2e-killmany-gone')
  for (const name of [a, b, keep]) await target.tmux('new-session', '-d', '-s', name)
  const res = await mutate(request, 'POST', `${sessionsPath}/kill`, { names: [a, gone, b] })
  expect(res.status(), await res.text()).toBe(200)
  const body = await res.json()
  expect(body.killed).toEqual([a, b])
  expect(body.failed).toEqual([expect.objectContaining({ name: gone, hint: expect.any(String) })])
  const left = await target.sessions()
  expect(left).not.toContain(a)
  expect(left).not.toContain(b)
  expect(left).toContain(keep)
  // The batch refreshed the inventory once at the end: the list is current right away.
  const listed = (await listSessions(request)).map((s) => s.name)
  expect(listed).not.toContain(a)
  expect(listed).not.toContain(b)
  expect(listed).toContain(keep)

  const invalid = await mutate(request, 'POST', `${sessionsPath}/kill`, { names: [keep, 'bad name'] })
  expect(invalid.status()).toBe(400)
  expect(await target.sessions()).toContain(keep)
})

test('(T10) Create with a taken name numbers it; rename still conflicts', async ({ target, request }) => {
  const name = uniqueName('e2e-api-dup')
  const another = uniqueName('e2e-api-rename')
  forbidInLogs(name, another)
  const first = await mutate(request, 'POST', sessionsPath, { name, path: '~' })
  expect(first.status()).toBe(201)
  expect(await first.json()).toEqual({ name })
  const second = await mutate(request, 'POST', sessionsPath, { name, path: '~' })
  expect(second.status()).toBe(201)
  expect(await second.json()).toEqual({ name: `${name}-1` })
  const other = await mutate(request, 'POST', sessionsPath, { name: another, path: '~' })
  expect(other.status()).toBe(201)
  const rename = await mutate(request, 'PATCH', `${sessionsPath}/${another}`, { name })
  expect(rename.status()).toBe(409)
  expect(await target.sessions()).toEqual(expect.arrayContaining([name, `${name}-1`, another]))
})

// V2-M1 T0: start commands run in the user's login shell (PATH from
// ~/.profile), in the chosen folder, and a command that ends keeps its session.
test('(V2-M1 T0) Session with start command: API variant', async ({ target, request }) => {
  const dir = `${uniqueName('e2e-startapi')} x`
  const tool = uniqueName('hostbud-e2e-tool')
  forbidInLogs(dir, tool)
  await target.run(`mkdir -p ~/.local/bin ${shq('/home/dev/' + dir)} && printf '#!/bin/sh\\necho "TOOL_OK[$*]"\\nexec sleep 3600\\n' > ~/.local/bin/${tool} && chmod 755 ~/.local/bin/${tool}`)
  try {
    const name = uniqueName('e2e-startapi')
    const created = await mutate(request, 'POST', sessionsPath, {
      name,
      path: `~/${dir}`,
      startCommand: `${tool} --flag 'a b' "$HOME" ~/x`,
    })
    expect(created.status(), await created.text()).toBe(201)
    await expect.poll(() => target.capture(name)).toContain('TOOL_OK[--flag a b /home/dev /home/dev/x]')
    expect(await target.display(name, '#{pane_current_path}')).toBe(`/home/dev/${dir}`)

    const ended = uniqueName('e2e-startapi-end')
    const done = await mutate(request, 'POST', sessionsPath, { name: ended, path: `~/${dir}`, startCommand: `sh -c 'echo HOSTBUD_START_OK'` })
    expect(done.status(), await done.text()).toBe(201)
    await expect.poll(() => target.capture(ended)).toContain('HOSTBUD_START_OK')
    expect(await target.sessions()).toContain(ended)
  } finally {
    await target.run(`rm -f ~/.local/bin/${tool}`)
  }
})

// API validation (T12)
test('API validation: invalid names return 400 {error, hint}', async ({ target, request }) => {
  for (const bad of ['a.b', 'a:b', 'a b.c']) {
    const res = await mutate(request, 'POST', sessionsPath, { name: bad })
    expect(res.status()).toBe(400)
    const body = await res.json()
    expect(body.error).toBeTruthy()
    expect(body.hint).toBeTruthy()
  }
  expect(await target.sessions()).toEqual([])
})

test('API: spaces in a session name become hyphens', async ({ target, request }) => {
  const base = uniqueName('e2e-api-space')
  const res = await mutate(request, 'POST', sessionsPath, { name: ` ${base} new  session ` })
  expect(res.status(), await res.text()).toBe(201)
  expect((await res.json()).name).toBe(`${base}-new-session`)
  expect(await target.sessions()).toEqual([`${base}-new-session`])
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
