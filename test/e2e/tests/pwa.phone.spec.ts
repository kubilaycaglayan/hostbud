import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

test('(T33) Installed PWA keeps the in-app terminal shortcut buttons', async ({ page, ui, target }) => {
  await page.addInitScript(() => {
    const nativeMatchMedia = window.matchMedia.bind(window)
    window.matchMedia = (query: string) => query === '(display-mode: standalone)'
      ? ({ matches: true, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false } } as MediaQueryList)
      : nativeMatchMedia(query)
  })
  await target.resetTmux()
  const session = uniqueName('pwa-shortcuts')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await page.goto('/')
  await ui.tree().waitFor()
  await ui.openTerminal(session)
  await expect(page.getByRole('button', { name: 'Show keyboard' })).toBeVisible()
  await expect(page.getByTestId('key-bar')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Slash' })).toBeVisible()
})

test('(T7) Theme and status-bar metadata are present on phones', async ({ page }) => {
  await page.goto('/')
  // One meta the app updates to the resolved theme (M6 T7 replaced M5's two media variants).
  await expect(page.locator('meta[name="theme-color"]')).toHaveCount(1)
  await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', /^#(0f1115|ffffff)$/)
  await expect(page.locator('meta[name="mobile-web-app-capable"]')).toHaveAttribute('content', 'yes')
  await expect(page.locator('meta[name="apple-mobile-web-app-capable"]')).toHaveAttribute('content', 'yes')
  await expect(page.locator('meta[name="apple-mobile-web-app-title"]')).toHaveAttribute('content', 'hostbud')
  await expect(page.locator('meta[name="apple-mobile-web-app-status-bar-style"]')).toHaveAttribute('content', 'black-translucent')
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute('content', /viewport-fit=cover/)
})
