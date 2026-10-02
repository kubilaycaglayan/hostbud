import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, getUIState, listSessions, MACHINE, mutate, ORIGIN, POLL_INTERVAL_MS, putUIState } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { shq, uniqueName } from '../helpers/target.ts'
import { UI, dragSortable } from '../helpers/ui.ts'

async function account(ui: import('../helpers/ui.ts').UI) {
  const fresh = newAccount('e2e-tree-custom')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
}

async function addProject(request: Parameters<typeof mutate>[0], path: string, name: string) {
  const res = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name }, ORIGIN)
  const body = await res.text()
  expect(res.status(), body).toBe(201)
  return JSON.parse(body) as { id: string }
}

// The terminal's tmux client attaches asynchronously after the pane opens;
// wait for it so "attached clients unchanged" compares a real pid.
async function attachedClientPid(target: { run(command: string): Promise<string> }, session: string) {
  let pid = ''
  await expect.poll(async () => {
    pid = (await target.run(`tmux list-clients -t ${shq('=' + session)} -F '#{client_pid}'`)).trim()
    return pid
  }, { timeout: 10_000 }).not.toBe('')
  return pid
}

async function createSession(target: { run(command: string): Promise<unknown> }, name: string, path: string) {
  await target.run(`mkdir -p ${shq(path)} && tmux new-session -d -s ${shq(name)} -c ${shq(path)}`)
}

test('(T2) Tree state upgrades from M4', async ({ page, ui, target, request }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-v1')}`
  const projectName = uniqueName('tree-project')
  const project = await addProject(request, path, projectName)
  const first = uniqueName('tree-first')
  const second = uniqueName('tree-second')
  await createSession(target, first, path)
  await createSession(target, second, path)
  // Do not seed a manual order until the server's inventory has observed both
  // sessions. Otherwise an early one-session snapshot can prune the other key.
  await expect.poll(async () => (await listSessions(request)).map((session) => session.name), { timeout: 3 * POLL_INTERVAL_MS })
    .toEqual(expect.arrayContaining([first, second]))
  // Leave the app first: its pending tree save is flushed on unload and
  // would overwrite the seeded legacy state.
  await page.goto('about:blank')
  await putUIState(page.request, 'tree', { version: 1, projects: [project.id], sessions: { [project.id]: [second, first] } })
  await page.goto('/')
  const rows = page.getByRole('group', { name: `Sessions in ${projectName}` }).locator('[data-session-row]')
  await expect.poll(async () => (await rows.allTextContents()).map((text) => text.trim())).toEqual([second, first])
  await ui.treeItem(second).focus()
  await page.keyboard.press('Alt+ArrowDown')
  await ui.waitForSave('tree')
  expect(await getUIState(page.request, 'tree')).toMatchObject({ version: 2, projects: [project.id], sessions: { [project.id]: [first, second] } })
})

test('(T24) Session names use the row width and actions reveal on desktop hover', async ({ page, ui, target }) => {
  const name = uniqueName('row-hover')
  await createSession(target, name, '/home/dev')
  await page.reload()
  const row = ui.treeItem(name)
  const title = row.locator('[data-session-row]')
  const actions = row.locator('[data-session-actions]')
  const trigger = row.getByRole('button', { name: `More actions for ${name}` })
  await expect(title).toBeVisible()
  const layout = await page.evaluate(() => ({ hover: matchMedia('(hover: hover) and (pointer: fine)').matches }))
  const actionStyle = await actions.evaluate((el) => getComputedStyle(el))
  const buttonStyle = await trigger.evaluate((el) => getComputedStyle(el))
  if (layout.hover) {
    expect(actionStyle.position).toBe('absolute')
    expect(buttonStyle.opacity).toBe('0')
    const rowBox = await row.boundingBox()
    const titleBox = await title.boundingBox()
    expect(rowBox && titleBox && titleBox.width).toBeGreaterThan((rowBox?.width ?? 0) * 0.8)
    await row.hover()
    await expect(trigger).toHaveCSS('opacity', '1')
  } else {
    expect(actionStyle.position).toBe('static')
    expect(buttonStyle.opacity).toBe('1')
  }
})

test('(T25) Project names use the row width and terminal header shows session context', async ({ page, ui, target, request }) => {
  const path = `/home/dev/${uniqueName('header-directory')}`
  const projectName = uniqueName('header-project')
  const sessionName = uniqueName('header-session')
  const project = await addProject(request, path, projectName)
  await createSession(target, sessionName, path)
  const sectionId = 'session-header-purple'
  const savedTree = await getUIState(page.request, 'tree') as {
    projects?: string[]; sessions?: Record<string, string[]>; pinned?: string[]
    hidden?: { projects: string[]; sessions: string[] }; collapsed?: string[]; collapsedSections?: string[]
    expanded?: string[]; showHidden?: boolean; sections?: { id: string; name: string; color: string }[]
    projectSections?: Record<string, string>
  } | null
  await putUIState(page.request, 'tree', {
    version: 4,
    projects: [...new Set([...(savedTree?.projects ?? []), project.id])],
    sessions: savedTree?.sessions ?? {},
    pinned: savedTree?.pinned ?? [],
    hidden: savedTree?.hidden ?? { projects: [], sessions: [] },
    collapsed: savedTree?.collapsed ?? [],
    collapsedSections: savedTree?.collapsedSections ?? [],
    expanded: savedTree?.expanded ?? [],
    showHidden: savedTree?.showHidden ?? false,
    sections: [...(savedTree?.sections ?? []).filter((section) => section.id !== sectionId), { id: sectionId, name: 'Header accent', color: 'purple' }],
    projectSections: { ...(savedTree?.projectSections ?? {}), [project.id]: sectionId },
  })
  await page.reload()
  const projectRow = ui.treeItem(projectName)
  const projectHeader = projectRow.locator(':scope > .tree-row')
  const title = projectHeader.locator('span.font-semibold')
  const actions = projectHeader.locator('[data-project-actions]')
  const more = projectRow.getByRole('button', { name: `More actions for ${projectName}` })
  const hover = await page.evaluate(() => matchMedia('(hover: hover) and (pointer: fine)').matches)
  expect((await actions.evaluate((el) => getComputedStyle(el))).position).toBe(hover ? 'absolute' : 'static')
  expect(await more.evaluate((el) => getComputedStyle(el).opacity)).toBe(hover ? '0' : '1')
  if (hover) {
    const rowBox = await projectHeader.boundingBox()
    const titleBox = await title.boundingBox()
    expect(rowBox && titleBox && titleBox.width).toBeGreaterThan((rowBox?.width ?? 0) * 0.65)
    await projectHeader.hover()
    await expect(more).toHaveCSS('opacity', '1')
  }

  await ui.openTerminal(sessionName)
  const terminalHeader = page.getByRole('region', { name: `Terminal: ${sessionName}` })
  const sessionTitle = terminalHeader.locator('[data-terminal-session-name]')
  await expect(terminalHeader.locator('[data-terminal-directory-icon]').locator('xpath=following-sibling::*[1]')).toHaveAttribute('data-terminal-session-name', '')
  await expect(sessionTitle).toHaveText(sessionName)
  await expect(terminalHeader.locator('[data-terminal-header]')).toHaveAttribute('style', /hb-section-purple/)
  await expect(terminalHeader.locator('[data-terminal-directory]')).toHaveText(path.split('/').at(-1)!)
  await expect(terminalHeader.locator('[data-terminal-header]')).toHaveClass(/text-section-fg/)
  await expect(sessionTitle).toHaveCSS('color', 'rgb(23, 26, 33)')
  await expect(terminalHeader.locator('[data-terminal-directory]')).toHaveCSS('color', 'rgb(23, 26, 33)')
  await expect(terminalHeader.locator('[data-terminal-directory]')).toHaveAttribute('title', path)
})

test('(T2) Tree state survives an app restart', async ({ page, ui, target, request }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-restart')}`
  const projectName = uniqueName('tree-project')
  await addProject(request, path, projectName)
  const first = uniqueName('tree-first')
  const second = uniqueName('tree-second')
  await createSession(target, first, path)
  await createSession(target, second, path)
  await page.reload()
  await expect(ui.treeItem(first)).toBeVisible()
  await ui.treeItem(first).focus()
  await page.keyboard.press('Alt+ArrowDown')
  // This immediate reload exercises pagehide's keepalive flush.
  await page.reload()
  await ctl.restartApp()
  await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
  await page.reload()
  const rows = page.getByRole('group', { name: `Sessions in ${projectName}` }).locator('[data-session-row]')
  await expect.poll(async () => (await rows.allTextContents()).map((text) => text.trim())).toEqual([second, first])
})

test('(T2) Another open tab never overwrites a manual session order', async ({ page, ui, target, request }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-tabs')}`
  const projectName = uniqueName('tree-project')
  const project = await addProject(request, path, projectName)
  const first = uniqueName('tree-first')
  const second = uniqueName('tree-second')
  await createSession(target, first, path)
  await createSession(target, second, path)
  await page.reload()
  const rowNames = (p: typeof page) => async () =>
    (await p.getByRole('group', { name: `Sessions in ${projectName}` }).locator('[data-session-row]').allTextContents()).map((text) => text.trim())
  // A second tab (another device, in effect) loaded before the reorder.
  const stale = await page.context().newPage()
  await stale.goto('/')
  await expect.poll(rowNames(stale)).toEqual([first, second])
  await ui.treeItem(first).focus()
  await page.keyboard.press('Alt+ArrowDown')
  await ui.waitForSave('tree')
  // A live event reaches the stale tab; it must not save its old order.
  const third = uniqueName('tree-third')
  await createSession(target, third, path)
  await expect.poll(rowNames(stale)).toEqual([first, second, third])
  await stale.waitForTimeout(1500) // past the 500 ms save debounce
  expect(await getUIState(page.request, 'tree')).toMatchObject({ sessions: { [project.id]: [second, first] } })
  // Returning to the stale tab picks up the saved order.
  await stale.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  await expect.poll(rowNames(stale)).toEqual([second, first, third])
  await page.reload()
  await expect.poll(rowNames(page)).toEqual([second, first, third])
  await stale.close()
})

test('(T2) Keyboard tree navigation', async ({ page, ui, target }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-keys')}`
  const first = uniqueName('tree-first')
  const second = uniqueName('tree-second')
  await createSession(target, first, path)
  await createSession(target, second, path)
  await page.reload()
  const firstRow = ui.treeItem(first)
  await firstRow.focus()
  await page.keyboard.press('Home')
  await expect(page.locator('[data-tree-key]:focus')).toHaveAttribute('aria-level', '1')
  await page.keyboard.press('End')
  await expect(ui.treeItem(second)).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(ui.treeItem(first)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('region', { name: `Terminal: ${first}` })).toBeVisible()
  await ui.showList()
  await ui.treeItem(first).focus()
  await page.keyboard.press('Alt+ArrowDown')
  await ui.waitForSave('tree')
  await page.reload()
  await ui.showList()
  const orderedRows = page.getByRole('group', { name: 'Other sessions' }).locator('[data-session-row]')
  await expect.poll(async () => (await orderedRows.allTextContents()).map((text) => text.trim())).toEqual([second, first])
})

for (const mode of ['collapse', 'hierarchy'] as const) {
  test(`(T2) ${mode === 'collapse' ? 'Collapse state persists' : 'Tree shows hierarchy'}`, async ({ page, ui, target, request }) => {
    await account(ui)
    const path = `/home/dev/${uniqueName('tree-' + mode)}`
    const projectName = uniqueName('tree-project')
    await target.run(`mkdir -p ${shq(path)}`)
    await addProject(request, path, projectName)
    const inProject = uniqueName('tree-session')
    const other = uniqueName('tree-other')
    await createSession(target, inProject, path)
    await createSession(target, other, `/home/dev/${uniqueName('unmatched')}`)
    await page.reload()
    await expect(ui.treeItem(projectName)).toBeVisible()
    await expect(ui.treeItem('Other sessions')).toBeVisible()

    if (mode === 'hierarchy') {
      const header = ui.treeItem(projectName)
      const row = ui.treeItem(inProject)
      await expect(header).toContainText('~/' + path.split('/').at(-1))
      await expect(header.locator(`[title="${path}"]`)).toHaveCount(1)
      await expect(header.locator('[data-project-count]')).toHaveCount(0)
      await expect(header).not.toHaveClass(/bg-tree-header/)
      await header.getByRole('button', { name: `Collapse ${projectName}` }).click()
      await expect(header.locator('[data-project-count]')).toHaveCount(0)
      await header.getByRole('button', { name: `Expand ${projectName}` }).click()
      await expect(header.locator('[data-project-count]')).toHaveCount(0)
      await expect(ui.treeItem('Other sessions').locator('[data-other-label]')).toHaveCSS('text-transform', 'uppercase')
      const positions = await Promise.all([header.locator(':scope > .tree-row').boundingBox(), row.locator('button[data-session-row]').boundingBox()])
      expect(positions[0]?.x).toBeLessThan(positions[1]?.x ?? 0)
      return
    }

    await ui.openTerminal(inProject)
    const pid = (await target.run(`tmux list-clients -t ${shq('=' + inProject)} -F '#{client_pid}'`)).trim()
    await ui.toggle(projectName)
    await ui.toggle('Other sessions')
    await expect(ui.treeItem(inProject)).toHaveCount(0)
    await expect(ui.treeItem(other)).toHaveCount(0)
    expect((await target.run(`tmux list-clients -t ${shq('=' + inProject)} -F '#{client_pid}'`)).trim()).toBe(pid)
    await ui.waitForSave('tree')
    await page.reload()
    await ctl.restartApp()
    await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
    await page.reload()
    await ui.showList()
    await expect(ui.treeItem(projectName)).toHaveAttribute('aria-expanded', 'false')
    await expect(ui.treeItem('Other sessions')).toHaveAttribute('aria-expanded', 'false')
  })
}

test('Session rows show only the name, without pane titles, activity ages or attachment dots', async ({ page, ui, target }) => {
  await account(ui)
  const titled = uniqueName('tree-titled')
  const plain = uniqueName('tree-plain')
  await createSession(target, titled, `/home/dev/${uniqueName('titled')}`)
  await createSession(target, plain, `/home/dev/${uniqueName('plain')}`)
  await target.run(`tmux select-pane -t ${shq('=' + titled + ':')} -T ${shq('✳ deploy the changes and commit them')}`)
  await target.run(`tmux select-pane -t ${shq('=' + plain + ':')} -T "$(hostname)"`)
  await page.reload()
  await expect(ui.treeItem(titled)).toBeVisible()
  await expect(ui.treeItem(titled)).not.toContainText('deploy the changes')
  await expect(page.locator('[data-session-subtitle]')).toHaveCount(0)
  await expect(page.locator('[data-session-age], [data-session-dot]')).toHaveCount(0)
  await ui.openTerminal(titled)
  await ui.showList() // the compact tree drawer closes on open
  await expect(ui.treeItem(titled)).toHaveClass(/(^|\s)bg-selected(?:\s|$)/)
  await expect(ui.treeItem(plain)).not.toHaveClass(/(^|\s)bg-selected(?:\s|$)/)
})

test('(T2) Empty Other sessions group stays hidden', async ({ page, ui, target, request }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-empty-other')}`
  const projectName = uniqueName('tree-project')
  await target.run(`mkdir -p ${shq(path)}`)
  await addProject(request, path, projectName)
  const name = uniqueName('tree-matched')
  await createSession(target, name, path)
  await page.reload()
  await expect(ui.treeItem('Other sessions')).toHaveCount(0)
  await expect(page.getByText('No tmux sessions yet.')).toHaveCount(0)
  const unmatchedPath = `/home/dev/${uniqueName('tree-unmatched')}`
  const unmatched = uniqueName('tree-unmatched')
  await createSession(target, unmatched, unmatchedPath)
  await expect(ui.treeItem('Other sessions')).toBeVisible({ timeout: 5_000 })
  await page.getByRole('button', { name: `More actions for ${unmatched}` }).click()
  await page.getByRole('menuitem', { name: 'Save as project' }).click()
  await expect(ui.treeItem('Other sessions')).toHaveCount(0)
  await expect.poll(async () => (await request.get('/api/projects')).status()).toBe(200)
})

test('(T4) Inline rename a project', async ({ page, ui, target, request }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-rename-project')}`
  const oldName = uniqueName('tree-project')
  const nextName = uniqueName('renamed-project')
  await target.run(`mkdir -p ${shq(path)}`)
  await addProject(request, path, oldName)
  const session = uniqueName('project-session')
  await createSession(target, session, path)
  await page.reload()
  await expect(ui.treeItem(oldName)).toBeVisible()

  const secondPage = await page.context().newPage()
  await secondPage.goto('/')
  await expect(secondPage.getByRole('navigation', { name: 'Project and session tree' })).toBeVisible()
  await ui.sessionAction(oldName, 'Rename')
  const input = ui.treeItem(oldName).getByRole('textbox', { name: `Rename ${oldName}` })
  await expect(input).toBeFocused()
  expect(await input.evaluate((element) => [(element as HTMLInputElement).selectionStart, (element as HTMLInputElement).selectionEnd])).toEqual([0, oldName.length])
  await input.pressSequentially('x')
  await expect(input).toHaveValue('x')
  await input.fill(`  ${nextName}  `)
  await input.press('Enter')
  await expect(ui.treeItem(nextName)).toBeVisible()
  await expect(secondPage.getByRole('treeitem', { name: nextName, exact: true })).toBeVisible()
  await expect(ui.treeItem(session)).toBeVisible()
  await ui.sessionAction(nextName, 'Rename')
  const again = ui.treeItem(nextName).getByRole('textbox', { name: `Rename ${nextName}` })
  await again.fill(uniqueName('cancelled'))
  await again.press('Escape')
  await expect(ui.treeItem(nextName)).toBeVisible()
  await page.reload()
  await ctl.restartApp()
  await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
  await page.reload()
  await expect(ui.treeItem(nextName)).toBeVisible()
  await expect(ui.treeItem(session)).toBeVisible()
  await secondPage.close()
})

test('(T4) Inline rename a session', async ({ page, ui, target }) => {
  await account(ui)
  const path = `/home/dev/${uniqueName('tree-rename-session')}`
  const oldName = uniqueName('rename-session')
  const taken = uniqueName('taken-session')
  const nextName = uniqueName('renamed-session')
  await createSession(target, oldName, path)
  // Two windows: only a session with something to expand has a chevron (M8 T2).
  await target.tmux('new-window', '-d', '-t', `=${oldName}:`)
  await createSession(target, taken, path)
  await page.reload()
  await expect(ui.treeItem(oldName)).toBeVisible()
  await expect(ui.treeItem(taken)).toBeVisible()
  const oldIndex = (await ui.sessionNames()).indexOf(oldName)
  await ui.treeItem(oldName).getByRole('button', { name: `Expand ${oldName}` }).click()
  await expect(ui.treeItem(oldName)).toHaveAttribute('aria-expanded', 'true')
  await ui.openTerminal(oldName)
  await ui.showList()
  const pid = await attachedClientPid(target, oldName)
  await ui.sessionAction(oldName, 'Rename')
  let input = page.getByRole('textbox', { name: `Rename ${oldName}` })
  await expect(input).toBeFocused()
  await expect.poll(() => input.evaluate((el: HTMLInputElement) => [el.selectionStart, el.selectionEnd])).toEqual([0, oldName.length])
  await input.pressSequentially('x')
  await expect(input).toHaveValue('x')
  await input.press('Escape')
  await ui.treeItem(oldName).focus()
  await page.keyboard.press('F2')
  input = page.getByRole('textbox', { name: `Rename ${oldName}` })
  await expect(input).toBeFocused()
  await input.fill(taken)
  await input.press('Enter')
  await expect(page.getByRole('alert').first()).toContainText(/already exists|in use/i)
  await expect(ui.treeItem(oldName)).toBeVisible()
  await input.press('Escape')
  await ui.treeItem(oldName).focus()
  await page.keyboard.press('F2')
  input = page.getByRole('textbox', { name: `Rename ${oldName}` })
  await input.fill(nextName)
  await input.press('Enter')
  // The rename returns focus to the terminal (before reopening a compact tree).
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
  await ui.showList()
  await expect(ui.treeItem(nextName)).toBeVisible()
  await expect(ui.treeItem(nextName)).toHaveAttribute('aria-expanded', 'true')
  await expect(ui.treeItem(nextName).locator('[data-tree-key^="window:"]')).toHaveCount(2)
  await expect.poll(async () => (await ui.sessionNames()).indexOf(nextName)).toBe(oldIndex)
  expect((await target.run(`tmux list-clients -t ${shq('=' + nextName)} -F '#{client_pid}'`)).trim()).toBe(pid)
})

test('(T5) Hide and unhide', async ({ page, ui, target, request }) => {
  await account(ui)
  const projectPath = `/home/dev/${uniqueName('tree-hidden-project')}`
  const projectName = uniqueName('hidden-project')
  const hiddenSession = uniqueName('hidden-session')
  await target.run(`mkdir -p ${shq(projectPath)}`)
  await addProject(request, projectPath, projectName)
  await createSession(target, hiddenSession, `/home/dev/${uniqueName('outside-hidden-project')}`)
  await page.reload()
  await ui.openTerminal(hiddenSession)
  const clientPid = await attachedClientPid(target, hiddenSession)
  await ui.showList()
  await ui.treeItem(hiddenSession).getByRole('button', { name: `More actions for ${hiddenSession}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await ui.treeItem(projectName).getByRole('button', { name: `More actions for ${projectName}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await expect(ui.treeItem(hiddenSession)).toHaveCount(0)
  await expect(ui.treeItem(projectName)).toHaveCount(0)
  expect((await target.run(`tmux list-clients -t ${shq('=' + hiddenSession)} -F '#{client_pid}'`)).trim()).toBe(clientPid)
  expect(await target.sessions()).toContain(hiddenSession)

  const toggle = page.getByRole('button', { name: 'Show hidden (2)' })
  await expect(toggle).toHaveAttribute('aria-pressed', 'false')
  await toggle.click()
  const hiddenSessionRow = page.getByRole('treeitem', { name: `${hiddenSession}, hidden`, exact: true })
  const hiddenProjectRow = page.getByRole('treeitem', { name: `${projectName}, hidden`, exact: true })
  await expect(hiddenSessionRow).toHaveClass(/opacity-50/)
  await expect(hiddenProjectRow).toHaveClass(/opacity-50/)
  await hiddenSessionRow.getByRole('button', { name: `More actions for ${hiddenSession}` }).click()
  await page.getByRole('menuitem', { name: 'Unhide', exact: true }).click()
  await hiddenProjectRow.getByRole('button', { name: `More actions for ${projectName}` }).click()
  await page.getByRole('menuitem', { name: 'Unhide', exact: true }).click()
  await expect(ui.treeItem(hiddenSession)).toBeVisible()
  await expect(ui.treeItem(projectName)).toBeVisible()

  // With Show hidden enabled, hidden rows remain visible and dimmed through reload and restart.
  await ui.treeItem(hiddenSession).getByRole('button', { name: `More actions for ${hiddenSession}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await ui.treeItem(projectName).getByRole('button', { name: `More actions for ${projectName}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await ui.waitForSave('tree')
  await page.reload()
  await ui.showList()
  await expect(page.getByRole('button', { name: 'Show hidden (2)' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('treeitem', { name: `${hiddenSession}, hidden`, exact: true })).toBeVisible()
  await ctl.restartApp()
  await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
  await page.reload()
  await ui.showList()
  await expect(page.getByRole('button', { name: 'Show hidden (2)' })).toHaveAttribute('aria-pressed', 'true')

  await page.getByRole('treeitem', { name: `${hiddenSession}, hidden`, exact: true }).getByRole('button', { name: `More actions for ${hiddenSession}` }).click()
  await page.getByRole('menu').getByRole('menuitem', { name: 'Kill…', exact: true }).click()
  const confirm = page.getByRole('alertdialog', { name: `Kill session ${hiddenSession}?` })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: 'Kill session' }).click()
  await expect.poll(async () => (await target.sessions()).includes(hiddenSession)).toBe(false)
  await createSession(target, hiddenSession, `/home/dev/${uniqueName('outside-hidden-project')}`)
  await expect(ui.treeItem(hiddenSession)).toBeVisible()
})

test('(T6) Pin projects and keep section order', async ({ page, ui, target }) => {
  await account(ui)
  const api = page.context().request
  const entries = ['pin-first', 'pin-second', 'pin-third'].map((label) => ({
    path: `/home/dev/${uniqueName(label)}`,
    name: uniqueName(label),
  }))
  const projectIDs: string[] = []
  for (const entry of entries) {
    await target.run(`mkdir -p ${shq(entry.path)}`)
    projectIDs.push((await addProject(api, entry.path, entry.name)).id)
  }
  await page.goto('about:blank') // flush the app's pending tree save before seeding
  await putUIState(api, 'tree', { version: 2, projects: projectIDs, sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false })
  await page.goto('/')

  for (const index of [0, 1]) {
    await ui.treeItem(entries[index].name).getByRole('button', { name: `More actions for ${entries[index].name}` }).click()
    await page.getByRole('menuitem', { name: 'Pin', exact: true }).click()
    await expect(page.getByRole('menu')).toHaveCount(0)
  }
  const pinned = page.getByRole('group', { name: 'Pinned projects', exact: true })
  await expect.poll(async () => (await pinned.locator('[data-tree-kind="project"]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('aria-label')))))
    .toEqual([entries[0].name, entries[1].name])

  await ui.treeItem(entries[0].name).getByRole('button', { name: `Unpin ${entries[0].name}` }).click()
  const unpinned = page.getByRole('group', { name: 'Projects', exact: true })
  await expect.poll(async () => {
    const labels = await unpinned.locator('[data-tree-kind="project"]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('aria-label')))
    return labels.filter((name) => entries.some((entry) => entry.name === name))
  })
    .toEqual([entries[2].name, entries[0].name])
  await ui.waitForSave('tree')
  const unpinnedHandle = ui.treeItem(entries[2].name).getByRole('button', { name: `Drag to reorder project ${entries[2].name}` })
  await dragSortable(unpinnedHandle, ui.treeItem(entries[0].name), { x: 20, y: 1 })
  await ui.waitForSave('tree')
  const afterWithinSection = (await getUIState(api, 'tree') as { projects: string[] }).projects
  expect(afterWithinSection.filter((id) => projectIDs.includes(id))).toEqual([projectIDs[1], projectIDs[0], projectIDs[2]])
  await dragSortable(ui.treeItem(entries[1].name).getByRole('button', { name: `Drag to reorder project ${entries[1].name}` }), ui.treeItem(entries[2].name), { x: 20, y: 1 })
  expect((await getUIState(api, 'tree') as { projects: string[] }).projects).toEqual(afterWithinSection)
})

test('(T6) Every tree customization survives reload and restart', async ({ page, ui, target, request }) => {
  await account(ui)
  const api = page.context().request
  const projectPath = `/home/dev/${uniqueName('tree-custom-project')}`
  const secondProjectPath = `/home/dev/${uniqueName('tree-custom-project-two')}`
  const otherPath = `/home/dev/${uniqueName('tree-custom-other')}`
  const projectName = uniqueName('custom-project')
  const secondProjectName = uniqueName('custom-project-two')
  const first = uniqueName('custom-session')
  const second = uniqueName('custom-session-two')
  const third = uniqueName('custom-session-three')
  const outsider = uniqueName('custom-outsider')
  await target.run(`mkdir -p ${shq(projectPath)} ${shq(secondProjectPath)} ${shq(otherPath)}`)
  const projectID = (await addProject(api, projectPath, projectName)).id
  const secondProjectID = (await addProject(api, secondProjectPath, secondProjectName)).id
  await createSession(target, first, projectPath)
  await createSession(target, second, projectPath)
  await createSession(target, third, projectPath)
  await createSession(target, outsider, otherPath)
  await target.run(`tmux new-window -t ${shq('=' + first)} -n extra && tmux split-window -t ${shq('=' + first + ':1')} -h`)
  // Seed a known session order once the inventory has every session (an
  // earlier snapshot would prune the missing ones), away from the app.
  await expect.poll(async () => (await listSessions(api)).map((session) => session.name), { timeout: 3 * POLL_INTERVAL_MS })
    .toEqual(expect.arrayContaining([first, second, third, outsider]))
  await page.goto('about:blank')
  await putUIState(api, 'tree', { version: 2, projects: [projectID, secondProjectID], sessions: { [projectID]: [first, second, third] }, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false })
  await page.goto('/')
  await ui.showList()
  const projectRows = async () => (await page.getByRole('group', { name: `Sessions in ${projectName}` }).locator('[data-session-row]').allTextContents()).map((text) => text.trim())
  await expect.poll(projectRows).toEqual([first, second, third])

  await ui.treeItem(projectName).focus()
  await page.keyboard.press('Alt+ArrowDown')
  await expect.poll(async () => (await page.getByRole('group', { name: 'Projects', exact: true }).locator('[data-tree-kind="project"]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('aria-label'))))
    .filter((name) => name === projectName || name === secondProjectName)).toEqual([secondProjectName, projectName])
  await ui.treeItem(first).focus()
  await page.keyboard.press('Alt+ArrowDown')
  await expect.poll(projectRows).toEqual([second, first, third])
  await dragSortable(ui.treeItem(third).getByRole('button', { name: `Drag to reorder session ${third}` }), ui.treeItem(second), { x: 20, y: 1 })
  await expect.poll(projectRows).toEqual([third, second, first])
  await ui.treeItem(projectName).getByRole('button', { name: `More actions for ${projectName}` }).click()
  await page.getByRole('menu').getByRole('menuitem', { name: 'Rename', exact: true }).click()
  await expect(ui.treeItem(projectName).getByRole('button', { name: `Rename ${projectName}` })).toHaveCount(0)
  const renamedProject = uniqueName('custom-project-renamed')
  let editor = page.getByRole('textbox', { name: `Rename ${projectName}` })
  await editor.fill(renamedProject)
  await editor.press('Enter')
  await ui.showList()
  await ui.treeItem(first).focus()
  await page.keyboard.press('F2')
  const renamedSession = uniqueName('custom-session-renamed')
  editor = page.getByRole('textbox', { name: `Rename ${first}` })
  await editor.fill(renamedSession)
  await editor.press('Enter')

  await ui.showList()
  await ui.treeItem(second).getByRole('button', { name: `More actions for ${second}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await page.getByRole('button', { name: 'Show hidden (1)' }).click()
  await ui.treeItem(third).getByRole('button', { name: `More actions for ${third}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  // Shown dimmed while Show hidden is on.
  await ui.treeItem(`${third}, hidden`).getByRole('button', { name: `More actions for ${third}` }).click()
  await page.getByRole('menuitem', { name: 'Unhide', exact: true }).click()
  await ui.treeItem(secondProjectName).getByRole('button', { name: `More actions for ${secondProjectName}` }).click()
  await page.getByRole('menuitem', { name: 'Pin', exact: true }).click()
  await ui.treeItem('Other sessions').getByRole('button', { name: 'Collapse Other sessions' }).click()
  await ui.treeItem(renamedSession).getByRole('button', { name: `Expand ${renamedSession}` }).click()
  await expect(page.locator(`[data-tree-key^="window:host/${renamedSession}/"]`)).toHaveCount(2)
  await page.locator(`[data-tree-key^="window:host/${renamedSession}/"]`).filter({ hasText: 'extra' }).getByRole('button', { name: 'Expand window 2' }).click()
  await expect(page.locator(`[data-tree-key^="pane:host/${renamedSession}/"]`)).toHaveCount(2)

  await ui.waitForSave('tree')
  await page.reload()
  await ui.showList()
  const persistedTree = await getUIState(api, 'tree') as { sessions: Record<string, string[]> }
  const persistedGroup = Object.keys(persistedTree.sessions).find((key) => persistedTree.sessions[key]?.includes(renamedSession))
  expect(persistedGroup).toBeTruthy()
  expect(persistedTree.sessions[persistedGroup!]).toEqual([third, second, renamedSession])
  await expect(ui.treeItem(renamedProject)).toBeVisible()
  await expect(ui.treeItem(renamedSession)).toHaveAttribute('aria-expanded', 'true')
  await expect(ui.treeItem('Other sessions')).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByRole('button', { name: 'Show hidden (1)' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('treeitem', { name: `${second}, hidden`, exact: true })).toBeVisible()
  await expect(ui.treeItem(third)).toBeVisible()
  await expect(page.getByRole('group', { name: 'Pinned projects', exact: true }).getByRole('treeitem', { name: secondProjectName })).toBeVisible()
  await expect(page.locator(`[data-tree-key^="window:host/${renamedSession}/"]`)).toHaveCount(2)
  await expect(page.locator(`[data-tree-key^="pane:host/${renamedSession}/"]`)).toHaveCount(2)
  await ctl.restartApp()
  await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
  await page.reload()
  await ui.showList()
  await expect(ui.treeItem(renamedProject)).toBeVisible()
  await expect(ui.treeItem(renamedSession)).toHaveAttribute('aria-expanded', 'true')
  await expect(ui.treeItem('Other sessions')).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByRole('button', { name: 'Show hidden (1)' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator(`[data-tree-key^="pane:host/${renamedSession}/"]`)).toHaveCount(2)
})

test('(T6) Customizations are per account', async ({ page, browser, ui, target }) => {
  await account(ui)
  const api = page.context().request
  const path = `/home/dev/${uniqueName('tree-per-account')}`
  const secondPath = `/home/dev/${uniqueName('tree-per-account-two')}`
  const projectName = uniqueName('per-account-project')
  const secondProjectName = uniqueName('per-account-project-two')
  const sessionName = uniqueName('per-account-session')
  await target.run(`mkdir -p ${shq(path)} ${shq(secondPath)}`)
  await addProject(api, path, projectName)
  await addProject(api, secondPath, secondProjectName)
  await createSession(target, sessionName, path)
  await expect.poll(async () => (await listSessions(api)).map((session) => session.name), { timeout: 3 * POLL_INTERVAL_MS })
    .toContain(sessionName)
  await page.reload()
  await ui.showList()
  await ui.treeItem(secondProjectName).getByRole('button', { name: `More actions for ${secondProjectName}` }).click()
  await page.getByRole('menuitem', { name: 'Pin', exact: true }).click()
  await ui.showList()
  await ui.treeItem(sessionName).getByRole('button', { name: `More actions for ${sessionName}` }).click()
  await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
  await ui.waitForSave('tree')

  const context = await browser.newContext({ baseURL: new URL(page.url()).origin })
  try {
    const secondPage = await context.newPage()
    const fresh = newAccount('e2e-tree-custom-account-b')
    forbidInLogs(fresh.email, fresh.password)
    await owner.allow(fresh.email)
    const secondUI = new UI(secondPage)
    await secondUI.createAccount(fresh)
    await expect(secondUI.tree()).toBeVisible()
    await expect(secondUI.treeView().getByRole('group', { name: 'Pinned projects', exact: true })).toHaveCount(0)
    await expect(secondPage.getByRole('button', { name: /^Show hidden/ })).toHaveCount(0)
    await expect(secondUI.treeItem(projectName)).toHaveAttribute('aria-expanded', 'true')
    const defaultProjects = await secondUI.treeView().locator('[data-tree-kind="project"]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('aria-label')))
    expect(defaultProjects.filter((name) => name === projectName || name === secondProjectName)).toEqual([projectName, secondProjectName])
    await expect(secondUI.treeItem(sessionName)).toBeVisible()
  } finally {
    await context.close()
  }
})
