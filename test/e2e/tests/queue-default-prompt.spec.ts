import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { mutate } from '../helpers/api.ts'
import { newProject } from '../helpers/queues.ts'

// The queue default prompt (Settings): opt-in, ", commit regularly." by
// default; when on, the Queue panel's new-item instruction starts with it.
// It is only a prefill: the server stores the instruction as sent.

const path = '/api/machines/host/default-prompt'
const off = { enabled: false, text: ', commit regularly.' }

test.describe('queue default prompt (API)', () => {
  test('Off by default, Origin-checked, validated and reported by GET /api/queues', async ({ request }) => {
    try {
      await mutate(request, 'PUT', path, off)
      expect(await (await request.get(path)).json()).toEqual(off)
      expect((await mutate(request, 'PUT', path, { enabled: true, text: 'x' }, 'http://evil.example.com')).status()).toBe(403)
      expect((await mutate(request, 'PUT', path, { enabled: true, text: 'a\nb' })).status()).toBe(400)
      expect((await mutate(request, 'PUT', path, { enabled: true })).status()).toBe(400)
      expect((await mutate(request, 'PUT', '/api/machines/server-a/default-prompt', off)).status()).toBe(404)
      expect(await (await request.get(path)).json()).toEqual(off)
      const on = await mutate(request, 'PUT', path, { enabled: true, text: ', commit regularly.' })
      expect(await on.json()).toEqual({ enabled: true, text: ', commit regularly.' })
      expect((await (await request.get('/api/queues')).json()).defaultPrompt).toEqual({ enabled: true, text: ', commit regularly.' })
    } finally {
      await mutate(request, 'PUT', path, off)
    }
  })
})

const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })

async function openSettings(page: Page) {
  await page.getByRole('button', { name: 'Account', exact: true }).click()
  await page.getByRole('button', { name: 'Settings', exact: true }).click()
  return page.getByRole('dialog', { name: 'Settings' }).getByRole('form', { name: 'Queue default prompt' })
}

test.describe('queue default prompt (desktop)', { tag: '@desktop' }, () => {
  test('Opting in prefills the new item instruction; the item keeps what was typed', async ({ page, ui, request, target }) => {
    try {
      await mutate(request, 'PUT', path, off)
      const project = await newProject(request, target, 'e2e-default-prompt')
      await ui.open()
      const form = await openSettings(page)
      await expect(form.getByLabel('Start new queue items with the default prompt')).not.toBeChecked()
      await expect(form.getByLabel('Default prompt')).toHaveValue(', commit regularly.')
      await form.getByLabel('Start new queue items with the default prompt').check()
      await form.getByRole('button', { name: 'Save' }).click()
      await expect(form.getByRole('status')).toContainText('start with the default prompt')
      await page.getByRole('button', { name: 'Close settings' }).click()

      await page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()
      await panel(page).getByLabel('Project').selectOption(project.id)
      await panel(page).getByRole('button', { name: 'Create queue' }).click()
      const add = panel(page).getByRole('form', { name: 'Add item' })
      const instruction = add.getByLabel('Instruction')
      await expect(instruction).toHaveValue(', commit regularly.')
      // The caret lands before the prompt: typing completes the sentence.
      await instruction.focus()
      await expect.poll(() => instruction.evaluate((el) => (el as HTMLTextAreaElement).selectionStart)).toBe(0)
      await page.keyboard.type('/goal e2e prompt done')
      await expect(instruction).toHaveValue('/goal e2e prompt done, commit regularly.')
      await add.getByRole('button', { name: 'Add item' }).click()
      await expect(panel(page).getByRole('listitem', { name: /: \/goal e2e prompt done, commit regularly\.$/ })).toBeVisible()
      await expect(instruction).toHaveValue(', commit regularly.')
    } finally {
      await mutate(request, 'PUT', path, off)
    }
  })
})
