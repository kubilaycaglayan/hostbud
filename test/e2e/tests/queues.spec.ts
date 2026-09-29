import type { Page } from '@playwright/test'
import { MULTI_URL } from '../helpers/api.ts'
import { MULTI_STORAGE_STATE } from '../helpers/auth.ts'
import { expect, test } from '../helpers/multi.ts'
import { newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M2 T5: the Queue panel with several queues on hostbud-e2e-app-multi
// (HOSTBUD_PARALLEL_QUEUES=true): the switcher, the shared-directory
// warning, the cap in Settings with the waiting state clearing live, rename
// and a confirmed delete. Everything updates from /ws/events: no reload.

const stubs = new Stubs()

test.use({ baseURL: MULTI_URL, storageState: MULTI_STORAGE_STATE })

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
const row = (page: Page, condition: string) => panel(page).getByRole('listitem', { name: new RegExp(`: /goal ${condition}$`) })

async function addItem(page: Page, condition: string) {
  const form = panel(page).getByRole('form', { name: 'Add item' })
  await form.getByLabel('Instruction').fill(`/goal ${condition}`)
  await form.getByRole('button', { name: 'Add item' }).click()
  await expect(row(page, condition)).toBeVisible()
}

async function setCap(page: Page, value: string) {
  await panel(page).getByRole('button', { name: 'Close queue panel' }).click()
  await page.getByRole('button', { name: 'Account', exact: true }).click()
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  const settings = page.getByRole('dialog', { name: 'Settings' })
  await expect(settings.getByLabel('Maximum parallel runs')).toBeEnabled()
  await settings.getByLabel('Maximum parallel runs').fill(value)
  await settings.getByRole('button', { name: 'Save' }).click()
  await expect(settings.getByRole('form', { name: 'Queue runs' }).getByRole('status')).toContainText(value ? `at most ${value}` : 'default limit')
  await settings.getByRole('button', { name: 'Close settings' }).click()
  await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
}

test.describe('Queues panel (desktop, parallel queues)', () => {
  test.describe.configure({ timeout: 150_000 })
  test.skip(({ isMobile }) => isMobile, 'the phone variant is queues.phone.spec.ts')

  test('(V2-M2 T5, T9) Queues panel and confirmed cap settings', async ({ page, ui, multi, target }) => {
    page.on('dialog', dialog => dialog.accept())
    const project = await newProject(multi, target, 'e2e-panel-multi')
    await stubs.setBehavior('e2e multi alpha', 'achieve:8', 1)
    await stubs.setBehavior('e2e multi beta', 'achieve:2', 0.5)
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByLabel('Queue name').fill('Alpha')
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    await addItem(page, 'e2e multi alpha')
    // A second queue on the same project.
    await panel(page).getByRole('button', { name: 'New queue' }).click()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByLabel('Queue name').fill('Beta')
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    await expect(panel(page).getByRole('button', { name: 'Show queue Beta' })).toHaveAttribute('aria-current', 'true')
    await addItem(page, 'e2e multi beta')
    await expect(panel(page).getByRole('navigation', { name: 'Queues' }).getByRole('button')).toHaveCount(2)

    // Cap 1 in Settings, then start both: Beta waits for a free slot.
    await setCap(page, '1')
    await panel(page).getByRole('button', { name: 'Show queue Alpha' }).click()
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(row(page, 'e2e multi alpha').getByTestId('item-status')).toHaveText(/^Running/, { timeout: 15_000 })
    // The running queue's tab has a green border; the idle one doesn't.
    await expect(panel(page).getByRole('button', { name: 'Show queue Alpha' })).toHaveAttribute('data-running', 'true')
    await expect(panel(page).getByRole('button', { name: 'Show queue Beta' })).not.toHaveAttribute('data-running', 'true')
    await panel(page).getByRole('button', { name: 'Show queue Beta' }).click()
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(row(page, 'e2e multi beta').getByTestId('item-status')).toHaveText('Queued · waiting for a free slot')
    // Both queues are busy in one directory: the warning shows on both.
    await expect(panel(page).getByTestId('queue-warning')).toContainText(`Queue Alpha also runs in ${project.path}`)
    await panel(page).getByRole('button', { name: 'Show queue Alpha' }).click()
    await expect(panel(page).getByTestId('queue-warning')).toContainText(`Queue Beta also runs in ${project.path}`)

    // Clearing the cap in Settings starts Beta at once; the panel follows live.
    await setCap(page, '')
    await panel(page).getByRole('button', { name: 'Show queue Beta' }).click()
    await expect(row(page, 'e2e multi beta').getByTestId('item-status')).toHaveText(/^(Running|Done)/, { timeout: 15_000 })
    await expect(row(page, 'e2e multi beta').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    await panel(page).getByRole('button', { name: 'Show queue Alpha' }).click()
    await expect(row(page, 'e2e multi alpha').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    await expect(panel(page).getByTestId('queue-warning')).toHaveCount(0)

    // Rename Beta.
    await panel(page).getByRole('button', { name: 'Show queue Beta' }).click()
    await panel(page).getByRole('button', { name: 'Rename queue' }).click()
    const rename = panel(page).getByRole('form', { name: 'Rename queue' })
    await rename.getByLabel('Queue name').fill('Beta docs')
    await rename.getByRole('button', { name: 'Save' }).click()
    await expect(panel(page).getByRole('button', { name: 'Show queue Beta docs' })).toBeVisible()

    // Delete Alpha, confirmed.
    await panel(page).getByRole('button', { name: 'Show queue Alpha' }).click()
    await panel(page).getByRole('button', { name: 'Delete queue' }).click()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText('Delete queue Alpha?')
    await confirm.getByRole('button', { name: 'Delete queue' }).click()
    await expect(panel(page).getByRole('navigation', { name: 'Queues' }).getByRole('button')).toHaveCount(1)
    await expect(panel(page).getByRole('button', { name: 'Show queue Beta docs' })).toHaveAttribute('aria-current', 'true')
  })
})
