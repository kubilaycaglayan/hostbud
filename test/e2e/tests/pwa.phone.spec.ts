import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'
import { openShell } from '../helpers/shell.ts'

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

test('(M8 T35) Installed PWA landscape shows only the tmux terminal', async ({ page, ui, target, request, baseURL }, testInfo) => {
  await page.addInitScript(() => {
    const nativeMatchMedia = window.matchMedia.bind(window)
    window.matchMedia = (query: string) => query === '(display-mode: standalone)'
      ? ({ matches: true, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false } } as MediaQueryList)
      : nativeMatchMedia(query)
  })
  await target.resetTmux()
  const session = uniqueName('pwa-landscape')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await target.tmux('send-keys', '-t', session, 'echo LANDSCAPE-TMUX-CONTENT', 'Enter')
  const paneId = 'landscape-pane'
  const saved = await request.put('/api/ui-state/layout', {
    data: {
      version: 1,
      tabs: [{ id: 'landscape-tab', root: { type: 'pane', id: paneId, machine: 'host', session }, focusedPane: paneId }],
      activeTab: 'landscape-tab',
    },
    headers: { Origin: new URL(baseURL!).origin },
  })
  expect(saved.status()).toBe(204)
  await page.setViewportSize({ width: 844, height: 390 })
  await ui.open()

  const terminal = page.getByRole('region', { name: `Terminal: ${session}` })
  await expect(terminal).toBeVisible()
  const expectLandscape = async () => {
    await expect(page.locator('.pwa-landscape-terminal')).toBeVisible()
    await expect(page.locator('header')).toBeHidden()
    await expect(page.locator('[data-terminal-header]')).toBeHidden()
    await expect(page.getByTestId('key-bar')).toBeHidden()
    await expect(page.getByRole('button')).toHaveCount(0)
    await expect(terminal).toBeVisible()
    await expect.poll(() => ui.termText(session)).toContain('LANDSCAPE-TMUX-CONTENT')
    await expect(page.locator('.xterm-helper-textarea')).not.toBeFocused()
    await expect.poll(async () => {
      const box = await terminal.boundingBox()
      return box ? { top: Math.round(box.y), height: Math.round(box.height) } : null
    }).toEqual({ top: 0, height: 390 })
  }
  await expectLandscape()

  // Rotation back to portrait restores the regular PWA controls; entering
  // landscape again hides them without detaching the live tmux window.
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.locator('.pwa-landscape-terminal')).toHaveCount(0)
  await expect(page.locator('header')).toBeVisible()
  await expect(page.getByTestId('key-bar')).toBeVisible()

  // Rotating while the native keyboard is open dismisses it and leaves the
  // terminal as the only visible view.
  await page.getByRole('button', { name: 'Show keyboard' }).click()
  await expect(page.locator('.xterm-helper-textarea')).toBeFocused()
  await page.setViewportSize({ width: 844, height: 390 })
  await expectLandscape()

  // A dialog opened in portrait also closes when entering the fullscreen view.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'New session' }).first().click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.setViewportSize({ width: 844, height: 390 })
  await expectLandscape()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  // Reload while landscape with a saved terminal layout. It remains fullscreen
  // and does not focus xterm to reopen the native keyboard.
  await expect.poll(async () => {
    const response = await request.get('/api/ui-state/layout')
    if (!response.ok()) return false
    const layout = await response.json() as { tabs?: { root?: { session?: string } }[] }
    return layout.tabs?.some((tab) => JSON.stringify(tab.root).includes(session)) ?? false
  }).toBe(true)
  await page.reload()
  await expectLandscape()
  await page.screenshot({ path: testInfo.outputPath('landscape-terminal.png') })
})

test('(M8 T36) Installed PWA reports extra landscape rows below the viewport', async ({ page, ui, target }, testInfo) => {
  await page.addInitScript(() => {
    const nativeMatchMedia = window.matchMedia.bind(window)
    window.matchMedia = (query: string) => query === '(display-mode: standalone)'
      ? ({ matches: true, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false } } as MediaQueryList)
      : nativeMatchMedia(query)
  })
  const session = await openShell(ui, target, 'pwa-landscape-rows')
  const terminal = page.getByRole('region', { name: `Terminal: ${session}` })
  await expect(terminal).toBeVisible()

  await page.setViewportSize({ width: 844, height: 390 })
  await expect(page.locator('.pwa-landscape-terminal')).toBeVisible()
  await expect.poll(async () => {
    const box = await page.getByTestId('terminal').boundingBox()
    return box?.height ?? 0
  }).toBeGreaterThan(520)
  await expect.poll(() => page.evaluate(() => window.__hostbud!.termSize().rows)).toBeGreaterThanOrEqual(27)
  await expect.poll(async () => {
    const box = await terminal.boundingBox()
    return box ? { top: Math.round(box.y), height: Math.round(box.height) } : null
  }).toEqual({ top: 0, height: 390 })
  await expect(page.locator('.xterm-helper-textarea')).not.toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('landscape-extra-rows.png') })
})

test('(M8 T34) Installed iPhone PWA repeats Backspace while it is held', async ({ page, ui, target }) => {
  await page.addInitScript(() => {
    const nativeMatchMedia = window.matchMedia.bind(window)
    window.matchMedia = (query: string) => query === '(display-mode: standalone)'
      ? ({ matches: true, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false } } as MediaQueryList)
      : nativeMatchMedia(query)
  })
  const name = await openShell(ui, target, 'pwa-backspace')
  await ui.type('echo abcdef')
  await page.keyboard.down('Backspace')
  await page.waitForTimeout(700)
  await page.keyboard.up('Backspace')
  await ui.type('Z', true)
  await expect.poll(() => target.capture(name)).toMatch(/^abZ$/m)
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
