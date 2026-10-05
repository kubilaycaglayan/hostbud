import { mutate } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { addServer, removeServers, server, SERVER_HOST, waitForStatus } from '../helpers/servers.ts'
import { shq, uniqueName } from '../helpers/target.ts'

// V2-M13: other SSH servers from the UI. Each test starts and ends without
// added servers, so the rest of the suite sees only the host.
test.beforeEach(async ({ request }) => removeServers(request))
test.afterEach(async ({ request }) => removeServers(request))

test('(V2-M13 T3) Add a server from the header and remove it', async ({ page, ui }) => {
  await ui.open()
  await page.getByRole('banner').getByRole('button', { name: 'Add server', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Servers' })
  await expect(dialog.getByTestId('server-row')).toHaveCount(1)
  await expect(dialog.getByTestId('server-row')).toContainText('(this host)')

  await dialog.getByLabel('Nickname').fill('E2E second')
  await dialog.getByLabel('Host').fill(SERVER_HOST)
  await dialog.getByLabel('User').fill('dev')
  await dialog.getByRole('button', { name: 'Check host key' }).click()
  // Nothing is trusted before the owner sees the fingerprints.
  await expect(dialog.getByTestId('fingerprint').first()).toContainText('SHA256:')
  await expect(dialog.getByTestId('server-row')).toHaveCount(1)
  await dialog.getByRole('button', { name: 'Trust and add' }).click()

  const row = dialog.getByTestId('server-row').filter({ hasText: 'E2E second' })
  await expect(row).toContainText(`dev@${SERVER_HOST}:22`)
  await expect(row).toContainText('Connected', { timeout: 20_000 })

  await dialog.getByRole('button', { name: 'Remove E2E second' }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText('tmux sessions keep running')
  await confirm.getByRole('button', { name: 'Remove server' }).click()
  await expect(row).toHaveCount(0)
})

test('(V2-M13 T4) A project on another server shows its nickname chip and opens its session', async ({ request, ui }) => {
  const added = await addServer(request, 'E2E second')
  await waitForStatus(request, added.id, 'ok')
  const name = uniqueName('e2e-srv-proj')
  const dir = `/home/dev/${name}`
  await server.run(`mkdir -p ${shq(dir)}`)
  const project = await mutate(request, 'POST', '/api/projects', { machineId: added.id, path: dir, name })
  expect(project.status(), await project.text()).toBe(201)
  const session = uniqueName('e2e-srv-sess')
  const looseSession = uniqueName('e2e-srv-loose')
  try {
    await server.tmux('new-session', '-d', '-s', session, '-c', dir)
    await server.tmux('new-session', '-d', '-s', looseSession, '-c', '/tmp')
    await ui.open()
    const row = ui.treeItem(`${name}, on E2E second`)
    await expect(row).toBeVisible({ timeout: 20_000 })
    const projectChip = row.locator('[data-machine-chip]').first()
    await expect(projectChip).toHaveText('E2E second')
    await expect(projectChip).toHaveCSS('font-weight', '900')
    await expect(row.getByRole('treeitem', { name: session, exact: true })).toBeVisible()
    const looseRow = ui.treeItem(`${looseSession}, on E2E second`)
    const looseChip = looseRow.locator('[data-machine-chip]')
    await expect(looseChip).toHaveText('E2E second')
    await expect(looseChip).toHaveCSS('font-weight', '900')

    // The terminal attaches to the server's tmux, not the host's.
    await row.getByRole('button', { name: session, exact: true }).click()
    await ui.waitForTerminal(session)
    const terminalHeader = page.getByRole('region', { name: `Terminal: ${session}` })
    await expect(terminalHeader.locator('[data-terminal-machine-chip]')).toHaveText('E2E second')
    await expect(terminalHeader.locator('[data-terminal-machine-chip]')).toHaveCSS('font-weight', '900')
    const marker = uniqueName('on-server')
    await ui.type(`echo ${marker}`, true)
    await expect.poll(async () => server.capture(session), { timeout: 15_000 }).toContain(marker)
  } finally {
    await server.tmux('kill-session', '-t', `=${session}`).catch(() => {})
    await server.tmux('kill-session', '-t', `=${looseSession}`).catch(() => {})
  }
})

test('(V2-M13 T5) New session and Browse files ask for the server', async ({ page, request, ui }) => {
  const added = await addServer(request, 'E2E second')
  await waitForStatus(request, added.id, 'ok')
  const folder = uniqueName('e2e-srv-browse')
  const folderPath = `/home/dev/${folder}`
  await server.run(`mkdir -p ${shq(folderPath)}`)
  const session = uniqueName('e2e-srv-new')
  try {
    await ui.open()

    // New session: the Server select defaults to the host.
    await ui.headerAction('New session')
    const create = page.getByRole('dialog', { name: 'New session' })
    const picker = create.getByLabel('Server')
    await expect(picker).toHaveValue('host')
    await picker.selectOption({ label: 'E2E second' })
    await expect(create).toContainText('on E2E second')
    await create.getByLabel('Name').fill(session)
    await create.getByRole('button', { name: 'Create' }).click()
    await ui.waitForTerminal(session)
    await expect.poll(() => server.sessions()).toContain(session)

    // Browse files: the chosen server's files; the project lands there.
    const dialog = await ui.openFileBrowser()
    await dialog.getByLabel('Server').selectOption({ label: 'E2E second' })
    await dialog.getByLabel('Current path').fill(folderPath)
    await dialog.getByRole('button', { name: 'Go', exact: true }).click()
    await dialog.getByRole('button', { name: 'Add this directory as project' }).click()
    await expect(dialog.getByText(`Project: ${folder}`)).toBeVisible()
    const listed = await (await request.get('/api/projects', { params: { machine: added.id } })).json()
    expect((listed.projects as { path: string }[]).map((p) => p.path)).toContain(folderPath)
    await dialog.getByRole('button', { name: 'Close file browser' }).click()
    await ui.showList()
    const row = ui.treeItem(`${folder}, on E2E second`)
    await expect(row).toBeVisible()
    await expect(row.locator('[data-machine-chip]').first()).toHaveText('E2E second')
  } finally {
    await server.tmux('kill-session', '-t', `=${session}`).catch(() => {})
  }
})

test('(V2-M13 queues) Create queue on a server session opens its queue with the server chip', async ({ page, request, ui }) => {
  const added = await addServer(request, 'E2E second')
  await waitForStatus(request, added.id, 'ok')
  const name = uniqueName('e2e-srv-queue')
  const dir = `/home/dev/${name}`
  await server.run(`mkdir -p ${shq(dir)}`)
  const project = await mutate(request, 'POST', '/api/projects', { machineId: added.id, path: dir, name })
  expect(project.status(), await project.text()).toBe(201)
  const session = uniqueName('e2e-srv-qsess')
  await server.tmux('new-session', '-d', '-s', session, '-c', dir)
  try {
    await ui.open()
    const row = ui.treeItem(`${name}, on E2E second`)
    await expect(row.getByRole('treeitem', { name: session, exact: true })).toBeVisible({ timeout: 20_000 })
    await row.getByRole('button', { name: `More actions for ${session}` }).click()
    await page.getByRole('menuitem', { name: 'Create queue' }).click()
    const panel = page.getByRole('dialog', { name: 'Queue' })
    await expect(panel.getByTestId('queue-server')).toHaveText('E2E second')
    await expect(panel.getByText(dir, { exact: true })).toBeVisible()
    const { queues } = await (await request.get('/api/queues')).json() as { queues: { id: string; machineId: string; projectPath: string }[] }
    const mine = queues.filter((q) => q.projectPath === dir)
    expect(mine.map((q) => q.machineId)).toEqual([added.id])
    for (const q of mine) await mutate(request, 'DELETE', `/api/queues/${q.id}`)
  } finally {
    await server.tmux('kill-session', '-t', `=${session}`).catch(() => {})
  }
})
