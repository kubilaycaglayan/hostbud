import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { ctl } from '../helpers/ctl.ts'
import { getUIState, listSessions, MACHINE, mutate, ORIGIN, POLL_INTERVAL_MS, putUIState } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

// M8 T17/T18: named, reorderable visual groupings in the project tree.
test('(T17, T18) Project sections and ordering', async ({ page, ui, isMobile }) => {
  test.setTimeout(90_000)
  const account = newAccount('e2e-tree-sections')
  await owner.allow(account.email)
  await ui.createAccount(account)
  const api = page.context().request
  const names = [uniqueName('section-a'), uniqueName('section-b')]
  const ids: string[] = []
  for (const name of names) {
    const response = await mutate(api, 'POST', '/api/projects', { machineId: MACHINE, path: `/home/dev/${name}`, name }, ORIGIN)
    expect(response.status()).toBe(201)
    ids.push(((await response.json()) as { id: string }).id)
  }
  await putUIState(api, 'tree', { version: 4, projects: ids, sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], collapsedSections: [], expanded: [], showHidden: false, sections: [], projectSections: {} })
  await page.reload()
  if (isMobile) await ui.showList()
  const gutter = page.getByRole('tree', { name: 'Projects and sessions' })
  const gutterWidth = (await gutter.boundingBox())!.width
  const gutterNav = page.getByRole('navigation', { name: 'Project and session tree' })
  const createSection = page.getByRole('button', { name: 'Create a new section' })
  const footerBox = (await createSection.boundingBox())!
  const navBox = (await gutterNav.boundingBox())!
  expect(Math.abs(footerBox.y + footerBox.height - navBox.y - navBox.height)).toBeLessThanOrEqual(3)
  expect(await createSection.evaluate((element) => element.closest('[role="tree"]'))).toBeNull()
  const originalRow = ui.treeItem(names[0])
  const originalX = (await originalRow.boundingBox())!.x

  await page.getByRole('button', { name: 'Create a new section' }).click()
  const dialog = page.getByRole('dialog', { name: 'Create a new section' })
  await dialog.getByLabel('Section name').fill('Research')
  await dialog.getByRole('button', { name: 'green' }).click()
  await dialog.getByRole('button', { name: 'Save' }).click()
  const section = page.getByRole('group', { name: 'Research section' })
  await expect(section).toContainText('Empty section')
  expect(await section.locator('header button').evaluateAll((buttons) => buttons.map((button) => button.getAttribute('aria-label')))).toEqual([
    'Collapse section Research',
    'Edit section Research',
    'Drag to reorder section Research',
  ])

  await createSection.click()
  const secondDialog = page.getByRole('dialog', { name: 'Create a new section' })
  await secondDialog.getByLabel('Section name').fill('Planning')
  await secondDialog.getByRole('button', { name: 'orange' }).click()
  await secondDialog.getByRole('button', { name: 'Save' }).click()

  await originalRow.getByRole('button', { name: `More actions for ${names[0]}` }).click()
  await page.getByRole('menuitem', { name: 'Move to Research' }).click()
  await expect(page.getByRole('menu')).toHaveCount(0)
  await expect(section.getByRole('treeitem', { name: new RegExp(names[0]) })).toBeVisible()
  const movedRow = section.getByRole('treeitem', { name: new RegExp(names[0]) })
  expect(Math.abs((await movedRow.boundingBox())!.x - originalX)).toBeLessThanOrEqual(4)
  expect((await gutter.boundingBox())!.width).toBe(gutterWidth)
  await page.screenshot({ path: test.info().outputPath(`tree-sections-${isMobile ? 'phone' : 'desktop'}.png`) })
  await ui.waitForSave('tree')

  await section.getByRole('button', { name: 'Edit section Research' }).click()
  const edit = page.getByRole('dialog', { name: 'Edit section' })
  await edit.getByLabel('Section name').fill('Tools')
  await edit.getByRole('button', { name: 'purple' }).click()
  await edit.getByRole('button', { name: 'Save' }).click()
  await ui.waitForSave('tree')
  const sectionOrder = page.locator('[data-section-order-list] > section[data-project-section-id]')
  expect(await page.locator('[data-section-order-list]').evaluate((element) => getComputedStyle(element).rowGap)).toBe('4px')
  await page.getByRole('button', { name: 'Drag to reorder section Tools' }).dragTo(page.getByRole('button', { name: 'Drag to reorder section Planning' }))
  await expect(sectionOrder).toHaveCount(2)
  await expect(sectionOrder.nth(0)).toHaveAttribute('aria-label', 'Planning section')
  await expect(sectionOrder.nth(1)).toHaveAttribute('aria-label', 'Tools section')
  await ui.waitForSave('tree')
  await page.reload()
  if (isMobile) await ui.showList()
  await expect(page.getByRole('group', { name: 'Tools section' })).toBeVisible()
  await ctl.restartApp()
  await page.reload()
  if (isMobile) await ui.showList()
  await expect(page.getByRole('group', { name: 'Tools section' })).toBeVisible()
  expect(await getUIState(api, 'tree')).toMatchObject({ sections: [{ name: 'Planning', color: 'orange' }, { name: 'Tools', color: 'purple' }], projectSections: { [ids[0]]: expect.any(String) } })

  await page.getByRole('group', { name: 'Tools section' }).getByRole('button', { name: 'Edit section Tools' }).click()
  await page.getByRole('dialog', { name: 'Edit section' }).getByRole('button', { name: 'Delete section' }).click()
  await expect(ui.treeItem(names[0])).toBeVisible()
  await expect(page.getByRole('group', { name: 'Tools section' })).toHaveCount(0)
  for (const id of ids) expect((await mutate(api, 'DELETE', `/api/projects/${id}`)).status()).toBe(204)
})

test('(T19, T20) Selected content stays marked when its project or section collapses', async ({ page, ui, isMobile, target }) => {
  test.setTimeout(90_000)
  const account = newAccount('e2e-collapsed-selection')
  await owner.allow(account.email)
  await ui.createAccount(account)
  const api = page.context().request
  const names = [uniqueName('collapsed-a'), uniqueName('collapsed-b')]
  const sessions = [uniqueName('selected-a'), uniqueName('selected-b')]
  const projects: { id: string; path: string }[] = []
  try {
    for (const name of names) {
      const path = `/home/dev/${name}`
      const response = await mutate(api, 'POST', '/api/projects', { machineId: MACHINE, path, name }, ORIGIN)
      expect(response.status()).toBe(201)
      projects.push({ id: ((await response.json()) as { id: string }).id, path })
    }
    for (let index = 0; index < sessions.length; index++) {
      await target.run(`mkdir -p ${shq(projects[index].path)} && tmux new-session -d -s ${shq(sessions[index])} -c ${shq(projects[index].path)}`)
    }
    await expect.poll(async () => (await listSessions(api)).map((session) => session.name), { timeout: 3 * POLL_INTERVAL_MS })
      .toEqual(expect.arrayContaining(sessions))
    await page.reload()
    if (isMobile) await ui.showList()
    for (const name of names) await expect(ui.treeItem(name)).toBeVisible()
    for (const session of sessions) await expect(ui.treeItem(session)).toBeVisible()

    await page.getByRole('button', { name: 'Create a new section' }).click()
    const dialog = page.getByRole('dialog', { name: 'Create a new section' })
    await dialog.getByLabel('Section name').fill('Selected work')
    await dialog.getByRole('button', { name: 'purple' }).click()
    await dialog.getByRole('button', { name: 'Save' }).click()
    for (const name of names) {
      await ui.treeItem(name).getByRole('button', { name: `More actions for ${name}` }).click()
      await page.getByRole('menuitem', { name: 'Move to Selected work' }).click()
    }
    const section = page.locator('[data-section-order-list] > section[data-project-section-id]')

    await ui.treeItem(sessions[0]).click()
    if (isMobile) await ui.showList()
    await expect(ui.treeItem(sessions[0])).toHaveAttribute('aria-selected', 'true')
    const firstProject = ui.treeItem(names[0])
    await firstProject.getByRole('button', { name: `Collapse ${names[0]}` }).click()
    await expect(firstProject).toHaveAttribute('aria-selected', 'true')
    await expect(firstProject.locator(':scope > .tree-row')).toHaveClass(/bg-selected/)
    await expect(ui.treeItem(sessions[0])).toHaveCount(0)

    await ui.treeItem(sessions[1]).click()
    if (isMobile) await ui.showList()
    await expect(ui.treeItem(sessions[1])).toHaveAttribute('aria-selected', 'true')
    await expect(firstProject).not.toHaveAttribute('aria-selected', 'true')
    const secondProject = ui.treeItem(names[1])
    await secondProject.getByRole('button', { name: `Collapse ${names[1]}` }).click()
    await expect(secondProject).toHaveAttribute('aria-selected', 'true')

    await firstProject.getByRole('button', { name: `Expand ${names[0]}` }).click()
    await ui.treeItem(sessions[0]).click()
    if (isMobile) await ui.showList()
    const sectionID = await section.getAttribute('data-project-section-id')
    await section.getByRole('button', { name: 'Collapse section Selected work' }).click()
    await expect(section).toHaveAttribute('data-selected-session', 'true')
    await expect(section).toHaveAttribute('aria-label', 'Selected work section, contains selected session')
    await expect(ui.treeItem(names[0])).toHaveCount(0)
    await expect(ui.treeItem(names[1])).toHaveCount(0)
    await expect(section.getByRole('button', { name: 'Expand section Selected work' })).toHaveAttribute('aria-expanded', 'false')
    await ui.waitForSave('tree')
    expect(await getUIState(api, 'tree')).toMatchObject({ version: 4, collapsedSections: [sectionID] })
    await page.reload()
    if (isMobile) await ui.showList()
    await expect(section.getByRole('button', { name: 'Expand section Selected work' })).toHaveAttribute('aria-expanded', 'false')
    await ctl.restartApp()
    await page.reload()
    if (isMobile) await ui.showList()
    await expect(section.getByRole('button', { name: 'Expand section Selected work' })).toHaveAttribute('aria-expanded', 'false')
  } finally {
    for (const project of projects) await mutate(api, 'DELETE', `/api/projects/${project.id}`)
  }
})
