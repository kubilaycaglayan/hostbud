import { expect, test } from '../helpers/multi.ts'
import { multiDb, queues } from '../helpers/db.ts'
import { addItem, control, createQueue, getQueue, itemOf, listQueues, newProject, type QueueItem } from '../helpers/queues.ts'
import { Stubs, type StubBehavior } from '../helpers/stubs.ts'
import type { APIRequestContext } from '@playwright/test'
import { shq, type Target } from '../helpers/target.ts'
import { mutate } from '../helpers/api.ts'

// V2-M2: several queues and parallel runs on hostbud-e2e-app-multi
// (HOSTBUD_PARALLEL_QUEUES=true), with the stub clients on the shared
// throwaway target. API level; the panel scenarios are in queues*.spec.ts.

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

type Setup = { condition: string; behavior: StubBehavior }

async function queueOn(request: APIRequestContext, projectId: string, name: string, items: Setup[]) {
  const queue = await createQueue(request, projectId, name)
  const added: QueueItem[] = []
  for (const it of items) {
    await stubs.setBehavior(it.condition, it.behavior, 0.5)
    added.push(await addItem(request, queue.id, { instruction: `/goal ${it.condition}` }))
  }
  return { queue, items: added }
}

async function waitItem(request: APIRequestContext, queueId: string, itemId: string, status: QueueItem['status'], timeout = 30_000) {
  await expect.poll(async () => (await itemOf(request, queueId, itemId)).status, { timeout }).toBe(status)
  return await itemOf(request, queueId, itemId)
}

async function project(request: APIRequestContext, target: Target, prefix: string) {
  return await newProject(request, target, prefix)
}

test.describe('parallel queues', () => {
  test.describe.configure({ timeout: 120_000 })

  test('(V2-M2 T2) Two queues in parallel', async ({ multi, target }) => {
    const pa = await project(multi, target, 'e2e-par-a')
    const pb = await project(multi, target, 'e2e-par-b')
    const a = await queueOn(multi, pa.id, 'Alpha', [
      { condition: 'e2e parallel a1', behavior: 'slow:6' },
      { condition: 'e2e parallel a2', behavior: 'achieve:1' },
    ])
    const b = await queueOn(multi, pb.id, 'Beta', [
      { condition: 'e2e parallel b1', behavior: 'slow:6' },
      { condition: 'e2e parallel b2', behavior: 'achieve:1' },
    ])
    expect((await control(multi, a.queue.id, 'start')).status()).toBe(200)
    expect((await control(multi, b.queue.id, 'start')).status()).toBe(200)

    // Both first items run at once; each queue's second item waits.
    await expect.poll(async () => [
      (await itemOf(multi, a.queue.id, a.items[0].id)).status,
      (await itemOf(multi, b.queue.id, b.items[0].id)).status,
    ], { timeout: 20_000 }).toEqual(['running', 'running'])
    for (const q of [a, b]) {
      expect((await getQueue(multi, q.queue.id)).items.map((i) => i.status)).toEqual(['running', 'queued'])
    }
    const running = await Promise.all([a, b].map(async (q) => (await itemOf(multi, q.queue.id, q.items[0].id)).run!.sessionName))
    await expect.poll(async () => (await target.sessions()).filter((s) => running.includes(s)).length).toBe(2)

    // Within a queue the next item starts only after its own achieved record.
    for (const q of [a, b]) {
      const second = await waitItem(multi, q.queue.id, q.items[1].id, 'running', 30_000)
      const first = await itemOf(multi, q.queue.id, q.items[0].id)
      expect(first.status).toBe('done')
      expect(first.run!.status).toBe('achieved')
      expect(second.run!.id > first.run!.id).toBe(true)
    }
    for (const q of [a, b]) {
      await expect.poll(async () => (await getQueue(multi, q.queue.id)).status, { timeout: 30_000 }).toBe('finished')
    }
    const order = await multiDb.runOrder()
    expect(order.map((r) => r.status)).toEqual(['achieved', 'achieved', 'achieved', 'achieved'])
  })

  test('(V2-M2 T2) Same-directory warning', async ({ multi, target }) => {
    const shared = await project(multi, target, 'e2e-shared')
    const a = await queueOn(multi, shared.id, 'Alpha', [{ condition: 'e2e shared a1', behavior: 'slow:8' }])
    const b = await queueOn(multi, shared.id, 'Beta', [{ condition: 'e2e shared b1', behavior: 'slow:8' }])
    const other = await project(multi, target, 'e2e-shared-other')
    const c = await queueOn(multi, other.id, 'Gamma', [{ condition: 'e2e shared c1', behavior: 'slow:8' }])

    const startA = await control(multi, a.queue.id, 'start')
    expect(startA.status()).toBe(200)
    expect((await startA.json()).warnings ?? []).toEqual([])
    // Another directory: no warning.
    const startC = await control(multi, c.queue.id, 'start')
    expect(startC.status()).toBe(200)
    expect((await startC.json()).warnings ?? []).toEqual([])

    // The second queue in the same directory starts (never blocked) and warns.
    const startB = await control(multi, b.queue.id, 'start')
    expect(startB.status()).toBe(200)
    const warnings = (await startB.json()).warnings
    expect(warnings).toHaveLength(1)
    expect(warnings[0]).toMatchObject({ code: 'shared_directory', queues: [{ id: a.queue.id, name: 'Alpha' }] })
    expect(warnings[0].message).toContain(shared.path)

    // GET /api/queues marks both queues, and not the other directory's.
    const list = await listQueues(multi)
    const byName = Object.fromEntries(list.map((q) => [q.name, q]))
    expect(byName.Alpha.warnings?.[0]).toMatchObject({ code: 'shared_directory', queues: [{ id: b.queue.id }] })
    expect(byName.Beta.warnings?.[0]).toMatchObject({ code: 'shared_directory', queues: [{ id: a.queue.id }] })
    expect(byName.Gamma.warnings ?? []).toEqual([])
    // Both run at once despite the warning.
    await expect.poll(async () => [
      (await itemOf(multi, a.queue.id, a.items[0].id)).status,
      (await itemOf(multi, b.queue.id, b.items[0].id)).status,
    ], { timeout: 20_000 }).toEqual(['running', 'running'])

    // Once both finish, the warning is gone.
    for (const q of [a, b, c]) {
      await expect.poll(async () => (await getQueue(multi, q.queue.id)).status, { timeout: 40_000 }).toBe('finished')
    }
    for (const q of await listQueues(multi)) expect(q.warnings ?? []).toEqual([])
  })

  test('(V2-M2 T3) Name collision', async ({ multi, target }) => {
    const p = await project(multi, target, 'e2e-names')
    const first = await queueOn(multi, p.id, 'Alpha', [{ condition: 'e2e names a1', behavior: 'slow:6' }])
    const docs = await queueOn(multi, p.id, 'Docs', [{ condition: 'e2e names d1', behavior: 'slow:6' }])
    const late = await queueOn(multi, p.id, 'Late', [{ condition: 'e2e names l1', behavior: 'slow:6' }])
    // A leftover session already holds the third queue's name.
    await target.run(`tmux new-session -d -s ${shq(`${p.name}-Late-q1`)}`)
    for (const q of [first, docs, late]) expect((await control(multi, q.queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => Promise.all([first, docs, late].map(async (q) => (await itemOf(multi, q.queue.id, q.items[0].id)).run?.sessionName ?? '')), { timeout: 20_000 })
      .toEqual([`${p.name}-q1`, `${p.name}-Docs-q1`, `${p.name}-Late-q1-1`])
    const sessions = await target.sessions()
    for (const name of [`${p.name}-q1`, `${p.name}-Docs-q1`, `${p.name}-Late-q1`, `${p.name}-Late-q1-1`]) expect(sessions).toContain(name)
    // Renaming a queue never renames its run's session.
    const renamed = await mutate(multi, 'PATCH', `/api/queues/${docs.queue.id}`, { name: 'Changelog' })
    expect(renamed.status()).toBe(200)
    expect((await itemOf(multi, docs.queue.id, docs.items[0].id)).run!.sessionName).toBe(`${p.name}-Docs-q1`)
  })
})

// Criterion 3 on the switch-off app (hostbud-e2e-app): a queue left over
// from switch-on stays usable, one at a time.
test.describe('parallel queues switched off', () => {
  test.describe.configure({ timeout: 90_000 })

  test('(V2-M2 T2) Switch off: a leftover queue waits for the active one', async ({ request, target }) => {
    await queues.deleteAll() // the switch-off app allows one queue
    const p = await newProject(request, target, 'e2e-off')
    const a = await queueOn(request, p.id, 'Alpha', [{ condition: 'e2e off a1', behavior: 'slow:4' }])
    const leftoverId = await queues.seedQueue(p.id, 'Leftover')
    await stubs.setBehavior('e2e off l1', 'achieve:1', 0.5)
    await addItem(request, leftoverId, { instruction: '/goal e2e off l1' })
    expect((await listQueues(request)).map((q) => q.name).sort()).toEqual(['Alpha', 'Leftover'])

    expect((await control(request, a.queue.id, 'start')).status()).toBe(200)
    const refused = await control(request, leftoverId, 'start')
    expect(refused.status()).toBe(409)
    expect((await refused.json()).error).toBe(
      'Parallel queues are off — pause queue Alpha and wait for its run to end, or set `HOSTBUD_PARALLEL_QUEUES=true`',
    )
    // Nothing was cancelled: Alpha's run finishes on its own, then the
    // leftover queue may start.
    await expect.poll(async () => (await getQueue(request, a.queue.id)).status, { timeout: 30_000 }).toBe('finished')
    expect((await control(request, leftoverId, 'start')).status()).toBe(200)
    await expect.poll(async () => (await getQueue(request, leftoverId)).status, { timeout: 30_000 }).toBe('finished')
  })
})
