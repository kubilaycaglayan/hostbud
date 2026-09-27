import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { shq, uniqueName } from '../helpers/target.ts'

async function account(ui: import('../helpers/ui.ts').UI) {
  const fresh = newAccount('e2e-tree-windows')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
}

async function createSplitSession(target: { tmux(...args: string[]): Promise<string> }, name: string) {
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await target.tmux('rename-window', '-t', `=${name}:0`, 'main')
  await target.tmux('new-window', '-d', '-t', `=${name}:`, '-n', 'editor')
  await target.tmux('split-window', '-d', '-t', `=${name}:1`)
}

test('(T3) Windows load when a session is expanded', async ({ page, ui, target, request }) => {
  await account(ui)
  const name = uniqueName('tree-windows')
  await createSplitSession(target, name)
  const requests: string[] = []
  page.on('request', (req) => {
    if (new URL(req.url()).pathname.endsWith(`/sessions/${name}/windows`)) requests.push(req.url())
  })
  await page.reload()
  await expect(ui.treeItem(name)).toBeVisible()
  expect(requests).toHaveLength(0)
  await ui.treeItem(name).getByRole('button', { name: `Expand ${name}` }).click()
  await expect.poll(() => requests.length).toBe(1)
  const rows = page.locator(`[data-tree-key^="window:host/${name}/"]`)
  await expect(rows).toHaveCount(2)
  const targetRows = (await target.tmux('list-windows', '-t', `=${name}`, '-F', '#{window_index}\t#{window_name}'))
    .trim().split('\n').map((row) => row.split('\t'))
    .map(([index, title]) => `${Number(index) + 1}: ${title}`)
  await expect.poll(async () => (await rows.allTextContents()).map((text) => text.trim().replace(/\s+\(current\)$/u, ''))).toEqual(targetRows)
  await ui.treeItem(name).focus()
  await page.keyboard.press('ArrowRight')
  await expect(rows.nth(0)).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(rows.nth(1)).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(rows.nth(1)).toHaveAttribute('aria-expanded', 'true')
  await page.keyboard.press('ArrowDown')
  const panes = page.locator(`[data-tree-key^="pane:host/${name}/"]`)
  await expect(panes).toHaveCount(2)
  await expect(panes.nth(0)).toBeFocused()
  const positions = await Promise.all([ui.treeItem(name).boundingBox(), rows.nth(0).boundingBox(), panes.nth(0).boundingBox()])
  expect(positions[0]?.x).toBeLessThan(positions[1]?.x ?? 0)
  expect(positions[1]?.x).toBeLessThan(positions[2]?.x ?? 0)
  await ui.waitForSave('tree')
  await page.reload()
  await expect(ui.treeItem(name)).toHaveAttribute('aria-expanded', 'true')
  await expect(page.locator(`[data-tree-key^="window:host/${name}/"]`)).toHaveCount(2)
  await expect(page.locator(`[data-tree-key^="pane:host/${name}/"]`)).toHaveCount(2)
  await ctl.restartApp()
  await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
  await page.reload()
  await expect(page.locator(`[data-tree-key^="window:host/${name}/"]`)).toHaveCount(2)
})

test('(T3) Window rows follow the real terminal', async ({ page, ui, target }) => {
  await account(ui)
  const name = uniqueName('tree-live-windows')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await page.reload()
  await expect(ui.treeItem(name)).toBeVisible()
  // One window: nothing to expand, so no chevron (M8 T2).
  const expand = ui.treeItem(name).getByRole('button', { name: `Expand ${name}` })
  await expect(expand).toHaveCount(0)
  await expect(ui.treeItem(name)).not.toHaveAttribute('aria-expanded', /.*/)
  // A second window appears live: the chevron follows, and expands to both.
  await target.tmux('new-window', '-d', '-t', `=${name}:`, '-n', 'extra')
  await expect(expand).toBeVisible({ timeout: 5_000 })
  await expand.click()
  await expect(page.locator(`[data-tree-key^="window:host/${name}/"]`)).toHaveCount(2)
  const extra = page.locator(`[data-tree-key^="window:host/${name}/"]`).filter({ hasText: 'extra' })
  await expect(extra).toBeVisible({ timeout: 5_000 })
  await target.tmux('new-window', '-d', '-t', `=${name}:`, '-n', 'third')
  await expect(page.locator(`[data-tree-key^="window:host/${name}/"]`)).toHaveCount(3, { timeout: 5_000 })
  const index = await target.tmux('display-message', '-p', '-t', `=${name}:extra`, '#{window_index}')
  await target.tmux('kill-window', '-t', `=${name}:${index.trim()}`)
  await expect(extra).toHaveCount(0, { timeout: 5_000 })
  await expect(page.locator(`[data-tree-key^="window:host/${name}/"]`)).toHaveCount(2)
})

test('(T3) Open at a window and pane', async ({ page, ui, target }) => {
  await account(ui)
  const name = uniqueName('tree-open-window')
  const marker = uniqueName('WINDOW_MARKER')
  await createSplitSession(target, name)
  await target.tmux('send-keys', '-t', `=${name}:1.0`, `printf ${shq(marker)}`, 'Enter')
  await page.reload()
  await expect(ui.treeItem(name)).toBeVisible()
  await ui.treeItem(name).getByRole('button', { name: `Expand ${name}` }).click()
  const editor = page.locator(`[data-tree-key^="window:host/${name}/"]`).filter({ hasText: '2: editor' })
  await expect(editor).toBeVisible()
  const editorID = (await editor.getAttribute('data-tree-window'))!
  await editor.locator('button').last().click()
  await ui.waitForTerminal(name)
  await expect.poll(() => ui.termText(name)).toContain(marker)
  expect(await target.display(name, '#{window_index}')).toBe('1')

  await ui.showList()
  await expect(editor).toBeVisible()
  await editor.getByRole('button', { name: 'Expand window 2' }).click()
  const pane = page.locator(`[data-tree-key^="pane:host/${name}/${editorID}/"]`).nth(1)
  await expect(pane).toBeVisible()
  const paneID = (await pane.getAttribute('data-tree-pane'))!
  await pane.locator('button').click()
  await expect.poll(() => target.display(name, '#{window_index} #{pane_index} #{pane_id}')).toBe(`1 1 ${paneID}`)
})
