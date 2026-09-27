import type { Locator, Page } from '@playwright/test'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, mutate } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { shq, uniqueName } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Compact file browser and the autocomplete policy (M8 T3).

async function createAccount(ui: UI) {
  const account = newAccount('e2e-compact-browser')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)
}

async function box(locator: Locator) {
  const b = await locator.boundingBox()
  if (!b) throw new Error('not visible')
  return b
}

test('(T3) Compact file browser', async ({ page, request, target, ui, isMobile }) => {
  test.setTimeout(90_000)
  await createAccount(ui)
  const parent = uniqueName('e2e-compact')
  const folders = ['alpha', 'beta', 'gamma', 'delta']
  for (const f of folders) await target.run(`mkdir -p ${shq(`/home/dev/${parent}/${f}`)}`)
  const created = uniqueName('e2e-made')
  const session = uniqueName('e2e-compact-s')
  forbidInLogs(parent, created, session)

  await page.goto('/')
  const dialog = await ui.openFileBrowser()
  await dialog.getByLabel('Current path').fill(`/home/dev/${parent}`)
  await dialog.getByRole('button', { name: 'Go' }).click()
  const list = dialog.getByRole('list', { name: 'Directory entries' })
  await expect(list.getByRole('listitem')).toHaveCount(folders.length)

  // Bounds: inside the viewport and no bigger than it needs to be.
  const viewport = page.viewportSize()!
  const d = await box(dialog)
  expect(d.x).toBeGreaterThanOrEqual(0)
  expect(d.y).toBeGreaterThanOrEqual(0)
  expect(d.x + d.width).toBeLessThanOrEqual(viewport.width + 0.5)
  expect(d.y + d.height).toBeLessThanOrEqual(viewport.height + 0.5)
  expect(d.height).toBeLessThanOrEqual(viewport.height * 0.85 + 1)
  if (isMobile) expect(d.width).toBeCloseTo(viewport.width, 0)
  else expect(d.width).toBeLessThanOrEqual(38 * 16 + 1)
  // A short listing leaves no tall empty sheet: the dialog fits its content.
  expect(d.height).toBeLessThan(viewport.height * 0.85 - 40)

  // Rows sit close together, while items keep usable targets (44 px on touch).
  const rows = await list.getByRole('listitem').all()
  for (const row of rows) {
    const r = await box(row)
    if (isMobile) expect(r.height).toBeLessThanOrEqual(52)
    else expect(r.height).toBeLessThanOrEqual(40)
    const open = await box(row.getByRole('button').first())
    expect(open.height).toBeGreaterThanOrEqual(isMobile ? 44 : 28)
  }
  const first = await box(rows[0])
  const second = await box(rows[1])
  expect(second.y - (first.y + first.height)).toBeLessThanOrEqual(1)
  await dialog.screenshot({ path: test.info().outputPath(`file-browser-${isMobile ? 'phone' : 'desktop'}.png`) })

  // Navigation: into a folder by its row, back by the breadcrumb.
  await list.getByRole('button', { name: 'beta/' }).click()
  await expect(dialog.getByLabel('Current path')).toHaveValue(`/home/dev/${parent}/beta`)
  await dialog.locator('[aria-label="Breadcrumbs"]').getByRole('button', { name: parent, exact: true }).click()
  await expect(dialog.getByLabel('Current path')).toHaveValue(`/home/dev/${parent}`)

  // Create a folder, then make it a project and start a session there.
  await dialog.getByLabel('New folder name').fill(created)
  await dialog.getByRole('button', { name: 'Create folder' }).click()
  const row = list.getByRole('listitem').filter({ hasText: `${created}/` })
  await expect(row).toBeVisible()
  await expect.poll(() => target.run(`test -d ${shq(`/home/dev/${parent}/${created}`)} && echo yes`).then((r) => r.trim())).toBe('yes')
  await row.getByRole('button', { name: `Add ${created} as project` }).click()
  await expect(dialog.getByText(`Project: ${created}`)).toBeVisible()
  await expect(row.getByRole('button', { name: `Open project ${created}` })).toBeVisible()
  await dialog.getByRole('button', { name: 'New session here' }).click()
  const create = page.getByRole('dialog', { name: 'New session here' })
  await create.getByLabel('Name').fill(session)
  await create.getByRole('button', { name: 'Create session' }).click()
  await expect.poll(() => target.sessions()).toContain(session)

  const projects = (await (await request.get('/api/projects?machine=host')).json()).projects as { id: string; path: string }[]
  const project = projects.find((p) => p.path === `/home/dev/${parent}/${created}`)
  expect(project).toBeTruthy()
  expect((await mutate(request, 'DELETE', `/api/projects/${project!.id}`)).status()).toBe(204)
  await target.tmux('kill-session', '-t', `=${session}`)
})

/** Every form control on the page (visible or not) whose autocomplete isn't
 * "off", described for the failure message. */
function autocompleteOffenders(page: Page) {
  return page.locator('input, textarea, select').evaluateAll((els) =>
    els
      .filter((el) => el.getAttribute('autocomplete') !== 'off')
      .map((el) => `${el.tagName.toLowerCase()}[type=${el.getAttribute('type')}][name=${el.getAttribute('name')}][aria-label=${el.getAttribute('aria-label')}] autocomplete=${el.getAttribute('autocomplete')}`),
  )
}

test('(T3) No browser autocomplete outside login password', async ({ browser, baseURL, page, ui, target, isMobile }) => {
  test.skip(isMobile, 'desktop scenario')
  test.setTimeout(90_000)

  // The login screen, signed out: the password keeps its attributes.
  const anonymous = await browser.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  const login = await anonymous.newPage()
  await login.goto('/')
  const password = login.getByLabel('Password')
  await expect(password).toBeVisible()
  await expect(password).toHaveAttribute('type', 'password')
  await expect(password).toHaveAttribute('name', 'password')
  await expect(password).toHaveAttribute('autocomplete', 'current-password')
  await expect(password).toHaveAttribute('required', '')
  await expect(login.getByLabel('Email')).toHaveAttribute('autocomplete', 'off')
  await login.getByRole('tab', { name: 'Create account' }).click()
  await expect(login.getByLabel('Password')).toHaveAttribute('autocomplete', 'new-password')
  // Every other control on the login screen is off.
  const others = await login.locator('input:not([type=password])').evaluateAll((els) => els.map((el) => el.getAttribute('autocomplete')))
  expect(others.length).toBeGreaterThan(0)
  expect(others.every((v) => v === 'off')).toBe(true)
  await anonymous.close()

  // Signed in: every screen with inputs.
  const name = uniqueName('e2e-autocomplete')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openAccountMenu()
  await expect(page.getByRole('radio').first()).toBeAttached()
  expect(await autocompleteOffenders(page)).toEqual([])
  await page.keyboard.press('Escape')

  const browserDialog = await ui.openFileBrowser()
  await expect(browserDialog.getByLabel('Current path')).toBeVisible()
  expect(await autocompleteOffenders(page)).toEqual([])
  await browserDialog.getByRole('button', { name: 'Close file browser' }).click()

  await page.getByRole('button', { name: 'New session', exact: true }).click()
  const create = page.getByRole('dialog', { name: 'New session' })
  await expect(create).toBeVisible()
  expect(await autocompleteOffenders(page)).toEqual([])
  await page.keyboard.press('Escape')
  await expect(create).toBeHidden()

  await ui.session(name).focus()
  await page.keyboard.press('Control+Shift+K')
  await expect(page.getByRole('combobox', { name: 'Command palette' })).toBeVisible()
  expect(await autocompleteOffenders(page)).toEqual([])
  await page.keyboard.press('Escape')

  // A terminal (xterm's input) and its search bar.
  await ui.openTerminal(name)
  await page.keyboard.press('Control+Shift+F')
  await expect(page.getByRole('textbox', { name: 'Find' })).toBeVisible()
  expect(await autocompleteOffenders(page)).toEqual([])
  await page.keyboard.press('Escape')
  await target.tmux('kill-session', '-t', `=${name}`)
})
