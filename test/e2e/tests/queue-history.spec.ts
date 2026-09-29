import { expect, test } from '../helpers/fixtures.ts'
import { mutate } from '../helpers/api.ts'
import { queues as dbQueues } from '../helpers/db.ts'
import { addItem, createQueue, newProject } from '../helpers/queues.ts'

test('(V2-M9 T2/T3) queue history survives deletion and is visible in the History view', async ({ request, target, page, ui }) => {
  const marker = `e2e history ${Date.now()}`
  const project = await newProject(request, target, 'e2e-history')
  const queue = await createQueue(request, project.id, `History ${Date.now()}`)
  const item = await addItem(request, queue.id, { instruction: `/goal ${marker}` })
  const detail = `synthetic failure ${Date.now()}`
  await dbQueues.setItemFailure(item.id, detail)

  const deleted = await mutate(request, 'DELETE', `/api/queues/${queue.id}`, undefined)
  expect(deleted.status()).toBe(204)
  const historyResponse = await request.get('/api/queue-history?limit=200')
  expect(historyResponse.status()).toBe(200)
  const history = await historyResponse.json() as { items: { queueId: string; queueName: string; instruction: string; status: string; action: string }[] }
  expect(history.items.filter((entry) => entry.queueId === queue.id).map((entry) => entry.status)).toContain('needs_attention')
  expect(history.items.find((entry) => entry.queueId === queue.id)).toMatchObject({ queueName: queue.name, instruction: `/goal ${marker}`, detail })

  await ui.open()
  await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
  const panel = page.getByRole('dialog', { name: 'Queue' })
  await panel.getByRole('button', { name: 'History' }).click()
  const view = panel.getByTestId('queue-history')
  await expect(view.getByText(`/goal ${marker}`)).toBeVisible()
  await expect(view.getByText(/needs attention/).first()).toBeVisible()
  await expect(view.getByText(detail)).toBeVisible()
  await expect(view).not.toContainText('HISTORY_PRIVATE_OUTPUT_SENTINEL')
  expect(JSON.stringify(history)).not.toContain('HISTORY_PRIVATE_OUTPUT_SENTINEL')
})
