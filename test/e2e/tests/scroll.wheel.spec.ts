import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { openShell } from '../helpers/shell.ts'

// Readable terminal scrolling (M8 T6), desktop mouse wheel, both paths:
//  - tmux `mouse off`: the wheel scrolls xterm's own scrollback; each notch
//    animates row by row (smoothScrollDuration) instead of jumping;
//  - tmux `mouse on`: the wheel goes to tmux copy mode, whose redraws arrive
//    as synchronized updates (the browser's client attaches with -T sync).

// Distinct numbered lines between runs of identical ones.
const OUTPUT = `clear; for i in $(seq 1 300); do if [ $((i % 10)) -lt 6 ]; then echo 'repeated output line ======================'; else echo "numbered line $i"; fi; done`

/** The first numbered line shown and its row: the viewport's top in buffer
 * lines, comparable across frames. */
function topOf(viewport: string): number | null {
  const rows = viewport.split('\n')
  for (let r = 0; r < rows.length; r++) {
    const m = rows[r].match(/numbered line (\d+)/)
    if (m) return Number(m[1]) - r
  }
  return null
}

/** Samples the viewport on every animation frame for `ms`. */
async function sampleFrames(page: Page, ms: number, during: () => Promise<void>): Promise<string[]> {
  await page.evaluate((duration) => {
    const w = window as unknown as { __frames: string[] }
    w.__frames = []
    const end = performance.now() + duration
    const tick = () => {
      w.__frames.push(window.__hostbud?.termViewport() ?? '')
      if (performance.now() < end) requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  }, ms)
  await during()
  await page.waitForTimeout(ms)
  return page.evaluate(() => (window as unknown as { __frames: string[] }).__frames)
}

/** Samples xterm's rendered scroll offset, in terminal rows, per animation frame. */
async function sampleScrollRows(page: Page, ms: number, during: () => Promise<void>): Promise<number[]> {
  await page.evaluate((duration) => {
    const w = window as unknown as { __scrollRows: number[] }
    w.__scrollRows = []
    const viewport = document.querySelector('.xterm-viewport')
    const firstRow = document.querySelector('.xterm-rows > div')
    if (!(viewport instanceof HTMLElement) || !(firstRow instanceof HTMLElement)) return
    const rowHeight = firstRow.getBoundingClientRect().height
    const end = performance.now() + duration
    const tick = () => {
      w.__scrollRows.push(rowHeight ? viewport.scrollTop / rowHeight : 0)
      if (performance.now() < end) requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  }, ms)
  await during()
  await page.waitForTimeout(ms)
  return page.evaluate(() => (window as unknown as { __scrollRows: number[] }).__scrollRows)
}

test.beforeEach(({ isMobile }) => {
  test.skip(isMobile, 'mouse-wheel scenarios are desktop only')
})

test('(T6) Readable terminal scrolling', async ({ page, ui, target }) => {
  test.setTimeout(90_000)
  const name = await openShell(ui, target, 'e2e-wheel')
  await ui.type(OUTPUT, true)
  await expect.poll(() => ui.termText()).toContain('numbered line 299')
  const terminal = page.getByTestId('terminal')
  const box = (await terminal.boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  const bottom = topOf(await page.evaluate(() => window.__hostbud!.termViewport()))!
  await page.screenshot({ path: test.info().outputPath('wheel-before.png') })

  // mouse off, controlled notches: up moves toward older lines, one row per
  // frame at most, and reaches the same place as before the change.
  const upOffsets = await sampleScrollRows(page, 700, async () => {
    for (let i = 0; i < 4; i++) {
      await page.mouse.wheel(0, -100)
      await page.waitForTimeout(100)
    }
  })
  const afterUp = topOf(await page.evaluate(() => window.__hostbud!.termViewport()))!
  expect(afterUp).toBeLessThan(bottom)
  expect(upOffsets.length).toBeGreaterThan(1)
  for (let i = 1; i < upOffsets.length; i++) {
    expect(upOffsets[i]).toBeLessThanOrEqual(upOffsets[i - 1]) // never backwards
    expect(upOffsets[i - 1] - upOffsets[i]).toBeLessThanOrEqual(2) // no jumps over two rows per frame
  }
  await page.screenshot({ path: test.info().outputPath('wheel-scrolled-up.png') })
  expect(await target.display(name, '#{pane_in_mode}')).toBe('0')

  // Rapid notches: still monotonic, then back down to the live bottom.
  const rapid = await sampleFrames(page, 900, async () => {
    for (let i = 0; i < 8; i++) await page.mouse.wheel(0, -100)
  })
  const rapidTops = rapid.map(topOf).filter((t): t is number => t !== null)
  for (let i = 1; i < rapidTops.length; i++) expect(rapidTops[i]).toBeLessThanOrEqual(rapidTops[i - 1])
  const down = await sampleFrames(page, 1500, async () => {
    for (let i = 0; i < 40; i++) await page.mouse.wheel(0, 100)
  })
  const downTops = down.map(topOf).filter((t): t is number => t !== null)
  for (let i = 1; i < downTops.length; i++) expect(downTops[i]).toBeGreaterThanOrEqual(downTops[i - 1])
  await expect.poll(async () => topOf(await page.evaluate(() => window.__hostbud!.termViewport()))).toBe(bottom)

  // mouse on: tmux copy mode scrolls, both directions, with synchronized
  // redraws declared for this client.
  expect(await target.run(`tmux list-clients -t ${'=' + name} -F '#{client_termfeatures}'`)).toContain('sync')
  await target.tmux('set-option', '-t', name, 'mouse', 'on')
  for (let i = 0; i < 4; i++) {
    await page.mouse.wheel(0, -100)
    await page.waitForTimeout(80)
  }
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('1')
  const position = Number(await target.display(name, '#{scroll_position}'))
  expect(position).toBeGreaterThan(0)
  await expect.poll(() => page.evaluate(() => window.__hostbud!.termViewport())).toMatch(/numbered line \d+/)
  await page.screenshot({ path: test.info().outputPath('wheel-copy-mode.png') })
  await page.mouse.wheel(0, 100)
  await expect.poll(async () => Number(await target.display(name, '#{scroll_position}'))).toBeLessThan(position)
  for (let i = 0; i < 20; i++) await page.mouse.wheel(0, 100)
  await expect.poll(() => target.display(name, '#{pane_in_mode}')).toBe('0') // copy-mode -e leaves at the bottom
  await target.tmux('set-option', '-t', name, '-u', 'mouse')
})
