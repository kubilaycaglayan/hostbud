import { forbidInLogs } from '../helpers/api.ts'
import { notifications } from '../helpers/db.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { putNotificationSettings } from '../helpers/notifications.ts'
import { newDevice, pushfake, received, subscribe } from '../helpers/push.ts'
import { addItem, control, createQueue, getQueue, itemOf, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M4 gates and notifications (Web Push to hostbud-e2e-pushfake): a
// failed verify and a pending approval notify as needs attention; the
// payload never carries the command or its output.

const stubs = new Stubs()
const ALLOWED = ['body', 'key', 'kind', 'outcome', 'position', 'project', 'title', 'url', 'v']

test.describe('gate notifications', () => {
  test.describe.configure({ timeout: 120_000 })

  test.beforeEach(async ({ target }) => {
    await target.resetTmux()
    await stubs.reset()
    await pushfake.reset()
    await notifications.reset()
  })

  test('(V2-M4 T2) Failed verify notifies', async ({ request, target }) => {
    const d = newDevice()
    expect((await subscribe(request, d)).status()).toBe(204)
    await putNotificationSettings(request, { enabled: true })
    const marker = `verifymark${Date.now()}`
    forbidInLogs(marker)
    const project = await newProject(request, target, 'e2e-gate-notify')
    const queue = await createQueue(request, project.id)
    await stubs.setBehavior('e2e gate notify verify', 'achieve:1', 0.5)
    const item = await addItem(request, queue.id, { instruction: '/goal e2e gate notify verify', verifyCommand: `sh -c 'echo ${marker}-output; exit 7' ${marker}-arg` })
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 60_000 }).toBe('paused')
    const run = (await itemOf(request, queue.id, item.id)).run!
    await expect.poll(async () => (await received(d)).length, { timeout: 15_000 }).toBe(1)
    const [payload] = await received(d)
    expect(Object.keys(payload).sort()).toEqual(ALLOWED)
    expect(payload).toMatchObject({ kind: 'attention', outcome: 'verify_failed', key: `run:${run.id}:verify:1`, position: 1 })
    const text = JSON.stringify(payload)
    expect(text).not.toContain(marker)
    expect(text).not.toContain('exit 7')
    expect(text).not.toContain(project.path)
    // No done notification for an item that didn't pass its gates.
    expect((await notifications.outbox()).map((o) => o.key)).not.toContain(`run:${run.id}:done`)
  })
})
