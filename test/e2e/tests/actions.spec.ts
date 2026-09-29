import { expect, test } from '../helpers/fixtures.ts'
import { forbidInLogs, POLL_INTERVAL_MS } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

const soon = { timeout: POLL_INTERVAL_MS + 4_000 }

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

// Create with defaults (T16)
test('create with defaults: only a directory ⇒ named after it, started there', async ({ ui, target }) => {
  const dir = uniqueName('proj.e2e') // the '.' isn't allowed in names: it becomes '-'
  forbidInLogs(dir)
  await target.run(`mkdir -p ~/e2e-work/${dir}`)
  await ui.open()
  await ui.createSession({ directory: `~/e2e-work/${dir}` })

  const name = dir.replace('.', '-')
  await expect(ui.page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible(soon)
  await ui.showList()
  await expect(ui.session(name)).toBeVisible(soon)
  expect(await target.sessions()).toContain(name)
  expect(await target.display(name, '#{session_path}')).toBe(`/home/dev/e2e-work/${dir}`)

  // The same directory again: numbered.
  await ui.createSession({ directory: `~/e2e-work/${dir}` })
  await expect(ui.page.getByRole('region', { name: `Terminal: ${name}-1` })).toBeVisible(soon)
})

// Create with start command (T16)
test('create with a name, a ~/ path and a start command runs it there', async ({ ui, target }) => {
  const name = uniqueName('e2e-htop')
  await target.run('mkdir -p ~/e2e-work/tools')
  await ui.open()
  await ui.createSession({ name, directory: '~/e2e-work/tools', startCommand: 'htop' })
  // A created session opens right away (on narrow screens it replaces the list).
  await expect(ui.page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible(soon)
  await ui.showList()
  await expect(ui.session(name)).toBeVisible(soon)
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('htop')
  expect(await target.display(name, '#{pane_current_path}')).toBe('/home/dev/e2e-work/tools')
})

test.describe('refusals', () => {
  // The backend's missing-path 400 answer is expected here.
  test.use({
    allowedBrowserErrors:
      /^HTTP 400: POST http:\/\/localhost:9055\/api\/machines\/host\/sessions|status of 400/,
  })

  // Invalid input (T16)
  test('(T16 updated T10) Invalid input: bad names are refused; a taken name is numbered and a missing path explains why', async ({
    page,
    ui,
    target,
  }) => {
    await ui.open()
    await ui.headerAction('New session')
    const dialog = page.getByRole('dialog', { name: 'New session' })
    for (const bad of ['a.b', 'a:b', 'a b.c']) {
      await dialog.getByLabel('Name').fill(bad)
      await dialog.getByRole('button', { name: 'Create' }).click()
      await expect(dialog.getByText("Use only letters, digits, '-' and '_'.")).toBeVisible()
      await expect(dialog.getByLabel('Name')).toHaveAttribute('aria-invalid', 'true')
    }
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    expect(await target.sessions()).toEqual([])

    const dup = uniqueName('e2e-dup')
    await target.tmux('new-session', '-d', '-s', dup)
    await ui.createSession({ name: dup })
    await expect(page.getByRole('region', { name: `Terminal: ${dup}-1` })).toBeVisible()
    await expect(page.locator('section[aria-label="Notifications"] [role="status"]')).toContainText(`Named "${dup}-1": "${dup}" was already taken.`)

    const missing = uniqueName('e2e-nowhere')
    forbidInLogs(missing)
    await ui.createSession({ name: uniqueName('e2e-np'), directory: `~/${missing}` })
    const pathError = dialog.getByRole('alert', { name: "Couldn't create the session" })
    await expect(pathError).toContainText(`Directory /home/dev/${missing} doesn't exist on the host.`)
    await expect(pathError).toContainText('Pick an existing directory')
    expect(await target.sessions()).toEqual(expect.arrayContaining([dup, `${dup}-1`]))
  })
})

// Rename (T16)
test('rename: the new name is in tmux ls and in the list', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-old')
  const renamed = uniqueName('e2e-new')
  await target.tmux('new-session', '-d', '-s', name)
  await ui.open()
  await expect(ui.session(name)).toBeVisible(soon)

  await ui.sessionAction(name, 'Rename')
  // Rename is inline in the tree (M5 T4).
  const editor = page.getByRole('textbox', { name: `Rename ${name}` })
  await expect(editor).toHaveValue(name)
  await editor.fill(renamed)
  await editor.press('Enter')

  await ui.showList()
  await expect(ui.session(renamed)).toBeVisible(soon)
  await expect(ui.session(name)).toHaveCount(0)
  expect(await target.sessions()).toEqual([renamed])
})

test('spaces in a typed name become hyphens without asking (create and rename)', async ({ page, ui, target }) => {
  const base = uniqueName('e2e-space')
  await ui.open()
  await ui.createSession({ name: `${base} new  session` })
  const created = `${base}-new-session`
  await expect(page.getByRole('region', { name: `Terminal: ${created}` })).toBeVisible(soon)
  await expect(page.locator('section[aria-label="Notifications"] [role="status"]')).toHaveCount(0)
  expect(await target.sessions()).toEqual([created])

  await ui.showList()
  await ui.sessionAction(created, 'Rename')
  const editor = page.getByRole('textbox', { name: `Rename ${created}` })
  await editor.fill(`${base} renamed one`)
  await editor.press('Enter')
  const renamed = `${base}-renamed-one`
  await ui.showList()
  await expect(ui.session(renamed)).toBeVisible(soon)
  expect(await target.sessions()).toEqual([renamed])
})

test('New session prefills an unused name, focused and selected: Enter creates it, typing replaces it', async ({ page, ui, target }) => {
  // "~" is /home/dev on the target, so the default name is "dev"; it's taken.
  await target.tmux('new-session', '-d', '-s', 'dev', '-c', '/home/dev')
  await ui.open()
  await expect(ui.session('dev')).toBeVisible(soon)
  const selection = (el: HTMLInputElement) => [el.selectionStart, el.selectionEnd]

  await ui.headerAction('New session')
  let dialog = page.getByRole('dialog', { name: 'New session' })
  let name = dialog.getByLabel('Name')
  await expect(name).toBeFocused()
  await expect(name).toHaveValue('dev-1')
  expect(await name.evaluate(selection)).toEqual([0, 'dev-1'.length])
  await page.keyboard.press('Enter')
  await expect(page.getByRole('region', { name: 'Terminal: dev-1' })).toBeVisible(soon)
  await expect(page.locator('section[aria-label="Notifications"] [role="status"]')).toHaveCount(0)

  const typed = uniqueName('e2e-typed')
  await ui.showList()
  await expect(ui.session('dev-1')).toBeVisible(soon)
  await ui.headerAction('New session')
  dialog = page.getByRole('dialog', { name: 'New session' })
  name = dialog.getByLabel('Name')
  await expect(name).toBeFocused()
  await expect(name).toHaveValue('dev-2')
  await page.keyboard.type(typed)
  await expect(name).toHaveValue(typed)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('region', { name: `Terminal: ${typed}` })).toBeVisible(soon)
  expect((await target.sessions()).sort()).toEqual(['dev', 'dev-1', typed].sort())
})

// Kill (T16)
test('kill asks first: Cancel keeps the session, Confirm removes it', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-kill')
  await target.tmux('new-session', '-d', '-s', name)
  await ui.open()
  await expect(ui.session(name)).toBeVisible(soon)

  await ui.sessionAction(name, 'Kill…')
  const confirm = page.getByRole('alertdialog', { name: `Kill session ${name}?` })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(confirm).toHaveCount(0)
  expect(await target.sessions()).toContain(name)
  await expect(ui.session(name)).toBeVisible()

  await ui.sessionAction(name, 'Kill…')
  await page.getByRole('alertdialog', { name: `Kill session ${name}?` }).getByRole('button', { name: 'Kill session' }).click()
  await expect(ui.session(name)).toHaveCount(0, soon)
  expect(await target.sessions()).not.toContain(name)
})
