import { expect, test } from '../helpers/fixtures.ts'
import { listQueues, newProject, createQueue } from '../helpers/queues.ts'
import { uniqueName } from '../helpers/target.ts'

// Harness smoke (T5): the runner can act as a real terminal on the target.
test('harness smoke: target.tmux creates, lists and kills; capture reads the pane', async ({
  target,
}) => {
  const name = uniqueName('e2e-smoke')
  const marker = uniqueName('marker')

  await target.tmux('new-session', '-d', '-s', name, '-x', '120', '-y', '30')
  expect(await target.sessions()).toContain(name)

  await target.tmux('send-keys', '-t', `=${name}:`, `echo ${marker}`, 'Enter')
  await expect.poll(() => target.capture(name)).toContain(marker)

  await target.tmux('kill-session', '-t', `=${name}`)
  expect(await target.sessions()).not.toContain(name)
})

// Regression: API-only tests must run the same state reset as browser tests.
test('request-only scenario can leave a queue for the next scenario to reset', async ({ request, target }) => {
  const project = await newProject(request, target, uniqueName('e2e-request-isolation'))
  await createQueue(request, project.id)
  expect(await listQueues(request)).toHaveLength(1)
})

test('request-only scenario starts with no queue left by the previous scenario', async ({ request }) => {
  expect(await listQueues(request)).toHaveLength(0)
})
