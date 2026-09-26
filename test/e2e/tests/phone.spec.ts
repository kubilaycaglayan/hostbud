import { devices, type Page } from '@playwright/test'
import { forbidInLogs } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Phone usability (M2 T3). Runs in the phone projects only: iphone-13-pro on
// the port-forward path and iphone-13-pro-domain on the HTTPS domain path.
// The user taps instead of clicking and types like an on-screen keyboard:
// text arrives as input events without key presses; Enter is a key.

const PORTRAIT = devices['iPhone 13 Pro'].viewport
const LANDSCAPE = devices['iPhone 13 Pro landscape'].viewport

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
  await target.resetTmux()
})

async function newSession(target: Target, prefix: string) {
  const name = uniqueName(prefix)
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  return name
}

const attached = (target: Target, name: string) => target.display(name, '#{session_attached}')
const windowSize = (target: Target, name: string) => target.display(name, '#{window_width}x#{window_height}')
const input = (page: Page) => page.getByRole('textbox', { name: 'Terminal input' })

/** Taps a session in the list and waits for its terminal to attach. */
async function tapSession(ui: UI, target: Target, name: string) {
  const back = ui.page.getByRole('button', { name: 'Back to sessions' })
  if (await back.isVisible()) await back.tap()
  await ui.page.getByRole('button', { name, exact: true }).tap()
  await expect(ui.page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
  await ui.page.waitForFunction(() => window.__hostbud !== undefined)
  await expect.poll(() => attached(target, name), { timeout: 10_000 }).toBe('1')
}

/** Taps the terminal (which focuses its input: the keyboard comes up), then
 * types the line like an on-screen keyboard and presses Enter. */
async function softType(page: Page, line: string) {
  await page.getByTestId('terminal').tap()
  await expect(input(page)).toBeFocused()
  await page.keyboard.insertText(line)
  await page.keyboard.press('Enter')
}

// Phone attach and type (T3)
test('phone attach and type: tap a session and the terminal, type with the on-screen keyboard', async ({
  ui,
  target,
}) => {
  const name = await newSession(target, 'e2e-ph')
  const marker = uniqueName('e2e-mark')
  forbidInLogs(marker)
  await ui.open()
  await tapSession(ui, target, name)

  await softType(ui.page, `echo ${marker}-$((6*7))`)
  // The shell ran it: the output (not just the echoed command line) shows.
  await expect.poll(() => target.capture(name)).toContain(`${marker}-42`)
  await expect.poll(() => ui.termText()).toContain(`${marker}-42`)

  // Show keyboard focuses the terminal input again after it lost focus.
  await input(ui.page).blur()
  await ui.page.getByRole('button', { name: 'Show keyboard' }).tap()
  await expect(input(ui.page)).toBeFocused()
})

// Switch sessions (T3)
test('switch sessions: back to the list, open another session, type there', async ({ ui, target }) => {
  const first = await newSession(target, 'e2e-sw1')
  const second = await newSession(target, 'e2e-sw2')
  const marker = uniqueName('e2e-mark')
  forbidInLogs(marker)
  await ui.open()
  await tapSession(ui, target, first)

  await tapSession(ui, target, second)
  await softType(ui.page, `echo ${marker}`)
  await expect.poll(() => target.capture(second)).toContain(marker)
  expect(await target.capture(first)).not.toContain(marker)
  await expect.poll(() => attached(target, first)).toBe('0')
})

// Rotate (T3)
test('rotate: portrait → landscape → portrait resizes the tmux window', async ({ page, ui, target }) => {
  const name = await newSession(target, 'e2e-rot')
  await ui.open()
  await tapSession(ui, target, name)
  await expect.poll(() => windowSize(target, name)).not.toBe('80x24') // fitted to the phone
  const portrait = await windowSize(target, name)

  await page.setViewportSize(LANDSCAPE)
  await expect.poll(() => windowSize(target, name)).not.toBe(portrait)
  const landscape = await windowSize(target, name)
  const [pw, ph] = portrait.split('x').map(Number)
  const [lw, lh] = landscape.split('x').map(Number)
  expect(lw).toBeGreaterThan(pw)
  expect(lh).toBeLessThan(ph)

  await page.setViewportSize(PORTRAIT)
  await expect.poll(() => windowSize(target, name)).toBe(portrait)
})

// Fits the viewport (T3)
test('fits the viewport: in both orientations, no horizontal scroll, 16px input', async ({ page, ui, target }) => {
  const name = await newSession(target, 'e2e-fit')
  await ui.open()
  await tapSession(ui, target, name)

  for (const viewport of [PORTRAIT, LANDSCAPE]) {
    await page.setViewportSize(viewport)
    await expect
      .poll(() =>
        page.evaluate(() => {
          const term = document.querySelector('[data-testid=terminal]')!.getBoundingClientRect()
          const screen = document.querySelector('.xterm-screen')!.getBoundingClientRect()
          const app = document.getElementById('app')!
          return {
            inside: term.left >= 0 && term.top >= 0 && term.right <= innerWidth && term.bottom <= innerHeight,
            screenFits: screen.width <= term.width && screen.height <= term.height,
            noHorizontalScroll:
              app.scrollWidth <= app.clientWidth && document.documentElement.scrollWidth <= innerWidth,
          }
        }),
      )
      .toEqual({ inside: true, screenFits: true, noHorizontalScroll: true })
  }
  const fontSize = await input(page).evaluate((el) => parseFloat(getComputedStyle(el).fontSize))
  expect(fontSize).toBeGreaterThanOrEqual(16)
})
