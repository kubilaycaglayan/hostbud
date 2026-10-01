import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'
import { MACHINE, mutate, ORIGIN } from '../helpers/api.ts'

async function account(ui: import('../helpers/ui.ts').UI, label = 'e2e-shortcuts') {
  const fresh = newAccount(label)
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
}

async function createSessions(target: { tmux(...args: string[]): Promise<string> }, names: string[]) {
  for (const name of names) await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
}

async function captured(target: { tmux(...args: string[]): Promise<string> }, name: string) {
  return target.tmux('capture-pane', '-p', '-t', `=${name}:`)
}

test('(T23) Command palette groups commands by purpose and color', async ({ page, ui, target }) => {
  await account(ui, 'e2e-palette-groups')
  await createSessions(target, [uniqueName('palette-group')])
  await page.reload()
  await page.keyboard.press('Control+Shift+K')
  const dialog = page.getByRole('dialog', { name: 'Command palette' })
  await expect(dialog).toBeVisible()
  for (const group of ['Create', 'Open', 'Organize', 'Terminal', 'Appearance', 'Account', 'Destructive']) {
    await expect(dialog.locator(`[data-palette-group="${group}"] .palette-group-label`)).toBeVisible()
  }
  await expect(dialog.locator('[data-palette-group="Create"]')).toContainText('New session')
  await expect(dialog.locator('[data-palette-group="Open"]')).toContainText('Browse files')
  await expect(dialog.locator('[data-palette-group="Organize"]')).toContainText('Collapse all')
  await expect(dialog.locator('[data-palette-group="Terminal"]')).toContainText('Next open visible session')
  await expect(dialog.locator('[data-palette-group="Appearance"]')).toContainText('Theme: Dark')
  await expect(dialog.locator('[data-palette-group="Account"]')).toContainText('Sign out')
  await expect(dialog.locator('[data-palette-group="Destructive"]')).toContainText('Kill palette-group')
  const colors = await dialog.locator('.palette-group').evaluateAll((groups) => groups.map((group) => ({
    name: group.getAttribute('data-palette-group'),
    color: getComputedStyle(group).getPropertyValue('--palette-group-color').trim(),
  })))
  expect(colors.find((item) => item.name === 'Create')?.color).not.toBe(colors.find((item) => item.name === 'Destructive')?.color)
  expect(colors.find((item) => item.name === 'Organize')?.color).not.toBe(colors.find((item) => item.name === 'Account')?.color)
  await page.keyboard.press('Escape')
})

test('(T8) Keyboard shortcuts help opens from both scopes and restores focus', async ({ page, ui, target }) => {
  await account(ui)
  const session = uniqueName('keys-help')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await page.reload()
  const row = ui.treeItem(session)
  await row.focus()
  await page.keyboard.press('Control+Shift+/')
  const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('term').filter({ hasText: 'Command palette' })).toBeVisible()
  await expect(dialog.getByRole('term').filter({ hasText: 'Switch to last session' })).toBeVisible()
  await expect(dialog.getByRole('term').filter({ hasText: 'New session here' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(row).toBeFocused()
  await page.keyboard.press('Shift+Slash') // '?' (Playwright's Shift+/ sends key '/')
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
})

test('(T22) Switch visible open sessions from the keyboard and wrap in both directions', async ({ page, ui, target }) => {
  await account(ui, 'e2e-shortcuts-cycle')
  const names = [uniqueName('keys-a'), uniqueName('keys-b'), uniqueName('keys-c')]
  await createSessions(target, names)
  await page.reload()
  for (const name of names) await ui.openTerminal(name)
  await page.keyboard.press('Control+Shift+]')
  await expect.poll(() => ui.activeTabName()).toBe(names[0])
  await ui.type('echo shortcut-next-a', true)
  await expect.poll(() => captured(target, names[0])).toContain('shortcut-next-a')
  await page.keyboard.press('Control+Shift+[')
  await expect.poll(() => ui.activeTabName()).toBe(names[2])
  await ui.type('echo shortcut-next-c', true)
  await expect.poll(() => captured(target, names[2])).toContain('shortcut-next-c')

  await expect(page.getByRole('tablist', { name: 'Open terminals' })).toHaveCount(0)
})

test('(T8) Toggle to the most recently selected session', async ({ page, ui, target }) => {
  await account(ui, 'e2e-shortcuts-mru')
  const names = [uniqueName('mru-a'), uniqueName('mru-b'), uniqueName('mru-c')]
  await createSessions(target, names)
  await page.reload()
  for (const name of names) await ui.openTerminal(name)
  await ui.tab(names[0]).click()
  await ui.tab(names[2]).click()
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await page.keyboard.press('Control+Shift+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[0])
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await ui.type('echo shortcut-mru-a', true)
  await expect.poll(() => captured(target, names[0])).toContain('shortcut-mru-a')
  await page.keyboard.press('Control+Shift+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[2])
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await ui.type('echo shortcut-mru-c', true)
  await expect.poll(() => captured(target, names[2])).toContain('shortcut-mru-c')
  // The extra Ctrl+Command+D binding is Mac-only; emulate that platform here
  // so the same browser scenario covers the user's reported chord.
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'platform', { configurable: true, get: () => 'MacIntel' })
    Object.defineProperty(navigator, 'userAgentData', { configurable: true, value: { platform: 'MacIntel' } })
  })
  await page.keyboard.press('Control+Meta+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[0])
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await page.keyboard.press('Control+Meta+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[2])
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await ui.tab(names[0]).click()
  // Selecting b from a updates the two-session history.
  await ui.tab(names[1]).click()
  await page.keyboard.press('Control+Shift+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[0])
})

test('(T8) Global shortcuts do not reach the running program', async ({ page, ui, target }) => {
  await account(ui, 'e2e-shortcuts-isolation')
  const vim = uniqueName('keys-vim')
  const shell = uniqueName('keys-shell')
  await createSessions(target, [vim, shell])
  await page.reload()
  await ui.openTerminal(vim)
  await ui.type('vim', true)
  await expect.poll(() => target.display(vim, '#{pane_current_command}')).toContain('vim')
  await ui.type('iSHORTCUT_BUFFER')
  await ui.openTerminal(shell)
  await page.keyboard.press('Control+Shift+]')
  await expect.poll(() => ui.activeTabName()).toBe(vim)
  // The viewport changes below resize vim's window: compare its text, not
  // the screen's filler lines or the ruler's padding.
  const vimText = async () => (await captured(target, vim)).split('\n').map((line) => line.replace(/\s+/g, ' ').trim()).filter((line) => line !== '' && line !== '~')
  const before = await vimText()
  await page.keyboard.press('Control+Shift+E')
  await expect(ui.treeItem(vim)).toBeFocused()
  await page.keyboard.press('Control+Shift+E')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.keyboard.press('Control+Shift+E')
  const drawer = page.getByRole('dialog', { name: 'Project tree' })
  await expect(drawer).toBeVisible()
  await expect(ui.treeItem(vim)).toBeFocused()
  await page.keyboard.press('Control+Shift+E')
  await expect(drawer).toBeHidden()
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.keyboard.press('Control+Shift+[')
  await expect.poll(() => ui.activeTabName()).toBe(shell)
  await page.keyboard.press('Control+Shift+E')
  await expect(ui.treeItem(shell)).toBeFocused()
  await page.keyboard.press('Control+Shift+E')
  await ui.type('echo shortcut-shell', true)
  await expect.poll(() => captured(target, shell)).toContain('shortcut-shell')
  expect(await vimText()).toEqual(before)
})

test('(T9) Palette opens without stealing Ctrl+K from the shell', async ({ page, ui, target }) => {
  await account(ui, 'e2e-palette-ctrl-k')
  const shell = uniqueName('palette-shell')
  await target.tmux('new-session', '-d', '-s', shell, '-c', '/home/dev')
  await page.reload()
  await ui.openTerminal(shell)
  await ui.type('echo abcdef')
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('Control+K')
  const line = await captured(target, shell)
  expect(line).toContain('echo abc')
  expect(line).not.toContain('abcdef')
  await page.keyboard.press('Control+Shift+K')
  await expect(page.getByRole('dialog', { name: 'Command palette' })).toBeVisible()
})

test('(T9) Palette jumps to a session and Escape restores terminal focus', async ({ page, ui, target }) => {
  await account(ui, 'e2e-palette-sessions')
  const names = [uniqueName('palette-a'), uniqueName('palette-b')]
  await createSessions(target, names)
  await page.reload()
  await ui.openTerminal(names[0])
  await page.keyboard.press('Control+Shift+K')
  const dialog = page.getByRole('dialog', { name: 'Command palette' })
  await dialog.getByRole('combobox', { name: 'Command palette' }).fill(names[1].slice(0, 12))
  await page.keyboard.press('Enter')
  await ui.waitForTerminal(names[1])
  const secondInput = ui.pane(names[1]).getByRole('textbox', { name: 'Terminal input' })
  await expect(secondInput).toBeFocused()

  await page.keyboard.press('Control+Shift+K')
  await dialog.getByRole('combobox', { name: 'Command palette' }).fill(names[0].slice(0, 12))
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(secondInput).toBeFocused()
})

test('(T9) Palette runs theme, tree, session and confirmation actions', async ({ page, ui, target, request }) => {
  await account(ui, 'e2e-palette-actions')
  const path = `/home/dev/${uniqueName('palette-project-path')}`
  const project = uniqueName('palette-project')
  const session = uniqueName('palette-action-session')
  const nextSession = uniqueName('palette-project-session')
  await target.run(`mkdir -p '${path}' && tmux new-session -d -s '${session}' -c /home/dev`)
  const created = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name: project }, ORIGIN)
  expect(created.status(), await created.text()).toBe(201)
  await page.reload()
  await ui.openTerminal(session)

  const palette = page.getByRole('dialog', { name: 'Command palette' })
  const open = async (query: string) => {
    await page.keyboard.press('Control+Shift+K')
    await palette.getByRole('combobox', { name: 'Command palette' }).fill(query)
  }
  const choose = async (label: string) => {
    await page.getByRole('option').filter({ hasText: label }).first().click()
  }

  await open('Theme: Light')
  await choose('Theme: Light')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await open(`Hide ${session}`)
  await choose(`Hide ${session}`)
  // The page's own account (page.request), after the debounced save.
  const hiddenSessions = async () => {
    const state = await (await page.request.get('/api/ui-state/tree')).json() as { hidden?: { sessions?: string[] } }
    return state.hidden?.sessions ?? []
  }
  await expect.poll(hiddenSessions).toContain(`host/${session}`)
  await open(`Unhide ${session}`)
  await choose(`Unhide ${session}`)
  await expect.poll(hiddenSessions).not.toContain(`host/${session}`)
  await open(`New session in ${project}`)
  await choose(`New session in ${project}`)
  const newSessionDialog = page.getByRole('dialog', { name: 'New session here' })
  await newSessionDialog.getByLabel('Name').fill(nextSession)
  await newSessionDialog.getByRole('button', { name: 'Create' }).click()
  await ui.waitForTerminal(nextSession)
  await ui.showList() // opening the terminal closed the compact drawer
  await expect(ui.treeItem(project).getByRole('treeitem', { name: nextSession })).toBeVisible()
  await ui.closeList()

  await open(`Kill ${session}`)
  await choose(`Kill ${session}`)
  const confirmation = page.getByRole('alertdialog', { name: `Kill session ${session}?` })
  await expect(confirmation).toBeVisible()
  await confirmation.getByRole('button', { name: 'Cancel' }).click()
  await expect.poll(() => target.tmux('has-session', '-t', `=${session}`).then(() => true, () => false)).toBe(true)
})

test('(T9) Palette Rename expands a collapsed project and starts inline editing', async ({ page, ui, target, request }) => {
  await account(ui, 'e2e-palette-rename')
  const path = `/home/dev/${uniqueName('palette-rename-path')}`
  const project = uniqueName('palette-rename-project')
  const session = uniqueName('palette-rename-session')
  await target.run(`mkdir -p '${path}' && tmux new-session -d -s '${session}' -c '${path}'`)
  const created = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name: project }, ORIGIN)
  expect(created.status(), await created.text()).toBe(201)
  await page.reload()
  await ui.showList()
  await ui.toggle(project)
  await page.keyboard.press('Control+Shift+K')
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await palette.getByRole('combobox', { name: 'Command palette' }).fill(`Rename ${session}`)
  // Enter runs the highlighted result: wait for the filtered list first.
  await expect(palette.getByRole('option', { name: new RegExp(`^Rename ${session}`) })).toBeVisible()
  await page.keyboard.press('Enter')
  await expect(palette).toBeHidden()
  await ui.showList()
  const editor = page.getByRole('textbox', { name: `Rename ${session}` })
  await expect(editor).toBeFocused()
  await expect(ui.treeItem(project)).toHaveAttribute('aria-expanded', 'true')
})
