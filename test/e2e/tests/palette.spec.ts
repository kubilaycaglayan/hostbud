import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

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
  return target.tmux('capture-pane', '-p', '-t', `=${name}`)
}

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
  await expect(dialog.getByText('Command palette', { exact: true })).toBeVisible()
  await expect(dialog.getByText('Switch to last tab', { exact: true })).toBeVisible()
  await expect(dialog.getByText('New session here', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(row).toBeFocused()
  await page.keyboard.press('Shift+/')
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
})

test('(T8) Switch tabs from the keyboard and wrap in both directions', async ({ page, ui, target }) => {
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

  await page.getByRole('button', { name: `Close ${names[0]}` }).click()
  await page.getByRole('button', { name: `Close ${names[1]}` }).click()
  await expect.poll(() => ui.activeTabName()).toBe(names[2])
  const before = await captured(target, names[2])
  await page.keyboard.press('Control+Shift+]')
  await page.keyboard.press('Control+Shift+[')
  expect(await ui.activeTabName()).toBe(names[2])
  expect(await captured(target, names[2])).toBe(before)
})

test('(T8) Toggle to the last tab, dropping closed tabs from its history', async ({ page, ui, target }) => {
  await account(ui, 'e2e-shortcuts-mru')
  const names = [uniqueName('mru-a'), uniqueName('mru-b'), uniqueName('mru-c')]
  await createSessions(target, names)
  await page.reload()
  for (const name of names) await ui.openTerminal(name)
  await ui.tab(names[0]).click()
  await ui.tab(names[2]).click()
  await page.keyboard.press('Control+Shift+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[0])
  await ui.type('echo shortcut-mru-a', true)
  await expect.poll(() => captured(target, names[0])).toContain('shortcut-mru-a')
  await page.keyboard.press('Control+Shift+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[2])
  await ui.type('echo shortcut-mru-c', true)
  await expect.poll(() => captured(target, names[2])).toContain('shortcut-mru-c')
  await ui.tab(names[0]).click()
  await page.getByRole('button', { name: `Close ${names[0]}` }).click()
  await page.keyboard.press('Control+Shift+D')
  await expect.poll(() => ui.activeTabName()).toBe(names[1])
})

test('(T8) Global shortcuts do not reach the running program', async ({ page, ui, target }) => {
  await account(ui, 'e2e-shortcuts-isolation')
  const vim = uniqueName('keys-vim')
  const shell = uniqueName('keys-shell')
  await createSessions(target, [vim, shell])
  await page.reload()
  await ui.openTerminal(vim)
  await ui.type('vim', true)
  await expect.poll(() => target.tmux('display-message', '-p', '-t', `=${vim}`, '#{pane_current_command}')).toContain('vim')
  await ui.type('iSHORTCUT_BUFFER')
  await ui.openTerminal(shell)
  await page.keyboard.press('Control+Shift+]')
  await expect.poll(() => ui.activeTabName()).toBe(vim)
  const before = await captured(target, vim)
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
  await expect(page.locator('.xterm-helper-textarea')).toBeFocused()
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.keyboard.press('Control+Shift+[')
  await expect.poll(() => ui.activeTabName()).toBe(shell)
  await page.keyboard.press('Control+Shift+E')
  await expect(ui.treeItem(shell)).toBeFocused()
  await page.keyboard.press('Control+Shift+E')
  await ui.type('echo shortcut-shell', true)
  await expect.poll(() => captured(target, shell)).toContain('shortcut-shell')
  expect(await captured(target, vim)).toBe(before)
})
