import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { mutate } from '../helpers/api.ts'
import { createQueue, newProject, type Queue } from '../helpers/queues.ts'

// A queue's default prompt: opt-in per queue, ", commit regularly." by
// default; when on, the Queue panel's new-item instruction for that queue
// starts with it. It is only a prefill: the server stores the instruction
// as sent.

test.describe('queue default prompt (API)', () => {
  test('Per queue, off by default, Origin-checked and validated', async ({ request, target }) => {
    const project = await newProject(request, target, 'e2e-default-prompt-api')
    const a = await createQueue(request, project.id, 'Alpha')
    const b = await createQueue(request, project.id, 'Beta')
    expect((a as Queue & { defaultPrompt?: unknown }).defaultPrompt).toBeUndefined()
    const path = `/api/queues/${a.id}/default-prompt`
    expect((await mutate(request, 'PUT', path, { enabled: true, text: 'x' }, 'http://evil.example.com')).status()).toBe(403)
    expect((await mutate(request, 'PUT', path, { enabled: true, text: 'a\nb' })).status()).toBe(400)
    expect((await mutate(request, 'PUT', path, { enabled: true })).status()).toBe(400)
    expect((await mutate(request, 'PUT', '/api/queues/queue_missing/default-prompt', { enabled: true, text: 'x' })).status()).toBe(404)
    const on = await mutate(request, 'PUT', path, { enabled: true, text: ', commit regularly.' })
    expect(on.status()).toBe(200)
    expect((await on.json()).defaultPrompt).toEqual({ enabled: true, text: ', commit regularly.' })
    const list = (await (await request.get('/api/queues')).json()).queues as (Queue & { defaultPrompt?: unknown })[]
    expect(list.find((q) => q.id === a.id)?.defaultPrompt).toEqual({ enabled: true, text: ', commit regularly.' })
    expect(list.find((q) => q.id === b.id)?.defaultPrompt).toBeUndefined()
  })
})

const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })

test.describe('queue default prompt (desktop)', { tag: '@desktop' }, () => {
  test('Opting a queue in prefills its new item instruction; the item keeps what was typed', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-default-prompt')
    await ui.open()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    const settings = panel(page).getByRole('form', { name: 'Default prompt' })
    const add = panel(page).getByRole('form', { name: 'Add item' })
    const instruction = add.getByLabel('Instruction')
    await expect(settings.getByLabel('Start new items with the default prompt')).not.toBeChecked()
    await expect(settings.getByLabel('Default prompt', { exact: true })).toHaveValue(', commit regularly.')
    await expect(instruction).toHaveValue('')

    await settings.getByLabel('Start new items with the default prompt').check()
    await expect(settings.getByLabel('Start new items with the default prompt')).toBeChecked()
    await expect(instruction).toHaveValue(', commit regularly.')
    // The caret lands before the prompt: typing completes the sentence.
    await instruction.focus()
    await expect.poll(() => instruction.evaluate((el) => (el as HTMLTextAreaElement).selectionStart)).toBe(0)
    await page.keyboard.type('/goal e2e prompt done')
    await expect(instruction).toHaveValue('/goal e2e prompt done, commit regularly.')
    await add.getByRole('button', { name: 'Add item' }).click()
    await expect(panel(page).getByRole('listitem', { name: /: \/goal e2e prompt done, commit regularly\.$/ })).toBeVisible()
    await expect(instruction).toHaveValue(', commit regularly.')

    // A changed text is saved for this queue and prefills the next item.
    await settings.getByLabel('Default prompt', { exact: true }).fill(', commit and push regularly.')
    await settings.getByRole('button', { name: 'Save default prompt' }).click()
    await expect(instruction).toHaveValue(', commit and push regularly.')
    await page.reload()
    await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
    await expect(panel(page).getByRole('form', { name: 'Default prompt' }).getByLabel('Default prompt', { exact: true })).toHaveValue(', commit and push regularly.')
  })
})
