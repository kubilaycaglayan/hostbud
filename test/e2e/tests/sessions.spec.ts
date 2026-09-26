import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { POLL_INTERVAL_MS } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

// "Within one poll interval", plus slack for ssh, the event and rendering.
const withinPoll = { timeout: POLL_INTERVAL_MS + 2_000 }

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

// Empty list (T15)
test('empty list: a fresh target shows no sessions and no error', async ({ page, ui }) => {
  await ui.open()
  await expect(page.getByText('No tmux sessions yet.')).toBeVisible()
  await expect(ui.banner()).toHaveCount(0)
})

// Real-terminal create/kill (T15)
test('real-terminal create and kill show up within one poll interval', async ({ ui, target }) => {
  await ui.open()
  const name = uniqueName('e2e-rt')
  await target.tmux('new-session', '-d', '-s', name)
  await expect(ui.session(name)).toBeVisible(withinPoll)
  await target.tmux('kill-session', '-t', `=${name}`)
  await expect(ui.session(name)).toHaveCount(0, withinPoll)
})

// Attached state (T15)
test('attached state and window count follow the real terminal', async ({ ui, target }) => {
  const name = uniqueName('e2e-att')
  await target.tmux('new-session', '-d', '-s', name)
  await ui.open()
  const item = ui.session(name)
  await expect(item.getByRole('img', { name: 'detached' })).toBeVisible(withinPoll)
  await expect(item).toContainText('1 window')

  const detach = target.attachClient(name)
  try {
    await expect(item.getByRole('img', { name: 'attached', exact: true })).toBeVisible({ timeout: 10_000 })
  } finally {
    detach()
  }
  await expect(item.getByRole('img', { name: 'detached' })).toBeVisible({ timeout: 10_000 })

  await target.tmux('new-window', '-t', `=${name}:`)
  await expect(item).toContainText('2 windows', withinPoll)
})

test.describe('recovery', () => {
  // Restarts and outages make the browser log failed reconnects and 502s.
  test.use({
    allowedBrowserErrors:
      /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/events' failed|^HTTP 502: GET http:\/\/localhost:9055\/api\/auth\/me|status of 502/,
  })

  // App restart (T15)
  test('app restart: the list recovers with the same sessions', async ({ ui, target }) => {
    test.setTimeout(120_000)
    const names = [uniqueName('e2e-keep'), uniqueName('e2e-keep')].sort()
    for (const n of names) await target.tmux('new-session', '-d', '-s', n)
    await ui.open()
    await expect.poll(() => ui.sessionNames(), withinPoll).toEqual(names)

    await ctl.restartApp()
    await expect(ui.page.getByRole('status')).toHaveCount(0, { timeout: 60_000 })
    await expect.poll(() => ui.sessionNames(), { timeout: 15_000 }).toEqual(names)
  })

  // Host unreachable (T15)
  test('host unreachable: banner with a hint, then recovery without a reload', async ({ page, ui, target }) => {
    test.setTimeout(120_000)
    const name = uniqueName('e2e-down')
    await target.tmux('new-session', '-d', '-s', name)
    await ui.open()
    await expect(ui.session(name)).toBeVisible(withinPoll)

    await ctl.stopSshd()
    try {
      await expect(ui.banner()).toContainText('Host unreachable', { timeout: 15_000 })
      await expect(ui.banner()).toContainText('sshd')
    } finally {
      await ctl.startSshd()
    }
    const url = page.url()
    await expect(ui.banner()).toHaveCount(0, { timeout: 30_000 })
    await expect(ui.session(name)).toBeVisible()
    expect(page.url()).toBe(url) // no reload happened
  })
})

// tmux missing (T15)
test('tmux missing: the tmux-less host shows the install hint', async ({ page }) => {
  // A second app (same database, so the same session) on the tmux-less target.
  await page.goto('http://localhost:9056/')
  await expect(page.getByRole('complementary', { name: 'Sessions' })).toBeVisible()
  const banner = page.getByRole('alert', { name: 'tmux not found on the host' })
  await expect(banner).toContainText('tmux not found on the host', { timeout: 10_000 })
  await expect(banner).toContainText('sudo apt install tmux')
})
