import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, getUIState, MACHINE, mutate, ORIGIN, putUIState } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { shq, uniqueName } from '../helpers/target.ts'

async function account(ui: import('../helpers/ui.ts').UI) {
  const fresh = newAccount('e2e-tree-custom')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
}

async function addProject(request: Parameters<typeof mutate>[0], path: string, name: string) {
  const res = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name }, ORIGIN)
  expect(res.status(), await res.text()).toBe(201)
  return await res.json() as { id: string }
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
  await putUIState(request, 'tree', { version: 1, projects: [project.id], sessions: { [project.id]: [second, first] } })
  await page.reload()
  const rows = page.getByRole('group', { name: `Sessions in ${projectName}` }).locator('[data-session-row]')
  await expect.poll(async () => (await rows.allTextContents()).map((text) => text.trim())).toEqual([second, first])
  await ui.treeItem(second).focus()
  await page.keyboard.press('Alt+ArrowDown')
  await ui.waitForSave('tree')
  expect(await getUIState(request, 'tree')).toMatchObject({ version: 2, projects: [project.id], sessions: { [project.id]: [first, second] } })
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
      await expect(header.locator('[title]')).toHaveAttribute('title', path)
      await expect(header).toHaveClass(/bg-tree-header/)
      await expect(ui.treeItem('Other sessions')).toHaveClass(/bg-tree-header/)
      const positions = await Promise.all([header.locator('span').first().boundingBox(), row.locator('button[data-session-row]').boundingBox()])
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
    await expect(ui.treeItem(projectName)).toHaveAttribute('aria-expanded', 'false')
    await expect(ui.treeItem('Other sessions')).toHaveAttribute('aria-expanded', 'false')
  })
}

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
  await ui.treeItem(oldName).getByRole('button', { name: `Rename ${oldName}` }).click()
  const input = ui.treeItem(oldName).getByRole('textbox', { name: `Rename ${oldName}` })
  await input.fill(`  ${nextName}  `)
  await input.press('Enter')
  await expect(ui.treeItem(nextName)).toBeVisible()
  await expect(secondPage.getByRole('treeitem', { name: nextName, exact: true })).toBeVisible()
  await expect(ui.treeItem(session)).toBeVisible()
  await ui.treeItem(nextName).getByRole('button', { name: `Rename ${nextName}` }).click()
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
  await createSession(target, taken, path)
  await page.reload()
  await expect(ui.treeItem(oldName)).toBeVisible()
  await expect(ui.treeItem(taken)).toBeVisible()
  const oldIndex = (await ui.sessionNames()).indexOf(oldName)
  await ui.treeItem(oldName).getByRole('button', { name: `Expand ${oldName}` }).click()
  await expect(ui.treeItem(oldName)).toHaveAttribute('aria-expanded', 'true')
  await ui.openTerminal(oldName)
  const pid = (await target.run(`tmux list-clients -t ${shq('=' + oldName)} -F '#{client_pid}'`)).trim()
  await ui.treeItem(oldName).focus()
  await page.keyboard.press('F2')
  let input = page.getByRole('textbox', { name: `Rename ${oldName}` })
  await input.fill(taken)
  await input.press('Enter')
  await expect(page.getByRole('alert')).toContainText(/already exists|in use/i)
  await expect(ui.treeItem(oldName)).toBeVisible()
  await input.press('Escape')
  await ui.treeItem(oldName).focus()
  await page.keyboard.press('F2')
  input = page.getByRole('textbox', { name: `Rename ${oldName}` })
  await input.fill(nextName)
  await input.press('Enter')
  await expect(ui.treeItem(nextName)).toBeVisible()
  await expect(ui.treeItem(nextName)).toHaveAttribute('aria-expanded', 'true')
  await expect(ui.treeItem(nextName).locator('[data-tree-key^="window:"]')).toHaveCount(1)
  await expect.poll(async () => (await ui.sessionNames()).indexOf(nextName)).toBe(oldIndex)
  expect((await target.run(`tmux list-clients -t ${shq('=' + nextName)} -F '#{client_pid}'`)).trim()).toBe(pid)
})
