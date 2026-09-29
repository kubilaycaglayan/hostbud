import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { getQueue, newProject, type Queue } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M1 T10: the Queue panel on desktop, driving stub clients on the
// throwaway target. Everything updates from /ws/events: no reload.

const stubs = new Stubs()

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

function panel(page: Page) {
  return page.getByRole('dialog', { name: 'Queue' })
}

function row(page: Page, condition: string) {
  return panel(page).getByRole('listitem', { name: new RegExp(`: /goal ${condition}$`) })
}

async function addItem(page: Page, condition: string, agent: 'claude' | 'codex' = 'claude') {
  const form = panel(page).getByRole('form', { name: 'Add item' })
  await form.getByLabel('Agent').selectOption(agent)
  const instruction = form.getByLabel('Instruction')
  await expect(instruction).toHaveValue('/goal ')
  await instruction.fill(`/goal ${condition}`)
  await form.getByRole('button', { name: 'Add item' }).click()
  await expect(row(page, condition)).toBeVisible()
}

async function order(page: Page): Promise<string[]> {
  return panel(page).getByRole('listitem').evaluateAll((items) => items.map((i) => (i.getAttribute('aria-label') ?? '').replace(/^Item \d+: \/goal /, '')))
}

test.describe('Queue panel (desktop)', () => {
  test.describe.configure({ timeout: 120_000 })
  test.skip(({ isMobile }) => isMobile, 'the phone variant is queue.phone.spec.ts')

  test('(V2-M1 T16) Permission flags are quick to select and default by agent', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-queue-permission-flags')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const form = dialog.getByRole('form', { name: 'Add item' })
    const agent = form.getByLabel('Agent')
    const flags = form.getByLabel('Flags')
    await expect(flags).toHaveValue('--dangerously-skip-permissions')
    await expect(form.getByLabel('Skip permission prompts')).toBeChecked()
    await expect(flags).toHaveAttribute('dir', 'ltr')
    await flags.fill('--')
    await expect(flags).toHaveValue('--')
    await flags.fill('')
    await agent.selectOption('codex')
    await expect(flags).toHaveValue('--yolo')
    await expect(form.getByLabel('YOLO mode')).toBeChecked()
    await form.getByLabel('YOLO mode').uncheck()
    await expect(flags).toHaveValue('')
    await form.getByLabel('YOLO mode').check()
    await form.getByLabel('Instruction').fill('/goal permission mode defaults')
    await form.getByRole('button', { name: 'Add item' }).click()
    await expect(row(page, 'permission mode defaults')).toContainText('--yolo')
  })

  test('(V2-M1 T15) Compact queue forms and actions on desktop', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-queue-compact')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const form = dialog.getByRole('form', { name: 'Add item' })
    const instruction = form.getByLabel('Instruction')
    await expect(instruction).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(instruction).toHaveCSS('resize', 'vertical')
    const addBox = await form.getByRole('button', { name: 'Add item' }).boundingBox()
    const formBox = await form.boundingBox()
    expect(addBox!.x + addBox!.width).toBeCloseTo(formBox!.x + formBox!.width, 0)
    await instruction.fill('/goal compact form test')
    await form.getByRole('button', { name: 'Add item' }).click()
    const item = row(page, 'compact form test')
    await expect(item).toBeVisible()
    const editButton = item.getByRole('button', { name: /^Edit item/ })
    expect((await editButton.boundingBox())!.width).toBeLessThanOrEqual(40)
    expect((await item.getByRole('button', { name: /^Delete item/ }).boundingBox())!.width).toBeLessThanOrEqual(40)
    await editButton.click()
    const edit = dialog.getByRole('form', { name: /^Edit item/ })
    const editInstruction = edit.getByLabel('Instruction')
    await expect(editInstruction).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(editInstruction).toHaveCSS('resize', 'vertical')
    await editInstruction.fill('/goal compact edit saved')
    await edit.getByRole('button', { name: 'Save' }).click()
    await expect(dialog.getByRole('listitem', { name: /: \/goal compact edit saved$/ })).toBeVisible()
  })

  test('(V2-M1 T10) Queue panel', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-panel')
    await stubs.setBehavior('e2e panel a', 'achieve:2')
    await stubs.setBehavior('e2e panel b', 'pending')
    await stubs.setBehavior('e2e panel f', 'fail')
    await ui.open()

    // Open the panel from the header (the command palette has it too).
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    await expect(panel(page)).toBeVisible()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    await expect(panel(page).getByTestId('queue-status')).toHaveText('idle')

    // Validation happens before anything is sent.
    const form = panel(page).getByRole('form', { name: 'Add item' })
    const instruction = form.getByLabel('Instruction')
    await expect(instruction).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(instruction).toHaveCSS('resize', 'vertical')
    const addButtonBox = await form.getByRole('button', { name: 'Add item' }).boundingBox()
    const formBox = await form.boundingBox()
    expect(addButtonBox!.x + addButtonBox!.width).toBeCloseTo(formBox!.x + formBox!.width, 0)
    await instruction.fill('work on M2')
    await form.getByRole('button', { name: 'Add item' }).click()
    await expect(form).toContainText('Start with /goal followed by the condition')
    await form.getByLabel('Flags').fill(`--model 'opus`)
    await instruction.fill('/goal e2e panel a')
    await form.getByRole('button', { name: 'Add item' }).click()
    await expect(form).toContainText('unbalanced single quote')
    await form.getByLabel('Flags').fill('')
    await instruction.fill('/goal ')
    for (const input of await panel(page).locator('input, select').all()) await expect(input).toHaveAttribute('autocomplete', 'off')

    await addItem(page, 'e2e panel a')
    await addItem(page, 'e2e panel b')
    await addItem(page, 'e2e panel f')
    // Reorder by drag, then by keyboard: a, f, b → a, b, f → a, f, b.
    await row(page, 'e2e panel f').getByRole('button', { name: /^Drag to reorder item/ }).dragTo(row(page, 'e2e panel b'), { targetPosition: { x: 20, y: 1 } })
    await expect.poll(() => order(page)).toEqual(['e2e panel a', 'e2e panel f', 'e2e panel b'])
    await row(page, 'e2e panel f').focus()
    await page.keyboard.press('Alt+ArrowDown')
    await expect.poll(() => order(page)).toEqual(['e2e panel a', 'e2e panel b', 'e2e panel f'])
    await row(page, 'e2e panel f').focus()
    await page.keyboard.press('Alt+ArrowUp')
    await expect.poll(() => order(page)).toEqual(['e2e panel a', 'e2e panel f', 'e2e panel b'])
    // Edit a queued item.
    await row(page, 'e2e panel b').getByRole('button', { name: /^Edit item/ }).click()
    const edit = panel(page).getByRole('form', { name: /^Edit item/ })
    const editInstruction = edit.getByLabel('Instruction')
    await expect(editInstruction).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(editInstruction).toHaveCSS('resize', 'vertical')
    await edit.getByLabel('Flags').fill('--dangerously-skip-permissions')
    await stubs.setBehavior('e2e panel b updated', 'pending')
    await editInstruction.fill('/goal e2e panel b updated')
    await edit.getByRole('button', { name: 'Save' }).click()
    await expect(panel(page).getByRole('listitem', { name: /: \/goal e2e panel b updated$/ })).toContainText('--dangerously-skip-permissions')

    // Start: item a runs and turns done, then f starts — all without a reload.
    const requests: string[] = []
    page.on('request', (r) => { if (r.url().includes('/api/queues')) requests.push(`${r.method()} ${new URL(r.url()).pathname}`) })
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(row(page, 'e2e panel a').getByTestId('item-status')).toHaveText(/^Running/, { timeout: 15_000 })
    await expect(row(page, 'e2e panel a').getByRole('button', { name: /^(Edit|Delete|Move)/ })).toHaveCount(0)
    await expect(row(page, 'e2e panel a').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    // f fails: needs attention with its reason; the queue pauses.
    await expect(row(page, 'e2e panel f').getByTestId('item-status')).toHaveText(/^Needs attention/, { timeout: 30_000 })
    await expect(row(page, 'e2e panel f')).toContainText("can't be achieved")
    await expect(panel(page).getByTestId('queue-status')).toHaveText('paused')
    expect(requests.filter((r) => r.startsWith('GET '))).toEqual([]) // no polling
    // Retry (fails again), then Skip after confirming.
    await row(page, 'e2e panel f').getByRole('button', { name: /^Retry item/ }).click()
    await expect(row(page, 'e2e panel f').getByTestId('item-status')).toHaveText(/^Queued/)
    await panel(page).getByRole('button', { name: 'Resume' }).click()
    await expect(row(page, 'e2e panel f').getByTestId('item-status')).toHaveText(/^Needs attention/, { timeout: 30_000 })
    await row(page, 'e2e panel f').getByRole('button', { name: /^Skip item/ }).click()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText('Skip item')
    await confirm.getByRole('button', { name: 'Skip' }).click()
    await expect(row(page, 'e2e panel f').getByTestId('item-status')).toHaveText(/^Skipped/)
    await panel(page).getByRole('button', { name: 'Resume' }).click()
    // b runs (pending): open its session in a tab and see the stub's output.
    await expect(row(page, 'e2e panel b updated').getByTestId('item-status')).toHaveText(/^Running · running/, { timeout: 30_000 })
    const q = (await (await request.get('/api/queues')).json() as { queues: Queue[] }).queues[0]
    const session = (await getQueue(request, q.id)).items.find((i) => i.instruction === '/goal e2e panel b updated')!.run!.sessionName
    await row(page, 'e2e panel b updated').getByRole('button', { name: /^Open session of item/ }).click()
    await expect(panel(page)).toBeHidden()
    await ui.waitForTerminal(session)
    await expect.poll(() => ui.termText(session), { timeout: 15_000 }).toContain('still working')
  })

  test('(V2-M1 T10) Queue panel: Mark done confirms, and the palette opens the panel', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-panel-done')
    await stubs.setBehavior('e2e panel done x', 'exit')
    await stubs.setBehavior('e2e panel done y', 'achieve:1')
    await ui.open()
    await page.keyboard.press('Control+Shift+K')
    const palette = page.getByRole('dialog', { name: 'Command palette' })
    await palette.getByRole('combobox', { name: 'Command palette' }).fill('Open queue panel')
    await expect(palette.getByRole('option', { name: 'Open queue panel' })).toBeVisible()
    await page.keyboard.press('Enter')
    await expect(panel(page)).toBeVisible()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    await addItem(page, 'e2e panel done x')
    await addItem(page, 'e2e panel done y', 'codex')
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(row(page, 'e2e panel done x').getByTestId('item-status')).toHaveText(/^Needs attention · exited/, { timeout: 30_000 })
    await row(page, 'e2e panel done x').getByRole('button', { name: /^Mark item .* done$/ }).click()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText("overrides the agent's own /goal verdict")
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(row(page, 'e2e panel done x').getByTestId('item-status')).toHaveText(/^Needs attention/)
    await row(page, 'e2e panel done x').getByRole('button', { name: /^Mark item .* done$/ }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Mark done' }).click()
    await expect(row(page, 'e2e panel done x').getByTestId('item-status')).toHaveText(/^Done/)
    await panel(page).getByRole('button', { name: 'Resume' }).click()
    await expect(row(page, 'e2e panel done y').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 40_000 })
    await expect(panel(page).getByTestId('queue-status')).toHaveText('finished')
    // A second queue is refused with the V2-M2 message (the panel has one queue).
    const second = await request.fetch('/api/queues', { method: 'POST', data: { projectId: project.id, name: 'Two' }, headers: { Origin: 'http://localhost:9055' } })
    expect((await second.json()).error).toContain('V2-M2')
  })
})
