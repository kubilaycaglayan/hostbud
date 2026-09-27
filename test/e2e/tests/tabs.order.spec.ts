import type { Locator, Page } from '@playwright/test'
import { ctl } from '../helpers/ctl.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Custom tab order (M8 T4): tabs drag directly to a new position, with no
// handle and no restyling. The order is saved in the account's layout.

// Restarting the app drops every terminal's connection for a moment: the
// browser logs failed reconnects.
test.use({
  allowedBrowserErrors:
    /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/(events|term\?[^']*)' failed|^HTTP 502: (GET|PUT) http:\/\/localhost:9055\/api\/|status of 502/,
})

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

const attached = (target: Target, name: string) => target.display(name, '#{session_attached}')

/** The saved layout's sessions in tab order, the active one marked `*`. */
async function savedTabs(page: Page): Promise<string[]> {
  const res = await page.request.get('/api/ui-state/layout')
  if (!res.ok()) return []
  const l = (await res.json()) as { tabs: { id: string; root: { session: string } }[]; activeTab: string }
  return l.tabs.map((t) => (t.id === l.activeTab ? '*' : '') + t.root.session)
}

async function center(locator: Locator) {
  const box = await locator.boundingBox()
  if (!box) throw new Error('not visible')
  return { x: box.x + box.width / 2, y: box.y + box.height / 2, box }
}

/** Drags tab `from` onto tab `to`'s far side: a mouse drag on desktop, a
 * touch hold then move on the phone (Sortable listens to touch events in
 * WebKit; Playwright's touchscreen only taps). */
async function dragTab(ui: UI, from: string, to: string, isMobile: boolean) {
  const start = await center(ui.tab(from))
  const end = await center(ui.tab(to))
  // Past the target's middle, toward its far edge.
  const endX = end.x + Math.sign(end.x - start.x) * (end.box.width / 2 - 2)
  if (!isMobile) {
    await ui.page.mouse.move(start.x, start.y)
    await ui.page.mouse.down()
    for (let i = 1; i <= 12; i++) {
      await ui.page.mouse.move(start.x + ((endX - start.x) * i) / 12, start.y)
      await ui.page.waitForTimeout(60) // Sortable samples the pointer every 50 ms
    }
    await ui.page.mouse.up()
    return
  }
  const touch = (x: number, y: number) => [{ identifier: 1, clientX: x, clientY: y, pageX: x, pageY: y }]
  const el = ui.tab(from)
  await el.dispatchEvent('touchstart', { touches: touch(start.x, start.y), targetTouches: touch(start.x, start.y), changedTouches: touch(start.x, start.y) })
  await ui.page.waitForTimeout(400) // the hold that starts a touch drag (TabBar's delay)
  for (let i = 1; i <= 12; i++) {
    const x = start.x + ((endX - start.x) * i) / 12
    await el.dispatchEvent('touchmove', { touches: touch(x, start.y), targetTouches: touch(x, start.y), changedTouches: touch(x, start.y) })
    await ui.page.waitForTimeout(60) // Sortable samples the pointer every 50 ms
  }
  await el.dispatchEvent('touchend', { touches: [], targetTouches: [], changedTouches: touch(endX, start.y) })
}

/** Each tab's look: its classes, size and children (label and close only). */
async function tabLooks(ui: UI) {
  const items = ui.page.getByRole('tablist', { name: 'Open terminals' }).locator('[data-tab-item]')
  return items.evaluateAll((els) =>
    els.map((el) => ({
      classes: el.className,
      tab: el.querySelector('[role=tab]')?.className,
      height: Math.round(el.getBoundingClientRect().height),
      children: [...el.children].map((c) => c.getAttribute('role') ?? c.getAttribute('aria-label')?.replace(/ .*/, '')),
    })),
  )
}

test('(T4) Custom tab order', async ({ ui, target, isMobile }) => {
  test.setTimeout(150_000)
  const [a, b, c] = [uniqueName('e2e-oa'), uniqueName('e2e-ob'), uniqueName('e2e-oc')]
  for (const n of [a, b, c]) await target.tmux('new-session', '-d', '-s', n, '-c', '/home/dev')
  await ui.open()
  for (const n of [a, b, c]) {
    if (isMobile) {
      await ui.showList()
      await ui.page.getByRole('button', { name: n, exact: true }).tap()
    } else {
      await ui.openTerminal(n)
    }
    await ui.waitForTerminal(n)
    await expect.poll(() => attached(target, n), { timeout: 15_000 }).toBe('1')
  }
  expect(await ui.tabNames()).toEqual([a, b, c])
  expect(await ui.activeTabName()).toBe(c)
  const looks = await tabLooks(ui)
  // No drag handle: each tab is its label and its close button.
  for (const look of looks) expect(look.children).toEqual(['tab', 'Close'])
  await expect(ui.page.getByRole('tablist', { name: 'Open terminals' }).getByRole('button', { name: /drag|reorder|move/i })).toHaveCount(0)

  // Drag the first tab past the last: the order changes, nothing else.
  await dragTab(ui, a, c, isMobile)
  await expect.poll(() => ui.tabNames()).toEqual([b, c, a])
  expect(await ui.activeTabName()).toBe(c)
  // Same looks, moved with their tabs (no drag classes left behind).
  expect(await tabLooks(ui)).toEqual([looks[1], looks[2], looks[0]])
  // The terminals stayed attached, and the active one still takes input.
  for (const n of [a, b, c]) expect(await attached(target, n)).toBe('1')
  const marker = uniqueName('mark')
  await ui.type(`echo ${marker}`, true)
  await expect.poll(() => target.capture(c)).toMatch(new RegExp(`^${marker}$`, 'm'))
  await expect.poll(() => savedTabs(ui.page)).toEqual([b, '*' + c, a])

  // A newly opened session goes at the end.
  const d = uniqueName('e2e-od')
  await target.tmux('new-session', '-d', '-s', d, '-c', '/home/dev')
  if (isMobile) {
    await ui.showList()
    await expect(ui.page.getByRole('button', { name: d, exact: true })).toBeVisible({ timeout: 10_000 })
    await ui.page.getByRole('button', { name: d, exact: true }).tap()
  } else {
    await ui.openTerminal(d)
  }
  await ui.waitForTerminal(d)
  expect(await ui.tabNames()).toEqual([b, c, a, d])

  // Keyboard navigation follows the custom order (desktop).
  if (!isMobile) {
    await ui.tab(b).click()
    await ui.tab(b).press('ArrowRight')
    expect(await ui.activeTabName()).toBe(c)
    await ui.tab(c).press('ArrowRight')
    expect(await ui.activeTabName()).toBe(a)
  }
  const active = await ui.activeTabName()
  await expect.poll(() => savedTabs(ui.page)).toEqual([b, c, a, d].map((n) => (n === active ? '*' + n : n)))

  const check = async () => {
    await expect.poll(() => ui.tabNames(), { timeout: 30_000 }).toEqual([b, c, a, d])
    expect(await ui.activeTabName()).toBe(active)
    for (const n of [a, b, c, d]) await expect.poll(() => attached(target, n), { timeout: 30_000 }).toBe('1')
  }
  await ui.page.reload()
  await check()

  await ctl.restartApp()
  await expect.poll(async () => (await ui.page.request.get('/api/health')).status(), { timeout: 60_000 }).toBe(200)
  await ui.page.reload()
  await check()
})
