import { expect, test } from '../helpers/fixtures.ts'
import { removeServers, SERVER_HOST } from '../helpers/servers.ts'

// V2-M13: other SSH servers from the UI. Each test starts and ends without
// added servers, so the rest of the suite sees only the host.
test.beforeEach(async ({ request }) => removeServers(request))
test.afterEach(async ({ request }) => removeServers(request))

test('(V2-M13 T3) Add a server from the header and remove it', async ({ page, ui }) => {
  await ui.open()
  await page.getByRole('banner').getByRole('button', { name: 'Add server', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Servers' })
  await expect(dialog.getByTestId('server-row')).toHaveCount(1)
  await expect(dialog.getByTestId('server-row')).toContainText('(this host)')

  await dialog.getByLabel('Nickname').fill('E2E second')
  await dialog.getByLabel('Host').fill(SERVER_HOST)
  await dialog.getByLabel('User').fill('dev')
  await dialog.getByRole('button', { name: 'Check host key' }).click()
  // Nothing is trusted before the owner sees the fingerprints.
  await expect(dialog.getByTestId('fingerprint').first()).toContainText('SHA256:')
  await expect(dialog.getByTestId('server-row')).toHaveCount(1)
  await dialog.getByRole('button', { name: 'Trust and add' }).click()

  const row = dialog.getByTestId('server-row').filter({ hasText: 'E2E second' })
  await expect(row).toContainText(`dev@${SERVER_HOST}:22`)
  await expect(row).toContainText('Connected', { timeout: 20_000 })

  await dialog.getByRole('button', { name: 'Remove E2E second' }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText('tmux sessions keep running')
  await confirm.getByRole('button', { name: 'Remove server' }).click()
  await expect(row).toHaveCount(0)
})
