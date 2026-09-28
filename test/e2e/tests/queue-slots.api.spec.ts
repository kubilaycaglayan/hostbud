import type { APIRequestContext } from '@playwright/test'
import { expect, test } from '../helpers/multi.ts'
import { ctl } from '../helpers/ctl.ts'
import { multiDb } from '../helpers/db.ts'
import { addItem, control, createQueue, getQueue, itemOf, newProject, override, type QueueItem } from '../helpers/queues.ts'
import { Stubs, type StubBehavior } from '../helpers/stubs.ts'

// V2-M2 T4: the dispatcher's run slots on hostbud-e2e-app-multi
// (HOSTBUD_PARALLEL_QUEUES=true; stale window 10 s), stub clients on the
// shared throwaway target. The cap is seeded in the multi app's database
// before the queues start (the capacity API scenarios are in
// queues-multi.api.spec.ts).

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

type Setup = { condition: string; behavior: StubBehavior; delay?: number }

async function queueWith(request: APIRequestContext, projectId: string, name: string, items: Setup[]) {
  const queue = await createQueue(request, projectId, name)
  const added: QueueItem[] = []
  for (const it of items) {
    await stubs.setBehavior(it.condition, it.behavior, it.delay ?? 0.5)
    added.push(await addItem(request, queue.id, { instruction: `/goal ${it.condition}` }))
  }
  return { queue, items: added }
}

async function status(request: APIRequestContext, q: { queue: { id: string }; items: QueueItem[] }, n = 0) {
  return await itemOf(request, q.queue.id, q.items[n].id)
}

test.describe('run slots', () => {
  test.describe.configure({ timeout: 150_000 })

  test('(V2-M2 T4) Cap of one', async ({ multi, target }) => {
    await multiDb.setCapacity(1)
    const pa = await newProject(multi, target, 'e2e-cap-a')
    const pb = await newProject(multi, target, 'e2e-cap-b')
    const a = await queueWith(multi, pa.id, 'Alpha', [{ condition: 'e2e cap a1', behavior: 'achieve:4', delay: 1 }])
    const b = await queueWith(multi, pb.id, 'Beta', [{ condition: 'e2e cap b1', behavior: 'achieve:1' }])
    expect((await control(multi, a.queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await status(multi, a)).status).toBe('running')
    const started = await control(multi, b.queue.id, 'start')
    expect(started.status()).toBe(200)
    // Beta waits with "waiting for a free slot".
    await expect.poll(async () => (await status(multi, b)).waitingForSlot).toBe(true)
    const waiting = await status(multi, b)
    expect(waiting.status).toBe('queued')
    expect(waiting.run).toBeUndefined()
    expect((await getQueue(multi, b.queue.id)).status).toBe('running')
    // It starts when Alpha's run achieves.
    await expect.poll(async () => (await status(multi, a)).status, { timeout: 30_000 }).toBe('done')
    await expect.poll(async () => (await status(multi, b)).run?.status ?? 'none', { timeout: 20_000 }).not.toBe('none')
    expect((await status(multi, b)).waitingForSlot ?? false).toBe(false)
    const order = await multiDb.runOrder()
    expect(order.map((r) => r.queue_id)).toEqual([a.queue.id, b.queue.id])
    await expect.poll(async () => (await getQueue(multi, b.queue.id)).status, { timeout: 30_000 }).toBe('finished')
  })

  test('(V2-M2 T4) Slots in start order', async ({ multi, target }) => {
    await multiDb.setCapacity(1)
    const p = await newProject(multi, target, 'e2e-turns')
    const qs: Awaited<ReturnType<typeof queueWith>>[] = []
    for (const name of ['A', 'B', 'C']) {
      qs.push(await queueWith(multi, p.id, name, [
        { condition: `e2e turns ${name}1`, behavior: 'achieve:1' },
        { condition: `e2e turns ${name}2`, behavior: 'achieve:1' },
      ]))
    }
    for (const q of qs) expect((await control(multi, q.queue.id, 'start')).status()).toBe(200)
    for (const q of qs) {
      await expect.poll(async () => (await getQueue(multi, q.queue.id)).status, { timeout: 90_000 }).toBe('finished')
    }
    // The start order interleaves: no queue waits twice in a row.
    const order = (await multiDb.runOrder()).map((r) => qs.findIndex((q) => q.queue.id === r.queue_id))
    expect(order).toEqual([0, 1, 2, 0, 1, 2])
  })

  test('(V2-M2 T4) Stale holds a slot', async ({ multi, target }) => {
    await multiDb.setCapacity(1)
    const pa = await newProject(multi, target, 'e2e-stale-a')
    const pb = await newProject(multi, target, 'e2e-stale-b')
    const a = await queueWith(multi, pa.id, 'Alpha', [{ condition: 'e2e stale slot a1', behavior: 'silent' }])
    const b = await queueWith(multi, pb.id, 'Beta', [{ condition: 'e2e stale slot b1', behavior: 'achieve:1' }])
    expect((await control(multi, a.queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await status(multi, a)).status).toBe('running')
    expect((await control(multi, b.queue.id, 'start')).status()).toBe(200)
    // Alpha's run goes stale (10 s without a signal) and keeps the slot.
    await expect.poll(async () => (await status(multi, a)).run?.status, { timeout: 30_000 }).toBe('stale')
    expect((await status(multi, a)).status).toBe('needs_attention')
    await new Promise((resolve) => setTimeout(resolve, 2_000))
    const still = await status(multi, b)
    expect(still.status).toBe('queued')
    expect(still.waitingForSlot).toBe(true)
    // Skip on Alpha cancels the stale run (its session stays) and frees the slot.
    const alphaSession = (await status(multi, a)).run!.sessionName
    expect((await override(multi, a.items[0].id, 'skip')).status()).toBe(200)
    await expect.poll(async () => (await status(multi, b)).run?.status ?? 'none', { timeout: 20_000 }).not.toBe('none')
    expect((await status(multi, a)).run!.status).toBe('cancelled')
    expect(await target.sessions()).toContain(alphaSession)
    await expect.poll(async () => (await getQueue(multi, b.queue.id)).status, { timeout: 30_000 }).toBe('finished')
  })

  test('(V2-M2 T4) Cap after restart', async ({ multi, target }) => {
    await multiDb.setCapacity(1)
    const pa = await newProject(multi, target, 'e2e-restart-a')
    const pb = await newProject(multi, target, 'e2e-restart-b')
    // Turns every 1.5 s (well inside the 10 s stale window), about 18 s in all.
    const a = await queueWith(multi, pa.id, 'Alpha', [{ condition: 'e2e restart slot a1', behavior: 'achieve:12', delay: 1.5 }])
    const b = await queueWith(multi, pb.id, 'Beta', [{ condition: 'e2e restart slot b1', behavior: 'achieve:1' }])
    expect((await control(multi, a.queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await status(multi, a)).run?.status).toBe('running')
    expect((await control(multi, b.queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await status(multi, b)).waitingForSlot).toBe(true)

    await ctl.multiRestart()
    // After the restart Alpha's run still counts: Beta keeps waiting.
    const after = await status(multi, b)
    expect(after.status).toBe('queued')
    expect(after.waitingForSlot).toBe(true)
    expect((await multiDb.runOrder()).filter((r) => ['starting', 'running', 'stale'].includes(r.status))).toHaveLength(1)
    // Then Beta starts once Alpha achieves.
    await expect.poll(async () => (await status(multi, a)).status, { timeout: 45_000 }).toBe('done')
    await expect.poll(async () => (await getQueue(multi, b.queue.id)).status, { timeout: 30_000 }).toBe('finished')
    expect((await multiDb.runOrder()).map((r) => r.queue_id)).toEqual([a.queue.id, b.queue.id])
  })
})
