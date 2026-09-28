import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, getUIState, MACHINE, mutate, ORIGIN, putUIState } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test.describe('custom tree on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  async function freshAccount(ui: import('../helpers/ui.ts').UI) {
    const account = newAccount('e2e-tree-phone')
    forbidInLogs(account.email, account.password)
    await owner.allow(account.email)
    await ui.createAccount(account)
  }

  async function addProject(request: Parameters<typeof mutate>[0], path: string, name: string) {
    const response = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name }, ORIGIN)
    const body = await response.text()
    expect(response.status(), body).toBe(201)
    return JSON.parse(body) as { id: string }
  }

  async function createSession(target: { run(command: string): Promise<unknown> }, name: string, path: string) {
    await target.run(`mkdir -p ${shq(path)} && tmux new-session -d -s ${shq(name)} -c ${shq(path)}`)
  }

  test('(T2) Collapse state persists', async ({ page, ui, target, request }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T2 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const path = `/home/dev/${uniqueName('tree-phone')}`
    const project = uniqueName('tree-project')
    const session = uniqueName('tree-session')
    const other = uniqueName('tree-other')
    await target.run(`mkdir -p ${shq(path)} && tmux new-session -d -s ${shq(session)} -c ${shq(path)} && tmux new-session -d -s ${shq(other)} -c ${shq('/home/dev/' + uniqueName('outside'))}`)
    await addProject(request, path, project)
    await page.reload()
    await expect(ui.treeItem(project)).toBeVisible()
    await ui.openTerminal(session)
    const pid = (await target.run(`tmux list-clients -t ${shq('=' + session)} -F '#{client_pid}'`)).trim()
    await ui.showList()
    await ui.toggle(project)
    await ui.toggle('Other sessions')
    expect((await target.run(`tmux list-clients -t ${shq('=' + session)} -F '#{client_pid}'`)).trim()).toBe(pid)
    await ui.waitForSave('tree')
    await page.reload()
    await ctl.restartApp()
    await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
    await page.reload()
    await ui.showList()
    await expect(ui.treeItem(project)).toHaveAttribute('aria-expanded', 'false')
    await expect(ui.treeItem('Other sessions')).toHaveAttribute('aria-expanded', 'false')
  })

  test('(T2) Tree shows hierarchy', async ({ page, ui, target, request }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T2 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const path = `/home/dev/${uniqueName('tree-phone-path')}`
    const project = uniqueName('tree-project')
    const session = uniqueName('tree-session')
    const other = uniqueName('tree-other')
    await target.run(`mkdir -p ${shq(path)} && tmux new-session -d -s ${shq(session)} -c ${shq(path)} && tmux new-session -d -s ${shq(other)} -c ${shq('/home/dev/' + uniqueName('outside'))}`)
    await addProject(request, path, project)
    await page.reload()
    await ui.showList()
    const header = ui.treeItem(project)
    const row = ui.treeItem(session)
    await expect(header).toContainText('~/' + path.split('/').at(-1))
    await expect(header.locator(`[title="${path}"]`)).toHaveCount(1)
    await expect(header.locator('[data-project-count]')).toHaveText('1')
    await expect(header).not.toHaveClass(/bg-tree-header/)
    await expect(ui.treeItem('Other sessions').locator('[data-other-label]')).toHaveCSS('text-transform', 'uppercase')
    const positions = await Promise.all([header.locator('span').first().boundingBox(), row.locator('button[data-session-row]').boundingBox()])
    expect(positions[0]?.x).toBeLessThan(positions[1]?.x ?? 0)
  })

  test('(T4) Inline rename a project', async ({ page, ui, target, request }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T4 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const path = `/home/dev/${uniqueName('tree-phone-rename-project')}`
    const oldName = uniqueName('phone-project')
    const nextName = uniqueName('phone-renamed-project')
    await target.run(`mkdir -p ${shq(path)}`)
    await addProject(request, path, oldName)
    const session = uniqueName('phone-session')
    await target.run(`tmux new-session -d -s ${shq(session)} -c ${shq(path)}`)
    await page.reload()
    await ui.showList()
    await ui.sessionAction(oldName, 'Rename')
    const input = page.getByRole('textbox', { name: `Rename ${oldName}` })
    await input.fill(nextName)
    await input.press('Enter')
    await expect(ui.treeItem(nextName)).toBeVisible()
    await expect(ui.treeItem(session)).toBeVisible()
  })

  test('(T4) Inline rename a session from the phone menu', async ({ page, ui, target }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T4 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const oldName = uniqueName('phone-session')
    const nextName = uniqueName('phone-renamed')
    await target.run(`tmux new-session -d -s ${shq(oldName)} -c ${shq('/home/dev')}`)
    await page.reload()
    await ui.showList()
    await page.getByRole('button', { name: `More actions for ${oldName}` }).click()
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click()
    const input = page.getByRole('textbox', { name: `Rename ${oldName}` })
    await input.fill(nextName)
    await input.press('Enter')
    await expect(ui.treeItem(nextName)).toBeVisible()
  })

  test('(T5) Hide and unhide in the phone drawer', async ({ page, ui, target, request }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T5 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const projectPath = `/home/dev/${uniqueName('phone-hidden-project')}`
    const project = uniqueName('phone-hidden-project')
    const session = uniqueName('phone-hidden-session')
    await target.run(`mkdir -p ${shq(projectPath)} && tmux new-session -d -s ${shq(session)} -c ${shq('/home/dev')}`)
    await addProject(request, projectPath, project)
    await page.reload()
    await ui.openTerminal(session)
    const pid = (await target.run(`tmux list-clients -t ${shq('=' + session)} -F '#{client_pid}'`)).trim()
    await ui.showList()
    await page.getByRole('button', { name: `More actions for ${session}` }).click()
    await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
    await page.getByRole('button', { name: `More actions for ${project}` }).click()
    await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
    await expect(ui.treeItem(session)).toHaveCount(0)
    await expect(ui.treeItem(project)).toHaveCount(0)
    expect((await target.run(`tmux list-clients -t ${shq('=' + session)} -F '#{client_pid}'`)).trim()).toBe(pid)
    await page.getByRole('button', { name: 'Show hidden (2)' }).click()
    await page.getByRole('treeitem', { name: `${session}, hidden`, exact: true }).getByRole('button', { name: `More actions for ${session}` }).click()
    await page.getByRole('menuitem', { name: 'Unhide', exact: true }).click()
    await page.getByRole('treeitem', { name: `${project}, hidden`, exact: true }).getByRole('button', { name: `More actions for ${project}` }).click()
    await page.getByRole('menuitem', { name: 'Unhide', exact: true }).click()
    await expect(ui.treeItem(session)).toBeVisible()
    await expect(ui.treeItem(project)).toBeVisible()
  })

  test('(T6) Pin projects in the phone tree', async ({ page, ui, target }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T6 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const api = page.context().request
    const entries = ['phone-pin-first', 'phone-pin-second', 'phone-pin-third'].map((label) => ({
      path: `/home/dev/${uniqueName(label)}`,
      name: uniqueName(label),
    }))
    const projectIDs: string[] = []
    for (const entry of entries) {
      await target.run(`mkdir -p ${shq(entry.path)}`)
      projectIDs.push((await addProject(api, entry.path, entry.name)).id)
    }
    await putUIState(api, 'tree', { version: 2, projects: projectIDs, sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false })
    await page.reload()
    await ui.showList()
    for (const index of [0, 1]) {
      if (index === 0) {
        const header = ui.treeItem(entries[index].name).locator(':scope > div')
        await header.dispatchEvent('pointerdown', { pointerType: 'touch', clientX: 12, clientY: 12 })
        await page.waitForTimeout(550)
        await header.dispatchEvent('pointerup', { pointerType: 'touch', clientX: 12, clientY: 12 })
      } else {
        await ui.treeItem(entries[index].name).getByRole('button', { name: `More actions for ${entries[index].name}` }).click()
      }
      await page.getByRole('menuitem', { name: 'Pin', exact: true }).click()
    }
    const pinned = page.getByRole('group', { name: 'Pinned projects', exact: true })
    await expect.poll(async () => pinned.locator('[data-tree-kind="project"]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('aria-label'))))
      .toEqual([entries[0].name, entries[1].name])
    await ui.treeItem(entries[0].name).getByRole('button', { name: `Unpin ${entries[0].name}` }).click()
    const unpinned = page.getByRole('group', { name: 'Projects', exact: true })
    await expect.poll(async () => {
      const labels = await unpinned.locator('[data-tree-kind="project"]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('aria-label')))
      return labels.filter((name) => entries.some((entry) => entry.name === name))
    })
      .toEqual([entries[2].name, entries[0].name])
    await ui.waitForSave('tree')
    const before = (await getUIState(api, 'tree') as { projects: string[] }).projects
    await ui.treeItem(entries[1].name).getByRole('button', { name: `Drag to reorder project ${entries[1].name}` }).dragTo(ui.treeItem(entries[2].name), { targetPosition: { x: 20, y: 1 } })
    expect((await getUIState(api, 'tree') as { projects: string[] }).projects).toEqual(before)
  })

  test('(T6) Every tree customization survives reload and restart on the phone', async ({ page, ui, target, request }) => {
    test.skip(test.info().project.name.endsWith('-domain'), 'T6 phone scenarios use the loopback access path')
    await freshAccount(ui)
    const api = page.context().request
    const projectPath = `/home/dev/${uniqueName('phone-custom-project')}`
    const secondProjectPath = `/home/dev/${uniqueName('phone-custom-project-two')}`
    const outsidePath = `/home/dev/${uniqueName('phone-custom-outside')}`
    const projectName = uniqueName('phone-custom-project')
    const secondProjectName = uniqueName('phone-custom-project-two')
    const first = uniqueName('phone-custom-session')
    const second = uniqueName('phone-custom-session-two')
    const third = uniqueName('phone-custom-session-three')
    const outsider = uniqueName('phone-custom-outsider')
    await target.run(`mkdir -p ${shq(projectPath)} ${shq(secondProjectPath)} ${shq(outsidePath)}`)
    const projectID = (await addProject(api, projectPath, projectName)).id
    const secondProjectID = (await addProject(api, secondProjectPath, secondProjectName)).id
    await putUIState(api, 'tree', { version: 2, projects: [projectID, secondProjectID], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false })
    await createSession(target, first, projectPath)
    await createSession(target, second, projectPath)
    await createSession(target, third, projectPath)
    await createSession(target, outsider, outsidePath)
    await target.run(`tmux new-window -t ${shq('=' + first)} -n extra && tmux split-window -t ${shq('=' + first + ':1')} -h`)
    await page.reload()
    await ui.showList()

    await ui.treeItem(projectName).focus()
    await page.keyboard.press('Alt+ArrowDown')
    await ui.treeItem(first).focus()
    await page.keyboard.press('Alt+ArrowDown')
    await ui.treeItem(third).getByRole('button', { name: `Drag to reorder session ${third}` }).dragTo(ui.treeItem(second), { targetPosition: { x: 20, y: 1 } })
    await ui.treeItem(projectName).getByRole('button', { name: `More actions for ${projectName}` }).click()
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click()
    const renamedProject = uniqueName('phone-custom-renamed-project')
    let editor = page.getByRole('textbox', { name: `Rename ${projectName}` })
    await editor.fill(renamedProject)
    await editor.press('Enter')
    await ui.treeItem(first).getByRole('button', { name: `More actions for ${first}` }).click()
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click()
    const renamedSession = uniqueName('phone-custom-renamed-session')
    editor = page.getByRole('textbox', { name: `Rename ${first}` })
    await editor.fill(renamedSession)
    await editor.press('Enter')

    await ui.treeItem(second).getByRole('button', { name: `More actions for ${second}` }).click()
    await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
    await page.getByRole('button', { name: 'Show hidden (1)' }).click()
    await ui.treeItem(third).getByRole('button', { name: `More actions for ${third}` }).click()
    await page.getByRole('menuitem', { name: 'Hide', exact: true }).click()
    await ui.treeItem(third).getByRole('button', { name: `More actions for ${third}` }).click()
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
    const persistedTree = await getUIState(api, 'tree') as { sessions: Record<string, string[]> }
    const persistedGroup = Object.keys(persistedTree.sessions).find((key) => persistedTree.sessions[key]?.includes(renamedSession))
    expect(persistedGroup).toBeTruthy()
    expect(persistedTree.sessions[persistedGroup!]).toEqual([third, second, renamedSession])
    await ui.showList()
    await expect(ui.treeItem(renamedProject)).toBeVisible()
    await expect(ui.treeItem(renamedSession)).toHaveAttribute('aria-expanded', 'true')
    await expect(ui.treeItem('Other sessions')).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByRole('button', { name: 'Show hidden (1)' })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('treeitem', { name: `${second}, hidden`, exact: true })).toBeVisible()
    await expect(ui.treeItem(third)).toBeVisible()
    await expect(page.getByRole('group', { name: 'Pinned projects', exact: true }).getByRole('treeitem', { name: secondProjectName })).toBeVisible()
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


})
