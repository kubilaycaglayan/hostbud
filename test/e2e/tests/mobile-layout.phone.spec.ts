import { devices } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

const PORTRAIT = devices['iPhone 13 Pro'].viewport
const LANDSCAPE = devices['iPhone 13 Pro landscape'].viewport

async function createAccount(ui: import('../helpers/ui.ts').UI) {
  const account = newAccount('e2e-layout-phone')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)
}

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

test('(T2) Tree drawer', async ({ page, target, ui }) => {
  const first = uniqueName('e2e-drawer-a')
  const second = uniqueName('e2e-drawer-b')
  await target.tmux('new-session', '-d', '-s', first, '-c', '/home/dev')
  await target.tmux('new-session', '-d', '-s', second, '-c', '/home/dev')
  await ui.open()
  await expect(ui.tree()).toBeVisible()
  await expect(page.getByText('Select a session to open a terminal.')).toHaveCount(0)
  await ui.openTerminal(first)

  const trigger = page.locator('header button[aria-controls="sessions-sidebar"]')
  await trigger.tap()
  const drawer = page.getByRole('dialog', { name: 'Project tree' })
  await expect(drawer).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(drawer).toBeHidden()
  await expect(trigger).toBeFocused()

  await trigger.tap()
  await page.mouse.click(PORTRAIT.width - 8, PORTRAIT.height / 2)
  await expect(drawer).toBeHidden()
  await trigger.tap()
  await drawer.getByRole('button', { name: 'Hide sidebar' }).tap()
  await expect(drawer).toBeHidden()
  await trigger.tap()
  await drawer.getByRole('button', { name: second, exact: true }).tap()
  await ui.waitForTerminal(second)
  await expect(drawer).toBeHidden()
  await expect(page.locator('[data-focused="true"] .xterm-helper-textarea')).toBeFocused()

  await page.getByRole('banner').getByRole('button', { name: 'New session', exact: true }).tap()
  const sheet = page.getByRole('dialog', { name: 'New session' })
  await expect(sheet).toBeVisible()
  const created = uniqueName('e2e-from-drawer')
  await sheet.getByLabel('Name').fill(created)
  await sheet.getByRole('button', { name: 'Create', exact: true }).tap()
  await ui.waitForTerminal(created)
})

test('(T16) Dialogs stay above the reopened tree gutter', async ({ page, target, ui }) => {
  const name = uniqueName('e2e-dialog-gutter')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(name)

  await page.locator('header button[aria-controls="sessions-sidebar"]').tap()
  const drawer = page.getByRole('dialog', { name: 'Project tree' })
  await expect(drawer).toBeVisible()

  // The global command-palette shortcut can open while the gutter is expanded.
  // The modal dialog must occupy the higher layer and remain usable.
  await page.keyboard.press('Control+Shift+k')
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await expect(palette).toBeVisible()
  // The drawer may be dismissed when focus moves into the palette; if it
  // stays open, it must sit on a lower layer.
  if (await drawer.count()) {
    const [drawerLayer, dialogLayer] = await Promise.all([
      drawer.evaluate((element) => Number(getComputedStyle(element).zIndex)),
      palette.evaluate((element) => Number(getComputedStyle(element).zIndex)),
    ])
    expect(dialogLayer).toBeGreaterThan(drawerLayer)
  }
  const search = palette.getByRole('combobox', { name: 'Command palette' })
  await expect(search).toBeVisible()
  // Nothing covers the search input: the topmost element at its center is in the palette.
  expect(await search.evaluate((element) => {
    const box = element.getBoundingClientRect()
    const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2)
    return !!hit && !!element.closest('[role="dialog"]')?.contains(hit)
  })).toBe(true)
  await search.fill(name)
  await expect(palette.getByRole('option', { name })).toBeVisible()
})

test('(T2) Drawer keeps the terminal attached', async ({ page, target, ui }) => {
  const name = uniqueName('e2e-drawer-attached')
  const marker = uniqueName('e2e-drawer-input')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(name)
  const client = async () => {
    const rows = await target.tmux('list-clients', '-F', '#{client_pid}|#{client_width}x#{client_height}|#{client_session}')
    return rows.split('\n').map((x) => x.trim()).find((x) => x.endsWith(`|${name}`)) ?? ''
  }
  await expect.poll(client).not.toBe('')
  const before = await client()
  const trigger = page.locator('header button[aria-controls="sessions-sidebar"]')
  await trigger.tap()
  await expect(page.getByRole('dialog', { name: 'Project tree' })).toBeVisible()
  expect(await client()).toBe(before)
  await page.getByRole('button', { name: 'Hide sidebar' }).tap()
  await expect.poll(client).toBe(before)
  await ui.type(`echo ${marker}`, true)
  await expect.poll(() => target.capture(name)).toContain(marker)
})

test('(T2) Single terminal view and rotation', async ({ page, target, ui }) => {
  const first = uniqueName('e2e-layout-a')
  const second = uniqueName('e2e-layout-b')
  const third = uniqueName('e2e-layout-c')
  for (const name of [first, second, third]) await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(first)
  await ui.openTerminal(second)
  await page.setViewportSize({ width: 1280, height: 800 })
  await ui.split(second, 'right', third)
  await page.setViewportSize(PORTRAIT)
  const visibleTerminals = async () => {
    const terminals = await page.getByRole('region', { name: /^Terminal: / }).all()
    return (await Promise.all(terminals.map((locator) => locator.isVisible()))).filter(Boolean).length
  }
  await expect.poll(visibleTerminals).toBe(1)
  const client = async () => {
    const rows = await target.tmux('list-clients', '-F', '#{client_pid}|#{client_width}x#{client_height}|#{client_session}')
    return rows.split('\n').map((x) => x.trim()).find((x) => x.endsWith(`|${third}`)) ?? ''
  }
  await expect.poll(client).not.toBe('')
  // Wait for the portrait layout to settle (fonts, key bar) before taking
  // its size: the same size three reads in a row, 200ms apart.
  const size = async () => (await client()).split('|')[1]
  let portraitSize = ''
  await expect.poll(async () => {
    const reads: string[] = []
    for (let i = 0; i < 3; i++) {
      reads.push(await size())
      await page.waitForTimeout(200)
    }
    portraitSize = reads[0]
    return reads.every((read) => read === reads[0])
  }).toBe(true)
  const [pid] = (await client()).split('|')
  await page.setViewportSize(LANDSCAPE)
  await expect.poll(async () => (await client()).split('|')[1]).not.toBe(portraitSize)
  expect((await client()).split('|')[0]).toBe(pid)
  await page.setViewportSize(PORTRAIT)
  await expect.poll(async () => (await client()).split('|')[1]).toBe(portraitSize)
  expect((await client()).split('|')[0]).toBe(pid)
  await expect(page.getByRole('region', { name: /^Terminal: / })).toHaveCount(1)
})

test('(T2) Account menu on the phone', async ({ page, ui }) => {
  await createAccount(ui) // it signs out; keep the shared session valid
  const header = page.locator('header')
  const trigger = header.getByRole('button', { name: 'Account' })
  await expect(trigger).toBeVisible()
  // No tab is open, so the tree fills the screen and there is no drawer toggle.
  await expect(page.locator('header button[aria-controls="sessions-sidebar"]')).toHaveCount(0)
  await trigger.tap()
  await expect(header.getByText(/@/)).toBeVisible()
  await expect(header.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await header.getByRole('button', { name: 'Sign out' }).tap()
  await expect(ui.authForm().tab('Sign in')).toBeVisible()
})

test('(T13) Left bar toggle and toolbar', async ({ page, target, ui }) => {
  await createAccount(ui)
  const name = uniqueName('e2e-t13-phone')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await ui.open()
  await ui.openTerminal(name)
  const toggle = page.locator('header button[aria-controls="sessions-sidebar"]')
  await expect(toggle).toHaveAttribute('aria-label', 'Show sidebar')
  await toggle.tap()
  const drawer = page.getByRole('dialog', { name: 'Project tree' })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByRole('heading', { name: 'Project tree' })).toHaveClass(/sr-only/)
  const close = drawer.getByRole('button', { name: 'Hide sidebar' })
  const closeRect = await close.boundingBox()
  expect(closeRect?.width).toBeGreaterThanOrEqual(44)
  expect(closeRect?.height).toBeGreaterThanOrEqual(44)
  await close.tap()
  await expect(drawer).toBeHidden()
  await expect(toggle).toHaveAttribute('aria-label', 'Show sidebar')
  // M8 T1: New session and Browse files moved from the drawer to the app bar.
  await toggle.tap()
  await expect(drawer.getByRole('button', { name: 'New session', exact: true })).toHaveCount(0)
  await expect(drawer.getByRole('button', { name: 'Add project', exact: true })).toHaveCount(0)
  await drawer.getByRole('button', { name: 'Hide sidebar' }).tap()
  const banner = page.getByRole('banner')
  const createButton = banner.getByRole('button', { name: 'New session', exact: true })
  const projectButton = banner.getByRole('button', { name: 'Browse files', exact: true })
  for (const button of [createButton, projectButton]) {
    const rect = await button.boundingBox()
    expect(rect?.width).toBeGreaterThanOrEqual(44)
    expect(rect?.height).toBeGreaterThanOrEqual(44)
    await expect(button).toHaveAttribute('title', /^(New session|Browse files)$/)
  }
  await createButton.tap()
  await expect(page.getByRole('dialog', { name: 'New session' })).toBeVisible()
  await page.getByRole('dialog', { name: 'New session' }).getByRole('button', { name: 'Cancel' }).tap()
  await projectButton.tap()
  await expect(page.getByRole('dialog', { name: 'Browse files' })).toBeVisible()
})
