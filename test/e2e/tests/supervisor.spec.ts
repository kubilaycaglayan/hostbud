import { expect, test } from '../helpers/fixtures.ts'
import { LLM_URL } from '../helpers/api.ts'
import { LLM_STORAGE_STATE } from '../helpers/auth.ts'
import { addItem, control, createQueue, itemOf, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

test.use({ baseURL: LLM_URL, storageState: LLM_STORAGE_STATE })
const stubs = new Stubs()

test.describe('V2-M5 supervisor panel (desktop)', () => {
  test.skip(({ isMobile }) => isMobile, 'the phone variant is supervisor.phone.spec.ts')
  test.describe.configure({ timeout: 90_000 })
  test.beforeEach(async ({ target, request }) => {
    await target.resetTmux()
    await stubs.reset()
    await request.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  })

  test('(V2-M5 T5) Flag badge and plain-text reason', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-llm-panel')
    const queue = await createQueue(request, project.id)
    await stubs.setBehavior('e2e llm panel', 'quiet-print', 0.1)
    const item = await addItem(request, queue.id, { instruction: '/goal e2e llm panel' })
    await control(request, queue.id, 'start')
    await expect.poll(async () => (await itemOf(request, queue.id, item.id)).run?.flag?.label, { timeout: 40_000 }).toBe('waiting_input')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Queue' })
    const badge = dialog.getByTestId('llm-flag')
    await expect(badge).toContainText('Supervisor: waiting input')
    await expect(badge).toContainText('The pane appears to be waiting for input.')
    await expect(badge.locator('p')).toHaveCount(2)
  })
})
