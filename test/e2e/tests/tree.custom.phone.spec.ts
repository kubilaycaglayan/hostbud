import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
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
    expect(response.status(), await response.text()).toBe(201)
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
    await expect(header.locator('[title]')).toHaveAttribute('title', path)
    await expect(header).toHaveClass(/bg-tree-header/)
    await expect(ui.treeItem('Other sessions')).toHaveClass(/bg-tree-header/)
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
    await ui.treeItem(oldName).getByRole('button', { name: `Rename ${oldName}` }).click()
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
})
