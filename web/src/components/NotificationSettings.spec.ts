import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import NotificationSettings from './NotificationSettings.vue'
import type { NotificationSettings as Settings } from '@/api/types'
import { useNotificationsStore } from '@/stores/notifications'
import { stubFetch } from '@/test-utils'

const settings = (over: Partial<Settings> = {}): Settings => ({
  enabled: false, onDone: true, onAttention: true, onFinished: true,
  push: { available: false, reason: 'Push is off: set HOSTBUD_VAPID_PUBLIC_KEY, HOSTBUD_VAPID_PRIVATE_KEY and HOSTBUD_VAPID_SUBJECT (make vapid-keys)' },
  ...over,
})

let shown: string[]
function fakeNotification(permission: NotificationPermission, answer: NotificationPermission = permission) {
  shown = []
  const requestPermission = vi.fn(async () => {
    FakeNotification.permission = answer
    return answer
  })
  class FakeNotification {
    static permission = permission
    static requestPermission = requestPermission
    onclick = null
    constructor(title: string) { shown.push(title) }
    close() {}
  }
  vi.stubGlobal('Notification', FakeNotification)
  return requestPermission
}

function userAgent(ua: string) {
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(ua)
}

async function mountWith(s: Settings, route: (method: string, path: string, body: unknown) => { status: number; body?: unknown } = () => ({ status: 200, body: s })) {
  const calls = stubFetch(route)
  const store = useNotificationsStore()
  store.settings = s
  const w = mount(NotificationSettings, { attachTo: document.body })
  await flushPromises()
  return { w, calls, store }
}

const toggle = (w: ReturnType<typeof mount>) => w.get<HTMLInputElement>('[data-testid="notifications-toggle"]')
const device = (w: ReturnType<typeof mount>) => w.get('[data-testid="notification-device"]').text()

beforeEach(() => {
  setActivePinia(createPinia())
  window.localStorage.clear()
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('Notification settings (V2-M3 T4)', () => {
  it('is off by default and never asks for permission before the click', async () => {
    const ask = fakeNotification('default')
    const { w, calls } = await mountWith(settings())
    expect(toggle(w).element.checked).toBe(false)
    expect(device(w)).toBe('Not set up on this device yet.')
    expect(ask).not.toHaveBeenCalled()
    expect(calls.filter((c) => c.method !== 'GET')).toEqual([])
  })

  it('asks on the click; granted turns the account on', async () => {
    const ask = fakeNotification('default', 'granted')
    const { w, calls } = await mountWith(settings(), (method) => ({ status: 200, body: settings({ enabled: method === 'PUT' }) }))
    await toggle(w).setValue(true)
    await flushPromises()
    expect(ask).toHaveBeenCalledOnce()
    expect(calls).toContainEqual(expect.objectContaining({ method: 'PUT', path: '/api/notifications/settings', body: { enabled: true } }))
    expect(toggle(w).element.checked).toBe(true)
    expect(device(w)).toBe('This device: notifications while hostbud is open.')
  })

  it('denied keeps it off and says how to allow notifications', async () => {
    fakeNotification('default', 'denied')
    const { w, calls } = await mountWith(settings())
    await toggle(w).setValue(true)
    await flushPromises()
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([])
    expect(toggle(w).element.checked).toBe(false)
    expect(device(w)).toMatch(/blocked for hostbud in this browser. Allow them in the site settings/)
  })

  it('shows a revoked permission', async () => {
    fakeNotification('default')
    const { w, store } = await mountWith(settings({ enabled: true }))
    store.revoked = true
    await flushPromises()
    expect(device(w)).toBe('Notifications are blocked on this device.')
    expect(w.text()).toContain('Set up this device')
  })

  it('on iPhone outside the installed app, says to add it to the home screen first', async () => {
    const ask = fakeNotification('default')
    userAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1')
    const { w, calls } = await mountWith(settings())
    expect(device(w)).toMatch(/Add to Home Screen/)
    await toggle(w).setValue(true)
    await flushPromises()
    expect(ask).not.toHaveBeenCalled()
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([])
  })

  it('shows the push-unavailable reason as is', async () => {
    fakeNotification('granted')
    const { w } = await mountWith(settings())
    expect(w.get('[data-testid="push-unavailable"]').text()).toBe(settings().push.reason)
  })

  it('saves each event choice', async () => {
    fakeNotification('granted')
    const { w, calls } = await mountWith(settings({ enabled: true }))
    const boxes = w.findAll('fieldset input[type="checkbox"]')
    await boxes[1].setValue(false)
    await flushPromises()
    expect(calls).toContainEqual(expect.objectContaining({ method: 'PUT', body: { onAttention: false } }))
  })

  it('the test button shows a notification here without push, and needs permission', async () => {
    fakeNotification('granted')
    const { w } = await mountWith(settings({ enabled: true }))
    const button = w.findAll('button').find((b) => b.text() === 'Send test notification')!
    await button.trigger('click')
    await flushPromises()
    expect(shown).toEqual(['hostbud: test notification'])
    expect(w.text()).toContain('Test shown on this device.')
    setActivePinia(createPinia())
    fakeNotification('default')
    const again = await mountWith(settings({ enabled: true }))
    expect(again.w.findAll('button').find((b) => b.text() === 'Send test notification')!.attributes('disabled')).toBeDefined()
  })
})
