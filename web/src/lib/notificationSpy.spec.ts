import { describe, expect, it } from 'vitest'
import { installNotificationSpy } from './notificationSpy'

type Status = { state: PermissionState; onchange: (() => void) | null }

function fakeWindow(realPermission: NotificationPermission, state: PermissionState, answer: NotificationPermission = 'granted') {
  const status: Status = { state, onchange: null }
  const Real = { permission: realPermission, requestPermission: async () => answer }
  const w = { Notification: Real, navigator: { permissions: { query: async () => status } } } as unknown as Window
  return { w, status }
}

const permission = (w: Window) => (w as unknown as { Notification: typeof Notification }).Notification.permission

describe('installNotificationSpy', () => {
  it('reports the Permissions API state over a stale Notification.permission', async () => {
    const { w, status } = fakeWindow('denied', 'granted')
    installNotificationSpy(w)
    await Promise.resolve(); await Promise.resolve()
    expect(permission(w)).toBe('granted')
    // Without a grant the browser's own value stands.
    status.state = 'prompt'
    status.onchange?.()
    expect(permission(w)).toBe('denied')
  })

  it('counts permission requests and keeps their answer', async () => {
    const { w } = fakeWindow('default', 'prompt', 'granted')
    installNotificationSpy(w)
    await Promise.resolve(); await Promise.resolve()
    const N = (w as unknown as { Notification: typeof Notification }).Notification
    expect(permission(w)).toBe('default')
    expect(await N.requestPermission()).toBe('granted')
    expect(w.__notificationPermissionRequests).toBe(1)
    expect(permission(w)).toBe('granted')
  })
})
