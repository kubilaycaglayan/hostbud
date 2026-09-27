import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { ctl } from '../helpers/ctl.ts'
import { MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

async function addProject(request: Parameters<typeof mutate>[0], path: string, name: string) {
  const res = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name }, ORIGIN)
  expect(res.status(), await res.text()).toBe(201)
  return await res.json() as { id: string }
}

async function createTargetSession(target: { run(command: string): Promise<unknown> }, name: string, path: string) {
  await target.run(`tmux new-session -d -s ${shq(name)} -c ${shq(path)}`)
}

async function createAccount(ui: import('../helpers/ui.ts').UI) {
  const account = newAccount('e2e-project-tree')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)
}

async function openProjectSession(page: import('@playwright/test').Page, name: string) {
  await page.getByRole('button', { name: `New session in ${name}` }).click()
  const dialog = page.getByRole('dialog', { name: 'New session here' })
  await expect(dialog).toBeVisible()
  // (fix) Only that dialog opens, not the file browser behind it.
  await expect(page.getByRole('dialog', { name: 'Browse files' })).toHaveCount(0)
  return dialog
}

/** Opens the tree drawer when a terminal covers the tree (compact screens,
 * whatever the profile's viewport). */
async function returnToTree(page: import('@playwright/test').Page) {
  const trigger = page.locator('header button[aria-controls="sessions-sidebar"]')
  const tree = page.getByRole('navigation', { name: 'Project and session tree' })
  if (!(await tree.isVisible()) && (await trigger.getAttribute('aria-expanded')) !== 'true') await trigger.click()
  await expect(tree).toBeVisible()
}

test('(T5) Longest-prefix project mapping', async ({ page, target, request }) => {
  const base = `/home/dev/${uniqueName('e2e-prefix')}`
  const nested = `${base}/app`
  const sibling = `${base}/application`
  const parentName = uniqueName('parent-project')
  const nestedName = uniqueName('nested-project')
  const siblingName = uniqueName('sibling-project')
  await target.run(`mkdir -p ${shq(nested)} ${shq(sibling)} ${shq(`${base}-outside`)}`)
  await addProject(request, base, parentName)
  await addProject(request, nested, nestedName)
  await addProject(request, sibling, siblingName)
  const nestedSession = uniqueName('e2e-nested')
  const parentSession = uniqueName('e2e-parent')
  const siblingSession = uniqueName('e2e-sibling')
  const otherSession = uniqueName('e2e-other')
  await createTargetSession(target, nestedSession, nested)
  await createTargetSession(target, parentSession, base)
  await createTargetSession(target, siblingSession, sibling)
  await createTargetSession(target, otherSession, `${base}-outside`)
  await page.goto('/')
  await expect(page.getByRole('group', { name: `Sessions in ${nestedName}` }).getByRole('button', { name: nestedSession, exact: true })).toBeVisible()
  await expect(page.getByRole('group', { name: `Sessions in ${parentName}` }).getByRole('button', { name: parentSession, exact: true })).toBeVisible()
  await expect(page.getByRole('group', { name: `Sessions in ${siblingName}` }).getByRole('button', { name: siblingSession, exact: true })).toBeVisible()
  await expect(page.getByRole('group', { name: 'Other sessions' }).getByRole('button', { name: otherSession, exact: true })).toBeVisible()
})

test('(T5) Linked session rename and cleanup', async ({ page, target, request, ui }) => {
  const root = `/home/dev/${uniqueName('e2e-linked')}`
  const outside = `/home/dev/${uniqueName('e2e-reused')}`
  const projectName = uniqueName('linked-project')
  const sessionName = uniqueName('linked-session')
  await target.run(`mkdir -p ${shq(root)} ${shq(outside)}`)
  await addProject(request, root, projectName)
  await page.goto('/')
  await page.getByRole('button', { name: `New session in ${projectName}` }).click()
  await expect(page.getByRole('dialog', { name: 'New session here' })).toBeVisible()
  await expect(page.getByRole('dialog', { name: 'Browse files' })).toHaveCount(0)
  const create = page.getByRole('dialog', { name: 'New session here' })
  await create.getByLabel('Name', { exact: true }).fill(sessionName)
  await create.getByLabel('Start command').fill('sleep 6')
  await create.getByRole('button', { name: 'Create session' }).click()
  await expect.poll(async () => {
    const data = await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()
    return data.sessions.find((session: { name: string }) => session.name === sessionName)?.path
  }).toBe(root)
  await ui.showList()
  await ui.sessionAction(sessionName, 'Rename')
  const editor = page.getByRole('textbox', { name: `Rename ${sessionName}` })
  await editor.fill(`${sessionName}-renamed`)
  await editor.press('Enter')
  await expect(page.getByRole('group', { name: `Sessions in ${projectName}` }).getByRole('button', { name: `${sessionName}-renamed`, exact: true })).toBeVisible()
  await expect.poll(async () => {
    const data = await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()
    return data.sessions.some((session: { name: string }) => session.name === `${sessionName}-renamed`)
  }, { timeout: 15_000 }).toBe(false)
  await createTargetSession(target, `${sessionName}-renamed`, outside)
  await expect(page.getByRole('group', { name: 'Other sessions' }).getByRole('button', { name: `${sessionName}-renamed`, exact: true })).toBeVisible()
})

for (const profile of ['desktop', 'phone'] as const) {
  test.describe(`project tree ${profile}`, () => {
    test.use(profile === 'phone' ? { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true } : {})

    test('(T11) Remove a project without stopping its sessions, then add the folder again', async ({ page, ui, target, request }) => {
      await createAccount(ui)
      const root = `/home/dev/${uniqueName('e2e-remove-root')}`
      const folder = `${root}/app`
      const projectName = uniqueName('remove-project')
      const sessionName = uniqueName('remove-session')
      await target.run(`mkdir -p ${shq(folder)}`)
      const originalProject = await addProject(request, folder, projectName)
      await page.reload()
      await page.getByRole('button', { name: `New session in ${projectName}` }).click()
      const create = page.getByRole('dialog', { name: 'New session here' })
      await create.getByLabel('Name').fill(sessionName)
      await create.getByLabel('Start command').fill('sleep 3600')
      await create.getByRole('button', { name: 'Create session' }).click()
      await expect(page.getByRole('region', { name: `Terminal: ${sessionName}` })).toBeVisible()
      const clientPID = (await target.run(`tmux list-clients -t ${shq('=' + sessionName)} -F '#{client_pid}'`)).trim()

      await ui.showList()
      const row = ui.treeItem(projectName)
      if (profile === 'phone') {
        const header = row.locator(':scope > div')
        await header.dispatchEvent('pointerdown', { pointerType: 'touch', clientX: 14, clientY: 14 })
        await page.waitForTimeout(550)
        await header.dispatchEvent('pointerup', { pointerType: 'touch', clientX: 14, clientY: 14 })
      } else {
        await row.getByRole('button', { name: `More actions for ${projectName}` }).click()
      }
      const removeItem = page.getByRole('menuitem', { name: 'Remove project…', exact: true })
      await expect(removeItem).toBeVisible()
      await removeItem.click()
      const confirmation = page.getByRole('alertdialog', { name: `Remove project ${projectName}?` })
      await expect(confirmation).toContainText('Its 1 session keeps running and move to')
      await expect(confirmation).toContainText(`Files in ~/${root.split('/').at(-1)}/app aren't touched.`)
      await expect(confirmation).toContainText('This removes it for every account.')
      await confirmation.getByRole('button', { name: 'Cancel' }).click()
      await expect(ui.treeItem(projectName)).toBeVisible()
      expect((await target.run(`tmux list-clients -t ${shq('=' + sessionName)} -F '#{client_pid}'`)).trim()).toBe(clientPID)

      if (profile === 'phone') {
        const header = ui.treeItem(projectName).locator(':scope > div')
        await header.dispatchEvent('pointerdown', { pointerType: 'touch', clientX: 14, clientY: 14 })
        await page.waitForTimeout(550)
        await header.dispatchEvent('pointerup', { pointerType: 'touch', clientX: 14, clientY: 14 })
      } else await ui.treeItem(projectName).getByRole('button', { name: `More actions for ${projectName}` }).click()
      await page.getByRole('menuitem', { name: 'Remove project…', exact: true }).click()
      await page.getByRole('alertdialog', { name: `Remove project ${projectName}?` }).getByRole('button', { name: 'Remove project' }).click()
      await expect(ui.treeItem(projectName)).toHaveCount(0)
      await expect(page.getByRole('group', { name: 'Other sessions' }).getByRole('button', { name: sessionName, exact: true })).toBeVisible()
      expect((await target.run(`tmux list-clients -t ${shq('=' + sessionName)} -F '#{client_pid}'`)).trim()).toBe(clientPID)
      expect(await target.run(`test -d ${shq(folder)} && echo exists`)).toContain('exists')

      await page.reload()
      await ctl.restartApp()
      await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
      await page.reload()
      await expect(ui.treeItem(projectName)).toHaveCount(0)

      await ui.openFileBrowser()
      await page.getByLabel('Current path').fill(root)
      await page.getByRole('button', { name: 'Go' }).click()
      await page.getByRole('button', { name: 'Add app as project' }).click()
      await expect(page.getByText('Project: app')).toBeVisible()
      const list = await request.get('/api/projects?machine=host')
      const projects = (await list.json()).projects as { id: string; path: string }[]
      const recreated = projects.find((item) => item.path === folder)
      expect(recreated).toBeTruthy()
      expect(recreated?.id).not.toBe(originalProject.id)
      const recent = await request.get(`/api/projects/${recreated!.id}/recent-commands`)
      expect(await recent.json()).toEqual({ commands: [] })
    })

    test('(T5) Other sessions and Save as project', async ({ page, target, request }) => {
      const path = `/home/dev/${uniqueName('e2e-unmatched')}`
      const name = uniqueName('e2e-loose')
      await target.run(`mkdir -p ${shq(path)}`)
      await createTargetSession(target, name, path)
      await page.goto('/')
      const row = page.getByRole('group', { name: 'Other sessions' }).getByRole('treeitem', { name, exact: true })
      await expect(row).toBeVisible()
      const original = (await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions.find((s: { name: string }) => s.name === name)
      await expect(page.getByRole('button', { name: `Save ${name} as project` })).toHaveCount(0)
      await row.getByRole('button', { name, exact: true }).click()
      await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
      await returnToTree(page)
      await row.getByRole('button', { name: `More actions for ${name}` }).click()
      await page.getByRole('menuitem', { name: 'Save as project' }).click()
      await expect(page.getByRole('group', { name: `Sessions in ${path.split('/').at(-1)}` }).getByRole('button', { name, exact: true })).toBeVisible()
      const after = (await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions.find((s: { name: string }) => s.name === name)
      expect(after).toMatchObject({ id: original.id, path: original.path, name: original.name })
    })

    test('(T5) Distinct project tree entries', async ({ page, target, request }) => {
      const root = `/home/dev/${uniqueName('e2e-distinct')}`
      const displayName = uniqueName('same-display-name')
      await target.run(`mkdir -p ${shq(`${root}/one`)} ${shq(`${root}/two`)}`)
      await addProject(request, `${root}/one`, displayName)
      await addProject(request, `${root}/two`, displayName)
      await page.goto('/')
      await expect(page.getByRole('group', { name: 'Projects' }).getByRole('treeitem', { name: displayName, exact: true })).toHaveCount(2)
    })

    test('(T5) Left bar custom order', async ({ page, target, request, ui }) => {
      const orderPrefix = uniqueName(`order-${profile}`)
      const firstName = `${orderPrefix}-first`
      const secondName = `${orderPrefix}-second`
      const thirdName = `${orderPrefix}-third`
      const root = `/home/dev/${uniqueName('e2e-order')}`
      const first = `${root}/first`
      const second = `${root}/second`
      const account = newAccount(`e2e-tree-${profile}`)
      forbidInLogs(account.email, account.password)
      await owner.allow(account.email)
      await target.run(`mkdir -p ${shq(first)} ${shq(second)}`)
      await ui.dropSession()
      await expect(ui.authForm().tab('Sign in')).toBeVisible()
      await ui.authForm().tab('Create account').click()
      await ui.authForm().email.fill(account.email)
      await ui.authForm().password.fill(account.password)
      await ui.authForm().submit('Create account').click()
      await expect(ui.tree()).toBeVisible()
      await addProject(request, first, firstName)
      await addProject(request, second, secondName)
      const one = uniqueName('e2e-order')
      const two = uniqueName('e2e-order')
      await createTargetSession(target, one, first)
      await createTargetSession(target, two, first)
      await page.goto('/')
      await page.getByRole('button', { name: `Drag to reorder project ${secondName}` }).dragTo(page.getByRole('button', { name: `Drag to reorder project ${firstName}` }))
      await page.getByRole('button', { name: `Drag to reorder session ${two}` }).dragTo(page.getByRole('button', { name: `Drag to reorder session ${one}` }))
      await target.run(`mkdir -p ${shq(`${root}/third`)}`)
      await addProject(request, `${root}/third`, thirdName)
      const addedSession = uniqueName('e2e-order-new')
      await createTargetSession(target, addedSession, `${root}/third`)
      const projectNames = () => page.getByRole('group', { name: 'Projects' }).locator(':scope > [role="treeitem"]').evaluateAll((items) => items.map((item) => item.getAttribute('aria-label') ?? ''))
      const orderedTestProjects = async () => (await projectNames()).map((name) => name.trim()).filter((name) => [firstName, secondName, thirdName].includes(name))
      await expect.poll(orderedTestProjects).toEqual([secondName, firstName, thirdName])
      await expect.poll(async () => page.getByRole('group', { name: `Sessions in ${firstName}` }).locator('button[data-session-row]').allTextContents()).toEqual([two, one])
      await expect(page.getByRole('group', { name: `Sessions in ${thirdName}` }).locator('button[data-session-row]')).toHaveText(addedSession)
      await page.waitForTimeout(650)
      await page.reload()
      await expect.poll(orderedTestProjects).toEqual([secondName, firstName, thirdName])
      await ui.signOut()
      await expect(ui.authForm().tab('Sign in')).toBeVisible()
      await ui.signIn(account.email, account.password)
      await expect.poll(orderedTestProjects).toEqual([secondName, firstName, thirdName])
      await ctl.restartApp()
      await expect.poll(async () => (await request.get('/api/health')).status(), { timeout: 20_000 }).toBe(200)
      await page.reload()
      await expect.poll(orderedTestProjects).toEqual([secondName, firstName, thirdName])
    })

    test('(T5) Left bar session actions', async ({ page, target, ui }) => {
      const name = uniqueName('e2e-actions')
      const path = `/home/dev/${name}`
      await target.run(`mkdir -p ${shq(path)}`)
      await createTargetSession(target, name, path)
      await page.goto('/')
      const session = page.locator('[data-session-row]').filter({ hasText: name })
      await expect(session).toBeVisible()
      const row = page.getByRole('treeitem', { name, exact: true })
      // M8 T2: the name leads; status and expand sit before the ⋯ group.
      const actionsFollowTitle = await row.evaluate((element, more) => {
        const title = element.querySelector('[data-session-row]')
        const trigger = element.querySelector(`[aria-label="${more}"]`)
        const actions = trigger?.closest('span')
        return Boolean(title && actions && actions.parentElement === title.parentElement &&
          title.compareDocumentPosition(actions) & Node.DOCUMENT_POSITION_FOLLOWING && actions.querySelectorAll('button').length === 1)
      }, `More actions for ${name}`)
      expect(actionsFollowTitle).toBe(true)
      const labels = await row.locator('button').evaluateAll((buttons) => buttons.map((button) => button.getAttribute('aria-label')).filter(Boolean))
      expect(labels).toContain(`More actions for ${name}`)
      expect(labels).not.toContain(`Kill ${name}`)
      expect(labels).not.toContain(`Rename ${name}`)
      await row.getByRole('button', { name: `More actions for ${name}` }).click()
      await expect(page.getByRole('menuitem', { name: 'Open in split right' })).toBeVisible()
      await expect(page.getByRole('menuitem', { name: 'Kill…' })).toBeVisible()
      await page.getByRole('menuitem', { name: 'Rename', exact: true }).click()
      // Rename is inline (M5 T4).
      const editor = page.getByRole('textbox', { name: `Rename ${name}` })
      await editor.fill(`${name}-renamed`)
      await editor.press('Enter')
      await expect(page.locator(`[data-session-row][aria-label="${name}-renamed"]`)).toBeVisible()
      await ui.sessionAction(`${name}-renamed`, 'Kill…')
      await expect(page.getByRole('alertdialog', { name: `Kill session ${name}-renamed?` })).toBeVisible()
      await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel' }).click()
    })

    test('(T5) No window counts', async ({ page, target }) => {
      const name = uniqueName('e2e-no-windows')
      const many = uniqueName('e2e-many-windows')
      const path = `/home/dev/${name}`
      const manyPath = `/home/dev/${many}`
      await target.run(`mkdir -p ${shq(path)} ${shq(manyPath)}`)
      await createTargetSession(target, name, path)
      await createTargetSession(target, many, manyPath)
      await target.run(`tmux new-window -t ${shq(many)}`)
      await page.goto('/')
      for (const sessionName of [name, many]) {
        const row = page.getByRole('treeitem', { name: sessionName, exact: true })
        await expect(row).toBeVisible()
        await expect(row).not.toContainText(/\b\d+ windows?\b/)
      }
    })

    test('(T5) Account controls in app header', async ({ page, target, ui }) => {
      await createAccount(ui) // it signs out; keep the shared session valid
      const name = uniqueName('e2e-account-header')
      const path = `/home/dev/${name}`
      await target.run(`mkdir -p ${shq(path)}`)
      await createTargetSession(target, name, path)
      await page.goto('/')
      const header = page.locator('header')
      await ui.expectAccountEmail(/@/)
      await expect(header.getByRole('button', { name: 'Sign out' })).toBeVisible()
      await page.getByRole('button', { name, exact: true }).click()
      await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
      if (profile === 'desktop') await page.getByRole('button', { name: 'Hide sidebar' }).click()
      if (profile === 'desktop') await expect(ui.tree()).toBeHidden()
      await ui.expectAccountEmail(/@/)
      await ui.signOut()
      await expect(page.getByRole('tab', { name: 'Sign in' })).toBeVisible()
    })

    test('(T6) Recent start command', async ({ page, target, request }) => {
      const path = `/home/dev/${uniqueName('e2e-recent')}`
      const projectName = uniqueName('recent-project')
      const firstSession = uniqueName('recent-first')
      const secondSession = uniqueName('recent-second')
      await target.run(`mkdir -p ${shq(path)}`)
      const project = await addProject(request, path, projectName)
      await page.goto('/')
      let dialog = await openProjectSession(page, projectName)
      await dialog.getByLabel('Name').fill(firstSession)
      await dialog.getByLabel('Start command').fill('sleep 30')
      await dialog.getByRole('button', { name: 'Create session' }).click()
      await expect.poll(async () => {
        const data = await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()
        return data.sessions.some((session: { name: string; path: string }) => session.name === firstSession && session.path === path)
      }).toBe(true)
      await expect.poll(() => target.display(firstSession, '#{pane_current_command}')).toBe('sleep')
      await returnToTree(page)
      dialog = await openProjectSession(page, projectName)
      const suggestion = dialog.getByRole('button', { name: 'Use recent command sleep 30' })
      await expect(suggestion).toBeVisible()
      const before = (await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions.length
      await suggestion.click()
      expect((await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions).toHaveLength(before)
      await dialog.getByLabel('Name').fill(secondSession)
      await dialog.getByRole('button', { name: 'Create session' }).click()
      await expect.poll(async () => {
        const data = await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()
        return data.sessions.some((session: { name: string; path: string }) => session.name === secondSession && session.path === path)
      }).toBe(true)
      await expect.poll(() => target.display(secondSession, '#{pane_current_command}')).toBe('sleep')
      const commands = await (await request.get(`/api/projects/${project.id}/recent-commands`)).json()
      expect(commands.commands[0]).toBe('sleep 30')
    })
  })
}

test('(T6) Recent commands are project-scoped and require selection', async ({ page, target, request, ui }) => {
  const firstPath = `/home/dev/${uniqueName('e2e-recent-one')}`
  const secondPath = `/home/dev/${uniqueName('e2e-recent-two')}`
  const firstName = uniqueName('recent-one')
  const secondName = uniqueName('recent-two')
  await target.run(`mkdir -p ${shq(firstPath)} ${shq(secondPath)}`)
  await addProject(request, firstPath, firstName)
  await addProject(request, secondPath, secondName)
  await page.goto('/')
  const first = await openProjectSession(page, firstName)
  await first.getByLabel('Name').fill(uniqueName('recent-seed'))
  await first.getByLabel('Start command').fill('sleep 30')
  await first.getByRole('button', { name: 'Create session' }).click()
  await expect.poll(async () => (await (await request.get(`/api/projects?machine=${MACHINE}`)).json()).projects.length).toBeGreaterThanOrEqual(2)
  await ui.showList()
  const before = (await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions.length
  const second = await openProjectSession(page, secondName)
  await expect(second.getByRole('button', { name: /^Use recent command/ })).toHaveCount(0)
  expect((await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions).toHaveLength(before)
  await second.getByRole('button', { name: 'Cancel' }).click()
  await ui.showList()
  const firstAgain = await openProjectSession(page, firstName)
  const suggestion = firstAgain.getByRole('button', { name: 'Use recent command sleep 30' })
  await suggestion.click()
  expect((await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions).toHaveLength(before)
  await firstAgain.getByLabel('Name').fill(uniqueName('recent-selected'))
  await firstAgain.getByRole('button', { name: 'Create session' }).click()
  await expect.poll(async () => (await (await request.get(`/api/machines/${MACHINE}/sessions`)).json()).sessions.length).toBe(before + 1)
})
