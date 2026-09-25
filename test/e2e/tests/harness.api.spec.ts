import { expect, test } from '../helpers/fixtures.ts'
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
