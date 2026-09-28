import { expect, test } from '../helpers/fixtures.ts'
import { forbidInLogs, mutate } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { queues } from '../helpers/db.ts'
import { addItem, control, createQueue, gateAction, getQueue, itemOf, newProject, type QueueItem } from '../helpers/queues.ts'
import { Stubs, type StubBehavior } from '../helpers/stubs.ts'
import { shq, type Target } from '../helpers/target.ts'

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

type APIRequest = Parameters<typeof itemOf>[0]
type Gated = { condition: string; behavior?: StubBehavior; verifyCommand?: string; requiresApproval?: boolean }

/** A queue in a fresh project with the given items (stub behaviors set). */
async function gatedQueue(request: APIRequest, target: Target, prefix: string, items: Gated[]) {
  const project = await newProject(request, target, prefix)
  const queue = await createQueue(request, project.id)
  const added: QueueItem[] = []
  for (const it of items) {
    await stubs.setBehavior(it.condition, it.behavior ?? 'achieve:1', 0.5)
    added.push(await addItem(request, queue.id, { instruction: `/goal ${it.condition}`, verifyCommand: it.verifyCommand, requiresApproval: it.requiresApproval }))
  }
  return { project, queue, items: added }
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

  test('(V2-M4 T2) Verify fails then passes', async ({ request, target }) => {
    const { project, queue, items } = await gatedQueue(request, target, 'e2e-verify', [
      { condition: 'e2e verify flag', verifyCommand: 'test -f done.flag' },
      { condition: 'e2e verify next' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const failed = await waitItem(request, queue.id, items[0].id, 'needs_attention')
    expect(failed.run?.status).toBe('achieved')
    expect(failed.run?.detail).toBe('verify failed (exit 1)')
    expect(failed.verify).toMatchObject({ attempt: 1, running: false, outcome: 'failed', exitCode: 1 })
    expect((await getQueue(request, queue.id)).status).toBe('paused')
    expect((await itemOf(request, queue.id, items[1].id)).status).toBe('queued')

    // The runner makes the check pass on the target; Re-run verify (no new
    // agent run) marks the item done. Owner actions keep the queue paused.
    await target.run(`touch ${shq(`${project.path}/done.flag`)}`)
    expect((await gateAction(request, items[0].id, 'reverify')).status()).toBe(200)
    const done = await waitItem(request, queue.id, items[0].id, 'done')
    expect(done.run?.id).toBe(failed.run?.id)
    expect(done.verify).toMatchObject({ attempt: 2, outcome: 'passed', exitCode: 0 })
    expect((await getQueue(request, queue.id)).status).toBe('paused')
    expect((await itemOf(request, queue.id, items[1].id)).status).toBe('queued')
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    await waitItem(request, queue.id, items[1].id, 'done')
  })

  test('(V2-M4 T2) Verify timeout', async ({ request, target }) => {
    // The e2e app's HOSTBUD_VERIFY_TIMEOUT is 5s.
    const { queue, items } = await gatedQueue(request, target, 'e2e-verify-timeout', [
      { condition: 'e2e verify timeout', verifyCommand: `sh -c 'echo waiting; sleep 47.5'` },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'verifying')
    const item = await waitItem(request, queue.id, items[0].id, 'needs_attention', 45_000)
    expect(item.run?.detail).toBe('verify timed out after 5s')
    expect(item.verify).toMatchObject({ outcome: 'timeout', output: 'waiting\n' })
    expect((await getQueue(request, queue.id)).status).toBe('paused')
    const left = await target.exec(`pgrep -f 'sleep 4[7].5' || true`)
    expect(left.stdout.trim()).toBe('')
  })

  test('(V2-M4 T2) Verify quoting', async ({ request, target }) => {
    const { project, queue, items } = await gatedQueue(request, target, 'e2e-verify-quoting', [
      { condition: 'e2e verify quoting', verifyCommand: `touch 'x; touch pwned' '$(touch pwned2)'` },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'done')
    const listing = (await target.run(`ls -1A ${shq(project.path)}`)).split('\n').filter(Boolean).sort()
    expect(listing).toEqual(expect.arrayContaining(['$(touch pwned2)', 'x; touch pwned']))
    expect(listing).not.toContain('pwned')
    expect(listing).not.toContain('pwned2')
  })

  test('(V2-M4 T2) Verify output capped', async ({ request, target }) => {
    const marker = `verifyout${Date.now()}`
    forbidInLogs(marker)
    const { queue, items } = await gatedQueue(request, target, 'e2e-verify-output', [
      { condition: 'e2e verify output', verifyCommand: `sh -c 'echo ${marker}; head -c 5000000 /dev/urandom; printf "\\033[31mred\\033[0m end"'` },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const item = await waitItem(request, queue.id, items[0].id, 'done')
    expect(item.verify?.truncated).toBe(true)
    const output = item.verify?.output ?? ''
    expect(Buffer.byteLength(output, 'utf8')).toBeLessThanOrEqual(16 * 1024)
    expect(output.endsWith('red end')).toBe(true)
    // eslint-disable-next-line no-control-regex
    expect(output).not.toMatch(/[\x00-\x08\x0b-\x1f\x7f]/)
    expect(output).not.toContain(marker) // the head was cut; only the tail is kept
  })

  test('(V2-M4 T2) Missing project directory', async ({ request, target }) => {
    const { project, queue, items } = await gatedQueue(request, target, 'e2e-verify-missing', [
      { condition: 'e2e verify missing', behavior: 'achieve:4', verifyCommand: 'true' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'running')
    await target.run(`rm -rf ${shq(project.path)}`)
    const item = await waitItem(request, queue.id, items[0].id, 'needs_attention', 45_000)
    expect(item.run?.detail).toBe('project directory is missing on the host — restore it, then Re-run verify')
    expect(item.verify?.outcome).toBe('missing_directory')
    expect((await getQueue(request, queue.id)).status).toBe('paused')
  })

  test('(V2-M4 T2) Restart during verify', async ({ request, target }) => {
    const { project, queue, items } = await gatedQueue(request, target, 'e2e-verify-restart', [
      { condition: 'e2e verify restart', verifyCommand: `sh -c 'echo x >> count; sleep 3'` },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'verifying')
    await expect.poll(async () => (await target.exec(`cat ${shq(`${project.path}/count`)}`)).stdout).toContain('x')
    await ctl.appRestart()
    const item = await waitItem(request, queue.id, items[0].id, 'needs_attention')
    expect(item.run?.detail).toBe('hostbud restarted during verify — Re-run verify')
    expect(item.verify?.outcome).toBe('interrupted')
    await new Promise((r) => setTimeout(r, 4_000))
    expect((await target.run(`cat ${shq(`${project.path}/count`)}`)).trim()).toBe('x')
    const events = await queues.events(item.run!.id)
    expect(events.filter((e) => e.source === 'verify' && e.kind === 'verify_started')).toHaveLength(1)
  })

  test('(V2-M4 T3) Approval', async ({ request, target }) => {
    const { queue, items } = await gatedQueue(request, target, 'e2e-approval', [
      { condition: 'e2e approval first', requiresApproval: true },
      { condition: 'e2e approval second', requiresApproval: true },
      { condition: 'e2e approval third' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    // The queue waits (running, not paused); the next item doesn't start.
    const waiting = await waitItem(request, queue.id, items[0].id, 'awaiting_approval')
    expect(waiting.run?.status).toBe('achieved')
    expect((await getQueue(request, queue.id)).status).toBe('running')
    await new Promise((r) => setTimeout(r, 2_000))
    expect((await itemOf(request, queue.id, items[1].id)).status).toBe('queued')

    // Approve ⇒ done and the queue advances.
    const approved = await gateAction(request, items[0].id, 'approve')
    expect(approved.status(), await approved.text()).toBe(200)
    expect((await itemOf(request, queue.id, items[0].id)).status).toBe('done')
    await waitItem(request, queue.id, items[1].id, 'awaiting_approval')

    // Reject ⇒ needs attention; the queue pauses.
    const rejected = await gateAction(request, items[1].id, 'reject')
    expect(rejected.status(), await rejected.text()).toBe(200)
    const item = await itemOf(request, queue.id, items[1].id)
    expect(item.status).toBe('needs_attention')
    expect(item.run?.detail).toMatch(/^rejected by /)
    expect((await getQueue(request, queue.id)).status).toBe('paused')
    expect((await itemOf(request, queue.id, items[2].id)).status).toBe('queued')
    const events = await queues.events(item.run!.id)
    expect(events.filter((e) => e.source === 'user' && e.kind === 'rejected')).toHaveLength(1)
    expect(events.find((e) => e.kind === 'rejected')!.payload_json).not.toContain('@')

    // Only from awaiting approval.
    const late = await gateAction(request, items[0].id, 'reject')
    expect(late.status()).toBe(409)
    expect((await late.json() as { error: string }).error).toContain('this item is done')
  })

  test('(V2-M4 T3) Approve and reject race', async ({ request, target }) => {
    const { queue, items } = await gatedQueue(request, target, 'e2e-approval-race', [
      { condition: 'e2e approval race', requiresApproval: true },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'awaiting_approval')
    const [a, r] = await Promise.all([gateAction(request, items[0].id, 'approve'), gateAction(request, items[0].id, 'reject')])
    expect([a.status(), r.status()].sort()).toEqual([200, 409])
    const loser = a.status() === 409 ? a : r
    expect((await loser.json() as { error: string }).error).toMatch(/this item is (done|waiting for your attention)/)
    const item = await itemOf(request, queue.id, items[0].id)
    const events = await queues.events(item.run!.id)
    expect(events.filter((e) => e.source === 'user' && (e.kind === 'approved' || e.kind === 'rejected'))).toHaveLength(1)
  })

  test('(V2-M4 T3) Restart while awaiting approval', async ({ request, target }) => {
    const { queue, items } = await gatedQueue(request, target, 'e2e-approval-restart', [
      { condition: 'e2e approval restart', requiresApproval: true },
      { condition: 'e2e approval restart next' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'awaiting_approval')
    await ctl.appRestart()
    expect((await itemOf(request, queue.id, items[0].id)).status).toBe('awaiting_approval')
    expect((await getQueue(request, queue.id)).status).toBe('running')
    expect((await itemOf(request, queue.id, items[1].id)).status).toBe('queued')
    expect((await gateAction(request, items[0].id, 'approve')).status()).toBe(200)
    await waitItem(request, queue.id, items[1].id, 'done')
  })
})
