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
})
