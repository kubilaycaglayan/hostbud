import { FOREIGN_ORIGIN, mutate } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { addServer, listMachines, removeServers, server, SERVER_HOST, waitForStatus } from '../helpers/servers.ts'
import { uniqueName } from '../helpers/target.ts'

// V2-M13 T2: servers through the API: scan, add with confirmed keys, use,
// refuse removal while a project uses it, remove without touching tmux.
test('(V2-M13 T2) server API: scan, add, create a session, guarded removal', async ({ request }) => {
  await removeServers(request)
  try {
    expect((await mutate(request, 'POST', '/api/machines/scan', { host: '-oProxyCommand=x' })).status()).toBe(400)
    expect((await mutate(request, 'POST', '/api/machines', { label: 'x', host: SERVER_HOST, user: 'dev', hostKeys: [] })).status()).toBe(400)
    expect((await mutate(request, 'POST', '/api/machines', { label: 'x', host: SERVER_HOST, user: 'dev', hostKeys: [] }, FOREIGN_ORIGIN)).status()).toBe(403)
    expect((await mutate(request, 'DELETE', '/api/machines/host')).status()).toBe(404)

    const added = await addServer(request, 'E2E second')
    expect(added).toMatchObject({ label: 'E2E second', source: 'custom', address: `dev@${SERVER_HOST}:22` })
    await waitForStatus(request, added.id, 'ok')
    expect((await mutate(request, 'POST', '/api/machines', { label: 'e2e SECOND', host: SERVER_HOST, user: 'dev', hostKeys: [] })).status()).toBe(409)

    const name = uniqueName('e2e-srv')
    const created = await mutate(request, 'POST', `/api/machines/${added.id}/sessions`, { name, path: '~' })
    expect(created.status(), await created.text()).toBe(201)
    expect(await server.sessions()).toContain(name)
    await expect.poll(async () => ((await (await request.get(`/api/machines/${added.id}/sessions`)).json()).sessions as { name: string }[]).map((s) => s.name)).toContain(name)

    const renamed = await mutate(request, 'PATCH', `/api/machines/${added.id}`, { label: 'E2E renamed' })
    expect(renamed.status()).toBe(200)
    expect((await listMachines(request)).find((m) => m.id === added.id)?.label).toBe('E2E renamed')

    const project = await mutate(request, 'POST', '/api/projects', { machineId: added.id, path: '/home/dev' })
    expect(project.status(), await project.text()).toBe(201)
    const { id: projectId } = await project.json()
    const queue = await mutate(request, 'POST', '/api/queues', { projectId, name: 'Q' })
    expect(queue.status()).toBe(400)
    expect(await queue.text()).toContain('host only')
    const refused = await mutate(request, 'DELETE', `/api/machines/${added.id}`)
    expect(refused.status()).toBe(409)
    expect(await refused.text()).toContain('projects')
    expect((await mutate(request, 'DELETE', `/api/projects/${projectId}`)).status()).toBe(204)
    expect((await mutate(request, 'DELETE', `/api/machines/${added.id}`)).status()).toBe(204)
    expect((await listMachines(request)).some((m) => m.id === added.id)).toBe(false)
    expect((await request.get(`/api/machines/${added.id}/sessions`)).status()).toBe(404)
    // Removing the server never touches its tmux.
    expect(await server.sessions()).toContain(name)
    await server.tmux('kill-session', '-t', `=${name}`)
  } finally {
    await removeServers(request)
  }
})
