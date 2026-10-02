import { createHash } from 'node:crypto'
import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newProject } from '../helpers/queues.ts'
import { shq } from '../helpers/target.ts'

// Pasting an image into a queue item's instruction saves it in the queue's
// project folder (like the terminal paste) and inserts its relative path at
// the caret, for a new item and while editing one.

const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
const openPanel = (page: Page) => page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()

async function pasteImage(box: Locator, name: string, bytes: number[]) {
  await box.evaluate((textarea, payload) => {
    const file = new File([new Uint8Array(payload.bytes)], payload.name, { type: 'image/png' })
    const data = new DataTransfer()
    data.items.add(file)
    textarea.dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data }))
  }, { bytes, name })
}

const sha = (bytes: number[]) => createHash('sha256').update(Buffer.from(bytes)).digest('hex')

test.describe('queue instruction image paste (desktop)', { tag: '@desktop' }, () => {
  test('A pasted image lands in the project and its path in the instruction', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-queue-paste')
    await ui.open()
    await openPanel(page)
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()

    const add = panel(page).getByRole('form', { name: 'Add item' })
    const instruction = add.getByLabel('Instruction')
    await instruction.fill('/goal fix the header')
    await instruction.evaluate((box: HTMLTextAreaElement) => box.setSelectionRange(5, 5))
    const bytes = [0, 255, 1, 2, 3, 128]
    await pasteImage(instruction, 'queue-shot.png', bytes)
    await expect(instruction).toHaveValue('/goal ./queue-shot.png fix the header')
    await expect(page.getByRole('region', { name: 'Notifications' }).getByText('Photo added to repo').last()).toBeVisible()
    expect((await target.run(`sha256sum ${shq(`${project.path}/queue-shot.png`)}`)).split(/\s+/)[0]).toBe(sha(bytes))
    await add.getByRole('button', { name: 'Add item' }).click()
    await expect(panel(page).getByRole('listitem', { name: /\/goal \.\/queue-shot\.png fix the header$/ })).toBeVisible()

    // Editing an item works the same way.
    await panel(page).getByRole('button', { name: 'Edit item 1' }).click()
    const edit = panel(page).getByRole('form', { name: 'Edit item 1' }).getByLabel('Instruction')
    await edit.evaluate((box: HTMLTextAreaElement) => box.setSelectionRange(box.value.length, box.value.length))
    await pasteImage(edit, 'queue-edit.png', bytes)
    await expect(edit).toHaveValue('/goal ./queue-shot.png fix the header ./queue-edit.png')
    expect((await target.run(`sha256sum ${shq(`${project.path}/queue-edit.png`)}`)).split(/\s+/)[0]).toBe(sha(bytes))
  })
})
