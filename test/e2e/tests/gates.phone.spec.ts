import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M4 T5: completion gates in the Queue panel on the phone — touch-sized
// gate fields, the gate state and output, Approve, and Reject's
// confirmation sheet.

const stubs = new Stubs()

test.describe('Completion gates on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  test.describe.configure({ timeout: 120_000 })

  test.beforeEach(async ({ target }) => {
    await target.resetTmux()
    await stubs.reset()
  })

  const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
  const row = (page: Page, condition: string) => panel(page).getByRole('listitem', { name: new RegExp(`: /goal ${condition}$`) })

  test('(V2-M4 T5) Gates panel', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-gates')
    await stubs.setBehavior('e2e phone gates first', 'achieve:1', 0.5)
    await stubs.setBehavior('e2e phone gates second', 'achieve:1', 0.5)
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).tap()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).tap()
    const form = panel(page).getByRole('form', { name: 'Add item' })
    for (const [condition, approval] of [['e2e phone gates first', true], ['e2e phone gates second', true]] as const) {
      await form.getByLabel('Instruction').fill(`/goal ${condition}`)
      const verify = form.getByLabel('Verify command')
      const box = await verify.boundingBox()
      expect(box?.height).toBeGreaterThanOrEqual(44)
      await verify.fill('true')
      if (approval) await form.getByLabel('Require approval').check()
      await form.getByRole('button', { name: 'Add item' }).tap()
      await expect(row(page, condition)).toBeVisible()
    }
    await panel(page).getByRole('button', { name: 'Start' }).tap()
    const first = row(page, 'e2e phone gates first')
    await expect(first.getByTestId('item-status')).toHaveText(/Awaiting approval/, { timeout: 30_000 })
    await expect(first).toContainText(/Verify attempt 1: passed/)
    await first.getByRole('button', { name: 'Approve item 1' }).tap()
    await expect(first.getByTestId('item-status')).toHaveText(/^Done/)

    const second = row(page, 'e2e phone gates second')
    await expect(second.getByTestId('item-status')).toHaveText(/Awaiting approval/, { timeout: 30_000 })
    await second.getByRole('button', { name: 'Reject item 2' }).tap()
    const sheet = page.getByRole('alertdialog')
    await expect(sheet).toContainText('Reject item 2?')
    await sheet.getByRole('button', { name: 'Reject' }).tap()
    await expect(second.getByTestId('item-status')).toHaveText(/Needs attention/)
    await expect(second).toContainText(/rejected by /)
    await expect(panel(page).getByTestId('queue-status')).toHaveText('paused')
    // Gates only on a needs-attention item.
    await second.getByRole('button', { name: 'Edit gates of item 2' }).tap()
    const gates = panel(page).getByRole('form', { name: 'Edit gates of item 2' })
    await expect(gates.getByLabel('Instruction')).toHaveCount(0)
    await gates.getByLabel('Require approval').uncheck()
    await gates.getByRole('button', { name: 'Save' }).tap()
    await expect(second).not.toContainText('requires approval')
    await expect(second.getByRole('button', { name: 'Re-run verify of item 2' })).toBeVisible()
    await second.getByRole('button', { name: 'Re-run verify of item 2' }).tap()
    await expect(second.getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    const scroll = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(scroll).toBeLessThanOrEqual(0)
  })
})
