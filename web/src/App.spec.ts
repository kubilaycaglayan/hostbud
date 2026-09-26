import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { useAppStore } from './stores/app'
import { stubFetch } from './test-utils'

// A socket that stays "connecting": the live connection is covered by
// api/live.spec.ts; here it only must not make real connections.
class IdleSocket {
  static instances: IdleSocket[] = []
  onopen = null
  onmessage = null
  onclose = null
  onerror = null
  closed = false
  constructor(readonly url: string) {
    IdleSocket.instances.push(this)
  }
  close() {
    this.closed = true
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  IdleSocket.instances = []
  vi.stubGlobal('WebSocket', IdleSocket)
})
afterEach(() => vi.unstubAllGlobals())

const signedIn = () =>
  stubFetch((method, path) =>
    path === '/api/auth/me' ? { status: 200, body: { email: 'person@example.com' } } : { status: method === 'POST' ? 204 : 200 },
  )

describe('App shell', () => {
  it('shows the sign-in screen without a session', async () => {
    stubFetch(() => ({ status: 401, body: { error: 'sign in required' } }))
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.find('input[type=password]').exists()).toBe(true)
  })

  it('renders the sidebar and empty main area when signed in', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('aside[aria-label="Sessions"]').text()).toContain('hostbud')
    expect(wrapper.get('aside').text()).toContain('person@example.com')
    expect(wrapper.get('main').text()).toContain('Select a session')
    // Signed in ⇒ live updates start (no polling).
    expect(IdleSocket.instances.map((x) => x.url)).toEqual(['ws://localhost:3000/ws/events'])
    expect(wrapper.get('[role=status]').text()).toBe('Connecting…')
  })

  it('hides the sidebar when toggled', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    useAppStore().toggleSidebar()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('aside').exists()).toBe(false)
  })

  it('signs out back to the sign-in screen', async () => {
    const calls = signedIn()
    const wrapper = mount(App)
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === 'Sign out')!.trigger('click')
    await flushPromises()
    expect(calls).toContainEqual(expect.objectContaining({ method: 'POST', path: '/api/auth/logout' }))
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(IdleSocket.instances[0].closed).toBe(true) // live updates stop on sign-out
    expect(wrapper.find('input[type=password]').exists()).toBe(true)
  })
})

describe('session selection', () => {
  it('opens the selected session in the terminal view', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    const { useSessionsStore } = await import('./stores/sessions')
    const { useMachinesStore } = await import('./stores/machines')
    const snap = {
      type: 'snapshot' as const,
      machines: [{ id: 'host', label: 'Host', status: 'ok' as const, os: 'Linux', home: '/home/dev', tmuxVersion: '3.4', tmuxMissing: false }],
      sessions: { host: [{ id: '$1', name: 'acc-a', path: '/home/dev', attached: 0, windows: 2, created: '', activity: '' }] },
    }
    useMachinesStore().apply(snap)
    useSessionsStore().apply(snap)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('aside').text()).toContain('2 windows')
    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    expect(wrapper.get('main section').attributes('aria-label')).toBe('Terminal: acc-a')
    expect(useAppStore().selected).toEqual({ machine: 'host', name: 'acc-a' })
  })
})
