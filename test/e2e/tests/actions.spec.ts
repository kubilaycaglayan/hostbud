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
  await expect(ui.session(name)).toBeVisible(soon)
  await expect(ui.page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
  expect(await target.sessions()).toContain(name)
  expect(await target.display(name, '#{session_path}')).toBe(`/home/dev/e2e-work/${dir}`)

  // The same directory again: numbered.
  await ui.createSession({ directory: `~/e2e-work/${dir}` })
  await expect(ui.session(`${name}-1`)).toBeVisible(soon)
})

// Create with start command (T16)
test('create with a name, a ~/ path and a start command runs it there', async ({ ui, target }) => {
  const name = uniqueName('e2e-htop')
  await target.run('mkdir -p ~/e2e-work/tools')
  await ui.open()
  await ui.createSession({ name, directory: '~/e2e-work/tools', startCommand: 'htop' })
  await expect(ui.session(name)).toBeVisible(soon)
  await expect.poll(() => target.display(name, '#{pane_current_command}')).toBe('htop')
  expect(await target.display(name, '#{pane_current_path}')).toBe('/home/dev/e2e-work/tools')
})

test.describe('refusals', () => {
  // The backend's 400/409 answers are expected here.
  test.use({
    allowedBrowserErrors:
      /^HTTP (400|409): POST http:\/\/localhost:9055\/api\/machines\/host\/sessions|status of (400|409)/,
  })

  // Invalid input (T16)
  test('invalid input: bad names are refused in the form; duplicate and missing path explain why', async ({
    page,
    ui,
    target,
  }) => {
    await ui.open()
    await page.getByRole('button', { name: 'New session' }).click()
    const dialog = page.getByRole('dialog', { name: 'New session' })
    for (const bad of ['a.b', 'a:b', 'a b']) {
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
    // Shown in the still-open dialog (a toast would be hidden behind the modal).
    const dupError = dialog.getByRole('alert', { name: "Couldn't create the session" })
    await expect(dupError).toContainText(`A session named "${dup}" already exists.`)
    await expect(dupError).toContainText('Pick another name')
    await dialog.getByRole('button', { name: 'Cancel' }).click()

    const missing = uniqueName('e2e-nowhere')
    forbidInLogs(missing)
    await ui.createSession({ name: uniqueName('e2e-np'), directory: `~/${missing}` })
    const pathError = dialog.getByRole('alert', { name: "Couldn't create the session" })
    await expect(pathError).toContainText(`Directory /home/dev/${missing} doesn't exist on the host.`)
    await expect(pathError).toContainText('Pick an existing directory')
    expect(await target.sessions()).toEqual([dup])
  })
})

// Rename (T16)
test('rename: the new name is in tmux ls and in the list', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-old')
  const renamed = uniqueName('e2e-new')
  await target.tmux('new-session', '-d', '-s', name)
  await ui.open()
  await expect(ui.session(name)).toBeVisible(soon)

  await ui.session(name).getByRole('button', { name: `Rename ${name}` }).click()
  const dialog = page.getByRole('dialog', { name: 'Rename session' })
  await expect(dialog.getByLabel('New name')).toHaveValue(name)
  await dialog.getByLabel('New name').fill(renamed)
  await dialog.getByRole('button', { name: 'Rename' }).click()

  await expect(ui.session(renamed)).toBeVisible(soon)
  await expect(ui.session(name)).toHaveCount(0)
  expect(await target.sessions()).toEqual([renamed])
})

// Kill (T16)
test('kill asks first: Cancel keeps the session, Confirm removes it', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-kill')
  await target.tmux('new-session', '-d', '-s', name)
  await ui.open()
  await expect(ui.session(name)).toBeVisible(soon)

  await ui.session(name).getByRole('button', { name: `Kill ${name}` }).click()
  const confirm = page.getByRole('alertdialog', { name: `Kill session ${name}?` })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(confirm).toHaveCount(0)
  expect(await target.sessions()).toContain(name)
  await expect(ui.session(name)).toBeVisible()

  await ui.session(name).getByRole('button', { name: `Kill ${name}` }).click()
  await page.getByRole('alertdialog', { name: `Kill session ${name}?` }).getByRole('button', { name: 'Kill session' }).click()
  await expect(ui.session(name)).toHaveCount(0, soon)
  expect(await target.sessions()).not.toContain(name)
})
