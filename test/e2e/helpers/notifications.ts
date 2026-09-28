import { expect, type APIRequestContext } from '@playwright/test'
import { mutate } from './api.ts'

// V2-M3 notifications through the API, like the Settings dialog uses it.

export interface NotificationSettings {
  enabled: boolean
  onDone: boolean
  onAttention: boolean
  onFinished: boolean
  push: { available: boolean; reason?: string }
  vapidPublicKey?: string
}

export async function getNotificationSettings(request: APIRequestContext): Promise<NotificationSettings> {
  const res = await request.get('/api/notifications/settings')
  if (!res.ok()) throw new Error(`GET notification settings: ${res.status()} ${await res.text()}`)
  return await res.json()
}

export async function putNotificationSettings(request: APIRequestContext, data: Partial<Omit<NotificationSettings, 'push' | 'vapidPublicKey'>>): Promise<NotificationSettings> {
  const res = await mutate(request, 'PUT', '/api/notifications/settings', data)
  if (!res.ok()) throw new Error(`PUT notification settings: ${res.status()} ${await res.text()}`)
  return await res.json()
}

/** What the page showed as in-app notifications (the e2e build's spy). */
export interface ShownNotification {
  title: string
  body: string
  tag: string
}

export async function shownNotifications(page: import('@playwright/test').Page): Promise<ShownNotification[]> {
  return await page.evaluate(() => [...((window as unknown as { __notifications?: ShownNotification[] }).__notifications ?? [])])
}

/** Clicks the index-th notification the page showed. */
export async function clickNotification(page: import('@playwright/test').Page, index: number): Promise<void> {
  await page.evaluate((i) => (window as unknown as { __clickNotification: (n: number) => void }).__clickNotification(i), index)
}

/** How often the page asked for notification permission (e2e spy). */
export async function permissionRequests(page: import('@playwright/test').Page): Promise<number> {
  return await page.evaluate(() => (window as unknown as { __notificationPermissionRequests?: number }).__notificationPermissionRequests ?? 0)
}

/** Opens Settings from the account menu (tap on phones). */
export async function openSettings(page: import('@playwright/test').Page, tap = false) {
  const press = (l: import('@playwright/test').Locator) => (tap ? l.tap() : l.click())
  await press(page.getByRole('button', { name: 'Account', exact: true }))
  await press(page.getByRole('button', { name: 'Settings', exact: true }))
  const dialog = page.getByRole('dialog', { name: 'Settings' })
  await expect(dialog.getByTestId('notifications-toggle')).toBeVisible()
  return dialog
}
