import type { Page } from '@playwright/test'
import { ctl } from '../helpers/ctl.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { promptLine } from '../helpers/shell.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Split view (M3 T8): up to 4 panes per tab, nested rows and columns.

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

const attached = (target: Target, name: string) => target.display(name, '#{session_attached}')
const width = async (target: Target, name: string) => Number(await target.display(name, '#{window_width}'))

async function newSessions(target: Target, ...prefixes: string[]): Promise<string[]> {
  const names = prefixes.map((p) => uniqueName(p))
  for (const n of names) await target.tmux('new-session', '-d', '-s', n, '-c', '/home/dev')
  return names
}

/** Opens a in a tab, then b right of a, then c below b: row[a, column[b, c]]. */
async function threePanes(ui: UI, target: Target) {
  const [a, b, c] = await newSessions(target, 'e2e-sa', 'e2e-sb', 'e2e-sc')
  await ui.open()
  await ui.openTerminal(a)
  await ui.split(a, 'right', b)
  await ui.split(b, 'down', c)
  for (const n of [a, b, c]) await expect.poll(() => attached(target, n), { timeout: 15_000 }).toBe('1')
  return [a, b, c]
}

const box = async (ui: UI, name: string) => (await ui.pane(name).boundingBox())!

/** Clicks into a pane (focusing it), types a fresh marker, and checks that
 * it reached that session only. */
async function typeInPane(ui: UI, target: Target, name: string, others: string[]) {
  await ui.pane(name).getByTestId('terminal').click()
  await expect.poll(async () => (await ui.panes()).find((p) => p.focused)?.session).toBe(name)
  await expect.poll(() => promptLine(target, name)).toMatch(/\$$/)
  const marker = uniqueName('mark')
  await ui.type(`echo ${marker}`, true)
  await expect.poll(() => target.capture(name)).toMatch(new RegExp(`^${marker}$`, 'm'))
  for (const o of others) expect(await target.capture(o)).not.toContain(marker)
}

/** The saved layout's root: sessions nested by direction, with sizes. */
async function savedRoot(page: Page): Promise<unknown> {
  const res = await page.request.get('/api/ui-state/layout')
  if (!res.ok()) return null
  type Node = { type: 'pane'; session: string } | { type: 'split'; dir: string; sizes: number[]; children: Node[] }
  const tree = (n: Node): unknown => (n.type === 'pane' ? n.session : { [n.dir]: n.children.map(tree) })
  const l = (await res.json()) as { tabs: { root: Node }[] }
  return l.tabs[0] ? tree(l.tabs[0].root) : null
}

test.describe('desktop', { tag: '@desktop' }, () => {

  test('(T11) Terminal menu asks for split position before listing sessions', async ({ ui, target }) => {
    const [a, b, other] = await newSessions(target, 'menu-split-a', 'menu-split-b', 'menu-split-c')
    await ui.open()
    await ui.openTerminal(a)
    const menu = ui.pane(a).getByRole('button', { name: 'Terminal actions' })
    await menu.click()
    await expect(ui.page.getByRole('menuitem', { name: 'Split pane…' })).toBeVisible()
    await expect(ui.page.getByRole('menuitem', { name: b, exact: true })).toHaveCount(0)
    await ui.page.getByRole('menuitem', { name: 'Split pane…' }).click()
    await ui.page.getByRole('menuitem', { name: 'Split down', exact: true }).click()
    await expect(ui.page.getByRole('menuitem', { name: b, exact: true })).toBeVisible()
    await ui.page.getByRole('menuitem', { name: b, exact: true }).click()
    await ui.waitForTerminal(b)
    await expect(ui.pane(other)).toHaveCount(0)
    const [upper, lower] = [await box(ui, a), await box(ui, b)]
    expect(Math.abs(upper.x - lower.x)).toBeLessThan(2)
    expect(upper.y + upper.height).toBeLessThanOrEqual(lower.y + 2)
    await expect.poll(() => attached(target, b), { timeout: 15_000 }).toBe('1')
  })

  // Split and type (T8)
  test('split and type: a nested split of three panes; input lands in each', async ({ ui, target }) => {
    const [a, b, c] = await threePanes(ui, target)
    // row[a, column[b, c]]: a on the left, b above c on the right.
    const [ba, bb, bc] = [await box(ui, a), await box(ui, b), await box(ui, c)]
    expect(ba.x + ba.width).toBeLessThanOrEqual(bb.x + 2)
    expect(Math.abs(bb.x - bc.x)).toBeLessThan(2)
    expect(bb.y + bb.height).toBeLessThanOrEqual(bc.y + 2)
    expect(ba.height).toBeGreaterThan(bb.height * 1.5)
    expect(await ui.tabNames()).toHaveLength(1)
    for (const n of [a, b, c]) await typeInPane(ui, target, n, [a, b, c].filter((x) => x !== n))
  })

  // Resize split (T8)
  test("resize: dragging the divider changes both panes' tmux widths", async ({ ui, target }) => {
    const [a, b] = await newSessions(target, 'e2e-za', 'e2e-zb')
    await ui.open()
    await ui.openTerminal(a)
    await ui.split(a, 'right', b)
    await expect.poll(() => attached(target, b), { timeout: 15_000 }).toBe('1')
    await expect.poll(async () => Math.abs((await width(target, a)) - (await width(target, b)))).toBeLessThan(4)
    const [wa, wb] = [await width(target, a), await width(target, b)]

    const splitter = (await ui.page.locator('.splitpanes__splitter').first().boundingBox())!
    const x = splitter.x + splitter.width / 2
    const y = splitter.y + splitter.height / 2
    await ui.page.mouse.move(x, y)
    await ui.page.mouse.down()
    await ui.page.mouse.move(x - 200, y, { steps: 10 })
    await ui.page.mouse.up()
    await expect.poll(() => width(target, a)).toBeLessThan(wa - 10)
    await expect.poll(() => width(target, b)).toBeGreaterThan(wb + 10)
  })

  // Close pane (T8)
  test('close pane: the others take its space; closed sessions stay listed, detached', async ({ ui, target }) => {
    const [a, b, c] = await threePanes(ui, target)
    await ui.terminalAction(c, 'Close pane')
    await expect(ui.pane(c)).toHaveCount(0)
    await expect.poll(() => attached(target, c)).toBe('0')
    await expect(ui.session(c)).toBeVisible()
    const [ba, bb] = [await box(ui, a), await box(ui, b)]
    expect(ba.x + ba.width).toBeLessThanOrEqual(bb.x + 2) // side by side
    expect(Math.abs(ba.height - bb.height)).toBeLessThan(2)

    await ui.terminalAction(b, 'Close pane')
    await expect.poll(() => attached(target, b)).toBe('0')
    const main = (await ui.page.getByRole('tabpanel').boundingBox())!
    await expect.poll(async () => (await box(ui, a)).width).toBeGreaterThan(main.width - 4)
    expect(await attached(target, a)).toBe('1')
    expect(await target.sessions()).toEqual(expect.arrayContaining([a, b, c]))
  })
})

// Splits survive reload (T8). The restart drops every connection for a
// moment: the browser logs failed reconnects.
test.describe('reload', { tag: '@desktop' }, () => {
  test.use({
    allowedBrowserErrors:
      /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/(events|term\?[^']*)' failed|^HTTP 502: (GET|PUT) http:\/\/localhost:9055\/api\/|status of 502/,
  })

  test('splits survive reload and an app restart: tree, divider, focus', async ({ ui, target }) => {
    test.setTimeout(120_000)
    const [a, b, c] = await threePanes(ui, target)
    // Move the outer divider, and focus b.
    const splitter = (await ui.page.locator('.splitpanes__splitter').first().boundingBox())!
    const y = splitter.y + splitter.height / 2
    await ui.page.mouse.move(splitter.x + splitter.width / 2, y)
    await ui.page.mouse.down()
    await ui.page.mouse.move(splitter.x - 150, y, { steps: 10 })
    await ui.page.mouse.up()
    await ui.pane(b).getByTestId('terminal').click()
    const main = (await ui.page.getByRole('tabpanel').boundingBox())!
    const share = async () => (await box(ui, a)).width / main.width
    const before = await share()
    await expect.poll(() => savedRoot(ui.page)).toEqual({ row: [a, { column: [b, c] }] })
    // The save is debounced: wait until the focus and divider are in too.
    await expect
      .poll(async () => {
        type Node = { type: 'pane'; id: string; session: string } | { type: 'split'; sizes: number[]; children: Node[] }
        const l = (await (await ui.page.request.get('/api/ui-state/layout')).json()) as {
          tabs: { root: Node; focusedPane: string }[]
        }
        const idOf = (n: Node): string | undefined =>
          n.type === 'pane' ? (n.session === b ? n.id : undefined) : n.children.map(idOf).find(Boolean)
        const t = l.tabs[0]
        return t.focusedPane === idOf(t.root) && t.root.type === 'split' && t.root.sizes[0] < 45
      })
      .toBe(true)

    const check = async () => {
      for (const n of [a, b, c]) await expect(ui.pane(n)).toBeVisible({ timeout: 30_000 })
      await expect.poll(() => share()).toBeCloseTo(before, 1)
      expect(Math.abs((await share()) - before)).toBeLessThan(0.02)
      await expect.poll(async () => (await ui.panes()).find((p) => p.focused)?.session).toBe(b)
      for (const n of [a, b, c]) await expect.poll(() => attached(target, n), { timeout: 30_000 }).toBe('1')
    }
    await ui.page.reload()
    await check()

    await ctl.restartApp()
    await expect.poll(async () => (await ui.page.request.get('/api/health')).status(), { timeout: 60_000 }).toBe(200)
    await ui.page.reload()
    await check()
  })
})

// Split on the phone (T8)
test('phone: a split tab shows one pane; the pane switcher changes which', { tag: '@phone' }, async ({ ui, target }) => {
  const [a, b] = await newSessions(target, 'e2e-qa', 'e2e-qb')
  await ui.open()
  await ui.page.getByRole('button', { name: a, exact: true }).tap()
  await ui.waitForTerminal(a)
  await ui.showList()
  await ui.session(b).getByRole('button', { name: `More actions for ${b}` }).tap()
  await ui.page.getByRole('menuitem', { name: 'Open in split right' }).tap()
  await ui.waitForTerminal(b)
  for (const n of [a, b]) await expect.poll(() => attached(target, n), { timeout: 15_000 }).toBe('1')

  // One pane, full width; the other stays mounted but hidden.
  await expect(ui.pane(a)).toBeHidden()
  const shown = (await box(ui, b))!
  const viewport = ui.page.viewportSize()!
  expect(shown.width).toBeGreaterThan(viewport.width - 4)
  expect(await ui.tabNames()).toHaveLength(1)

  const markers: Record<string, string> = {}
  for (const [name, other] of [
    [b, a],
    [a, b],
  ]) {
    if (name === a) {
      await ui.pane(b).getByRole('button', { name: /^Pane 2 of 2/ }).tap()
      await ui.waitForTerminal(a)
      await expect(ui.pane(b)).toBeHidden()
    }
    await expect.poll(() => promptLine(target, name)).toMatch(/\$$/)
    markers[name] = uniqueName('mark')
    await ui.type(`echo ${markers[name]}`, true)
    await expect.poll(() => target.capture(name)).toMatch(new RegExp(`^${markers[name]}$`, 'm'))
    expect(await target.capture(other)).not.toContain(markers[name])
  }
})
