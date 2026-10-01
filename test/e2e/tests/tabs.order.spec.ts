import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { shq, uniqueName, type Target } from '../helpers/target.ts'

async function account(ui: import('../helpers/ui.ts').UI) {
  const fresh = newAccount('e2e-visible-session-cycle')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
}

async function sessions(target: Target, path: string, ...prefixes: string[]): Promise<string[]> {
  const names = prefixes.map((prefix) => uniqueName(prefix))
  for (const name of names) await target.tmux('new-session', '-d', '-s', name, '-c', path)
  return names
}

test.beforeEach(async ({ target }) => { await target.resetTmux() })

async function visibleOpenOrder(page: import('@playwright/test').Page, names: string[]) {
  return page.locator('[data-tree-key^="session:"]').evaluateAll((rows, openNames) =>
    rows.map((row) => row.getAttribute('data-tree-key')!.slice('session:'.length)).filter((name) => openNames.includes(name)), names,
  )
}

async function activeSession(ui: import('../helpers/ui.ts').UI) {
  return ui.activeTabName()
}

test('(T22) Visible open-session shortcuts replace tab strips on desktop', { tag: '@desktop' }, async ({ page, ui, target, request }) => {
  await account(ui)
  const root = `/home/dev/${uniqueName('cycle-root')}`
  const firstProject = uniqueName('cycle-project-a')
  const secondProject = uniqueName('cycle-project-b')
  await target.run(`mkdir -p ${shq(`${root}/a`)} ${shq(`${root}/b`)}`)
  for (const [path, name] of [[`${root}/a`, firstProject], [`${root}/b`, secondProject]]) {
    const response = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name }, ORIGIN)
    expect(response.status(), await response.text()).toBe(201)
  }
  const [a, b] = await sessions(target, `${root}/a`, 'cycle-a', 'cycle-b')
  const [c, d, unopened] = await sessions(target, `${root}/b`, 'cycle-c', 'cycle-d', 'cycle-unopened')
  await page.reload()
  for (const name of [a, b, c, d]) await ui.openTerminal(name)

  await expect(page.getByRole('tablist', { name: 'Open terminals' })).toHaveCount(0)
  const order = await visibleOpenOrder(page, [a, b, c, d])
  expect(order).toHaveLength(4)
  expect(await ui.tabNames()).toEqual([a, b, c, d])
  const current = await activeSession(ui)
  const next = order[(order.indexOf(current) + 1) % order.length]
  await page.keyboard.press('Control+Shift+]')
  await expect.poll(() => activeSession(ui)).toBe(next)

  await page.keyboard.press('Control+Shift+[')
  await expect.poll(() => activeSession(ui)).toBe(current)
  expect(await target.sessions()).toContain(unopened)

  // Collapsing the first project skips its open sessions and cycles the other project.
  await ui.showList()
  await ui.treeItem(firstProject).getByRole('button', { name: `Collapse ${firstProject}` }).click()
  await page.keyboard.press('Control+Shift+]')
  await expect.poll(() => activeSession(ui)).toBe(c)
  expect(await visibleOpenOrder(page, [a, b, c, d])).toEqual([c, d])
  const closed = await activeSession(ui)

  await page.keyboard.press('Control+Shift+k')
  await page.getByRole('combobox', { name: 'Command palette' }).fill('Close terminal view')
  await page.getByRole('option', { name: 'Close terminal view' }).click()
  await expect.poll(() => target.display(closed, '#{session_attached}')).toBe('0')
  expect(await target.sessions()).toContain(closed)
})

test('(T22) Phone terminal has no tab strip and session cycling still works', { tag: '@phone' }, async ({ page, ui, target }) => {
  await account(ui)
  const [a, b] = await sessions(target, '/home/dev', 'cycle-phone-a', 'cycle-phone-b')
  await page.reload()
  await ui.openTerminal(a)
  await ui.openTerminal(b)
  await expect(page.getByRole('tablist', { name: 'Open terminals' })).toHaveCount(0)
  await ui.showList()
  const order = await visibleOpenOrder(page, [a, b])
  const current = await activeSession(ui)
  await page.keyboard.press('Control+Shift+[')
  await expect.poll(() => activeSession(ui)).toBe(order[(order.indexOf(current) - 1 + order.length) % order.length])
})
