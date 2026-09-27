import type { Locator } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, getUIState, MACHINE, mutate, ORIGIN, putUIState } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Compact, name-first project tree (M8 T2).

async function account(ui: UI) {
  const fresh = newAccount('e2e-tree-compact')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
}

async function box(locator: Locator) {
  const b = await locator.boundingBox()
  if (!b) throw new Error('not visible')
  return b
}

test('(T2) Compact tree', async ({ page, ui, target, isMobile }) => {
  test.setTimeout(90_000)
  await account(ui)
  const api = page.context().request
  const projects = [uniqueName('e2e-compact-pa'), uniqueName('e2e-compact-pb')]
  const ids: string[] = []
  for (const name of projects) {
    await target.run(`mkdir -p ${shq(`/home/dev/${name}`)}`)
    const res = await mutate(api, 'POST', '/api/projects', { machineId: MACHINE, path: `/home/dev/${name}`, name }, ORIGIN)
    expect(res.status()).toBe(201)
    ids.push(((await res.json()) as { id: string }).id)
  }
  await putUIState(api, 'tree', { version: 2, projects: ids, sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false })
  const single = uniqueName('e2e-compact-one')
  const multi = uniqueName('e2e-compact-two')
  await target.tmux('new-session', '-d', '-s', single, '-c', `/home/dev/${projects[0]}`)
  await target.tmux('new-session', '-d', '-s', multi, '-c', `/home/dev/${projects[0]}`)
  await target.tmux('new-window', '-d', '-t', `=${multi}:`, '-n', 'extra')
  await page.reload()
  if (isMobile) await ui.showList()
  await expect(ui.treeItem(single)).toBeVisible()

  // Project controls: drag handle and expand sit side by side.
  const project = ui.treeItem(projects[0])
  const handle = project.getByRole('button', { name: `Drag to reorder project ${projects[0]}` })
  const collapse = project.getByRole('button', { name: `Collapse ${projects[0]}` })
  const h = await box(handle)
  const c = await box(collapse)
  expect(c.x - (h.x + h.width)).toBeLessThanOrEqual(4)
  if (!isMobile) {
    expect(h.width).toBeLessThanOrEqual(24)
    expect(c.width).toBeLessThanOrEqual(28)
  }

  // Session rows: the name leads and stands out; no inert chevron.
  const singleRow = ui.treeItem(single)
  await expect(singleRow.getByRole('button', { name: `Expand ${single}` })).toHaveCount(0)
  await expect(singleRow).not.toHaveAttribute('aria-expanded', /.*/)
  const name = singleRow.getByRole('button', { name: single, exact: true })
  const nameBox = await box(name)
  const rowBox = await box(singleRow)
  expect(nameBox.x - rowBox.x).toBeLessThanOrEqual(8)
  expect(Number(await name.evaluate((el) => getComputedStyle(el).fontWeight))).toBeGreaterThanOrEqual(500)
  const others = await singleRow.getByRole('button').evaluateAll((els, label) => els.filter((el) => el.getAttribute('aria-label') !== label).map((el) => el.getBoundingClientRect().x), single)
  for (const x of others) expect(x).toBeGreaterThan(nameBox.x)
  // Rows are compact on desktop; phones keep 44 px targets.
  if (isMobile) {
    for (const button of await singleRow.getByRole('button').all()) {
      const b = await box(button)
      expect(b.height).toBeGreaterThanOrEqual(44)
    }
  } else {
    expect(rowBox.height).toBeLessThanOrEqual(34)
  }
  await page.screenshot({ path: test.info().outputPath(`tree-${isMobile ? 'phone' : 'desktop'}.png`) })

  // A row that really expands keeps its chevron, and it works.
  const multiRow = ui.treeItem(multi)
  await multiRow.getByRole('button', { name: `Expand ${multi}` }).click()
  await expect(page.locator(`[data-tree-key^="window:host/${multi}/"]`)).toHaveCount(2)
  await multiRow.getByRole('button', { name: `Collapse ${multi}` }).click()
  await expect(page.locator(`[data-tree-key^="window:host/${multi}/"]`)).toHaveCount(0)

  // Collapse and expand a project.
  await collapse.click()
  await expect(project).toHaveAttribute('aria-expanded', 'false')
  await expect(ui.treeItem(single)).toBeHidden()
  await project.getByRole('button', { name: `Expand ${projects[0]}` }).click()
  await expect(ui.treeItem(single)).toBeVisible()

  // Reorder projects with the handle.
  await ui.waitForSave('tree')
  await ui.treeItem(projects[1]).getByRole('button', { name: `Drag to reorder project ${projects[1]}` }).dragTo(project, { targetPosition: { x: 20, y: 1 } })
  await expect.poll(async () => ((await getUIState(api, 'tree')) as { projects: string[] }).projects.filter((id) => ids.includes(id))).toEqual([ids[1], ids[0]])

  // Keyboard (desktop): a single-window session doesn't expand; Enter opens it.
  if (!isMobile) {
    await ui.treeItem(single).focus()
    await page.keyboard.press('ArrowRight')
    await expect(ui.treeItem(single)).toBeFocused()
    await expect(ui.treeItem(single)).not.toHaveAttribute('aria-expanded', /.*/)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('region', { name: `Terminal: ${single}` })).toBeVisible()
    await ui.showList()
  }

  // Selecting a session still opens it.
  if (isMobile) await ui.showList()
  await ui.treeItem(multi).getByRole('button', { name: multi, exact: true }).click()
  await expect(page.getByRole('region', { name: `Terminal: ${multi}` })).toBeVisible()
  for (const id of ids) expect((await mutate(api, 'DELETE', `/api/projects/${id}`)).status()).toBe(204)
})
