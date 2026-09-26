import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { useAppStore } from './stores/app'
import SessionList from './components/SessionList.vue'
import { panesOf } from './lib/layout'
import { useLayoutStore } from './stores/layout'
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
    path === '/api/auth/me'
      ? { status: 200, body: { email: 'person@example.com' } }
      : path === '/api/ui-state/layout'
        ? { status: 404, body: { error: 'nothing saved yet' } }
        : path === '/api/ui-state/tree'
          ? { status: 404, body: { error: 'nothing saved yet' } }
          : path === '/api/projects?machine=host'
            ? { status: 200, body: { projects: [] } }
            : { status: method === 'POST' ? 204 : 200 },
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
    expect(wrapper.get('header').text()).toContain('hostbud')
    expect(wrapper.get('header').text()).toContain('person@example.com')
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

  it('keeps account controls in the app header while the sidebar is closed', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    useAppStore().toggleSidebar()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('header').text()).toContain('person@example.com')
    expect(wrapper.get('header').findAll('button').some((button) => button.text() === 'Sign out')).toBe(true)
  })
})

describe('tabs', () => {
  const snap = {
    type: 'snapshot' as const,
    machines: [{ id: 'host', label: 'Host', status: 'ok' as const, os: 'Linux', home: '/home/dev', tmuxVersion: '3.4', tmuxMissing: false }],
    sessions: {
      host: ['acc-a', 'acc-b'].map((name) => ({ id: '$1', name, path: '/home/dev', attached: 0, windows: 2, created: '', activity: '' })),
    },
  }

  async function signedInWith(saved: unknown) {
    let release = () => {}
    const gate = new Promise<void>((r) => (release = r))
    stubFetch((method, path) => {
      if (path === '/api/auth/me') return { status: 200, body: { email: 'person@example.com' } }
      if (path === '/api/projects?machine=host') return { status: 200, body: { projects: [] } }
      if (path === '/api/ui-state/layout' && method === 'GET')
        return saved === null ? { status: 404, body: { error: 'nothing saved yet' } } : { status: 200, body: saved }
      return { status: 204 }
    })
    // Holds the layout request until release(), to see what renders before it.
    const realFetch = globalThis.fetch
    vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
      if (path === '/api/ui-state/layout' && (init?.method ?? 'GET') === 'GET') await gate
      return realFetch(path, init)
    })
    // xterm can't render in jsdom; TerminalView has its own spec.
    const wrapper = mount(App, { global: { stubs: { TerminalView: true } } })
    const { useSessionsStore } = await import('./stores/sessions')
    const { useMachinesStore } = await import('./stores/machines')
    const feed = () => {
      useMachinesStore().apply(snap)
      useSessionsStore().apply(snap)
    }
    return { wrapper, release, feed }
  }
  const terms = (w: ReturnType<typeof mount>) => w.findAll('main terminal-view-stub').map((t) => t.attributes('session'))

  it('loads the saved layout before any terminal mounts, and live updates after it', async () => {
    const saved = {
      version: 1,
      tabs: [{ id: 't1', root: { type: 'pane', id: 'p1', machine: 'host', session: 'acc-b' }, focusedPane: 'p1' }],
      activeTab: 't1',
    }
    const { wrapper, release } = await signedInWith(saved)
    await flushPromises()
    expect(terms(wrapper)).toEqual([])
    expect(IdleSocket.instances).toEqual([])
    release()
    await flushPromises()
    expect(terms(wrapper)).toEqual(['acc-b'])
    expect(IdleSocket.instances).toHaveLength(1)
  })

  it('list clicks open tabs; an open session focuses its tab; × closes', async () => {
    const { wrapper, release, feed } = await signedInWith(null)
    release()
    await flushPromises()
    feed()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('main').text()).toContain('Select a session')
    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    await wrapper.get('button[aria-label="acc-b"]').trigger('click')
    expect(terms(wrapper)).toEqual(['acc-a', 'acc-b'])
    const tabs = () => wrapper.findAll('[role=tab]')
    expect(tabs().map((t) => t.attributes('aria-selected'))).toEqual(['false', 'true'])
    // The list marks the focused pane's session.
    expect(wrapper.get('button[aria-label="acc-b"]').attributes('aria-current')).toBe('true')
    // Only the active tab's terminal takes input; the other stays mounted.
    const stubs = wrapper.findAll('main terminal-view-stub')
    expect(stubs.map((s) => s.attributes('active'))).toEqual(['false', 'true'])

    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    expect(terms(wrapper)).toEqual(['acc-a', 'acc-b'])
    expect(tabs().map((t) => t.attributes('aria-selected'))).toEqual(['true', 'false'])
    expect(useLayoutStore().focused?.session).toBe('acc-a')

    await wrapper.get('button[aria-label="Close acc-a"]').trigger('click')
    expect(terms(wrapper)).toEqual(['acc-b'])
    expect(tabs().map((t) => t.attributes('aria-selected'))).toEqual(['true'])
  })

  it("a row menu's Open in split puts the session beside the focused pane", async () => {
    const { wrapper, release, feed } = await signedInWith(null)
    release()
    await flushPromises()
    feed()
    await wrapper.vm.$nextTick()
    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    wrapper.findComponent(SessionList).vm.$emit('split', 'acc-b', 'row')
    await wrapper.vm.$nextTick()
    const tabs = useLayoutStore().tabs
    expect(tabs).toHaveLength(1)
    expect(panesOf(tabs[0].root).map((p) => p.session)).toEqual(['acc-a', 'acc-b'])
    expect(useLayoutStore().focused?.session).toBe('acc-b')
  })

  it('signing out forgets the tabs', async () => {
    const { wrapper, release, feed } = await signedInWith(null)
    release()
    await flushPromises()
    feed()
    await wrapper.vm.$nextTick()
    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    await wrapper.findAll('button').find((b) => b.text() === 'Sign out')!.trigger('click')
    await flushPromises()
    expect(useLayoutStore().tabs).toEqual([])
    expect(useAppStore().terminalShown).toBe(false)
  })
})
