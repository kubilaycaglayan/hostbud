import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test.describe('command palette on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  async function createAccount(ui: import('../helpers/ui.ts').UI) {
    const fresh = newAccount('e2e-palette-phone')
    forbidInLogs(fresh.email, fresh.password)
    await owner.allow(fresh.email)
    await ui.createAccount(fresh)
  }

  test('(T9) Header palette opens and selecting a session closes the drawer', async ({ page, ui, target }) => {
    await createAccount(ui)
    const names = [uniqueName('phone-palette-a'), uniqueName('phone-palette-b')]
    for (const name of names) await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
    await page.reload()
    await ui.openTerminal(names[0])

    const open = page.getByRole('button', { name: 'Command palette' })
    await expect(open).toBeVisible()
    await open.click()
    const dialog = page.getByRole('dialog', { name: 'Command palette' })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('combobox', { name: 'Command palette' }).fill(names[1])
    await page.keyboard.press('Enter')
    await ui.waitForTerminal(names[1])
    await expect(dialog).toBeHidden()
    await expect(page.getByRole('dialog', { name: 'Project tree' })).toBeHidden()
    await expect(page.getByRole('region', { name: `Terminal: ${names[1]}` })).toBeVisible()
  })

  test('(T9) Palette Rename reveals a session in a collapsed project', { tag: '@loopback' }, async ({ page, ui, target, request }) => {
    await createAccount(ui)
    const path = `/home/dev/${uniqueName('phone-palette-path')}`
    const project = uniqueName('phone-palette-project')
    const session = uniqueName('phone-palette-session')
    await target.run(`mkdir -p ${shq(path)} && tmux new-session -d -s ${shq(session)} -c ${shq(path)}`)
    const created = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name: project }, ORIGIN)
    expect(created.status(), await created.text()).toBe(201)
    await page.reload()
    await ui.openTerminal(session)
    await ui.showList()
    await ui.toggle(project)
    await ui.closeList() // the drawer hides the app bar's palette button

    await page.getByRole('button', { name: 'Command palette' }).click()
    const palette = page.getByRole('dialog', { name: 'Command palette' })
    await palette.getByRole('combobox', { name: 'Command palette' }).fill(`Rename ${session}`)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog', { name: 'Project tree' })).toBeVisible()
    await ui.showList()
    const editor = page.getByRole('textbox', { name: `Rename ${session}` })
    await expect(editor).toBeFocused()
    await expect(ui.treeItem(project)).toHaveAttribute('aria-expanded', 'true')
  })
})
