import { FOREIGN_ORIGIN, mutate } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { addServer, listMachines, removeServers, server, SERVER_HOST, waitForStatus } from '../helpers/servers.ts'
import { uniqueName } from '../helpers/target.ts'
import { addItem, control, createQueue, newProject, waitItem } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

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
    // A server's project takes queues too (its runs: the next scenario).
    const queue = await mutate(request, 'POST', '/api/queues', { projectId, name: 'Q' })
    expect(queue.status(), await queue.text()).toBe(201)
    const { id: queueId, machineId } = await queue.json()
    expect(machineId).toBe(added.id)
    expect((await mutate(request, 'DELETE', `/api/queues/${queueId}`)).status()).toBe(204)
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

// Queue runs on servers (V2-M13 follow-up): a queue in a server's project
// starts its run session on that server (the stub client there), whose
// hooks reach hostbud at HOSTBUD_SERVER_HOOK_BASE_URL; the item finishes.
test('(V2-M13 queues) a server project\'s queue runs its item on the server', async ({ request }) => {
  await removeServers(request)
  const stubs = new Stubs(server)
  await stubs.reset()
  let projectId = ''
  try {
    const added = await addServer(request, 'E2E queue server')
    await waitForStatus(request, added.id, 'ok')
    const project = await newProject(request, server, 'e2e-srvq', added.id)
    projectId = project.id
    const queue = await createQueue(request, project.id, 'Remote')
    expect(queue.machineId).toBe(added.id)
    const condition = 'e2e server queue achieves'
    await stubs.setBehavior(condition, 'achieve:1', 0.5)
    const item = await addItem(request, queue.id, { instruction: `/goal ${condition}` })
    const started = await control(request, queue.id, 'start')
    expect(started.status(), await started.text()).toBe(200)
    const done = await waitItem(request, queue.id, item.id, 'done')
    const runId = done.run!.id
    // The stub that ran is the server's (its log is on the server).
    const log = await stubs.log(runId, 'claude')
    expect(log?.cwd).toBe(project.path)
    expect(log?.env.HOSTBUD_URL).toBe('http://hostbud-e2e-caddy:9055')
    expect((await mutate(request, 'DELETE', `/api/queues/${queue.id}`)).status()).toBe(204)
  } finally {
    if (projectId) await mutate(request, 'DELETE', `/api/projects/${projectId}`)
    await server.resetTmux()
    await removeServers(request)
  }
})
