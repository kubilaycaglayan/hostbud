import { expect, test } from '../helpers/fixtures.ts'
import { mutate, ORIGIN } from '../helpers/api.ts'
import { addItem, control, createQueue, getQueue, itemOf, newProject, type QueueItem } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M1 T8: the queue REST API through Caddy (cookie + Origin like every
// v1 route). Runs use the stub clients on the throwaway target.

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

test('(V2-M1 T8) Queue API: build, reorder, edit, delete, start and pause', async ({ request, target }) => {
  const project = await newProject(request, target)
  const queue = await createQueue(request, project.id)
  expect(queue).toMatchObject({ projectId: project.id, name: 'Milestones', status: 'idle', projectName: project.name, projectPath: project.path, items: [] })

  // A second queue can be created with parallel queues off (to organize
  // work); only running is limited to one queue at a time.
  const second = await mutate(request, 'POST', '/api/queues', { projectId: project.id, name: 'Another' }, ORIGIN)
  expect(second.status()).toBe(201)
  const another = await second.json() as { id: string; status: string }
  expect(another.status).toBe('idle')
  expect((await mutate(request, 'DELETE', `/api/queues/${another.id}`, undefined, ORIGIN)).status()).toBe(204)

  const conditions = ['e2e queue api m1 stays pending', 'e2e queue api m2', 'e2e queue api m3']
  await stubs.setBehavior(conditions[0], 'pending', 1)
  const items: QueueItem[] = []
  for (const condition of conditions) items.push(await addItem(request, queue.id, { instruction: `/goal ${condition}`, flags: `--model 'opus 4'` }))
  expect(items.map((i) => i.position)).toEqual([1, 2, 3])

  // Validation: the instruction is one non-empty line, the agent must be
  // known, and flags must split.
  for (const bad of [
    { agent: 'claude', flags: '', instruction: 'work on M2\nnext' },
    { agent: 'claude', flags: '', instruction: '  ' },
    { agent: 'gemini', flags: '', instruction: '/goal x' },
    { agent: 'claude', flags: `--model 'x`, instruction: '/goal x' },
  ]) {
    expect((await mutate(request, 'POST', `/api/queues/${queue.id}/items`, bad, ORIGIN)).status(), JSON.stringify(bad)).toBe(400)
  }

  // Reorder (exactly the queued items), edit a queued item, delete one.
  const reordered = await mutate(request, 'PUT', `/api/queues/${queue.id}/order`, { itemIds: [items[0].id, items[2].id, items[1].id] }, ORIGIN)
  expect(reordered.status(), await reordered.text()).toBe(200)
  expect((await reordered.json()).items.map((i: { id: string }) => i.id)).toEqual([items[0].id, items[2].id, items[1].id])
  expect((await mutate(request, 'PUT', `/api/queues/${queue.id}/order`, { itemIds: [items[0].id] }, ORIGIN)).status()).toBe(409)
  const edited = await mutate(request, 'PATCH', `/api/queue-items/${items[2].id}`, { instruction: `/goal ${conditions[2]} and docs`, agent: 'codex' }, ORIGIN)
  expect(edited.status(), await edited.text()).toBe(200)
  expect(await edited.json()).toMatchObject({ agent: 'codex', instruction: `/goal ${conditions[2]} and docs`, flags: `--model 'opus 4'` })
  expect((await mutate(request, 'DELETE', `/api/queue-items/${items[1].id}`, undefined, ORIGIN)).status()).toBe(204)
  let current = await getQueue(request, queue.id)
  expect(current.items.map((i) => [i.id, i.position])).toEqual([[items[0].id, 1], [items[2].id, 2]])

  // Start: item 1 runs and stays pending, so nothing advances.
  const started = await control(request, queue.id, 'start')
  expect(started.status(), await started.text()).toBe(200)
  await expect.poll(async () => (await itemOf(request, queue.id, items[0].id)).status, { timeout: 15_000 }).toBe('running')
  await expect.poll(async () => (await itemOf(request, queue.id, items[0].id)).run?.status, { timeout: 15_000 }).toBe('running')
  await new Promise((resolve) => setTimeout(resolve, 3_000))
  current = await getQueue(request, queue.id)
  expect(current.status).toBe('running')
  expect(current.items.map((i) => i.status)).toEqual(['running', 'queued'])

  // A running item can't be edited, deleted or moved.
  expect((await mutate(request, 'PATCH', `/api/queue-items/${items[0].id}`, { instruction: '/goal other' }, ORIGIN)).status()).toBe(409)
  expect((await mutate(request, 'DELETE', `/api/queue-items/${items[0].id}`, undefined, ORIGIN)).status()).toBe(409)
  expect((await mutate(request, 'PUT', `/api/queues/${queue.id}/order`, { itemIds: [items[2].id, items[0].id] }, ORIGIN)).status()).toBe(409)
  // Deleting the queue with an active run is refused.
  const busy = await mutate(request, 'DELETE', `/api/queues/${queue.id}`, undefined, ORIGIN)
  expect(busy.status()).toBe(409)
  expect((await busy.json()).error).toContain('A run is still active in this queue')

  // Pause: no new item starts, and the running session is left alone.
  const session = (await itemOf(request, queue.id, items[0].id)).run!.sessionName
  const paused = await control(request, queue.id, 'pause')
  expect(paused.status()).toBe(200)
  expect((await paused.json()).status).toBe('paused')
  expect((await control(request, queue.id, 'pause')).status()).toBe(409)
  expect((await control(request, queue.id, 'start')).status()).toBe(409)
  expect(await target.sessions()).toContain(session)
  expect((await itemOf(request, queue.id, items[0].id)).status).toBe('running')
  expect((await itemOf(request, queue.id, items[2].id)).status).toBe('queued')
})
