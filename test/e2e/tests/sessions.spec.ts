import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { ctl } from '../helpers/ctl.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN, POLL_INTERVAL_MS } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

// "Within one poll interval", plus slack for ssh, the event and rendering.
const withinPoll = { timeout: POLL_INTERVAL_MS + 2_000 }

async function createAccount(ui: import('../helpers/ui.ts').UI) {
  const account = newAccount('e2e-session-taken')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)
}

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

test('(T10) Taken session names get a number from New session and New session here', async ({ page, ui, target, request }) => {
  await createAccount(ui)
  const name = uniqueName('e2e-taken')
  const projectName = uniqueName('e2e-taken-project')
  const path = `/home/dev/${uniqueName('e2e-taken-path')}`
  forbidInLogs(name, projectName, path)
  await target.run(`mkdir -p '${path}' && tmux new-session -d -s '${name}' -c /home/dev`)
  const project = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name: projectName }, ORIGIN)
  expect(project.status(), await project.text()).toBe(201)
  await ui.open()

  await ui.createSession({ name })
  await expect(page.getByRole('region', { name: `Terminal: ${name}-1` })).toBeVisible(withinPoll)
  await expect(page.locator('section[aria-label="Notifications"] [role="status"]').filter({ hasText: `Named "${name}-1"` })).toContainText(`Named "${name}-1": "${name}" was already taken.`)
  expect(await target.sessions()).toEqual(expect.arrayContaining([name, `${name}-1`]))

  await ui.showList()
  await page.keyboard.press('Control+Shift+K')
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await palette.getByRole('combobox', { name: 'Command palette' }).fill(`New session in ${projectName}`)
  // Enter runs the match even when fill() dropped the highlight.
  await expect(palette.getByRole('option', { name: `New session in ${projectName}` })).toBeVisible()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('dialog', { name: 'New session here' })
  await expect(dialog).toContainText(`New session in ${projectName}`)
  await dialog.getByLabel('Name', { exact: true }).fill(name)
  await dialog.getByRole('button', { name: 'Create session' }).click()
  await expect(page.getByRole('region', { name: `Terminal: ${name}-2` })).toBeVisible(withinPoll)
  await expect(page.locator('section[aria-label="Notifications"] [role="status"]').filter({ hasText: `Named "${name}-2"` })).toContainText(`Named "${name}-2": "${name}" was already taken.`)
  await ui.showList()
  await expect(ui.treeItem(projectName).getByRole('treeitem', { name: `${name}-2` })).toBeVisible()
})

// Attached state (T15)
test('attached state and window count follow the real terminal', async ({ ui, target }) => {
  const name = uniqueName('e2e-att')
  await target.tmux('new-session', '-d', '-s', name)
  await ui.open()
  const item = ui.session(name)
  await expect(item.getByRole('img', { name: 'detached' })).toBeVisible(withinPoll)

  const detach = target.attachClient(name)
  try {
    await expect(item.getByRole('img', { name: 'attached', exact: true })).toBeVisible({ timeout: 10_000 })
  } finally {
    detach()
  }
  await expect(item.getByRole('img', { name: 'detached' })).toBeVisible({ timeout: 10_000 })

  // Rows show no window counts (M4 T5); a second window makes the row expandable.
  await target.tmux('new-window', '-t', `=${name}:`)
  await expect(item.getByRole('button', { name: `Expand ${name}` })).toBeVisible(withinPoll)
  await expect(item).not.toContainText(/\b\d+ windows?\b/)
})

test.describe('recovery', () => {
  // Restarts and outages make the browser log failed reconnects and 502s.
  test.use({
    allowedBrowserErrors:
      /WebSocket connection to 'ws:\/\/localhost:9055\/ws\/events' failed|^HTTP 502: (GET http:\/\/localhost:9055\/api\/auth\/me|PUT http:\/\/localhost:9055\/api\/ui-state\/layout)|status of 502/,
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
  await expect(page.getByRole('navigation', { name: 'Project and session tree' })).toBeVisible()
  const banner = page.getByRole('alert', { name: 'tmux not found on the host' })
  await expect(banner).toContainText('tmux not found on the host', { timeout: 10_000 })
  await expect(banner).toContainText('sudo apt install tmux')
})
