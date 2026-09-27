import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { openShell, promptLine, textRect } from '../helpers/shell.ts'
import { installPrompt, promptCaret, PROMPT_LINES, PROMPT_TITLE } from '../helpers/prompt.ts'

// Option-click caret placement (M8 T5). Option is Alt in the browser; the
// desktop project runs Chromium, where Alt+click is the same gesture.

/** Option/Alt-clicks the center of the `index`-th character of the last
 * on-screen occurrence of `text` in the browser terminal. */
async function altClickCell(page: Page, text: string, index: number) {
  const r = await textRect(page, text)
  const cell = r.width / text.length
  await page.keyboard.down('Alt')
  await page.mouse.click(r.x + (index + 0.5) * cell, r.y + r.height / 2)
  await page.keyboard.up('Alt')
}

/** The page position of character `index` in a text input (canvas-measured
 * with the input's own font). */
async function inputCharPoint(page: Page, label: string, index: number) {
  return page.getByLabel(label).evaluate((el, i) => {
    const input = el as HTMLInputElement
    const cs = getComputedStyle(input)
    const ctx = document.createElement('canvas').getContext('2d')!
    ctx.font = cs.font
    const rect = input.getBoundingClientRect()
    const left = rect.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft) - input.scrollLeft
    const x = left + ctx.measureText(input.value.slice(0, i)).width + 1
    return { x, y: rect.top + rect.height / 2 }
  }, index)
}

test.beforeEach(({ isMobile }) => {
  test.skip(isMobile, 'Option/Alt-click is a desktop pointer gesture')
})

test('(T5) Option-click caret placement', async ({ page, ui, target }) => {
  test.setTimeout(90_000)
  const name = await openShell(ui, target, 'e2e-caret')

  // Single-line: a shell prompt, same row.
  await ui.type('echo abcdefghij')
  await expect.poll(() => promptLine(target, name)).toMatch(/echo abcdefghij$/)
  await altClickCell(page, 'abcdefghij', 5) // "f"
  await page.waitForTimeout(300)
  await ui.type('X')
  await expect.poll(() => promptLine(target, name)).toMatch(/echo abcdeXfghij$/)
  await page.keyboard.press('Control+e') // Ctrl-U kills only left of the caret, which the click moved
  await page.keyboard.press('Control+u')

  // Single-line, soft-wrapped: the click is a row above the caret.
  const { cols } = await page.evaluate(() => window.__hostbud!.termSize())
  const long = 'a'.repeat(10) + 'MARK' + 'b'.repeat(cols)
  await ui.type(`echo ${long}`)
  await altClickCell(page, 'aMARKb', 1) // "M"
  await page.waitForTimeout(500)
  await ui.type('X')
  await expect.poll(async () => (await target.capture(name)).includes('aaaXMARKbbb')).toBe(true)
  await page.keyboard.press('Control+e') // Ctrl-U kills only left of the caret, which the click moved
  await page.keyboard.press('Control+u')

  // Multiline: a bordered prompt box; clicks land on the character and line
  // clicked, and never send up/down.
  const state = `/tmp/hostbud-caret-${name}`
  await ui.type(await installPrompt(target, state), true)
  await expect.poll(() => ui.termText()).toContain(PROMPT_TITLE)
  await expect.poll(() => promptCaret(target, state)).toBe('3 3')
  const clicks: [line: number, col: number][] = [[0, 12], [2, 5], [1, 3], [3, 0], [0, 0], [2, 16]]
  for (const [line, col] of clicks) {
    await altClickCell(page, PROMPT_LINES[line], col)
    await expect.poll(() => promptCaret(target, state)).toBe(`${line} ${col}`)
  }
  // Right of a shorter line: the caret goes to its end.
  const second = await textRect(page, 'second')
  await page.keyboard.down('Alt')
  await page.mouse.click(second.x + second.width * 3, second.y + second.height / 2)
  await page.keyboard.up('Alt')
  await expect.poll(() => promptCaret(target, state)).toBe('1 6')
  // Typing inserts at the clicked place.
  await altClickCell(page, 'third', 0)
  await expect.poll(() => promptCaret(target, state)).toBe('2 2')
  await ui.type('X')
  await expect.poll(() => page.evaluate(() => window.__hostbud?.termViewport() ?? '')).toContain('a Xthird line here')
  expect(await promptCaret(target, state)).not.toContain('vertical')
  // An ordinary click doesn't move the caret.
  const before = await promptCaret(target, state)
  const first = await textRect(page, 'first line')
  await page.mouse.click(first.x + 4, first.y + first.height / 2)
  await page.waitForTimeout(300)
  expect(await promptCaret(target, state)).toBe(before)
  await page.keyboard.press('Control+c')

  // An app text input: Option-click and an ordinary click place the caret
  // at the clicked character.
  await ui.headerAction('New session')
  const dialog = page.getByRole('dialog', { name: 'New session' })
  await dialog.getByLabel('Name').fill('abcdefghij')
  let p = await inputCharPoint(page, 'Name', 3)
  await page.keyboard.down('Alt')
  await page.mouse.click(p.x, p.y)
  await page.keyboard.up('Alt')
  expect(await dialog.getByLabel('Name').evaluate((el) => (el as HTMLInputElement).selectionStart)).toBe(3)
  await page.keyboard.type('X')
  await expect(dialog.getByLabel('Name')).toHaveValue('abcXdefghij')
  p = await inputCharPoint(page, 'Name', 8)
  await page.mouse.click(p.x, p.y)
  expect(await dialog.getByLabel('Name').evaluate((el) => (el as HTMLInputElement).selectionStart)).toBe(8)
  await dialog.getByRole('button', { name: 'Cancel' }).click()
})
