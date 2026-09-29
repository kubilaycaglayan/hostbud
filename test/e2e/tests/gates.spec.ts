import type { Page } from '@playwright/test'
import { STORAGE_STATE } from '../helpers/auth.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { gateAction, listQueues, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M4 T5: completion gates in the Queue panel on desktop (and a phone
// as the second device), with stub clients on the throwaway target.
// Everything updates from /ws/events: no reload.

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
const row = (page: Page, condition: string) => panel(page).getByRole('listitem', { name: new RegExp(`: /goal ${condition}$`) })

async function createQueue(page: Page, projectId: string) {
  await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
  await panel(page).getByLabel('Project').selectOption(projectId)
  await panel(page).getByRole('button', { name: 'Create queue' }).click()
}

async function addGatedItem(page: Page, condition: string, gates: { verify?: string; approval?: boolean }) {
  const form = panel(page).getByRole('form', { name: 'Add item' })
  await form.getByLabel('Instruction').fill(`/goal ${condition}`)
  const verify = form.getByLabel('Verify command')
  await expect(verify).toHaveAttribute('autocomplete', 'off')
  await expect(verify).toHaveClass(/font-mono/)
  if (gates.verify) await verify.fill(gates.verify)
  if (gates.approval) await form.getByLabel('Require approval').check()
  await form.getByRole('button', { name: 'Add item' }).click()
  await expect(row(page, condition)).toBeVisible()
}

test.describe('Completion gates (desktop)', () => {
  test.describe.configure({ timeout: 120_000 })
  test.skip(({ isMobile }) => isMobile, 'the phone variant is gates.phone.spec.ts')

  test('(V2-M4 T5) Gates panel', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-gates-panel')
    await stubs.setBehavior('e2e gates panel first', 'achieve:1', 0.5)
    await stubs.setBehavior('e2e gates panel second', 'achieve:1', 0.5)
    await ui.open()
    await createQueue(page, project.id)
    // Invalid verify command: shown before sending.
    const form = panel(page).getByRole('form', { name: 'Add item' })
    await form.getByLabel('Instruction').fill('/goal e2e gates panel bad')
    await form.getByLabel('Verify command').fill(`make 'x`)
    await form.getByRole('button', { name: 'Add item' }).click()
    await expect(form).toContainText('The verify command has an unbalanced single quote.')
    await form.getByLabel('Verify command').fill('')

    await addGatedItem(page, 'e2e gates panel first', { verify: `sh -c 'printf "%s\\n" "checking <b>x</b>"; sleep 2'`, approval: true })
    await addGatedItem(page, 'e2e gates panel second', {})
    const first = row(page, 'e2e gates panel first')
    await expect(first).toContainText(`verify sh -c 'printf "%s\\n" "checking <b>x</b>"; sleep 2' · requires approval`)
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(first.getByTestId('item-status')).toHaveText(/Verifying/, { timeout: 30_000 })
    await expect(first).toContainText(/Verify attempt 1: running/)
    await expect(first.getByTestId('item-status')).toHaveText(/Awaiting approval/, { timeout: 30_000 })
    await expect(first).toContainText(/Verify attempt 1: passed · exit 0/)
    await first.getByText('Output').click()
    await expect(first.getByTestId('verify-output')).toHaveText('checking <b>x</b>\n')
    await expect(first.getByTestId('verify-output').locator('b')).toHaveCount(0)
    await expect(row(page, 'e2e gates panel second').getByTestId('item-status')).toHaveText('Queued')
    for (const name of ['Retry item 1', 'Mark item 1 done', 'Edit item 1']) await expect(first.getByRole('button', { name })).toHaveCount(0)
    await first.getByRole('button', { name: 'Approve item 1' }).click()
    await expect(first.getByTestId('item-status')).toHaveText(/^Done/)
    await expect(row(page, 'e2e gates panel second').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
  })

  test('(V2-M4 T5) Two devices', async ({ page, ui, request, target, browser, baseURL }) => {
    const project = await newProject(request, target, 'e2e-gates-devices')
    await stubs.setBehavior('e2e gates devices', 'achieve:1', 0.5)
    await ui.open()
    await createQueue(page, project.id)
    await addGatedItem(page, 'e2e gates devices', { approval: true })
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(row(page, 'e2e gates devices').getByTestId('item-status')).toHaveText(/Awaiting approval/, { timeout: 30_000 })

    const phone = await browser.newContext({ baseURL, storageState: STORAGE_STATE, viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
    try {
      const other = await phone.newPage()
      await other.goto('/')
      await other.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).tap()
      const phoneRow = row(other, 'e2e gates devices')
      await expect(phoneRow.getByRole('button', { name: 'Approve item 1' })).toBeVisible()

      // The desktop approves: the phone's buttons go without a reload.
      await row(page, 'e2e gates devices').getByRole('button', { name: 'Approve item 1' }).click()
      await expect(phoneRow.getByTestId('item-status')).toHaveText(/^Done/)
      await expect(phoneRow.getByRole('button', { name: 'Approve item 1' })).toHaveCount(0)

      // A stale click (the request a device sends from an old view) gets the
      // 409 naming the state and who acted.
      const [queue] = await listQueues(request)
      const stale = await gateAction(request, queue.items[0].id, 'reject')
      expect(stale.status()).toBe(409)
      expect((await stale.json() as { error: string }).error).toMatch(/this item is done; Reject is only for items awaiting approval \(approved by .+\)/)
    } finally {
      await phone.close()
    }
  })
})
