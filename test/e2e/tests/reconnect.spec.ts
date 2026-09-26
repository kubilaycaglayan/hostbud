import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { openShell } from '../helpers/shell.ts'
import { uniqueName, type Target } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Auto-reconnect (M3 T3): terminals re-attach by themselves after a drop.
// While hostbud is unreachable the browser logs failed reconnects and Caddy
// answers 502: expected here, and only here.
test.use({
  allowedBrowserErrors:
    /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/(events|term\?[^']*)' failed|^HTTP 502: (GET http:\/\/localhost:9055\/api\/auth\/me|PUT http:\/\/localhost:9055\/api\/ui-state\/layout)|status of 502/,
})

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})
// A failed scenario must never leave the app cut off.
test.afterEach(async () => {
  await ctl.restoreNetwork()
})

const attached = (target: Target, name: string) => target.display(name, '#{session_attached}')
const clients = async (target: Target, name: string) =>
  (await target.tmux('list-clients', '-t', `=${name}`, '-F', '#{client_tty}')).split('\n').filter(Boolean).length
const reconnecting = (ui: UI) =>
  ui.page.getByRole('region', { name: /^Terminal: / }).getByRole('status').filter({ hasText: 'Reconnecting…' })

/** Types a fresh marker until it shows up on the target (keys typed while
 * the terminal isn't attached are dropped, by design). */
async function typeUntilSeen(ui: UI, target: Target, name: string, prefix: string, timeout = 30_000) {
  const marker = uniqueName(prefix)
  await expect(async () => {
    await ui.type(`echo ${marker}`, true)
    await expect.poll(() => target.capture(name), { timeout: 3_000 }).toMatch(new RegExp(`^${marker}$`, 'm'))
  }).toPass({ timeout, intervals: [1_000] })
}

// Network cut re-attach (T3)
test('network cut: the terminal re-attaches on its own, same session, one client', async ({ ui, target }) => {
  test.setTimeout(240_000)
  const name = await openShell(ui, target, 'e2e-cut')
  const id = await target.display(name, '#{session_id}')
  const before = uniqueName('before')
  await ui.type(`echo ${before}`, true)
  await expect.poll(() => ui.termText()).toMatch(new RegExp(`^${before}$`, 'm'))

  const cutAt = Date.now()
  await ctl.cutNetwork()
  // Changed during the cut: only a resync of the list can show it.
  const during = uniqueName('e2e-during')
  await target.tmux('new-session', '-d', '-s', during)
  // The cut hangs TCP (no close): only liveness detection notices.
  await expect(reconnecting(ui)).toBeVisible({ timeout: 40_000 })
  // The app's ssh ControlMaster hangs too, until ServerAlive (3 × 15 s)
  // gives up on it; a real cut this long behaves the same.
  await ui.page.waitForTimeout(Math.max(0, 50_000 - (Date.now() - cutAt)))
  await ctl.restoreNetwork()

  // No click: the terminal comes back by itself within 30 s.
  await expect.poll(() => attached(target, name), { timeout: 30_000 }).toBe('1')
  await expect(reconnecting(ui)).toHaveCount(0, { timeout: 15_000 })
  // The old output is still there (xterm keeps its buffer; tmux redraws).
  await expect.poll(() => ui.termText()).toMatch(new RegExp(`^${before}$`, 'm'))
  await typeUntilSeen(ui, target, name, 'after')
  expect(await target.display(name, '#{session_id}')).toBe(id)
  await expect.poll(() => clients(target, name), { timeout: 20_000 }).toBe(1)

  // The live list resynced: the session created during the cut is there.
  await ui.showList()
  await expect(ui.session(during)).toBeVisible({ timeout: 30_000 })
})

// App restart re-attach (T3); M1 T17 *Terminal after restart* did this by
// clicking Reconnect.
test('app restart: the terminal re-attaches by itself and input works', async ({ ui, target, isMobile }) => {
  test.skip(isMobile, 'desktop scenario')
  test.setTimeout(120_000)
  const name = await openShell(ui, target, 'e2e-rst')

  await ctl.restartApp()
  await expect.poll(() => attached(target, name), { timeout: 60_000 }).toBe('1')
  await typeUntilSeen(ui, target, name, 'back', 60_000)
  await expect(ui.termStatus()).toHaveCount(0)
})

// Detach doesn't loop (T3)
test('detach: the banner shows and nothing re-attaches until Reconnect', async ({ ui, target, isMobile }) => {
  test.skip(isMobile, 'desktop scenario')
  const name = await openShell(ui, target, 'e2e-det')

  await target.tmux('detach-client', '-s', `=${name}`)
  await expect(ui.termStatus()).toContainText('Session detached or ended.')
  for (let i = 0; i < 10; i++) {
    expect(await attached(target, name)).toBe('0')
    await ui.page.waitForTimeout(500)
  }
  await ui.termStatus().getByRole('button', { name: 'Reconnect' }).click()
  await expect.poll(() => attached(target, name)).toBe('1')
  await expect(ui.termStatus()).toHaveCount(0)
})
