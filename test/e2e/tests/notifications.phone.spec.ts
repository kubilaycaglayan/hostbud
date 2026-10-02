import { expect, test } from '../helpers/fixtures.ts'
import { openSettings, permissionRequests } from '../helpers/notifications.ts'

// V2-M3 T4: Settings → Notifications on the phone (WebKit, iPhone). In a
// browser tab iOS has no notifications: Settings says to install hostbud
// to the home screen first, and the switch stays off without a prompt.

test('(V2-M3 T4) Notification settings (phone)', async ({ page, ui }) => {
  await ui.open()
  const dialog = await openSettings(page, true)
  expect((await dialog.boundingBox())?.width).toBeGreaterThanOrEqual(389) // a full-screen sheet
  const toggle = dialog.getByTestId('notifications-toggle')
  await expect(toggle).not.toBeChecked()
  await expect(dialog.getByTestId('notification-device')).toContainText('Add to Home Screen')
  await toggle.tap()
  await expect(toggle).not.toBeChecked()
  expect(await permissionRequests(page)).toBe(0)
  const settings = async () => (await page.request.get('/api/notifications/settings')).json() as Promise<{ enabled: boolean; onAttention: boolean }>
  expect((await settings()).enabled).toBe(false)
  // The per-event choice still saves (for the account's other devices).
  await dialog.getByRole('checkbox', { name: 'An item needs attention' }).tap()
  await expect.poll(async () => (await settings()).onAttention).toBe(false)
  // The label remains a touch target while native control visuals stay compact.
  for (const checkbox of await dialog.getByRole('checkbox').all()) {
    const box = await checkbox.boundingBox()
    expect(box?.width).toBeLessThan(24)
    expect(box?.height).toBeLessThan(24)
    expect((await checkbox.locator('xpath=..').boundingBox())!.height).toBeGreaterThanOrEqual(44)
  }
})
