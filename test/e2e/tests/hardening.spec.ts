import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { uniqueName } from '../helpers/target.ts'

test.use({
  allowedBrowserErrors: /^HTTP 504: (POST .*\/api\/machines\/host\/sessions|GET .*\/api\/machines\/host\/fs)/,
  serviceWorkers: 'allow',
})

for (const project of ['desktop-chromium', 'iphone-13-pro']) {
  test(`(T6) CSP tour across app surfaces on ${project}`, async ({ page, ui, target }, info) => {
    test.skip(info.project.name !== project)
    const name = uniqueName('csp-tour')
    await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
    await ui.open()
    await ui.openTerminal(name)
    await ui.type('vim', true)
    await expect.poll(() => target.tmux('display-message', '-p', '-t', `=${name}`, '#{pane_current_command}')).toContain('vim')
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
}

for (const project of ['desktop-chromium', 'iphone-13-pro']) {
  test(`(T2) session create timeout keeps the form open on ${project}`, async ({ page, ui }, info) => {
    test.skip(info.project.name !== project)
    await ui.open()
    await ui.showList()
    await page.getByRole('button', { name: 'New session' }).click()
    const dialog = page.getByRole('dialog', { name: 'New session' })
    await dialog.getByLabel('Name').fill('kept-session-name')
    await page.route('**/api/machines/host/sessions', route =>
      route.fulfill({ status: 504, contentType: 'application/json', body: JSON.stringify({ error: "The host didn't answer within 10s", hint: 'hostbud will retry' }) }),
    )
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog).toBeVisible()
    await expect(dialog.getByLabel('Name')).toHaveValue('kept-session-name')
    await expect(dialog).toContainText("The host didn't answer within 10s")
  })

  test(`(T2) a real tmux stall shows timeout and recovers on ${project}`, async ({ page, ui, target }, info) => {
    test.skip(info.project.name !== project)
    const name = uniqueName('e2e-stall')
    await ui.open()
    await ui.showList()
    await page.getByRole('button', { name: 'New session' }).click()
    const dialog = page.getByRole('dialog', { name: 'New session' })
    await dialog.getByLabel('Name').fill(name)
    await ctl.stallTmux()
    try {
      await dialog.getByRole('button', { name: 'Create' }).click()
      await expect(dialog).toContainText("The host didn't answer within 10s", { timeout: 15_000 })
      await expect(dialog.getByLabel('Name')).toHaveValue(name)
      await expect(ui.banner()).toContainText('timed out', { timeout: 15_000 })
    } finally {
      await ctl.unstallTmux()
    }
    await expect.poll(async () => {
      const response = await page.request.get('/api/machines')
      const { machines } = await response.json() as { machines: { status: string }[] }
      return machines[0]?.status
    }, { timeout: 20_000 }).toBe('ok')
    await expect(ui.banner()).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible({ timeout: 10_000 })

    await ctl.stallTmux()
    try {
      await page.reload()
      await expect(page.getByText(/Reconnecting…/)).toBeVisible({ timeout: 15_000 })
    } finally {
      await ctl.unstallTmux()
    }
    await expect.poll(() => target.display(name, '#{session_attached}'), { timeout: 15_000 }).toBe('1')
    await expect(page.getByText(/Reconnecting…/)).toHaveCount(0)
  })

  test(`(T3) stalled file browser offers Retry on ${project}`, async ({ ui }, info) => {
    test.skip(info.project.name !== project)
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
}
