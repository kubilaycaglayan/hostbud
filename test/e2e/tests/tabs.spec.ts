import type { Page } from '@playwright/test'
import { ctl } from '../helpers/ctl.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { promptLine } from '../helpers/shell.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Tabs (M3 T7): several sessions open at once, each in its own tab.

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

const attached = (target: Target, name: string) => target.display(name, '#{session_attached}')

/** Creates sessions on the target, as from a real terminal. */
async function newSessions(target: Target, ...prefixes: string[]): Promise<string[]> {
  const names = prefixes.map((p) => uniqueName(p))
  for (const n of names) await target.tmux('new-session', '-d', '-s', n, '-c', '/home/dev')
  return names
}

/** Opens each session from the list and waits until all are attached. */
async function openAll(ui: UI, target: Target, names: string[]) {
  await ui.open()
  for (const n of names) {
    await ui.openTerminal(n)
    await expect.poll(() => attached(target, n), { timeout: 15_000 }).toBe('1')
  }
}

/** Activates a session's tab, types a fresh marker and checks it reached
 * that session (tmux's own screen). */
async function typeInTab(ui: UI, target: Target, name: string, tap = false) {
  await (tap ? ui.tab(name).tap() : ui.tab(name).click())
  await ui.waitForTerminal(name)
  await expect.poll(() => promptLine(target, name)).toMatch(/\$$/)
  const marker = uniqueName('mark')
  await ui.type(`echo ${marker}`, true)
  await expect.poll(() => target.capture(name)).toMatch(new RegExp(`^${marker}$`, 'm'))
  return marker
}

/** The layout the server holds for this account (saves are debounced):
 * the tabs' sessions, the active one marked with `*`. */
async function savedTabs(page: Page): Promise<string[]> {
  const res = await page.request.get('/api/ui-state/layout')
  if (!res.ok()) return []
  const l = (await res.json()) as { tabs: { id: string; root: { session: string } }[]; activeTab: string }
  return l.tabs.map((t) => (t.id === l.activeTab ? '*' : '') + t.root.session)
}

test.describe('desktop', { tag: '@desktop' }, () => {

  // Tabs (T7)
  test('tabs: three sessions in three tabs; input lands in each; re-picking focuses its tab', async ({ ui, target }) => {
    const [a, b, c] = await newSessions(target, 'e2e-ta', 'e2e-tb', 'e2e-tc')
    await openAll(ui, target, [a, b, c])
    expect(await ui.tabNames()).toEqual([a, b, c])
    expect(await ui.activeTabName()).toBe(c)

    const markers = new Map<string, string>()
    for (const n of [a, b, c]) markers.set(n, await typeInTab(ui, target, n))
    // Each marker only in its own session.
    for (const n of [a, b, c])
      for (const [other, m] of markers) if (other !== n) expect(await target.capture(n)).not.toContain(m)
    for (const n of [a, b, c]) expect(await attached(target, n)).toBe('1')

    // Picking an open session in the list activates its tab: no fourth tab.
    await ui.page.getByRole('button', { name: a, exact: true }).click()
    await ui.waitForTerminal(a)
    await expect(ui.page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()
    expect(await ui.tabNames()).toEqual([a, b, c])
    expect(await ui.activeTabName()).toBe(a)
  })

  // Close tab detaches (T7)
  test('close tab: the view detaches, the session keeps running, no dialog', async ({ ui, target }) => {
    const [a, b, c] = await newSessions(target, 'e2e-ca', 'e2e-cb', 'e2e-cc')
    await openAll(ui, target, [a, b, c])
    await ui.page.getByRole('button', { name: `Close ${b}`, exact: true }).click()
    expect(await ui.tabNames()).toEqual([a, c])
    await expect(ui.page.getByRole('dialog')).toHaveCount(0)
    await expect.poll(() => attached(target, b)).toBe('0')
    expect(await target.sessions()).toContain(b)
    await expect(ui.session(b)).toBeVisible()
    // The others stay attached.
    expect(await attached(target, a)).toBe('1')
    expect(await attached(target, c)).toBe('1')
  })

  // Tabs follow rename and kill (T7)
  test('tabs follow a UI rename and close when the session is killed elsewhere', async ({ page, ui, target }) => {
    const [a, b, c] = await newSessions(target, 'e2e-ra', 'e2e-rb', 'e2e-rc')
    await openAll(ui, target, [a, b, c])

    const renamed = uniqueName('e2e-rn')
    await ui.sessionAction(a, 'Rename')
    // Rename is inline in the tree (M5 T4).
    const editor = page.getByRole('textbox', { name: `Rename ${a}` })
    await editor.fill(renamed)
    await editor.press('Enter')
    await expect(ui.tab(renamed)).toBeVisible()
    expect(await ui.tabNames()).toEqual([renamed, b, c])
    await typeInTab(ui, target, renamed)
    await expect(ui.toast(`Session ${a} ended`)).toHaveCount(0)

    await target.tmux('kill-session', '-t', `=${c}`)
    await expect(ui.toast(`Session ${c} ended`)).toBeVisible({ timeout: 10_000 })
    expect(await ui.tabNames()).toEqual([renamed, b])
  })
})

// Tabs survive reload (T7). Restarting the app drops every terminal's
// connection for a moment: the browser logs failed reconnects.
test.describe('reload', { tag: '@desktop' }, () => {
  test.use({
    allowedBrowserErrors:
      /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/(events|term\?[^']*)' failed|^HTTP 502: (GET|PUT) http:\/\/localhost:9055\/api\/|status of 502/,
  })

  test('tabs survive reload and an app restart; a gone session is dropped with a notice', async ({ ui, target }) => {
    test.setTimeout(120_000)
    const [a, b, c] = await newSessions(target, 'e2e-la', 'e2e-lb', 'e2e-lc')
    await openAll(ui, target, [a, b, c])
    await ui.tab(b).click()
    await ui.waitForTerminal(b)
    await expect.poll(() => savedTabs(ui.page)).toEqual([a, '*' + b, c])

    const check = async (names: string[], active: string) => {
      await expect.poll(() => ui.tabNames(), { timeout: 30_000 }).toEqual(names)
      expect(await ui.activeTabName()).toBe(active)
      for (const n of names) await expect.poll(() => attached(target, n), { timeout: 30_000 }).toBe('1')
    }

    await ui.page.reload()
    await check([a, b, c], b)

    await ctl.restartApp()
    await expect.poll(async () => (await ui.page.request.get('/api/health')).status(), { timeout: 60_000 }).toBe(200)
    await ui.page.reload()
    await check([a, b, c], b)

    // A session that ends while no page is open: its tab is dropped on the
    // next load, with one notice.
    await ui.page.goto('about:blank')
    await expect.poll(() => attached(target, a)).toBe('0')
    await target.tmux('kill-session', '-t', `=${a}`)
    await ui.open()
    await expect(ui.toast(`Session ${a} ended`)).toBeVisible({ timeout: 10_000 })
    await check([b, c], b)
  })
})

// Tabs on the phone (T7)
test('phone: two tabs from the compact tab bar, typed into each', { tag: '@phone' }, async ({ ui, target }) => {
  const [a, b] = await newSessions(target, 'e2e-pa', 'e2e-pb')
  await ui.open()
  for (const n of [a, b]) {
    await ui.showList()
    await ui.page.getByRole('button', { name: n, exact: true }).tap()
    await ui.waitForTerminal(n)
    await expect.poll(() => attached(target, n), { timeout: 15_000 }).toBe('1')
  }
  expect(await ui.tabNames()).toEqual([a, b])
  await expect(ui.page.getByRole('tablist', { name: 'Open terminals' })).toBeVisible()
  await typeInTab(ui, target, a, true)
  await typeInTab(ui, target, b, true)
  expect(await attached(target, a)).toBe('1')
})
