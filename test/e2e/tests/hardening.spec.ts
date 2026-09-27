import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { uniqueName } from '../helpers/target.ts'

test.use({ allowedBrowserErrors: /^HTTP 504: POST .*\/api\/machines\/host\/sessions/ })

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

  test(`(T2) a real tmux stall shows timeout and recovers on ${project}`, async ({ page, ui }, info) => {
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
    } finally {
      await ctl.unstallTmux()
    }
    await expect.poll(async () => {
      const response = await page.request.get('/api/machines')
      const { machines } = await response.json() as { machines: { status: string }[] }
      return machines[0]?.status
    }, { timeout: 20_000 }).toBe('ok')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible({ timeout: 10_000 })
  })
}
