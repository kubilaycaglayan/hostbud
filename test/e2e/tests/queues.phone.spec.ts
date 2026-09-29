import type { Page } from '@playwright/test'
import { MULTI_URL } from '../helpers/api.ts'
import { MULTI_STORAGE_STATE } from '../helpers/auth.ts'
import { expect, test } from '../helpers/multi.ts'
import { newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M2 T5: several queues on the phone (hostbud-e2e-app-multi): the queue
// select, the shared-directory warning, the cap in Settings (a full-screen
// sheet) with "waiting for a free slot" clearing live, rename and a
// confirmed delete.

const stubs = new Stubs()

test.use({ baseURL: MULTI_URL, storageState: MULTI_STORAGE_STATE })

test.describe('Queues panel on iPhone 13 Pro (parallel queues)', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  test.describe.configure({ timeout: 150_000 })

  test.beforeEach(async ({ target }) => {
    await target.resetTmux()
    await stubs.reset()
  })

  const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
  const row = (page: Page, condition: string) => panel(page).getByRole('listitem', { name: new RegExp(`: /goal ${condition}$`) })
  // The label wraps the select, so its name continues with the selected option.
  const switcher = (page: Page) => panel(page).getByRole('combobox', { name: /^Queue\b(?! name)/ })

  async function addItem(page: Page, condition: string) {
    const form = panel(page).getByRole('form', { name: 'Add item' })
    await form.getByLabel('Instruction').fill(`/goal ${condition}`)
    await form.getByRole('button', { name: 'Add item' }).tap()
    await expect(row(page, condition)).toBeVisible()
  }

  async function setCap(page: Page, value: string) {
    await panel(page).getByRole('button', { name: 'Close queue panel' }).tap()
    await page.getByRole('button', { name: 'Account', exact: true }).tap()
    await page.getByRole('button', { name: 'Settings', exact: true }).tap()
    const settings = page.getByRole('dialog', { name: 'Settings' })
    expect((await settings.boundingBox())?.width).toBeGreaterThanOrEqual(389) // a full-screen sheet
    await expect(settings.getByLabel('Maximum parallel runs')).toBeEnabled()
    await settings.getByLabel('Maximum parallel runs').fill(value)
    await settings.getByRole('button', { name: 'Save' }).tap()
    // A changed cap asks through the in-app confirmation, never window.confirm.
    await page.getByRole('alertdialog', { name: /^Change the maximum parallel runs to / }).getByRole('button', { name: 'Change limit' }).tap()
    await expect(settings.getByRole('form', { name: 'Queue runs' }).getByRole('status')).toContainText(value ? `at most ${value}` : 'default limit')
    await settings.getByRole('button', { name: 'Close settings' }).tap()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).tap()
  }

  test('(V2-M2 T5, T9) Queues panel (phone) and confirmed cap settings', { tag: '@loopback' }, async ({ page, ui, multi, target }) => {
    page.on('dialog', (d) => { throw new Error(`unexpected native dialog: ${d.message()}`) })
    // The multi app has no domain site; iphone-13-pro covers the phone.
    const project = await newProject(multi, target, 'e2e-phone-multi')
    await stubs.setBehavior('e2e phone alpha', 'achieve:8', 1)
    await stubs.setBehavior('e2e phone beta', 'achieve:2', 0.5)
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).tap()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByLabel('Queue name').fill('Alpha')
    await panel(page).getByRole('button', { name: 'Create queue' }).tap()
    await addItem(page, 'e2e phone alpha')
    await panel(page).getByRole('button', { name: 'New queue' }).tap()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByLabel('Queue name').fill('Beta')
    await panel(page).getByRole('button', { name: 'Create queue' }).tap()
    await addItem(page, 'e2e phone beta')
    // The phone switcher is a select; no desktop button list.
    await expect(panel(page)).toBeVisible()
    await expect(panel(page).getByRole('navigation', { name: 'Queues' })).toHaveCount(0)
    const betaId = await switcher(page).locator('option:checked').getAttribute('value')
    const alphaId = await switcher(page).locator('option').first().getAttribute('value')

    await setCap(page, '1')
    await switcher(page).selectOption(alphaId!)
    await panel(page).getByRole('button', { name: 'Start' }).tap()
    await expect(row(page, 'e2e phone alpha').getByTestId('item-status')).toHaveText(/^Running/, { timeout: 15_000 })
    await switcher(page).selectOption(betaId)
    await panel(page).getByRole('button', { name: 'Start' }).tap()
    await expect(row(page, 'e2e phone beta').getByTestId('item-status')).toHaveText('Queued · waiting for a free slot')
    await expect(panel(page).getByTestId('queue-warning')).toContainText(`Queue Alpha also runs in ${project.path}`)

    await setCap(page, '')
    await switcher(page).selectOption(betaId)
    await expect(row(page, 'e2e phone beta').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    await switcher(page).selectOption(alphaId!)
    await expect(row(page, 'e2e phone alpha').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })

    await switcher(page).selectOption(betaId)
    await panel(page).getByRole('button', { name: 'Rename queue' }).tap()
    const rename = panel(page).getByRole('form', { name: 'Rename queue' })
    await rename.getByLabel('Queue name').fill('Beta docs')
    await rename.getByRole('button', { name: 'Save' }).tap()
    await expect(switcher(page).locator('option', { hasText: 'Beta docs' })).toHaveCount(1)

    await switcher(page).selectOption(alphaId!)
    await panel(page).getByRole('button', { name: 'Delete queue' }).tap()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText('Delete queue Alpha?')
    await confirm.getByRole('button', { name: 'Delete queue' }).tap()
    await expect(switcher(page).locator('option')).toHaveCount(1)
  })
})
