import { expect, test } from '../helpers/fixtures.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN, POLL_INTERVAL_MS } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

// V2-M1 T0: the start-command bug. The command runs in the user's login
// shell, in the chosen folder, and its output shows in the terminal.
const START_COMMAND = `sh -c 'echo HOSTBUD_START_OK; exec sh'`

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

test('(V2-M1 T0) Session with start command', async ({ page, ui, target, request }) => {
  const path = `/home/dev/${uniqueName('e2e-start')} dir`
  const projectName = uniqueName('start-project')
  const name = uniqueName('e2e-start')
  forbidInLogs(path, projectName, name)
  await target.run(`mkdir -p ${shq(path)}`)
  const project = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name: projectName }, ORIGIN)
  expect(project.status(), await project.text()).toBe(201)
  await ui.open()

  await ui.showList()
  await page.getByRole('button', { name: `New session in ${projectName}` }).click()
  const dialog = page.getByRole('dialog', { name: 'New session here' })
  await dialog.getByLabel('Name', { exact: true }).fill(name)
  await dialog.getByLabel('Start command').fill(START_COMMAND)
  await dialog.getByRole('button', { name: 'Create session' }).click()

  await ui.waitForTerminal(name)
  await expect.poll(() => ui.termText(name), { timeout: POLL_INTERVAL_MS + 5_000 }).toContain('HOSTBUD_START_OK')
  expect(await target.capture(name)).toContain('HOSTBUD_START_OK')
  expect(await target.display(name, '#{session_path}')).toBe(path)
  expect(await target.display(name, '#{pane_current_path}')).toBe(path)
  await ui.showList()
  await expect(ui.treeItem(projectName).getByRole('treeitem', { name })).toBeVisible()
})
