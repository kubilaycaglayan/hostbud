import { expect, test } from '../helpers/fixtures.ts'
import { clipboard, dragAcross, openShell, promptLine, termSelection } from '../helpers/shell.ts'
import { uniqueName, type Target } from '../helpers/target.ts'

// Copy and paste (M3 T2). Playwright grants clipboard permissions only in
// Chromium, so these run in desktop-chromium. Results are checked in the
// real shell on the target (capture-pane) and in the browser clipboard.

test.skip(({ isMobile }) => isMobile, 'clipboard permissions: desktop Chromium only')
test.use({ permissions: ['clipboard-read', 'clipboard-write'] })

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

/** How many lines of the pane are exactly `line`. */
async function countLines(target: Target, name: string, line: string): Promise<number> {
  return (await target.capture(name)).split('\n').filter((l) => l.trimEnd() === line).length
}

const menu = (ui: import('../helpers/ui.ts').UI) => ui.page.getByRole('menu', { name: 'Terminal menu' })

// Copy selection (T2)
test('copy selection: Ctrl+Shift+C and the context menu', async ({ ui, target, page }) => {
  const name = await openShell(ui, target, 'e2e-copy')
  const m = uniqueName('copy')
  await ui.type(`echo ${m}`, true)
  await expect.poll(() => countLines(target, name, m)).toBe(1)

  await dragAcross(page, m)
  await expect.poll(() => termSelection(page)).toBe(m)
  await page.keyboard.press('Control+Shift+C')
  await expect.poll(() => clipboard.read(page)).toBe(m)
  expect(await termSelection(page)).toBe(m) // the selection stays

  // The context menu's Copy (right-click on the selection keeps it).
  await clipboard.write(page, 'other')
  const r = (await page.evaluate((t) => window.__hostbud!.termTextRect(t), m))!
  await page.mouse.click(r.x + r.width / 2, r.y + r.height / 2, { button: 'right' })
  await expect(menu(ui).getByRole('menuitem', { name: 'Select all' })).toBeVisible()
  await menu(ui).getByRole('menuitem', { name: 'Copy' }).click()
  await expect(menu(ui)).toBeHidden()
  await expect.poll(() => clipboard.read(page)).toBe(m)
})

// Ctrl+C still interrupts (T2)
test('Ctrl+C still interrupts with a selection, and leaves the clipboard alone', async ({ ui, target, page }) => {
  const name = await openShell(ui, target, 'e2e-intr')
  const m = uniqueName('keep')
  await ui.type(`echo ${m}`, true)
  await expect.poll(() => countLines(target, name, m)).toBe(1)
  await ui.type('sleep 100', true)
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('sleep')

  await clipboard.write(page, 'sentinel')
  await dragAcross(page, m)
  await expect.poll(() => termSelection(page)).toBe(m)
  await page.keyboard.press('Control+C')
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('bash')
  await expect.poll(() => target.capture(name)).toMatch(/sleep 100\s*\n\^C\s*\n/)
  await expect.poll(() => promptLine(target, name)).toMatch(/\$$/)
  expect(await clipboard.read(page)).toBe('sentinel')
})

// Bracketed paste (T2)
test('bracketed paste: a two-line paste waits for Enter (keys and menu)', async ({ ui, target, page }) => {
  const name = await openShell(ui, target, 'e2e-paste')

  async function check(m: string, paste: () => Promise<void>) {
    await clipboard.write(page, `echo one-${m}\necho two-${m}`)
    await paste()
    // Both lines sit in bash's command line; nothing has run.
    await expect.poll(() => target.capture(name)).toContain(`echo two-${m}`)
    await page.waitForTimeout(500)
    expect(await countLines(target, name, `one-${m}`)).toBe(0)
    expect(await countLines(target, name, `two-${m}`)).toBe(0)

    await ui.type('', true)
    await expect.poll(() => countLines(target, name, `two-${m}`)).toBe(1)
    expect(await countLines(target, name, `one-${m}`)).toBe(1)
  }

  await check(uniqueName('k'), async () => {
    await page.getByRole('textbox', { name: 'Terminal input' }).focus()
    await page.keyboard.press('Control+Shift+V')
  })
  await check(uniqueName('m'), async () => {
    await page.getByTestId('terminal').click({ button: 'right' })
    await menu(ui).getByRole('menuitem', { name: 'Paste' }).click()
    await expect(menu(ui)).toBeHidden()
  })
})

// Forced selection (T2)
test('forced selection: Shift+drag selects when tmux captures the mouse', async ({ ui, target, page }) => {
  const name = await openShell(ui, target, 'e2e-force')
  await target.tmux('set', '-t', name, 'mouse', 'on')
  const m = uniqueName('force')
  const other = uniqueName('other')
  await ui.type(`echo ${m} ${other}`, true)
  await expect.poll(() => countLines(target, name, `${m} ${other}`)).toBe(1)

  // A plain drag goes to tmux (its own copy mode), not to the browser.
  await dragAcross(page, other)
  await page.waitForTimeout(300)
  expect(await termSelection(page)).toBe('')

  await dragAcross(page, m, { shift: true })
  await expect.poll(() => termSelection(page)).toBe(m)
  await page.keyboard.press('Control+Shift+C')
  await expect.poll(() => clipboard.read(page)).toBe(m)
})

// OSC 52 yank (T2)
test('OSC 52: a tmux copy-mode yank lands in the browser clipboard', async ({ ui, target, page }) => {
  const name = await openShell(ui, target, 'e2e-yank')
  const m = uniqueName('yank')
  await ui.type(`echo ${m}`, true)
  await expect.poll(() => countLines(target, name, m)).toBe(1)
  await clipboard.write(page, 'sentinel')

  // tmux's defaults (set-clipboard external) send the yank to the browser.
  const t = `=${name}:`
  await target.tmux('copy-mode', '-t', t)
  await target.tmux('send-keys', '-t', t, '-X', 'search-backward', m)
  await target.tmux('send-keys', '-t', t, '-X', 'select-line')
  await target.tmux('send-keys', '-t', t, '-X', 'copy-selection-and-cancel')
  await expect.poll(async () => (await clipboard.read(page)).trim()).toBe(m)
})

// OSC 52 read refused (T2)
test('OSC 52: a clipboard query gets no reply', async ({ ui, target, page }) => {
  // Every byte the browser sends to the terminal.
  const sent: string[] = []
  page.on('websocket', (ws) => ws.on('framesent', (f) => sent.push(f.payload.toString())))
  const name = await openShell(ui, target, 'e2e-query')
  // Passthrough lets the program's OSC 52 reach the browser unchanged
  // (tmux would otherwise answer or drop it itself).
  await target.tmux('set', '-t', name, 'allow-passthrough', 'on')

  // Control: a passthrough OSC 52 write does reach the browser.
  const m = uniqueName('ctl')
  await clipboard.write(page, 'sentinel')
  await ui.type(String.raw`printf '\ePtmux;\e\e]52;c;%s\a\e\\' "$(printf ${m} | base64)"`, true)
  await expect.poll(() => clipboard.read(page)).toBe(m)

  // The query: nothing answers within 1 s.
  await clipboard.write(page, 'secret-clipboard')
  await ui.type(
    String.raw`printf '\ePtmux;\e\e]52;c;?\a\e\\'; if IFS= read -rs -t 1 -d $'\a' r; then echo got-reply; else echo no-reply; fi`,
    true,
  )
  await expect.poll(() => countLines(target, name, 'no-reply'), { timeout: 10_000 }).toBe(1)
  expect(await countLines(target, name, 'got-reply')).toBe(0)
  expect(sent.filter((s) => s.includes('\x1b]52'))).toEqual([])
  expect(sent.join('')).not.toContain(Buffer.from('secret-clipboard').toString('base64'))
})
