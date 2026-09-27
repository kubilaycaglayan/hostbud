import { expect, test } from '../helpers/fixtures.ts'
import { openShell } from '../helpers/shell.ts'
import { uniqueName } from '../helpers/target.ts'

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
  await target.resetTmux()
})

test('(T8) Dictation commits clean terminal input across successive phrases', async ({ page, ui, target }) => {
  const session = await openShell(ui, target, 'e2e-dictation')
  const input = page.getByRole('textbox', { name: 'Terminal input' })

  async function dictate(command: string) {
    await input.evaluate((element, text) => {
      const textarea = element as HTMLTextAreaElement
      textarea.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
      textarea.value = text
      textarea.dispatchEvent(new CompositionEvent('compositionupdate', { bubbles: true, data: text }))
    }, command)
    await page.waitForTimeout(10)
    await input.evaluate((element, text) => {
      element.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: text }))
    }, command)
    await expect.poll(() => input.inputValue()).toBe('')
    await input.press('Enter')
  }

  const first = uniqueName('spoken-first')
  const second = uniqueName('spoken-second')
  await dictate(`printf '%s\\n' ${first}`)
  await expect.poll(() => target.capture(session)).toContain(first)
  await dictate(`printf '%s\\n' ${second}`)
  const output = await target.capture(session)
  expect(output.split('\n').filter((line) => line.trimEnd() === first)).toHaveLength(1)
  expect(output.split('\n').filter((line) => line.trimEnd() === second)).toHaveLength(1)
})
