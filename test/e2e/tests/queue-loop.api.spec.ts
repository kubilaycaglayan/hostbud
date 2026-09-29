import { expect, test } from '../helpers/fixtures.ts'
import { mutate, ORIGIN } from '../helpers/api.ts'
import { createQueue, getQueue, newProject, type Queue } from '../helpers/queues.ts'
import { shq } from '../helpers/target.ts'

// A pass of only existing-session commands ends at once, so the next pass
// waits for the minimum pass interval (1 min); the 30 s runtime limit has
// passed by the end of pass 2, so there is no pass 3.
test('Loop queue runs its items again until the runtime limit', async ({ request, target }) => {
  test.setTimeout(150_000)
  await target.resetTmux()
  const project = await newProject(request, target, 'e2e-loop')
  const session = 'loop-target'
  await target.run(`tmux new-session -d -s ${shq(session)} -c ${shq(project.path)}`)
  const queue = await createQueue(request, project.id)
  expect(queue.loop).toBeUndefined()
  const add = await mutate(request, 'POST', `/api/queues/${queue.id}/items`, {
    executionMode: 'session', targetSession: session, command: `printf '%s\\n' loop-pass-marker`, agent: 'claude', flags: '', instruction: '',
  }, ORIGIN)
  expect(add.status(), await add.text()).toBe(201)

  const bad = await mutate(request, 'PUT', `/api/queues/${queue.id}/loop`, { enabled: true, maxRuntime: 'forever' }, ORIGIN)
  expect(bad.status(), await bad.text()).toBe(400)
  const loop = await mutate(request, 'PUT', `/api/queues/${queue.id}/loop`, { enabled: true, maxRuntime: '30s' }, ORIGIN)
  expect(loop.status(), await loop.text()).toBe(200)
  expect((await loop.json() as Queue).loop).toMatchObject({ enabled: true, maxRuntimeSeconds: 30, pass: 1 })

  const start = await mutate(request, 'POST', `/api/queues/${queue.id}/start`, undefined, ORIGIN)
  expect(start.status(), await start.text()).toBe(200)
  const markers = async () => (await target.capture(session)).split('\n').filter((l) => l.trim() === 'loop-pass-marker').length
  await expect.poll(markers, { timeout: 10_000 }).toBe(1)
  // Pass 2 is scheduled, not started at once.
  await expect.poll(async () => {
    const q = await getQueue(request, queue.id)
    return { status: q.status, pass: q.loop?.pass, scheduled: Boolean(q.scheduledAt), item: q.items[0].status }
  }).toEqual({ status: 'running', pass: 2, scheduled: true, item: 'queued' })
  await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 100_000 }).toBe('finished')
  expect(await markers()).toBe(2)
  const done = await getQueue(request, queue.id)
  expect(done.loop?.pass).toBe(2)
  expect(done.items[0].status).toBe('done')
  expect(await target.sessions()).toContain(session)
})
