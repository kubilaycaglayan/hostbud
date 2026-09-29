import { expect, test } from '../helpers/fixtures.ts'
import { mutate, ORIGIN } from '../helpers/api.ts'
import { createQueue, getQueue, newProject } from '../helpers/queues.ts'
import { shq } from '../helpers/target.ts'

test('Queue attached to an existing non-queue session starts once that session is idle', async ({ request, target }) => {
  await target.resetTmux()
  const project = await newProject(request, target, 'e2e-attach')
  const manual = 'attach-manual'
  const sink = 'attach-sink'
  await target.run([
    'mkdir -p /home/dev/.hostbud-test-bin',
    'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/cly',
    `tmux new-session -d -s ${shq(manual)} -c ${shq(project.path)} /home/dev/.hostbud-test-bin/cly 120`,
    `tmux new-session -d -s ${shq(sink)} -c ${shq(project.path)}`,
  ].join(' && '))
  const pane = (await target.tmux('list-panes', '-t', `=${manual}:`, '-F', '#{pane_id}')).trim().split('\n')[0]
  await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'working')

  const queue = await createQueue(request, project.id, 'After manual', '', manual)
  expect(queue.afterSession).toBe(manual)
  const add = await mutate(request, 'POST', `/api/queues/${queue.id}/items`, {
    executionMode: 'session', targetSession: sink, command: "printf '%s\\n' attach-marker", agent: 'claude', flags: '', instruction: '',
  }, ORIGIN)
  expect(add.status(), await add.text()).toBe(201)
  const start = await mutate(request, 'POST', `/api/queues/${queue.id}/start`, {}, ORIGIN)
  expect(start.status(), await start.text()).toBe(200)

  // Several inventory polls pass while the manual session's agent works.
  await new Promise((resolve) => setTimeout(resolve, 4_000))
  expect((await getQueue(request, queue.id)).items[0].status).toBe('queued')

  await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'blocked')
  await expect.poll(async () => (await getQueue(request, queue.id)).items[0].status, { timeout: 15_000 }).toBe('done')
  expect(await target.capture(sink)).toContain('attach-marker')
  expect(await target.sessions()).toContain(manual)
})
