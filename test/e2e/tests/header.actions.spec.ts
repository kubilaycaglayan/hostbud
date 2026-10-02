import type { Locator } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'

// Header actions and compact controls (M8 T1): New session and Browse files
// follow the host name in the app bar, open their dialogs by pointer and
// keyboard, and keep a usable target while their visual box is compact.

async function box(locator: Locator) {
  const b = await locator.boundingBox()
  if (!b) throw new Error('not visible')
  return b
}

test('(T1) Header actions and compact controls', async ({ page, ui, isMobile }) => {
  await ui.open()
  const banner = page.getByRole('banner')
  const host = banner.getByRole('heading', { name: 'hostbud' })
  const create = banner.getByRole('button', { name: 'New session', exact: true })
  const browse = banner.getByRole('button', { name: 'Browse files', exact: true })
  await expect(create).toBeVisible()
  await expect(browse).toBeVisible()

  // Order: host name, New session, Browse files, in one row.
  const hostBox = await box(host)
  const createBox = await box(create)
  const browseBox = await box(browse)
  expect(createBox.x).toBeGreaterThan(hostBox.x + hostBox.width - 1)
  expect(browseBox.x).toBeGreaterThan(createBox.x + createBox.width - 1)
  expect(Math.abs(createBox.y + createBox.height / 2 - (browseBox.y + browseBox.height / 2))).toBeLessThan(2)
  // Nothing sits between the host name and the actions.
  expect(createBox.x - (hostBox.x + hostBox.width)).toBeLessThan(24)

  // No duplicates left in the tree panel.
  await expect(page.getByRole('button', { name: 'New session', exact: true })).toHaveCount(1)
  await expect(page.getByRole('button', { name: 'Add project', exact: true })).toHaveCount(0)

  // Compact visuals: the bordered box is the icon plus ~4 px padding; touch
  // screens still get a 44 px target, desktop a small one.
  for (const button of [create, browse, banner.locator('button[aria-controls="sessions-sidebar"]')]) {
    if (!(await button.isVisible())) continue
    const visual = await box(button.locator('[data-icon-box]'))
    expect(visual.width).toBeLessThanOrEqual(30)
    expect(visual.height).toBeLessThanOrEqual(30)
    const target = await box(button)
    if (isMobile) {
      expect(target.width).toBeGreaterThanOrEqual(44)
      expect(target.height).toBeGreaterThanOrEqual(44)
    } else {
      expect(target.width).toBeLessThanOrEqual(30)
    }
  }
  await page.screenshot({ path: test.info().outputPath(`header-actions-${isMobile ? 'phone' : 'desktop'}.png`) })

  // Pointer.
  if (isMobile) await create.tap()
  else await create.click()
  const createDialog = page.getByRole('dialog', { name: 'New session' })
  await expect(createDialog).toBeVisible()
  await createDialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(createDialog).toBeHidden()
  if (isMobile) await browse.tap()
  else await browse.click()
  const files = page.getByRole('dialog', { name: 'Browse files' })
  await expect(files).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(files).toBeHidden()

  // Keyboard: focus is visible and Enter/Space open the dialogs.
  await create.focus()
  await expect(create).toBeFocused()
  const ring = await create.evaluate((el) => getComputedStyle(el).boxShadow)
  expect(ring).not.toBe('none')
  await page.keyboard.press('Enter')
  await expect(createDialog).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(createDialog).toBeHidden()
  await browse.focus()
  await page.keyboard.press(' ')
  await expect(files).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(files).toBeHidden()
})

test('(T29) Focus mode covers the web app and closes on a tap', async ({ page, ui, isMobile }) => {
  await ui.open()
  const trigger = page.getByRole('banner').getByRole('button', { name: 'Focus mode' })
  await expect(trigger).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width)
  if (isMobile) await trigger.tap()
  else await trigger.click()

  const overlay = page.getByRole('button', { name: 'Exit focus mode' })
  await expect(overlay).toBeVisible()
  await expect(overlay).toHaveText('focus')
  await expect(overlay).toBeFocused()
  const bounds = await box(overlay)
  const viewport = page.viewportSize()!
  expect(bounds.x).toBe(0)
  expect(bounds.y).toBe(0)
  expect(bounds.width).toBeGreaterThanOrEqual(viewport.width)
  expect(bounds.height).toBeGreaterThanOrEqual(viewport.height)
  const word = await box(overlay.locator('span'))
  expect(Math.abs(word.x + word.width / 2 - viewport.width / 2)).toBeLessThan(2)
  expect(Math.abs(word.y + word.height / 2 - viewport.height / 2)).toBeLessThan(2)

  if (isMobile) await overlay.tap({ position: { x: 20, y: 20 } })
  else await overlay.click({ position: { x: 20, y: 20 } })
  await expect(overlay).toHaveCount(0)
  await expect(trigger).toBeFocused()
})
