import { request as playwrightRequest } from '@playwright/test'
import { FOREIGN_ORIGIN, ORIGIN, mutate, forbidInLogs } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test('project API: auth, Origin and live project events', async ({ page, request, target, baseURL }) => {
  const name = uniqueName('e2e-project')
  const path = `/home/dev/${name}`
  forbidInLogs(path, name)
  await target.run(`mkdir -p ${shq(path)}`)

  const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  try {
    expect((await anonymous.get('/api/projects')).status()).toBe(401)
    expect(
      (await anonymous.post('/api/projects', {
        data: { machineId: 'host', path, name },
        headers: { Origin: ORIGIN },
      })).status(),
    ).toBe(401)
    expect((await mutate(request, 'POST', '/api/projects', { machineId: 'host', path, name }, FOREIGN_ORIGIN)).status()).toBe(403)

    await page.goto('/')
    const eventPromise = page.evaluate(
      (projectName) =>
        new Promise<{ type: string; machine: string; payload: { action: string; project: { id: string; name: string } } }>(
          (resolve, reject) => {
            const wsURL = new URL('/ws/events', location.href)
            wsURL.protocol = wsURL.protocol === 'https:' ? 'wss:' : 'ws:'
            const socket = new WebSocket(wsURL)
            const timeout = window.setTimeout(() => {
              socket.close()
              reject(new Error('project event timeout'))
            }, 15_000)
            socket.addEventListener('message', (message) => {
              const event = JSON.parse(String(message.data))
              if (event.type === 'projects.changed' && event.payload?.project?.name === projectName) {
                window.clearTimeout(timeout)
                socket.close()
                resolve(event)
              }
            })
            socket.addEventListener('error', () => reject(new Error('project event socket failed')))
          },
        ),
      name,
    )

    const created = await mutate(request, 'POST', '/api/projects', { machineId: 'host', path, name })
    expect(created.status(), await created.text()).toBe(201)
    const project = await created.json()
    expect(project).toMatchObject({ machineId: 'host', path, name })

    const listed = await request.get('/api/projects', { params: { machine: 'host' } })
    expect(listed.status()).toBe(200)
    expect((await listed.json()).projects).toContainEqual(expect.objectContaining({ id: project.id, path, name }))

    const event = await eventPromise
    expect(event).toMatchObject({ type: 'projects.changed', machine: 'host', payload: { action: 'upsert', project: { id: project.id } } })
  } finally {
    await anonymous.dispose()
  }
})
