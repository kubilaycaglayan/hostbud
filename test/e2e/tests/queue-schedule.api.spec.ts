import { expect, test } from '../helpers/fixtures.ts'
import { mutate, ORIGIN } from '../helpers/api.ts'
import { createQueue, getQueue, newProject, type Queue } from '../helpers/queues.ts'
import { shq } from '../helpers/target.ts'

test('(V2-M8 T2/T3) Delayed start dispatches a command to an existing session', async ({ request, target }) => {
  await target.resetTmux()
  const project = await newProject(request, target, 'e2e-scheduled')
  const session = 'schedule-target'
  const markerFile = `/home/dev/${project.name}-should-not-exist`
  await target.run(`tmux new-session -d -s ${shq(session)} -c ${shq(project.path)}`)
  const queue = await createQueue(request, project.id)
  const command = `printf '%s\\n' 'schedule-marker; touch ${markerFile}'`
  const add = await mutate(request, 'POST', `/api/queues/${queue.id}/items`, {
    executionMode: 'session', targetSession: session, command, agent: 'claude', flags: '', instruction: '',
  }, ORIGIN)
  expect(add.status(), await add.text()).toBe(201)

  const start = await mutate(request, 'POST', `/api/queues/${queue.id}/start`, { delay: '2s' }, ORIGIN)
  expect(start.status(), await start.text()).toBe(200)
  expect((await start.json() as Queue).scheduledAt).toBeTruthy()
  await expect.poll(async () => (await getQueue(request, queue.id)).items[0].status).toBe('queued')
  await expect.poll(async () => (await getQueue(request, queue.id)).items[0].status, { timeout: 10_000 }).toBe('done')
  expect(await target.capture(session)).toContain('schedule-marker; touch')
  expect((await target.exec(`test -e ${shq(markerFile)}`)).code).not.toBe(0)
  expect(await target.sessions()).toContain(session)
})
