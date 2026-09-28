import { expect, test } from '../helpers/fixtures.ts'
import { mutate } from '../helpers/api.ts'
import { addItem, control, createQueue, itemOf, newProject, type QueueItem } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M4 completion gates through the API (Caddy's loopback site), with the
// stub clients on the throwaway target. Written per task (T1–T4); run on
// demand only.

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

async function waitItem(request: Parameters<typeof itemOf>[0], queueId: string, itemId: string, status: QueueItem['status'], timeout = 30_000) {
  await expect.poll(async () => (await itemOf(request, queueId, itemId)).status, { timeout }).toBe(status)
  return await itemOf(request, queueId, itemId)
}

test.describe('completion gates', () => {
  test.describe.configure({ timeout: 120_000 })

  test('(V2-M4 T1) Gate fields', async ({ request, target }) => {
    const project = await newProject(request, target, 'e2e-gates')
    const queue = await createQueue(request, project.id)

    // No gates by default; gates read back as sent.
    const plain = await addItem(request, queue.id, { instruction: '/goal e2e gates plain' })
    expect(plain).toMatchObject({ verifyCommand: '', requiresApproval: false })
    const gated = await addItem(request, queue.id, { instruction: '/goal e2e gates gated', verifyCommand: `sh -c 'test -f done.flag'`, requiresApproval: true })
    expect(gated).toMatchObject({ verifyCommand: `sh -c 'test -f done.flag'`, requiresApproval: true })
    expect(await itemOf(request, queue.id, gated.id)).toMatchObject({ verifyCommand: `sh -c 'test -f done.flag'`, requiresApproval: true })
    for (const it of [plain, gated]) expect((await mutate(request, 'DELETE', `/api/queue-items/${it.id}`, undefined)).status()).toBe(204)

    // Invalid verify commands → 400 with the argv hint.
    for (const bad of [`make 'test`, 'make\ntest', 'x'.repeat(4097)]) {
      const res = await mutate(request, 'POST', `/api/queues/${queue.id}/items`, { agent: 'claude', flags: '', instruction: '/goal e2e gates bad', verifyCommand: bad })
      expect(res.status(), bad).toBe(400)
      expect((await res.json() as { hint: string }).hint).toContain('sh -c')
    }

    // The gates alone are editable on a needs-attention item.
    await stubs.setBehavior('e2e gates failing', 'fail', 0.5)
    await stubs.setBehavior('e2e gates running', 'pending', 0.5)
    const failing = await addItem(request, queue.id, { instruction: '/goal e2e gates failing' })
    const running = await addItem(request, queue.id, { instruction: '/goal e2e gates running', verifyCommand: 'true' })
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, failing.id, 'needs_attention')
    const gatesOk = await mutate(request, 'PATCH', `/api/queue-items/${failing.id}`, { verifyCommand: 'make test', requiresApproval: true })
    expect(gatesOk.status(), await gatesOk.text()).toBe(200)
    expect(await gatesOk.json()).toMatchObject({ verifyCommand: 'make test', requiresApproval: true })
    const other = await mutate(request, 'PATCH', `/api/queue-items/${failing.id}`, { instruction: '/goal e2e gates changed' })
    expect(other.status()).toBe(409)
    expect((await other.json() as { error: string }).error).toContain('only its gates')

    // Every edit is refused while the item runs.
    expect((await mutate(request, 'POST', `/api/queue-items/${failing.id}/skip`, undefined)).status()).toBe(200)
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    await waitItem(request, queue.id, running.id, 'running')
    const refused = await mutate(request, 'PATCH', `/api/queue-items/${running.id}`, { verifyCommand: 'false' })
    expect(refused.status()).toBe(409)
    expect((await refused.json() as { error: string }).error).toContain('gates are fixed while the item is running')
    expect((await itemOf(request, queue.id, running.id)).verifyCommand).toBe('true')
    expect((await control(request, queue.id, 'pause')).status()).toBe(200)
  })
})
