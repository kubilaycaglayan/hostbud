import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newProject } from '../helpers/queues.ts'

// The Queue panel remembers the agent last picked for a new item on this
// browser, so a page reload keeps it (and its permission flag).

const panel = (page: Page) => page.getByRole('dialog', { name: 'Queue' })
const openPanel = (page: Page) => page.getByRole('banner').getByRole('button', { name: 'Queue', exact: true }).click()

test.describe('queue last agent (desktop)', { tag: '@desktop' }, () => {
  test('The picked agent survives a page reload', async ({ page, ui, request, target }) => {
    const project = await newProject(request, target, 'e2e-last-agent')
    await ui.open()
    await openPanel(page)
    await panel(page).getByLabel('Project').selectOption(project.id)
    await panel(page).getByRole('button', { name: 'Create queue' }).click()
    const add = panel(page).getByRole('form', { name: 'Add item' })
    const agent = add.locator('label').filter({ hasText: 'Agent' }).getByRole('combobox')
    await agent.selectOption('codex')
    await expect(add.locator('[data-agent-mark]')).toHaveAttribute('data-agent', 'codex')

    await page.reload()
    await openPanel(page)
    const reloaded = panel(page).getByRole('form', { name: 'Add item' })
    await expect(reloaded.locator('label').filter({ hasText: 'Agent' }).getByRole('combobox')).toHaveValue('codex')
    await expect(reloaded.locator('[data-agent-mark]')).toHaveAttribute('data-agent', 'codex')
    await expect(reloaded.getByLabel('Skip permission prompts').or(reloaded.getByLabel('YOLO mode'))).toBeChecked()
  })
})
