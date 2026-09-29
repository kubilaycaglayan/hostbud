import { expect, test } from '../helpers/fixtures.ts'
import { LLM_URL } from '../helpers/api.ts'
import { LLM_STORAGE_STATE } from '../helpers/auth.ts'
import { addItem, control, createQueue, itemOf, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

test.use({ baseURL: LLM_URL, storageState: LLM_STORAGE_STATE, viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
const stubs = new Stubs()

test('V2-M5 T5 Flag badge (phone)', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
  test.setTimeout(90_000)
  await target.resetTmux()
  await stubs.reset()
  await request.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  const project = await newProject(request, target, 'e2e-llm-phone')
  const queue = await createQueue(request, project.id)
  await stubs.setBehavior('e2e llm phone', 'quiet-print', 0.1)
  const item = await addItem(request, queue.id, { instruction: '/goal e2e llm phone' })
  await control(request, queue.id, 'start')
  await expect.poll(async () => (await itemOf(request, queue.id, item.id)).run?.flag?.label, { timeout: 40_000 }).toBe('waiting_input')
  await ui.open()
  await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).tap()
  const badge = page.getByRole('dialog', { name: 'Queue' }).getByTestId('llm-flag')
  await expect(badge).toContainText('Supervisor: waiting input')
  await expect(badge).toContainText('The pane appears to be waiting for input.')
})
