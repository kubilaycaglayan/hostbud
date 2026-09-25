import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { uniqueName } from '../helpers/target.ts'

// While the app restarts, the browser logs failed reconnects and Caddy
// answers 502: expected here, and only here.
test.use({
  allowedBrowserErrors:
    /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/events' failed|^HTTP 502: GET http:\/\/localhost:9055\/api\/auth\/me|status of 502/,
})

// Live connection (T14): extends "Open the app".
test('live connection: /ws/events on load; after an app restart it reconnects and resyncs', async ({
  page,
  ui,
  target,
}) => {
  test.setTimeout(120_000)
  const frames: string[] = []
  let sockets = 0
  page.on('websocket', (ws) => {
    if (!ws.url().endsWith('/ws/events')) return
    sockets++
    ws.on('framereceived', (f) => frames.push(String(f.payload)))
  })
  const snapshots = () => frames.filter((f) => f.includes('"type":"snapshot"')).length

  await ui.open()
  await expect.poll(snapshots).toBe(1)
  expect(sockets).toBe(1)

  const name = uniqueName('e2e-live')
  const before = frames.length
  await ctl.restartApp()
  // Changed while the UI was disconnected: only a resync can show it.
  await target.tmux('new-session', '-d', '-s', name)

  await expect.poll(snapshots, { timeout: 60_000 }).toBeGreaterThan(1)
  expect(sockets).toBeGreaterThan(1)
  await expect
    .poll(() => frames.slice(before).some((f) => f.includes(`"name":"${name}"`)), { timeout: 15_000 })
    .toBe(true)
  await expect(page.getByRole('status')).toHaveCount(0) // "Reconnecting…" is gone
  await target.tmux('kill-session', '-t', `=${name}`)
})
