import { expect, test } from '../helpers/fixtures.ts'

test('(T7) Theme and status-bar metadata are present on phones', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('meta[name="theme-color"]')).toHaveCount(2)
  await expect(page.locator('meta[name="mobile-web-app-capable"]')).toHaveAttribute('content', 'yes')
  await expect(page.locator('meta[name="apple-mobile-web-app-capable"]')).toHaveAttribute('content', 'yes')
  await expect(page.locator('meta[name="apple-mobile-web-app-title"]')).toHaveAttribute('content', 'hostbud')
  await expect(page.locator('meta[name="apple-mobile-web-app-status-bar-style"]')).toHaveAttribute('content', 'black-translucent')
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute('content', /viewport-fit=cover/)
})
