import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { ctl } from '../helpers/ctl.ts'
import { getUIState, MACHINE, mutate, ORIGIN, putUIState } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

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
  await putUIState(api, 'tree', { version: 3, projects: ids, sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false, sections: [], projectSections: {} })
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
