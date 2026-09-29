import { dragSortable } from '../helpers/ui.ts'
import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { mutate, ORIGIN } from '../helpers/api.ts'
import { getQueue, listQueues, newProject, pickDuration, type Queue } from '../helpers/queues.ts'
import { shq } from '../helpers/target.ts'
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
  await form.getByRole('combobox', { name: /^Agent\b/ }).selectOption(agent)
  const instruction = form.getByLabel('Instruction')
  await expect(instruction).toHaveValue('')
  await instruction.fill(`/goal ${condition}`)
  await form.getByRole('button', { name: 'Add item' }).click()
  await expect(row(page, condition)).toBeVisible()
}

async function order(page: Page): Promise<string[]> {
  return panel(page).getByRole('listitem').evaluateAll((items) => items.map((i) => (i.getAttribute('aria-label') ?? '').replace(/^Item \d+: \/goal /, '')))
}

test.describe('Queue panel (desktop)', { tag: '@desktop' }, () => {
  test.describe.configure({ timeout: 120_000 })
  // The phone variant is queue.phone.spec.ts.

  test('Queue dialog inputs show every "-" in "->" and "--"', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-queue-dashes')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const instruction = dialog.getByRole('form', { name: 'Add item' }).getByLabel('Instruction')
    await instruction.fill('a -> b -- c')
    await expect(instruction).toHaveValue('a -> b -- c')
    // A ligature would draw "->" or "--" as one merged glyph and hide a "-".
    const style = await instruction.evaluate((el) => {
      const cs = getComputedStyle(el)
      return { ligatures: cs.fontVariantLigatures, features: cs.fontFeatureSettings }
    })
    expect(style.ligatures).toBe('none')
    expect(style.features).toContain('"calt" 0')
  })

  test('Cmd/Ctrl+Enter in the Add item form adds the item, like the + button', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-queue-cmd-enter')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const instruction = dialog.getByRole('form', { name: 'Add item' }).getByLabel('Instruction')
    await instruction.fill('/goal cmd-enter')
    await instruction.press('ControlOrMeta+Enter')
    await expect(row(page, 'cmd-enter')).toBeVisible()
    await expect(instruction).toHaveValue('')
  })

  test('(V2-M8 T3) Schedule a command for an existing session', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-scheduled-ui')
    await target.run(`tmux new-session -d -s schedule-ui-target -c ${shq(project.path)}`)
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const form = dialog.getByRole('form', { name: 'Add item' })
    await form.getByLabel('Execution').selectOption('session')
    await form.getByRole('combobox', { name: /^Existing session/ }).selectOption('schedule-ui-target')
    await form.getByRole('textbox', { name: /^Command/ }).fill("echo 'scheduled ui command'")
    await form.getByRole('button', { name: 'Add item' }).click()
    // The picker's smallest delay is one minute.
    const delay = dialog.getByRole('group', { name: 'Start delay' })
    await expect(delay.getByRole('textbox')).toHaveCount(0)
    await pickDuration(delay, 'Minutes', 1)
    // The Start button keeps its size next to the taller picker, not stretched to its height.
    const startBox = await dialog.getByRole('button', { name: 'Start' }).boundingBox()
    expect(startBox?.height).toBeLessThan(56)
    await dialog.getByRole('button', { name: 'Start' }).click()
    await expect(dialog.getByTestId('queue-scheduled')).toContainText('Scheduled for')
    await expect(dialog.getByTestId('item-status')).toHaveText('Queued')
    await expect.poll(async () => target.capture('schedule-ui-target'), { timeout: 90_000 }).toContain('scheduled ui command')
    await expect(dialog.getByTestId('item-status')).toHaveText('Done')
  })

  test('Loop a queue until its runtime limit', async ({ page, ui, request, target }) => {
    test.setTimeout(180_000)
    const project = await newProject(request, target, 'e2e-loop-ui')
    await target.run(`tmux new-session -d -s loop-ui-target -c ${shq(project.path)}`)
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const form = dialog.getByRole('form', { name: 'Add item' })
    await form.getByLabel('Execution').selectOption('session')
    await form.getByRole('combobox', { name: /^Existing session/ }).selectOption('loop-ui-target')
    await form.getByRole('textbox', { name: /^Command/ }).fill("printf '%s\\n' loop-ui-marker")
    await form.getByRole('button', { name: 'Add item' }).click()
    const loop = dialog.getByRole('form', { name: 'Loop queue' })
    const limit = loop.getByRole('group', { name: 'Loop runtime limit' })
    await expect(limit.getByRole('combobox', { name: 'Hours' })).toHaveText('5')
    await expect(limit.getByRole('combobox', { name: 'Minutes' })).toHaveText('00')
    // The hours list stops at 48 and scrolls inside the viewport.
    await limit.getByRole('combobox', { name: 'Hours' }).click()
    const hours = page.getByRole('listbox')
    await expect(hours.getByRole('option')).toHaveCount(49)
    await expect(hours.getByRole('option').last()).toHaveText('48')
    const box = (await hours.boundingBox())!
    expect(box.y + box.height).toBeLessThanOrEqual(page.viewportSize()!.height)
    await page.keyboard.press('Escape')
    await pickDuration(limit, 'Hours', 0)
    await pickDuration(limit, 'Minutes', 1)
    await loop.getByLabel('Loop the queue').check()
    await expect(loop.getByLabel('Loop the queue')).toBeChecked()
    // The picker has minute steps; the API still takes seconds, so the test
    // tightens the limit to 30 s there to keep pass 2 the last.
    const looped = (await listQueues(request)).find((q) => q.projectId === project.id)!
    const tightened = await mutate(request, 'PUT', `/api/queues/${looped.id}/loop`, { enabled: true, maxRuntime: '30s' }, ORIGIN)
    expect(tightened.status(), await tightened.text()).toBe(200)
    await expect(loop.getByTestId('queue-loop-status')).toContainText('Pass 1')
    await dialog.getByRole('button', { name: 'Start' }).click()
    // Pass 1 dispatches at once; pass 2 waits for the one-minute spacing.
    await expect(loop.getByTestId('queue-loop-status')).toContainText('Pass 2')
    await expect(dialog.getByTestId('queue-scheduled')).toContainText('Scheduled for')
    await expect(dialog.getByTestId('item-status')).toHaveText('Queued')
    // By then the 30 s limit has passed: pass 2 is the last.
    await expect(dialog.getByTestId('queue-status')).toHaveText('finished', { timeout: 100_000 })
    const markers = (await target.capture('loop-ui-target')).split('\n').filter((l) => l.trim() === 'loop-ui-marker')
    expect(markers).toHaveLength(2)
    // Start runs a finished looping queue's items again.
    await expect(dialog.getByRole('button', { name: 'Start' })).toBeEnabled()
  })

  test('Attach a new queue to an existing non-queue session', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-attach-ui')
    await target.run([
      'mkdir -p /home/dev/.hostbud-test-bin',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/cly',
      `tmux new-session -d -s attach-ui-manual -c ${shq(project.path)} /home/dev/.hostbud-test-bin/cly 120`,
      `tmux new-session -d -s attach-ui-sink -c ${shq(project.path)}`,
    ].join(' && '))
    const pane = (await target.tmux('list-panes', '-t', '=attach-ui-manual:', '-F', '#{pane_id}')).trim().split('\n')[0]
    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'working')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByLabel('Start after (optional)').selectOption('session:attach-ui-manual')
    await expect(dialog).toContainText("waits until that session's agent finishes its turn")
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    await expect(dialog.getByTestId('queue-dependency')).toContainText('Waits for session attach-ui-manual to be idle')
    const form = dialog.getByRole('form', { name: 'Add item' })
    await form.getByLabel('Execution').selectOption('session')
    await form.getByRole('combobox', { name: /^Existing session/ }).selectOption('attach-ui-sink')
    await form.getByRole('textbox', { name: /^Command/ }).fill("echo 'attached queue ran'")
    await form.getByRole('button', { name: 'Add item' }).click()
    await dialog.getByRole('button', { name: 'Start' }).click()
    await expect(dialog.getByTestId('item-status')).toHaveText('Queued')
    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'blocked')
    await expect(dialog.getByTestId('item-status')).toHaveText('Done', { timeout: 15_000 })
    await expect.poll(async () => target.capture('attach-ui-sink')).toContain('attached queue ran')
    await expect(dialog.getByTestId('queue-dependency')).toContainText('Started after session attach-ui-manual was idle')
  })

  test('Set Start after on an existing queue from the panel', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-link-existing-ui')
    await target.run([
      'mkdir -p /home/dev/.hostbud-test-bin',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/cly',
      `tmux new-session -d -s link-ui-manual -c ${shq(project.path)} /home/dev/.hostbud-test-bin/cly 120`,
      `tmux new-session -d -s link-ui-sink -c ${shq(project.path)}`,
    ].join(' && '))
    const pane = (await target.tmux('list-panes', '-t', '=link-ui-manual:', '-F', '#{pane_id}')).trim().split('\n')[0]
    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'working')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    await expect(dialog.getByTestId('queue-dependency')).toHaveCount(0)
    const link = dialog.getByRole('form', { name: 'Start after' })
    await link.getByLabel('Start next item after').selectOption('session:link-ui-manual')
    await expect(link).toContainText('The next item to start waits')
    await link.getByRole('button', { name: 'Save Start after' }).click()
    await expect(dialog.getByTestId('queue-dependency')).toContainText('Waits for session link-ui-manual to be idle before its next item')
    const form = dialog.getByRole('form', { name: 'Add item' })
    await form.getByLabel('Execution').selectOption('session')
    await form.getByRole('combobox', { name: /^Existing session/ }).selectOption('link-ui-sink')
    await form.getByRole('textbox', { name: /^Command/ }).fill("echo 'linked queue ran'")
    await form.getByRole('button', { name: 'Add item' }).click()
    await dialog.getByRole('button', { name: 'Start' }).click()
    await expect(dialog.getByTestId('item-status')).toHaveText('Queued')
    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'blocked')
    await expect(dialog.getByTestId('item-status')).toHaveText('Done', { timeout: 15_000 })
    await expect.poll(async () => target.capture('link-ui-sink')).toContain('linked queue ran')
    await expect(dialog.getByTestId('queue-dependency')).toContainText('Started after session link-ui-manual was idle')
  })

  test('(V2-M10 T1) Plain Claude prompt waits for manual completion, with its elapsed time and tokens', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-plain-prompt')
    await stubs.setBehavior('implement the small change', 'achieve:1')
    await stubs.setBehavior('complete the second item', 'achieve:1')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    const form = panel(page).getByRole('form', { name: 'Add item' })
    await form.getByLabel('Instruction').fill('implement the small change')
    await form.getByRole('button', { name: 'Add item' }).click()
    const item = panel(page).getByRole('listitem', { name: /: implement the small change$/ })
    await expect(item).toBeVisible()
    await form.getByRole('combobox', { name: /^Agent\b/ }).selectOption('codex')
    await form.getByLabel('Instruction').fill('complete the second item')
    await form.getByRole('button', { name: 'Add item' }).click()
    const second = panel(page).getByRole('listitem', { name: /: complete the second item$/ })
    await expect(second).toBeVisible()
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(item.getByTestId('item-status')).toHaveText(/^Needs attention · exited/, { timeout: 30_000 })
    await expect(item).toContainText('Claude finished its turn')
    // The item's total elapsed time and its session's tokens (the stub's
    // one turn: 1,200 input, 34 output).
    await expect(item.getByTestId('item-elapsed')).toHaveText(/^\d+s$|^\d+m \d\ds$/)
    await expect(item.getByTestId('item-tokens')).toHaveText('1.2k in · 34 out tokens')
    await item.getByRole('button', { name: /^Mark item .* done$/ }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Mark done' }).click()
    await expect(item.getByTestId('item-status')).toHaveText(/^Done/)
    await panel(page).getByRole('button', { name: 'Resume' }).click()
    await expect(second.getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
  })

  test('(V2-M1 T16) Permission flags are quick to select and default by agent', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-queue-permission-flags')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    const dialog = panel(page)
    await dialog.getByLabel('Project').selectOption(project.id)
    await dialog.getByRole('button', { name: 'Create queue' }).click()
    const form = dialog.getByRole('form', { name: 'Add item' })
    const agent = form.getByRole('combobox', { name: /^Agent\b/ })
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

    // Deleting an item asks first, in the app's own dialog (never window.confirm).
    page.on('dialog', (d) => { throw new Error(`unexpected native dialog: ${d.message()}`) })
    const saved = dialog.getByRole('listitem', { name: /: \/goal compact edit saved$/ })
    await saved.getByRole('button', { name: /^Delete item/ }).click()
    const confirm = page.getByRole('alertdialog', { name: /^Delete item \d+\?$/ })
    await expect(confirm).toBeVisible()
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(confirm).toHaveCount(0)
    await expect(saved).toBeVisible()
    await saved.getByRole('button', { name: /^Delete item/ }).click()
    await confirm.getByRole('button', { name: 'Delete item' }).click()
    await expect(saved).toHaveCount(0)
  })

  test('(V2-M9 T6) Queue panel progress indicator', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-panel')
    await stubs.setBehavior('e2e panel a', 'achieve:2')
    await stubs.setBehavior('e2e panel b', 'pending')
    await stubs.setBehavior('e2e panel f', 'fail')
    await ui.open()
    // Collapse the project: Open session later expands it again.
    await ui.treeItem(project.name).getByRole('button', { name: `Collapse ${project.name}` }).click()
    await expect(ui.treeItem(project.name)).toHaveAttribute('aria-expanded', 'false')

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
    await instruction.fill('work on M2\nnext')
    await form.getByRole('button', { name: 'Add item' }).click()
    await expect(form).toContainText('The instruction must be one line.')
    await form.getByLabel('Flags').fill(`--model 'opus`)
    await instruction.fill('/goal e2e panel a')
    await form.getByRole('button', { name: 'Add item' }).click()
    await expect(form).toContainText('unbalanced single quote')
    await form.getByLabel('Flags').fill('')
    await instruction.fill('')
    for (const input of await panel(page).locator('input, select').all()) await expect(input).toHaveAttribute('autocomplete', 'off')

    await addItem(page, 'e2e panel a')
    await addItem(page, 'e2e panel b')
    await addItem(page, 'e2e panel f')
    await expect(panel(page).getByTestId('queue-progress')).toHaveAttribute('aria-label', '3 queued; 3 left')
    // Reorder by drag, then by keyboard: a, f, b → a, b, f → a, f, b.
    await dragSortable(row(page, 'e2e panel f').getByRole('button', { name: /^Drag to reorder item/ }), row(page, 'e2e panel b'), { x: 20, y: 1 })
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
    await expect(panel(page).getByTestId('queue-progress')).toHaveAttribute('aria-label', 'item 1 active; 1 in progress | 2 left')
    // Behind the modal panel, the header is hidden from role queries.
    await expect(page.locator('header').getByRole('button', { name: 'Queue', exact: true, includeHidden: true }).locator('[data-icon-box]')).toHaveClass(/border-accent/)
    await expect(panel(page).getByTestId('queue-status')).toHaveClass(/font-bold/)
    await expect(row(page, 'e2e panel a').getByRole('button', { name: /^(Edit|Delete|Move)/ })).toHaveCount(0)
    await expect(row(page, 'e2e panel a').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 30_000 })
    // f fails: needs attention with its reason; the queue pauses.
    await expect(row(page, 'e2e panel f').getByTestId('item-status')).toHaveText(/^Needs attention/, { timeout: 30_000 })
    await expect(row(page, 'e2e panel f')).toContainText("can't be achieved")
    await expect(panel(page).getByTestId('queue-status')).toHaveText('paused')
    await expect(panel(page).getByTestId('queue-progress')).toHaveAttribute('aria-label', 'item 2 needs attention; 1 left') // f is second (a, f, b)
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
    // The sidebar shows it: the project expanded, the session marked.
    await expect(ui.treeItem(project.name)).toHaveAttribute('aria-expanded', 'true')
    await expect(ui.treeItem(session)).toBeVisible()
    await expect(ui.treeItem(session)).toHaveAttribute('aria-selected', 'true')
  })

  test('Kill a done item\'s session, then all completed sessions of the queue', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-kill-done')
    await stubs.setBehavior('e2e kill done a', 'achieve:1')
    await stubs.setBehavior('e2e kill done b', 'achieve:1')
    await stubs.setBehavior('e2e kill done c', 'achieve:1')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    for (const c of ['a', 'b', 'c']) await addItem(page, `e2e kill done ${c}`, 'codex')
    await panel(page).getByRole('button', { name: 'Start' }).click()
    await expect(row(page, 'e2e kill done c').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 60_000 })
    const q = (await listQueues(request)).find((x) => x.projectId === project.id)!
    const names = (await getQueue(request, q.id)).items.map((i) => i.run!.sessionName)
    const alive = async (name: string) => target.tmux('has-session', '-t', `=${name}`).then(() => true, () => false)
    // One done item: its icon button kills its session after a confirmation.
    const killOne = row(page, 'e2e kill done a').getByRole('button', { name: /^Kill session of item/ })
    await killOne.click()
    const confirm = page.getByRole('alertdialog')
    await expect(confirm).toContainText(`Kill session ${names[0]}?`)
    await confirm.getByRole('button', { name: 'Kill session' }).click()
    await expect.poll(() => alive(names[0])).toBe(false)
    await expect(killOne).toBeHidden()
    // The rest at once: Kill completed sessions lists the open ones.
    await panel(page).getByRole('button', { name: 'Kill completed sessions' }).click()
    await expect(confirm).toContainText('Kill 2 completed sessions?')
    await confirm.getByRole('button', { name: 'Kill sessions' }).click()
    await expect.poll(async () => [await alive(names[1]), await alive(names[2])]).toEqual([false, false])
    await expect(panel(page).getByRole('button', { name: 'Kill completed sessions' })).toBeHidden()
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
    await expect(confirm).toContainText("Confirm that the agent's work is complete")
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    await expect(row(page, 'e2e panel done x').getByTestId('item-status')).toHaveText(/^Needs attention/)
    await row(page, 'e2e panel done x').getByRole('button', { name: /^Mark item .* done$/ }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Mark done' }).click()
    await expect(row(page, 'e2e panel done x').getByTestId('item-status')).toHaveText(/^Done/)
    await panel(page).getByRole('button', { name: 'Resume' }).click()
    await expect(row(page, 'e2e panel done y').getByTestId('item-status')).toHaveText(/^Done/, { timeout: 40_000 })
    await expect(panel(page).getByTestId('queue-status')).toHaveText('finished')
    // Parallel queues off: New queue still creates a second queue (to
    // organize work); the panel explains that only one runs at a time.
    await expect(panel(page).getByTestId('parallel-off')).toContainText('still create queues')
    await panel(page).getByRole('button', { name: 'New queue' }).click()
    await panel(page).getByLabel('Queue name').fill('Two')
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    await expect(panel(page).getByRole('button', { name: 'Show queue Two' })).toHaveAttribute('aria-current', 'true')
    await expect(panel(page).getByTestId('queue-status')).toHaveText('idle')
  })
})
