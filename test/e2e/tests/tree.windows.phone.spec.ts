import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test.describe('window tree on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  async function account(ui: import('../helpers/ui.ts').UI) {
    const fresh = newAccount('e2e-tree-windows-phone')
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
    const name = uniqueName('tree-phone-windows')
    await createSplitSession(target, name)
    let requests = 0
    page.on('request', (req) => {
      if (new URL(req.url()).pathname.endsWith(`/sessions/${name}/windows`)) requests++
    })
    await page.reload()
    await ui.showList()
    await expect(ui.treeItem(name)).toBeVisible()
    expect(requests).toBe(0)
    await ui.treeItem(name).getByRole('button', { name: `Expand ${name}` }).click()
    await expect.poll(() => requests).toBe(1)
    const windows = page.locator(`[data-tree-key^="window:host/${name}/"]`)
    await expect(windows).toHaveCount(2)
    const targetRows = (await target.tmux('list-windows', '-t', `=${name}`, '-F', '#{window_index}\t#{window_name}'))
      .trim().split('\n').map((row) => row.split('\t'))
      .map(([index, title]) => `${Number(index) + 1}: ${title}`)
    await expect.poll(async () => (await windows.allTextContents()).map((text) => text.trim().replace(/\s+\(current\)$/u, ''))).toEqual(targetRows)
    await windows.nth(1).getByRole('button', { name: 'Expand window 2' }).click()
    const panes = page.locator(`[data-tree-key^="pane:host/${name}/"]`)
    await expect(panes).toHaveCount(2)
    const positions = await Promise.all([ui.treeItem(name).boundingBox(), windows.nth(0).boundingBox(), panes.nth(0).boundingBox()])
    expect(positions[0]?.x).toBeLessThan(positions[1]?.x ?? 0)
    expect(positions[1]?.x).toBeLessThan(positions[2]?.x ?? 0)
    await ui.waitForSave('tree')
    await page.reload()
    await ui.showList()
    await expect(windows).toHaveCount(2)
    await expect(page.locator(`[data-tree-key^="pane:host/${name}/"]`)).toHaveCount(2)
    await ctl.restartApp()
    await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
    await page.reload()
    await ui.showList()
    await expect(windows).toHaveCount(2)
  })

  test('(T3) Open at a window and pane', async ({ page, ui, target }) => {
    await account(ui)
    const name = uniqueName('tree-phone-select')
    await createSplitSession(target, name)
    const marker = uniqueName('PHONE_WINDOW_MARKER')
    await target.tmux('send-keys', '-t', `=${name}:1.0`, `printf ${shq(marker)}`, 'Enter')
    await page.reload()
    await ui.showList()
    await ui.treeItem(name).getByRole('button', { name: `Expand ${name}` }).click()
    const editor = page.locator(`[data-tree-key^="window:host/${name}/"]`).filter({ hasText: '2: editor' })
    await expect(editor).toBeVisible()
    await editor.locator('button').last().click()
    await ui.waitForTerminal(name)
    await expect.poll(() => ui.termText(name)).toContain(marker)
    expect(await target.display(name, '#{window_index}')).toBe('1')

    await ui.showList()
    await editor.getByRole('button', { name: 'Expand window 2' }).click()
    const panes = page.locator(`[data-tree-key^="pane:host/${name}/"]`)
    await expect(panes).toHaveCount(2, { timeout: 15_000 })
    const pane = panes.nth(1)
    await expect(pane).toBeVisible()
    const paneID = (await pane.getAttribute('data-tree-pane'))!
    await pane.locator('button').click()
    await ui.waitForTerminal(name)
    await expect.poll(() => target.display(name, '#{window_index} #{pane_index} #{pane_id}')).toBe(`1 1 ${paneID}`)
  })
})
