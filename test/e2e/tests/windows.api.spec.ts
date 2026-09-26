import { request as playwrightRequest } from '@playwright/test'
import { FOREIGN_ORIGIN, MACHINE, mutate, forbidInLogs } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

test('(T1) Windows and panes API', async ({ request, target, baseURL }) => {
  const name = uniqueName('e2e-windows')
  const other = uniqueName('e2e-windows-other')
  forbidInLogs(name, other)
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await target.tmux('new-session', '-d', '-s', other, '-c', '/home/dev')
  try {
    await target.tmux('split-window', '-d', '-t', '=' + name + ':0')
    await target.tmux('new-window', '-d', '-t', '=' + name + ':', '-n', 'two λ')
    await target.tmux('new-window', '-d', '-t', '=' + name + ':', '-n', 'three')
    await expect.poll(async () => {
      const res = await request.get('/api/machines/' + MACHINE + '/sessions')
      return ((await res.json()).sessions as { name: string }[]).some((s) => s.name === name)
    }).toBe(true)

    const path = '/api/machines/' + MACHINE + '/sessions/' + name + '/windows'
    const listed = await request.get(path)
    expect(listed.status(), await listed.text()).toBe(200)
    const { windows, truncated } = await listed.json()
    expect(truncated).toBe(false)
    expect(windows).toHaveLength(3)
    expect(windows.map((w: { index: number }) => w.index)).toEqual([0, 1, 2])
    expect(windows.map((w: { name: string }) => w.name)).toContain('two λ')
    expect(windows[0].panes).toHaveLength(2)
    expect(windows[0].panes.map((p: { index: number }) => p.index)).toEqual([0, 1])
    const directWindows = (await target.tmux(
      'list-windows', '-t', '=' + name,
      '-F', '#{window_id}\t#{window_index}\t#{window_active}\t#{window_panes}\t#{window_name}',
    )).trim().split('\n').map((line) => line.split('\t'))
    expect(windows.map((w: { id: string; index: number; active: boolean; name: string; panes: unknown[] }) =>
      [w.id, String(w.index), w.active ? '1' : '0', String(w.panes.length), w.name],
    )).toEqual(directWindows)
    const directPanes = (await target.tmux(
      'list-panes', '-s', '-t', '=' + name,
      '-F', '#{window_id}\t#{pane_id}\t#{pane_index}\t#{pane_active}\t#{pane_width}\t#{pane_height}\t#{pane_current_command}',
    )).trim().split('\n').map((line) => line.split('\t'))
    expect(windows.flatMap((w: { id: string; panes: { id: string; index: number; active: boolean; width: number; height: number; command: string }[] }) =>
      w.panes.map((p) => [w.id, p.id, String(p.index), p.active ? '1' : '0', String(p.width), String(p.height), p.command]),
    )).toEqual(directPanes)

    const firstWindow = windows[0] as { id: string; panes: { id: string }[] }
    const secondWindow = windows[1] as { id: string }
    const foreign = (await target.tmux('display-message', '-p', '-t', '=' + other + ':', '#{pane_id}')).trim()
    const selectPath = '/api/machines/' + MACHINE + '/sessions/' + name + '/select'
    const selectedWindow = await mutate(request, 'POST', selectPath, { window: secondWindow.id })
    expect(selectedWindow.status(), await selectedWindow.text()).toBe(200)
    expect(await target.display(name, '#{window_id}')).toBe(secondWindow.id)

    const selectedPane = await mutate(request, 'POST', selectPath, { window: firstWindow.id, pane: firstWindow.panes[1].id })
    expect(selectedPane.status(), await selectedPane.text()).toBe(200)
    expect(await target.display(name, '#{window_id} #{pane_id}')).toBe(firstWindow.id + ' ' + firstWindow.panes[1].id)
    expect((await mutate(request, 'POST', selectPath, { window: firstWindow.id, pane: foreign })).status()).toBe(404)
    expect(await target.display(name, '#{window_id} #{pane_id}')).toBe(firstWindow.id + ' ' + firstWindow.panes[1].id)

    const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
    try {
      expect((await anonymous.get(path)).status()).toBe(401)
      expect((await anonymous.post(selectPath, { data: { window: secondWindow.id }, headers: { Origin: 'http://localhost:9055' } })).status()).toBe(401)
    } finally {
      await anonymous.dispose()
    }
    expect((await mutate(request, 'POST', selectPath, { window: secondWindow.id }, FOREIGN_ORIGIN)).status()).toBe(403)
    expect((await request.get('/api/machines/' + MACHINE + '/sessions/missing-session/windows')).status()).toBe(404)
  } finally {
    await target.tmux('kill-session', '-t', '=' + name).catch(() => undefined)
    await target.tmux('kill-session', '-t', '=' + other).catch(() => undefined)
  }
})
