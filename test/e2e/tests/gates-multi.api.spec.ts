import type { APIRequestContext } from '@playwright/test'
import { expect, test } from '../helpers/multi.ts'
import { multiDb } from '../helpers/db.ts'
import { addItem, control, createQueue, getQueue, itemOf, newProject, type QueueItem } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M4 gates and run slots on hostbud-e2e-app-multi (parallel queues, a
// cap of one): verifying holds its queue's slot; awaiting approval frees it.

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

async function oneItemQueue(request: APIRequestContext, projectId: string, name: string, condition: string, gates: { verifyCommand?: string; requiresApproval?: boolean } = {}) {
  const queue = await createQueue(request, projectId, name)
  await stubs.setBehavior(condition, 'achieve:1', 0.5)
  const item = await addItem(request, queue.id, { instruction: `/goal ${condition}`, ...gates })
  return { queue, item }
}

async function status(request: APIRequestContext, q: { queue: { id: string }; item: QueueItem }) {
  return await itemOf(request, q.queue.id, q.item.id)
}

test.describe('gates and run slots', () => {
  test.describe.configure({ timeout: 150_000 })

  test('(V2-M4 T2) Verify holds a slot', async ({ multi, target }) => {
    await multiDb.setCapacity(1)
    const pa = await newProject(multi, target, 'e2e-gate-slot-a')
    const pb = await newProject(multi, target, 'e2e-gate-slot-b')
    const a = await oneItemQueue(multi, pa.id, 'Alpha', 'e2e gate slot a', { verifyCommand: `sh -c 'sleep 4'` })
    const b = await oneItemQueue(multi, pb.id, 'Beta', 'e2e gate slot b')
    expect((await control(multi, a.queue.id, 'start')).status()).toBe(200)
    expect((await control(multi, b.queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await status(multi, a)).status, { timeout: 30_000 }).toBe('verifying')
    // Beta waits for the slot Alpha's verify command holds.
    const waiting = await status(multi, b)
    expect(waiting.status).toBe('queued')
    expect(waiting.waitingForSlot).toBe(true)
    expect(waiting.run).toBeUndefined()
    expect((await getQueue(multi, a.queue.id)).status).toBe('running')
    await expect.poll(async () => (await status(multi, a)).status, { timeout: 30_000 }).toBe('done')
    await expect.poll(async () => (await status(multi, b)).run?.status ?? 'none', { timeout: 20_000 }).not.toBe('none')
    const order = await multiDb.runOrder()
    expect(order.map((r) => r.queue_id)).toEqual([a.queue.id, b.queue.id])
  })
})
