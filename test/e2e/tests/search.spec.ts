import { expect, test } from '../helpers/fixtures.ts'
import { openShell } from '../helpers/shell.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Search (M3 T5): xterm's buffer, i.e. the screen plus the scrollback the
// browser received since attaching. Markers are printed with $((…)) so the
// command line itself doesn't match.

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

const bar = (ui: UI) => ui.page.getByRole('search', { name: 'Search the terminal' })
const results = (ui: UI) => bar(ui).getByTestId('search-results')
const viewport = (ui: UI) => ui.page.evaluate(() => window.__hostbud?.termViewport() ?? '')

async function printed(target: Target, name: string, line: string) {
  await expect
    .poll(async () => (await target.capture(name)).split('\n').some((l) => l.trimEnd() === line))
    .toBe(true)
}

// Search scrollback (T5)
test('search scrollback: a marker scrolled off screen is found and shown; Escape returns to the shell', { tag: '@desktop' }, async ({
  ui,
  target,
  page,
}) => {
  // Keyboard shortcut: desktop scenario.
  const name = await openShell(ui, target, 'e2e-find')
  const m = uniqueName('m')
  const needle = `needle-2-${m}`
  await ui.type(`echo needle-$((1+1))-${m}; seq 1 300`, true)
  await printed(target, name, '300')
  expect(await viewport(ui)).not.toContain(needle) // scrolled away

  await page.keyboard.press('Control+Shift+F')
  await expect(bar(ui)).toBeVisible()
  await expect(bar(ui).getByRole('textbox', { name: 'Find' })).toBeFocused()
  await page.keyboard.type(needle)
  await expect(results(ui)).toHaveText('1 of 1')
  await expect.poll(() => viewport(ui)).toContain(needle)

  await page.keyboard.press('Escape')
  await expect(bar(ui)).toBeHidden()
  const after = uniqueName('after')
  await page.keyboard.type(`echo ${after}-$((3+4))`)
  await page.keyboard.press('Enter')
  await printed(target, name, `${after}-7`)
})

// Search options (T5)
test('search options: match case and regex change the counts; an invalid regex says so', { tag: '@desktop' }, async ({
  ui,
  target,
}) => {
  const name = await openShell(ui, target, 'e2e-opts')
  // Quotes split the words on the command line, so only the output has them.
  await ui.type(`echo "F"oo "f"oo "F"OO`, true)
  await printed(target, name, 'Foo foo FOO')

  await ui.terminalAction(name, 'Search')
  const find = bar(ui).getByRole('textbox', { name: 'Find' })
  const matchCase = bar(ui).getByRole('checkbox', { name: 'Match case' })
  const regex = bar(ui).getByRole('checkbox', { name: 'Regex' })

  await find.fill('foo')
  await expect(results(ui)).toHaveText(/ of 3$/)
  await matchCase.check()
  await expect(results(ui)).toHaveText('1 of 1')

  await regex.check()
  await find.fill('f[o]{2}')
  await expect(results(ui)).toHaveText('1 of 1')
  await matchCase.uncheck()
  await expect(results(ui)).toHaveText(/ of 3$/)

  await find.fill('(')
  await expect(results(ui)).toHaveText('Invalid pattern')
})

// Search on the phone (T5)
test('search on the phone: the 🔍 button opens search and finds a marker', { tag: '@phone' }, async ({ ui, target }) => {
  const name = await openShell(ui, target, 'e2e-pfind')
  const m = uniqueName('m')
  await ui.type(`echo mark-$((2*3))-${m}`, true)
  await printed(target, name, `mark-6-${m}`)

  await ui.terminalAction(name, 'Search')
  await bar(ui).getByRole('textbox', { name: 'Find' }).fill(`mark-6-${m}`)
  await expect(results(ui)).toHaveText('1 of 1')
  await bar(ui).getByRole('button', { name: 'Close search' }).click()
  await expect(bar(ui)).toBeHidden()
})
