import { devices } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

const PORTRAIT = devices['iPhone 13 Pro'].viewport
const LANDSCAPE = devices['iPhone 13 Pro landscape'].viewport

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
  await target.resetTmux()
})

async function assertTouchTargets(page: import('@playwright/test').Page) {
  const undersized = await page.locator('button:visible, a[href]:visible, [role="tab"]:visible, [role="menuitem"]:visible, [role="treeitem"]:visible, input:visible, select:visible').evaluateAll((nodes) =>
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

  await page.getByRole('button', { name: 'Show project tree' }).tap()
  await assertTouchTargets(page)
  await page.getByRole('button', { name: 'New session', exact: true }).tap()
  const create = page.getByRole('dialog', { name: 'New session' })
  await expect(create).toBeVisible()
  await assertTouchTargets(page)
  await page.getByRole('button', { name: 'Cancel' }).tap()

  await page.getByRole('button', { name: 'Show project tree' }).tap()
  await page.getByRole('button', { name: `Rename ${session}` }).tap()
  await assertTouchTargets(page)
  await page.getByRole('button', { name: 'Cancel' }).tap()
  await page.getByRole('button', { name: `Kill ${session}` }).tap()
  await assertTouchTargets(page)
  await page.getByRole('button', { name: 'Cancel' }).tap()

  await page.getByRole('button', { name: 'Browse files' }).tap()
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
      smallInputs: [...document.querySelectorAll('input, textarea, select')]
        .filter((el) => Number.parseFloat(getComputedStyle(el).fontSize) < 16)
        .map((el) => `${el.tagName}.${(el as HTMLElement).className}`),
    }))
    expect(result.scale).toBe(1)
    expect(result.width).toBeLessThanOrEqual(result.clientWidth)
    expect(result.smallInputs).toEqual([])
  }
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
