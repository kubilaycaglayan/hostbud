import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { uniqueName } from '../helpers/target.ts'

test.use({
  allowedBrowserErrors:
    /^HTTP 504: (POST .*\/api\/machines\/host\/sessions|GET .*\/api\/machines\/host\/fs)|status of 504 .*@ https?:\/\/[^/]+\/api\/machines\/host\/(sessions|fs\/)/,
  serviceWorkers: 'allow',
})

test('(T6) CSP tour across app surfaces', async ({ page, ui, target }) => {
  const name = uniqueName('csp-tour')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(name)
  await ui.type('vim', true)
  // `=name:` targets the session's active pane (`=name` alone isn't a pane target).
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toContain('vim')
  const files = await ui.openFileBrowser()
  await files.getByRole('button', { name: 'Close file browser' }).click()
  await page.keyboard.press('Control+Shift+K')
  await expect(page.getByRole('dialog', { name: 'Command palette' })).toBeVisible()
  await page.keyboard.press('Escape')
  await ui.openAccountMenu()
  await page.getByRole('radio', { name: 'Light', exact: true }).check()
  await page.getByRole('radio', { name: 'System', exact: true }).check()
  const install = await page.evaluate(async () => {
    const manifest = await fetch('/manifest.webmanifest').then((r) => r.json())
    const registration = await navigator.serviceWorker.register('/sw.js')
    return { manifest: manifest.name, worker: Boolean(registration.installing || registration.waiting || registration.active) }
  })
  expect(install).toEqual({ manifest: 'hostbud', worker: true })
})

test('(T10) a changed host key blocks terminal attach until the pinned key is restored', async ({ ui, target }) => {
  const name = uniqueName('hostkey-change')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await expect(ui.session(name)).toBeVisible()
  try {
    await ctl.rotateHostKey()
    const banner = ui.banner()
    await expect(banner).toContainText("The host's SSH key changed.", { timeout: 20_000 })
    await expect(banner).toContainText('If you expected this')
    await ui.openTerminal(name)
    await expect(ui.page.getByRole('status').filter({ hasText: 'Reconnecting…' })).toBeVisible()
  } finally {
    await ctl.restoreHostKey()
  }
  await expect(ui.banner()).toHaveCount(0, { timeout: 20_000 })
  await expect.poll(() => target.display(name, '#{session_attached}'), { timeout: 20_000 }).toBe('1')
})

// WebKit sends API requests through the app's service worker, which
// page.route can't intercept: the stubbed create would reach the host.
test.describe('session create timeout', () => {
  test.use({ serviceWorkers: 'block' })
  test('(T2) session create timeout keeps the form open', async ({ page, ui }) => {
    await ui.open()
    await ui.showList()
    await ui.headerAction('New session')
    const dialog = page.getByRole('dialog', { name: 'New session' })
    await dialog.getByLabel('Name').fill('kept-session-name')
    await page.route(/\/api\/machines\/host\/sessions(?:\?.*)?$/, route => {
      if (route.request().method() !== 'POST') return route.continue()
      return route.fulfill({ status: 504, contentType: 'application/json', body: JSON.stringify({ error: "The host didn't answer within 10s", hint: 'hostbud will retry' }) })
    })
    const rejectedCreate = page.waitForResponse(response =>
      response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/machines/host/sessions',
    )
    await dialog.getByRole('button', { name: 'Create' }).click()
    expect((await rejectedCreate).status()).toBe(504)
    await expect(dialog).toBeVisible()
    await expect(dialog.getByLabel('Name')).toHaveValue('kept-session-name')
    await expect(dialog).toContainText("The host didn't answer within 10s")
  })
})

test('(T2) a real tmux stall shows timeout and recovers', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-stall')
  const hostBanner = page.getByRole('alert', { name: /Host unreachable|tmux not found on the host/, includeHidden: true })
  await ui.open()
  await ui.showList()
  await ui.headerAction('New session')
  const dialog = page.getByRole('dialog', { name: 'New session' })
  await dialog.getByLabel('Name').fill(name)
  await ctl.stallTmux()
  try {
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog).toContainText("The host didn't answer within 10s", { timeout: 15_000 })
    await expect(dialog.getByLabel('Name')).toHaveValue(name)
    // The open (modal) dialog hides the rest of the page from the accessibility tree.
    await expect(hostBanner).toContainText('timed out', { timeout: 15_000 })
  } finally {
    await ctl.unstallTmux()
  }
  await expect.poll(async () => {
    const response = await page.request.get('/api/machines')
    const { machines } = await response.json() as { machines: { status: string }[] }
    return machines[0]?.status
  }, { timeout: 20_000 }).toBe('ok')
  await expect(hostBanner).toHaveCount(0)
  // A timed-out SSH request may still finish on the host after the browser
  // receives the timeout. Reuse that session if it appeared instead of
  // retrying into a duplicate suffixed name.
  if ((await target.sessions()).includes(name)) {
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await ui.showList()
    await ui.openTerminal(name)
  } else {
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible({ timeout: 10_000 })
  }
  const activeSession = await ui.activeTabName()

  await ctl.stallTmux()
  try {
    await page.reload()
    // The terminal renders from the saved layout at once; the stall shows
    // as the host banner (after the exec timeout) or a connecting status.
    const connecting = page.getByText(/Connecting…|Reconnecting…/)
    await expect.poll(async () => (await hostBanner.isVisible()) || (await connecting.first().isVisible()), { timeout: 15_000 }).toBe(true)
    if (await hostBanner.isVisible()) await expect(hostBanner).toContainText('timed out')
  } finally {
    await ctl.unstallTmux()
  }
  await expect.poll(() => target.display(activeSession, '#{session_attached}'), { timeout: 15_000 }).toBe('1')
  await expect(page.getByText(/Connecting…|Reconnecting…/)).toHaveCount(0)
})

test('(T3) stalled file browser offers Retry', async ({ ui }) => {
  await ui.open()
  await ctl.stallSftp(60)
  const dialog = await ui.openFileBrowser()
  try {
    await expect(dialog.getByRole('alert')).toContainText("The host's file service didn't answer within 10s", { timeout: 15_000 })
    await expect(dialog.getByRole('button', { name: 'Retry' })).toBeVisible()
  } finally {
    await ctl.unstallSftp()
  }
  await dialog.getByRole('button', { name: 'Retry' }).click()
  await expect(dialog.getByRole('alert')).toHaveCount(0, { timeout: 10_000 })
  await expect(dialog.getByRole('button', { name: 'Close file browser' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Close file browser' }).click()
  await expect(dialog).toHaveCount(0)
})
