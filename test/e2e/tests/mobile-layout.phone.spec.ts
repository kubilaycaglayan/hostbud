import { devices } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

const PORTRAIT = devices['iPhone 13 Pro'].viewport
const LANDSCAPE = devices['iPhone 13 Pro landscape'].viewport

test.beforeEach(async ({ target, isMobile }) => {
  test.skip(!isMobile, 'phone projects only')
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

  const trigger = page.getByRole('button', { name: 'Show project tree' })
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
  await drawer.getByRole('button', { name: 'Close project tree' }).tap()
  await expect(drawer).toBeHidden()
  await trigger.tap()
  await drawer.getByRole('button', { name: second, exact: true }).tap()
  await ui.waitForTerminal(second)
  await expect(drawer).toBeHidden()

  await trigger.tap()
  await drawer.getByRole('button', { name: 'New session', exact: true }).tap()
  const sheet = page.getByRole('dialog', { name: 'New session' })
  await expect(sheet).toBeVisible()
  const created = uniqueName('e2e-from-drawer')
  await sheet.getByLabel('Name').fill(created)
  await sheet.getByRole('button', { name: 'Create', exact: true }).tap()
  await ui.waitForTerminal(created)
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
  const trigger = page.getByRole('button', { name: 'Show project tree' })
  await trigger.tap()
  await expect(page.getByRole('dialog', { name: 'Project tree' })).toBeVisible()
  expect(await client()).toBe(before)
  await page.getByRole('button', { name: 'Close project tree' }).tap()
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
  const [pid] = (await client()).split('|')
  const portraitSize = (await client()).split('|')[1]
  await page.setViewportSize(LANDSCAPE)
  await expect.poll(async () => (await client()).split('|')[1]).not.toBe(portraitSize)
  expect((await client()).split('|')[0]).toBe(pid)
  await page.setViewportSize(PORTRAIT)
  await expect.poll(async () => (await client()).split('|')[1]).toBe(portraitSize)
  expect((await client()).split('|')[0]).toBe(pid)
  await expect(page.getByRole('region', { name: /^Terminal: / })).toHaveCount(1)
})

test('(T2) Account menu on the phone', async ({ page, ui }) => {
  await ui.open()
  const header = page.locator('header')
  const trigger = header.getByRole('button', { name: 'Account' })
  await expect(trigger).toBeVisible()
  await expect(page.getByRole('button', { name: 'Show project tree' })).toHaveAttribute('aria-expanded', 'true')
  await trigger.tap()
  await expect(header.getByText(/@/)).toBeVisible()
  await expect(header.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await header.getByRole('button', { name: 'Sign out' }).tap()
  await expect(ui.authForm().tab('Sign in')).toBeVisible()
})
