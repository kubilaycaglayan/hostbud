import { devices } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

const PORTRAIT = devices['iPhone 13 Pro'].viewport
const LANDSCAPE = devices['iPhone 13 Pro landscape'].viewport

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
  await target.resetTmux()
})

async function assertTouchTargets(page: import('@playwright/test').Page) {
  const undersized = await page.locator('button:visible, a[href]:visible, [role="tab"]:visible, [role="menuitem"]:visible, [role="menuitemradio"]:visible, [role="treeitem"]:visible, label:has(input[type="radio"]):visible, input:visible:not([type="radio"]), select:visible').evaluateAll((nodes) =>
    nodes.flatMap((node) => {
      const rect = node.getBoundingClientRect()
      return rect.width >= 44 && rect.height >= 44 ? [] : [`${node.tagName}.${(node as HTMLElement).className}: ${rect.width}x${rect.height}`]
    }),
  )
  expect(undersized).toEqual([])
}

test('(T3) Touch targets', async ({ page, target, ui }) => {
  const session = uniqueName('e2e-touch')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await ui.open()
  await assertTouchTargets(page)
  await ui.openTerminal(session)
  await assertTouchTargets(page)

  await ui.showList()
  await assertTouchTargets(page)
  await page.keyboard.press('Escape')
  await page.getByRole('banner').getByRole('button', { name: 'New session', exact: true }).tap()
  const create = page.getByRole('dialog', { name: 'New session' })
  await expect(create).toBeVisible()
  await assertTouchTargets(page)
  await page.getByRole('button', { name: 'Cancel' }).tap()

  await ui.showList()
  await ui.sessionAction(session, 'Rename', true)
  await assertTouchTargets(page)
  // Rename is inline (M5 T4): Escape cancels it.
  await page.getByRole('textbox', { name: `Rename ${session}` }).press('Escape')
  await ui.sessionAction(session, 'Kill…', true)
  await assertTouchTargets(page)
  await page.getByRole('button', { name: 'Cancel' }).tap()

  await ui.openFileBrowser()
  await expect(page.getByRole('dialog', { name: 'Browse files' })).toBeVisible()
  await assertTouchTargets(page)
})

test('(T3) Usable without zoom', async ({ page, ui }) => {
  await ui.open()
  for (const viewport of [PORTRAIT, LANDSCAPE]) {
    await page.setViewportSize(viewport)
    const result = await page.evaluate(() => ({
      scale: window.visualViewport?.scale ?? 1,
      width: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      smallInputs: [...document.querySelectorAll('input:not([type="radio"]), textarea, select')]
        .filter((el) => Number.parseFloat(getComputedStyle(el).fontSize) < 16)
        .map((el) => `${el.tagName}.${(el as HTMLElement).className}`),
    }))
    expect(result.scale).toBe(1)
    expect(result.width).toBeLessThanOrEqual(result.clientWidth)
    expect(result.smallInputs).toEqual([])
  }
})

test('(T14) Touch targets and zoom for M6 controls', async ({ page, target, ui }) => {
  const account = newAccount('e2e-touch-m6')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)

  const project = uniqueName('e2e-touch-project')
  const path = `/home/dev/${project}`
  const session = uniqueName('e2e-touch-session')
  await target.run(`mkdir -p ${path}`)
  await target.tmux('new-session', '-d', '-s', session, '-c', path)
  await target.tmux('new-window', '-d', '-t', `=${session}:`, '-n', 'editor')
  await target.tmux('split-window', '-d', '-t', `=${session}:1`)

  const browser = await ui.openFileBrowser()
  await browser.getByLabel('Current path').fill(path)
  await browser.getByRole('button', { name: 'Go', exact: true }).click()
  await browser.getByRole('button', { name: 'Add this directory as project' }).click()
  await browser.getByRole('button', { name: 'Close file browser' }).click()
  await ui.openTerminal(session)
  await ui.showList()
  await assertTouchTargets(page)

  await ui.treeItem(session).getByRole('button', { name: `Expand ${session}` }).click()
  const windows = page.locator(`[data-tree-key^="window:host/${session}/"]`)
  await expect(windows).toHaveCount(2)
  await assertTouchTargets(page)
  await windows.filter({ hasText: 'editor' }).getByRole('button', { name: 'Expand window 2' }).click()
  await expect(page.locator(`[data-tree-key^="pane:host/${session}/"]`)).toHaveCount(2)
  await assertTouchTargets(page)

  const projectHeader = ui.treeItem(project).locator(':scope > .tree-row')
  await projectHeader.dispatchEvent('pointerdown', { pointerType: 'touch', clientX: 12, clientY: 12 })
  await page.waitForTimeout(550)
  await projectHeader.dispatchEvent('pointerup', { pointerType: 'touch', clientX: 12, clientY: 12 })
  const projectMenu = page.getByRole('menu')
  await expect(projectMenu.getByRole('menuitem', { name: 'Rename', exact: true })).toBeVisible()
  await expect(projectMenu.getByRole('menuitem', { name: 'Hide', exact: true })).toBeVisible()
  await expect(projectMenu.getByRole('menuitem', { name: 'Pin', exact: true })).toBeVisible()
  await assertTouchTargets(page)
  await projectMenu.getByRole('menuitem', { name: 'Pin', exact: true }).click()
  await expect(ui.treeItem(project).getByRole('button', { name: `Unpin ${project}` })).toBeVisible()
  await assertTouchTargets(page)
  await projectHeader.dispatchEvent('pointerdown', { pointerType: 'touch', clientX: 12, clientY: 12 })
  await page.waitForTimeout(550)
  await projectHeader.dispatchEvent('pointerup', { pointerType: 'touch', clientX: 12, clientY: 12 })
  const pinnedMenu = page.getByRole('menu')
  await expect(pinnedMenu.getByRole('menuitem', { name: 'Unpin', exact: true })).toBeVisible()
  await pinnedMenu.getByRole('menuitem', { name: 'Unpin', exact: true }).click()

  const sessionRow = ui.treeItem(session).locator('button[data-session-row]')
  await sessionRow.dispatchEvent('pointerdown', { pointerType: 'touch', pointerId: 1, clientX: 90, clientY: 180 })
  await page.waitForTimeout(550)
  await sessionRow.dispatchEvent('pointerup', { pointerType: 'touch', pointerId: 1, clientX: 90, clientY: 180 })
  const sessionMenu = page.getByRole('menu')
  await expect(sessionMenu.getByRole('menuitem', { name: 'Rename', exact: true })).toBeVisible()
  await expect(sessionMenu.getByRole('menuitem', { name: 'Hide', exact: true })).toBeVisible()
  await assertTouchTargets(page)
  await sessionMenu.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await page.getByRole('button', { name: 'Show hidden (1)' }).click()
  await ui.treeItem(session).locator('button[data-session-row]').dispatchEvent('pointerdown', { pointerType: 'touch', pointerId: 1, clientX: 90, clientY: 180 })
  await page.waitForTimeout(550)
  await ui.treeItem(session).locator('button[data-session-row]').dispatchEvent('pointerup', { pointerType: 'touch', pointerId: 1, clientX: 90, clientY: 180 })
  const hiddenMenu = page.getByRole('menu')
  await expect(hiddenMenu.getByRole('menuitem', { name: 'Unhide', exact: true })).toBeVisible()
  await assertTouchTargets(page)
  await hiddenMenu.getByRole('menuitem', { name: 'Unhide', exact: true }).click()

  await ui.sessionAction(session, 'Rename')
  const editor = page.getByRole('textbox', { name: `Rename ${session}` })
  await expect(editor).toHaveCSS('font-size', '16px')
  for (const viewport of [PORTRAIT, LANDSCAPE]) {
    await page.setViewportSize(viewport)
    await assertTouchTargets(page)
    const state = await page.evaluate(() => ({
      scale: window.visualViewport?.scale ?? 1,
      width: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      smallInputs: [...document.querySelectorAll('input:not([type="radio"]), textarea, select')]
        .filter((input) => Number.parseFloat(getComputedStyle(input).fontSize) < 16)
        .map((input) => `${input.tagName}.${(input as HTMLElement).className}`),
    }))
    expect(state.scale).toBe(1)
    expect(state.width).toBeLessThanOrEqual(state.clientWidth)
    expect(state.smallInputs).toEqual([])
  }
  await editor.press('Escape')

  await page.getByRole('button', { name: 'Command palette' }).click()
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await expect(palette).toBeVisible()
  await assertTouchTargets(page)
  await expect(palette.getByRole('combobox', { name: 'Command palette' })).toHaveCSS('font-size', '16px')
  await page.keyboard.press('Escape')

  await ui.openAccountMenu()
  await expect(page.getByRole('group', { name: 'Theme' }).getByRole('radio')).toHaveCount(3)
  await assertTouchTargets(page)
})

test('(T3) Long-press row menu', async ({ page, target, ui }) => {
  const session = uniqueName('e2e-longpress')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await ui.open()
  const row = page.getByRole('button', { name: session, exact: true })
  await row.dispatchEvent('pointerdown', { pointerType: 'touch', pointerId: 1, clientX: 80, clientY: 180, bubbles: true })
  await page.waitForTimeout(550)
  await row.dispatchEvent('pointerup', { pointerType: 'touch', pointerId: 1, clientX: 80, clientY: 180, bubbles: true })
  await expect(page.getByRole('menuitem', { name: 'Open in split right' })).toBeVisible()
  await page.keyboard.press('Escape')
  await row.tap()
  await ui.waitForTerminal(session)
})
