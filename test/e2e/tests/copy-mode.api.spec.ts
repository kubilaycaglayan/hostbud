import { request as playwrightRequest } from '@playwright/test'
import { FOREIGN_ORIGIN, MACHINE, ORIGIN, mutate } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

test('(T1) Copy-mode API', async ({ request, target, baseURL }) => {
  const name = uniqueName('e2e-scroll')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  try {
    await target.tmux('send-keys', '-t', `=${name}:`, `seq 1 400`, 'Enter')
    await expect.poll(async () => {
      const res = await request.get(`/api/machines/${MACHINE}/sessions`)
      return ((await res.json()).sessions as { name: string }[]).some((s) => s.name === name)
    }).toBe(true)
    await expect.poll(async () => Number(await target.display(name, '#{history_size}'))).toBeGreaterThan(100)
    const path = `/api/machines/${MACHINE}/sessions/${name}/copy-mode`
    const enter = await mutate(request, 'POST', path, { action: 'enter' }, ORIGIN)
    expect(enter.status(), await enter.text()).toBe(200)
    const first = await enter.json()
    expect(first.inMode).toBe(true)
    expect(first.scrollPosition).toBeGreaterThan(0)
    const page = await mutate(request, 'POST', path, { action: 'page-up' }, ORIGIN)
    expect(page.status(), await page.text()).toBe(200)
    expect((await page.json()).scrollPosition).toBeGreaterThan(first.scrollPosition)
    const exit = await mutate(request, 'POST', path, { action: 'exit' }, ORIGIN)
    expect(exit.status()).toBe(200)
    expect(await target.display(name, '#{pane_in_mode}')).toBe('0')
    const again = await mutate(request, 'POST', path, { action: 'exit' }, ORIGIN)
    expect(again.status()).toBe(200)
    expect(await again.json()).toMatchObject({ inMode: false })
    const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    try { expect((await anonymous.post(path, { data: { action: 'enter' }, headers: { Origin: ORIGIN } })).status()).toBe(401) }
    finally { await anonymous.dispose() }
    expect((await mutate(request, 'POST', path, { action: 'enter' }, FOREIGN_ORIGIN)).status()).toBe(403)
    expect((await mutate(request, 'POST', `/api/machines/${MACHINE}/sessions/missing-session/copy-mode`, { action: 'enter' }, ORIGIN)).status()).toBe(404)
  } finally {
    await target.tmux('kill-session', '-t', `=${name}`).catch(() => undefined)
  }
})
