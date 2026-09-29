import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newProject, pickDuration } from '../helpers/queues.ts'
import { shq } from '../helpers/target.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M1 T11: the Queue panel on the phone — a full-screen sheet, touch-sized
// controls, move buttons instead of drag, confirmation sheets, and "Open
// session" switching to the single-terminal view.

const stubs = new Stubs()

test.describe('Queue panel on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  test.describe.configure({ timeout: 120_000 })

  test.beforeEach(async ({ target }) => {
    await target.resetTmux()
    await stubs.reset()
  })

  test('(V2-M8 T3) Schedule a command for an existing session on phone', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-scheduled')
    await target.run(`tmux new-session -d -s phone-schedule-target -c ${shq(project.path)}`)
    await ui.open()
    await openPanelAndCreate(page, project.id)
    const dialog = panel(page)
    const form = dialog.getByRole('form', { name: 'Add item' })
    await form.getByLabel('Execution').selectOption('session')
    await expect.poll(async () => form.getByRole('combobox', { name: /^Existing session/ }).locator('option').count()).toBeGreaterThan(1)
    await form.getByRole('combobox', { name: /^Existing session/ }).selectOption('phone-schedule-target')
    await form.getByRole('textbox', { name: /^Command/ }).fill("echo 'scheduled phone command'")
    await form.getByRole('button', { name: 'Add item' }).tap()
    // The picker's smallest delay is one minute.
    const delay = dialog.getByRole('group', { name: 'Start delay' })
    await expect(delay.getByRole('textbox')).toHaveCount(0)
    await pickDuration(delay, 'Minutes', 1, true)
    await dialog.getByRole('button', { name: 'Start' }).tap()
    await expect(dialog.getByTestId('queue-scheduled')).toContainText('Scheduled for')
    await expect.poll(async () => target.capture('phone-schedule-target'), { timeout: 90_000 }).toContain('scheduled phone command')
    await expect(dialog.getByTestId('item-status')).toHaveText('Done')
    await noHorizontalScroll(page)
  })

  test('Loop a queue on phone; Pause cancels the pending pass', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-loop')
    await target.run(`tmux new-session -d -s phone-loop-target -c ${shq(project.path)}`)
    await ui.open()
    await openPanelAndCreate(page, project.id)
    const dialog = panel(page)
    const form = dialog.getByRole('form', { name: 'Add item' })
    await form.getByLabel('Execution').selectOption('session')
    await expect.poll(async () => form.getByRole('combobox', { name: /^Existing session/ }).locator('option').count()).toBeGreaterThan(1)
    await form.getByRole('combobox', { name: /^Existing session/ }).selectOption('phone-loop-target')
    await form.getByRole('textbox', { name: /^Command/ }).fill("echo 'phone loop command'")
    await form.getByRole('button', { name: 'Add item' }).tap()
    const loop = dialog.getByRole('form', { name: 'Loop queue' })
    await pickDuration(loop.getByRole('group', { name: 'Loop runtime limit' }), 'Hours', 2, true)
    await loop.getByLabel('Loop the queue').tap()
    await expect(loop.getByLabel('Loop the queue')).toBeChecked()
    await dialog.getByRole('button', { name: 'Start' }).tap()
    await expect.poll(async () => target.capture('phone-loop-target'), { timeout: 10_000 }).toContain('phone loop command')
    await expect(loop.getByTestId('queue-loop-status')).toContainText('Pass 2')
    await expect(dialog.getByTestId('queue-scheduled')).toContainText('Scheduled for')
    await dialog.getByRole('button', { name: 'Pause' }).tap()
    await expect(dialog.getByTestId('queue-scheduled')).toHaveCount(0)
    await expect(dialog.getByTestId('queue-status')).toHaveText('paused')
    await noHorizontalScroll(page)
  })

  const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
  const row = (page: Page, condition: string) => panel(page).getByRole('listitem', { name: new RegExp(`: /goal ${condition}$`) })

  test('(V2-M1 T16) Permission flags are quick to select and default by agent', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-queue-permission-flags')
    await ui.open()
    await openPanelAndCreate(page, project.id)
    const form = panel(page).getByRole('form', { name: 'Add item' })
    const flags = form.getByLabel('Flags')
    await expect(flags).toHaveValue('--dangerously-skip-permissions')
    await expect(form.getByLabel('Skip permission prompts')).toBeChecked()
    await flags.fill('--')
    await expect(flags).toHaveValue('--')
    await flags.fill('')
    await form.getByRole('combobox', { name: /^Agent\b/ }).selectOption('codex')
    await expect(flags).toHaveValue('--yolo')
    await expect(form.getByLabel('YOLO mode')).toBeChecked()
    await form.getByLabel('Instruction').fill('/goal phone permission mode defaults')
    await form.getByRole('button', { name: 'Add item' }).tap()
    await expect(row(page, 'phone permission mode defaults')).toContainText('--yolo')
    await noHorizontalScroll(page)
  })

  async function openPanelAndCreate(page: Page, projectId: string) {
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).tap()
    await expect(panel(page)).toBeVisible()
    const box = await panel(page).boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(389) // a full-screen sheet
    await panel(page).getByLabel('Project').selectOption(projectId)
    await panel(page).getByRole('button', { name: 'Create queue' }).tap()
  }

  async function addItem(page: Page, condition: string) {
    const form = panel(page).getByRole('form', { name: 'Add item' })
    await form.getByLabel('Instruction').fill(`/goal ${condition}`)
    await form.getByRole('button', { name: 'Add item' }).tap()
    await expect(row(page, condition)).toBeVisible()
  }

  async function noHorizontalScroll(page: Page) {
    const overflow = await panel(page).evaluate((el) => [...el.querySelectorAll('*')].some((c) => (c as HTMLElement).scrollWidth > (c as HTMLElement).clientWidth + 1 && getComputedStyle(c).overflowX !== 'hidden' && getComputedStyle(c).overflowX !== 'visible'))
    expect(overflow).toBe(false)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  }

  test('(V2-M1 T15) Compact queue forms and touch-sized actions on phone', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-queue-compact')
    await ui.open()
    await openPanelAndCreate(page, project.id)
    const dialog = panel(page)
    const form = dialog.getByRole('form', { name: 'Add item' })
    const instruction = form.getByLabel('Instruction')
    await expect(instruction).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(instruction).toHaveCSS('resize', 'vertical')
    await instruction.fill('/goal phone compact form')
    await form.getByRole('button', { name: 'Add item' }).tap()
    const item = row(page, 'phone compact form')
    await expect(item).toBeVisible()
    for (const label of [/^Move item .* up$/, /^Edit item/, /^Delete item/]) {
      const targetBox = await item.getByRole('button', { name: label }).boundingBox()
      expect(targetBox!.width).toBeGreaterThanOrEqual(44)
      expect(targetBox!.height).toBeGreaterThanOrEqual(44)
    }
    await item.getByRole('button', { name: /^Edit item/ }).tap()
    const edit = dialog.getByRole('form', { name: /^Edit item/ })
    const editInstruction = edit.getByLabel('Instruction')
    await expect(editInstruction).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(editInstruction).toHaveCSS('resize', 'vertical')
    await editInstruction.fill('/goal phone edit saved')
    await edit.getByRole('button', { name: 'Save' }).tap()
    await expect(dialog.getByRole('listitem', { name: /: \/goal phone edit saved$/ })).toBeVisible()
    await noHorizontalScroll(page)
  })

  test('(V2-M1 T11) Queue panel (phone): live hand-off to finished', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-queue')
    await stubs.setBehavior('e2e phone one', 'achieve:1')
    await stubs.setBehavior('e2e phone two', 'achieve:1')
    await ui.open()
    await openPanelAndCreate(page, project.id)
    await addItem(page, 'e2e phone two')
    await addItem(page, 'e2e phone one')
    // No drag handles on the phone: move buttons, at least 44 px.
    await expect(panel(page).getByRole('button', { name: /^Drag to reorder/ })).toHaveCount(0)
    const up = row(page, 'e2e phone one').getByRole('button', { name: /^Move item \d+ up$/ })
    const size = await up.boundingBox()
    expect(size!.height).toBeGreaterThanOrEqual(44)
    expect(size!.width).toBeGreaterThanOrEqual(44)
    await up.tap()
    await expect(panel(page).getByRole('listitem').first()).toHaveAccessibleName(/e2e phone one$/)
    await noHorizontalScroll(page)

    await panel(page).getByRole('button', { name: 'Start' }).tap()
    await expect(row(page, 'e2e phone one').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    await expect(row(page, 'e2e phone two').getByTestId('item-status')).toHaveText(/^(Running|Done)/, { timeout: 30_000 })
    await expect(row(page, 'e2e phone two').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    await expect(panel(page).getByTestId('queue-status')).toHaveText('finished')

    // Open a run's session: the sheet closes and the single-terminal view shows it.
    const session = `${project.name}-q1`
    await row(page, 'e2e phone one').getByRole('button', { name: /^Open session of item/ }).tap()
    await expect(panel(page)).toBeHidden()
    await ui.waitForTerminal(session)
    await expect.poll(() => ui.termText(session), { timeout: 15_000 }).toContain('Goal achieved')
  })

  test('(V2-M1 T11) Queue panel (phone): Retry, Skip and Mark done sheets', { tag: '@loopback' }, async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-phone-attention')
    await stubs.setBehavior('e2e phone fail', 'fail')
    await stubs.setBehavior('e2e phone exit', 'exit')
    await stubs.setBehavior('e2e phone skip', 'exit')
    await ui.open()
    await openPanelAndCreate(page, project.id)
    await addItem(page, 'e2e phone fail')
    await addItem(page, 'e2e phone exit')
    await addItem(page, 'e2e phone skip')
    await panel(page).getByRole('button', { name: 'Start' }).tap()
    await expect(row(page, 'e2e phone fail').getByTestId('item-status')).toHaveText(/^Needs attention/, { timeout: 30_000 })
    await expect(row(page, 'e2e phone fail')).toContainText("can't be achieved")
    // Retry: a new run in a suffixed session (the old one stays open).
    await stubs.setBehavior('e2e phone fail', 'achieve:1')
    await row(page, 'e2e phone fail').getByRole('button', { name: /^Retry item/ }).tap()
    await panel(page).getByRole('button', { name: 'Resume' }).tap()
    await expect(row(page, 'e2e phone fail').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    await expect(row(page, 'e2e phone fail')).toContainText(`${project.name}-q1-1`)
    // Mark done confirms through the phone sheet.
    await expect(row(page, 'e2e phone exit').getByTestId('item-status')).toHaveText(/^Needs attention/, { timeout: 30_000 })
    await row(page, 'e2e phone exit').getByRole('button', { name: /^Mark item .* done$/ }).tap()
    const sheet = page.getByRole('alertdialog')
    await expect(sheet).toBeVisible()
    expect((await sheet.boundingBox())!.width).toBeGreaterThanOrEqual(389)
    await sheet.getByRole('button', { name: 'Mark done' }).tap()
    await expect(row(page, 'e2e phone exit').getByTestId('item-status')).toHaveText(/^Done/)
    // Skip confirms through the sheet too.
    await panel(page).getByRole('button', { name: 'Resume' }).tap()
    await expect(row(page, 'e2e phone skip').getByTestId('item-status')).toHaveText(/^Needs attention/, { timeout: 30_000 })
    await row(page, 'e2e phone skip').getByRole('button', { name: /^Skip item/ }).tap()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Skip' }).tap()
    await expect(row(page, 'e2e phone skip').getByTestId('item-status')).toHaveText(/^Skipped/)
    expect(await target.sessions()).toEqual(expect.arrayContaining([`${project.name}-q1`, `${project.name}-q1-1`, `${project.name}-q2`, `${project.name}-q3`]))
  })
})
