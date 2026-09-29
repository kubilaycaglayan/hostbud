import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { useAppStore } from './stores/app'
import SessionList from './components/SessionList.vue'
import { panesOf } from './lib/layout'
import { useLayoutStore } from './stores/layout'
import { useTreeStore } from './stores/tree'
import { useProjectsStore } from './stores/projects'
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

// Renders like the default stub (terms() reads its tag and session) plus a
// focusable xterm textarea.
const terminalFocusStub = defineComponent({
  props: { focused: Boolean, active: Boolean, session: String },
  setup(props) {
    return () => h('terminal-view-stub', { session: props.session, active: String(props.active), 'data-focused': props.focused && props.active ? 'true' : undefined }, [h('textarea', { class: 'xterm-helper-textarea' })])
  },
})

Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: () => {} })

beforeEach(() => {
  setActivePinia(createPinia())
  window.localStorage.clear()
  IdleSocket.instances = []
  vi.stubGlobal('WebSocket', IdleSocket)
})
afterEach(() => {
  useTreeStore().reset()
  window.localStorage.clear()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const signedIn = () =>
  stubFetch((method, path) =>
    path === '/api/auth/me'
      ? { status: 200, body: { email: 'person@example.com' } }
      : path === '/api/ui-state/layout'
        ? { status: 404, body: { error: 'nothing saved yet' } }
      : path === '/api/ui-state/tree'
          ? { status: 404, body: { error: 'nothing saved yet' } }
          : path === '/api/ui-state/theme'
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

  it('shows unreachable instead of sign-in when startup auth cannot reach the server', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe("Can't reach hostbud")
    expect(wrapper.find('input[type=password]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Select a session')
    wrapper.unmount()
  })

  it('renders the sidebar and empty main area when signed in', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('header').text()).toContain('hostbud')
    expect(wrapper.get('header').text()).toContain('person@example.com')
    expect(wrapper.get('aside').attributes('aria-label')).toBe('Sessions')
    expect(wrapper.get('header button[aria-label="New session"]').find('svg').exists()).toBe(true)
    expect(wrapper.get('header button[aria-label="Browse files"]').find('svg').exists()).toBe(true)
    expect(wrapper.find('aside button[aria-label="New session"]').exists()).toBe(false)
    expect(wrapper.find('aside button[aria-label="Add project"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Projects')
    expect(wrapper.text()).not.toContain('Projects & sessions')
    expect(wrapper.get('main').text()).toContain('Select a session')
    // Signed in ⇒ live updates start (no polling).
    expect(IdleSocket.instances.map((x) => x.url)).toEqual(['ws://localhost:3000/ws/events'])
    expect(wrapper.get('[role=status]').text()).toBe('Connecting…')
  })

  it('offers a Theme radio group in the account menu and saves the choice', async () => {
    const calls = signedIn()
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    const details = wrapper.get('details')
    ;(details.element as HTMLDetailsElement).open = true
    await wrapper.vm.$nextTick()
    expect(details.findAll('input[type=radio]').map((radio) => (radio.element as HTMLInputElement).value)).toEqual(['system', 'dark', 'dimmed', 'solarized', 'light'])
    expect(details.find('input[type=radio]').classes()).toContain('theme-radio')
    expect(details.find('input[type=radio]').element.parentElement?.classList.contains('touch-target')).toBe(true)
    const light = details.get('input[value=light]')
    await light.setValue(true)
    await flushPromises()
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(calls).toContainEqual(expect.objectContaining({ method: 'PUT', path: '/api/ui-state/theme', body: { version: 1, mode: 'light' } }))
    wrapper.unmount()
  })

  it('toggles the sidebar and restores its visibility after an app reload', async () => {
    signedIn()
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    const hide = wrapper.get('header button[aria-label="Hide sidebar"]')
    expect(hide.attributes('aria-controls')).toBe('sessions-sidebar')
    expect(hide.attributes('aria-expanded')).toBe('true')
    await hide.trigger('click')
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(window.localStorage.getItem('hostbud.sidebarOpen')).toBe('false')
    wrapper.unmount()

    setActivePinia(createPinia())
    const reloaded = mount(App, { attachTo: document.body })
    await flushPromises()
    const show = reloaded.get('header button[aria-label="Show sidebar"]')
    expect(show.attributes('aria-expanded')).toBe('false')
    expect(reloaded.find('aside').exists()).toBe(false)
    await show.trigger('click')
    await reloaded.vm.$nextTick()
    expect(reloaded.find('aside').exists()).toBe(true)
    expect(window.localStorage.getItem('hostbud.sidebarOpen')).toBe('true')
    reloaded.unmount()
  })

  it('opens the file browser in a dialog outside the project sidebar', async () => {
    signedIn()
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    await wrapper.get('header button[aria-label="Browse files"]').trigger('click')
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog?.textContent).toContain('Browse files')
    expect(wrapper.text()).not.toContain('Projects & sessions')
    expect(wrapper.get('aside').text()).not.toContain('Current path')
    const close = document.body.querySelector<HTMLButtonElement>('button[aria-label="Close file browser"]')
    expect(close).not.toBeNull()
    close?.click()
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    wrapper.unmount()
  })

  it('puts compact New session and Browse files actions right after the host name (M8 T1)', async () => {
    signedIn()
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    const header = wrapper.get('header')
    const order = [...header.element.children].map((child) => child.getAttribute('aria-label') ?? child.tagName.toLowerCase())
    expect(order.slice(0, 4)).toEqual(['Hide sidebar', 'h1', 'New session', 'Browse files'])
    for (const name of ['Hide sidebar', 'New session', 'Browse files']) {
      const button = header.get(`button[aria-label="${name}"]`)
      expect(button.attributes('title')).toBe(name)
      expect(button.attributes('type')).toBe('button')
      // Compact visual box (4 px padding, was ~12 px), 44 px hit area on touch screens.
      expect(button.classes()).toContain('touch-target')
      expect(button.classes()).toContain('focus-visible:ring-2')
      expect(button.classes()).not.toContain('min-h-11')
      expect(button.get('[data-icon-box]').classes()).toContain('p-1')
      expect(button.text().trim()).toBe('')
      expect(button.find('svg').exists()).toBe(true)
    }
    await header.get('button[aria-label="New session"]').trigger('click')
    let dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-labelledby')).not.toBeNull()
    expect(dialog?.textContent).toContain('New session')
    ;[...(dialog?.querySelectorAll<HTMLButtonElement>('button') ?? [])].find((button) => button.textContent?.trim() === 'Cancel')?.click()
    await flushPromises()
    await header.get('button[aria-label="Browse files"]').trigger('click')
    dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-labelledby')).not.toBeNull()
    expect(dialog?.textContent).toContain('Browse files')
    wrapper.unmount()
  })

  it("opens only the new-session dialog from a project row's plus button", async () => {
    const project = { id: 'p1', machineId: 'host', path: '/home/dev/work', name: 'work', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }
    stubFetch((method, path) =>
      path === '/api/auth/me'
        ? { status: 200, body: { email: 'person@example.com' } }
        : path.startsWith('/api/ui-state/')
          ? { status: 404, body: { error: 'nothing saved yet' } }
          : path === '/api/projects?machine=host'
            ? { status: 200, body: { projects: [project] } }
          : path === '/api/projects/p1/recent-commands'
              ? { status: 200, body: { commands: [] } }
              : path === '/api/machines/host/fs/home'
                ? { status: 200, body: { path: '/home/dev' } }
                : path.startsWith('/api/machines/host/fs/list')
                  ? { status: 200, body: { path: '/home/dev', entries: [] } }
                  : { status: method === 'POST' ? 204 : 200 },
    )
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    useTreeStore().sync()
    await flushPromises()
    await wrapper.get('button[aria-label="New session in work"]').trigger('click')
    await flushPromises()
    const dialogs = [...document.body.querySelectorAll('[role="dialog"]')]
    expect(dialogs.map((d) => d.getAttribute('aria-label') ?? d.querySelector('h2')?.textContent?.trim())).toEqual(['New session here'])
    expect(dialogs[0].textContent).toContain('New session in work')

    useProjectsStore().apply({ type: 'projects.changed', machine: 'host', payload: { action: 'deleted', project } })
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.textContent).toContain('The New session here dialog closed because its project was removed.')
    wrapper.unmount()
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

  async function signedInWith(saved: unknown, attachTo?: Element, terminalStub: true | typeof terminalFocusStub = true) {
    let release = () => {}
    const gate = new Promise<void>((r) => (release = r))
    stubFetch((method, path) => {
      if (path === '/api/auth/me') return { status: 200, body: { email: 'person@example.com' } }
      if (path === '/api/projects?machine=host') return { status: 200, body: { projects: [] } }
      if (path === '/api/ui-state/tree' && method === 'GET') return { status: 404, body: { error: 'nothing saved yet' } }
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
    const wrapper = mount(App, { attachTo, global: { stubs: { TerminalView: terminalStub } } })
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
    const { wrapper, release, feed } = await signedInWith(null, document.body, terminalFocusStub)
    release()
    await flushPromises()
    feed()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('main').text()).toContain('Select a session')
    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    await wrapper.get('button[aria-label="acc-b"]').trigger('click')
    expect(document.activeElement).toBe(wrapper.get('[data-focused="true"] .xterm-helper-textarea').element)
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

  it('reordering tabs keeps the active tab and every mounted terminal in place (M8 T4)', async () => {
    const { wrapper, release, feed } = await signedInWith(null)
    release()
    await flushPromises()
    feed()
    await wrapper.vm.$nextTick()
    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    await wrapper.get('button[aria-label="acc-b"]').trigger('click')
    const panels = wrapper.findAll('main [role=tabpanel]').map((p) => p.element)
    const sockets = IdleSocket.instances.length
    const layout = useLayoutStore()
    const ids = layout.tabs.map((t) => t.id)
    layout.reorderTabs([ids[1], ids[0]])
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('[role=tab]').map((t) => t.text())).toEqual(['acc-b', 'acc-a'])
    expect(wrapper.findAll('[role=tab]').map((t) => t.attributes('aria-selected'))).toEqual(['true', 'false'])
    // Same panel elements, same DOM order, no new connections.
    expect(wrapper.findAll('main [role=tabpanel]').map((p) => p.element)).toEqual(panels)
    expect(terms(wrapper)).toEqual(['acc-a', 'acc-b'])
    expect(IdleSocket.instances).toHaveLength(sockets)
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

  it('moves focus between the tree and terminal from the global shortcut on desktop', async () => {
    const saved = {
      version: 1,
      tabs: [{ id: 't1', root: { type: 'pane', id: 'p1', machine: 'host', session: 'acc-b' }, focusedPane: 'p1' }],
      activeTab: 't1',
    }
    const desktop = await signedInWith(saved, document.body, terminalFocusStub)
    desktop.release()
    await flushPromises()
    desktop.feed()
    await flushPromises()
    const row = desktop.wrapper.get('[data-tree-key="session:acc-b"]')
    const terminal = desktop.wrapper.get('.xterm-helper-textarea')
    ;(terminal.element as HTMLTextAreaElement).focus()
    const toTree = new KeyboardEvent('keydown', { key: 'e', ctrlKey: true, shiftKey: true, bubbles: true, cancelable: true })
    window.dispatchEvent(toTree)
    await flushPromises()
    expect(toTree.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(row.element)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'e', ctrlKey: true, shiftKey: true, bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.activeElement).toBe(terminal.element)
    desktop.wrapper.unmount()
  })

  it('uses the tree as the compact home screen and opens it over a still-mounted terminal', async () => {
    let mediaListener: (() => void) | undefined
    let compactMatch = true
    vi.stubGlobal('matchMedia', vi.fn(() => ({
      media: '(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)',
      get matches() { return compactMatch },
      addEventListener: (_: string, cb: () => void) => { mediaListener = cb },
      removeEventListener: vi.fn(),
    })))
    const { wrapper, release, feed } = await signedInWith(null, document.body)
    release()
    await flushPromises()
    feed()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('main nav[aria-label="Project and session tree"]').exists()).toBe(true)
    expect(wrapper.find('terminal-view-stub').exists()).toBe(false)
    expect(wrapper.get('button[aria-label="Command palette"]').classes()).toContain('touch-target')
    await wrapper.get('button[aria-label="Command palette"]').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"][aria-label="Command palette"]')).not.toBeNull()
    expect(document.querySelector<HTMLInputElement>('input[aria-label="Command palette"]')?.className).toContain('text-base')
    document.querySelector<HTMLInputElement>('input[aria-label="Command palette"]')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"][aria-label="Command palette"]')).toBeNull()

    await wrapper.get('button[aria-label="acc-a"]').trigger('click')
    const terminal = wrapper.get('terminal-view-stub').element
    expect(wrapper.get('main').find('nav[aria-label="Project and session tree"]').exists()).toBe(false)
    const trigger = wrapper.get('header button[aria-controls="sessions-sidebar"]')
    expect(trigger.attributes('aria-label')).toBe('Show sidebar')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    ;(trigger.element as HTMLButtonElement).focus()
    await trigger.trigger('click')
    await flushPromises()
    let dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog?.textContent).toContain('Project tree')
    expect(dialog?.getAttribute('aria-labelledby')).not.toBeNull()
    expect(dialog?.querySelector('.sr-only')?.textContent).toContain('Project tree')
    expect(dialog?.querySelector('[aria-label="Hide sidebar"] svg')).not.toBeNull()
    // The drawer must stay below application dialogs in the shared portal stack,
    // so reopening it cannot cover a dialog after a responsive layout change.
    expect(dialog?.className).toMatch(/\bz-30\b/)
    expect(dialog?.previousElementSibling?.className).toMatch(/\bz-20\b/)
    expect(trigger.attributes('aria-label')).toBe('Hide sidebar')
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(trigger.attributes('aria-controls')).toBe('sessions-sidebar')
    expect(wrapper.get('terminal-view-stub').element).toBe(terminal)

    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await flushPromises()
    document.body.querySelector<HTMLElement>('.fixed.inset-0')?.dispatchEvent(
      new PointerEvent('pointerdown', { bubbles: true, pointerType: 'mouse' }),
    )
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    await trigger.trigger('click')
    await flushPromises()
    dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    dialog.querySelector<HTMLButtonElement>('[aria-label="Hide sidebar"]')?.click()
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(trigger.attributes('aria-label')).toBe('Show sidebar')
    expect(trigger.attributes('aria-expanded')).toBe('false')

    await trigger.trigger('click')
    await flushPromises()
    const dialogContent = document.body.querySelector('[role="dialog"]')!
    dialogContent.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, clientX: 200, clientY: 200 }))
    dialogContent.dispatchEvent(new MouseEvent('pointerup', { bubbles: true, clientX: 100, clientY: 205 }))
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()

    // A compact landscape update stays compact and does not replace the terminal.
    compactMatch = true
    mediaListener?.()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('terminal-view-stub').element).toBe(terminal)
    feed()
    await wrapper.vm.$nextTick()
    await trigger.trigger('click')
    await flushPromises()
    dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    dialog.querySelector<HTMLButtonElement>('button[aria-label="acc-b"]')?.click()
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(useLayoutStore().focused?.session).toBe('acc-b')
    wrapper.unmount()
  })

  it('puts email and sign-out in the compact Account menu and opens dialogs as sheets', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({
      media: '(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)',
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })))
    const { wrapper, release } = await signedInWith(null, document.body)
    release()
    await flushPromises()
    const account = wrapper.get('header summary[aria-label="Account"]')
    await account.trigger('click')
    expect(wrapper.get('[data-testid="account-email"]').text()).toBe('person@example.com')
    expect(wrapper.get('header').text()).toContain('Sign out')
    await wrapper.get('header button[aria-label="New session"]').trigger('click')
    await flushPromises()
    const sheet = [...document.body.querySelectorAll('[role="dialog"]')].find((element) => element.textContent?.includes('New session'))
    expect(sheet?.className).toContain('bottom-0')
    wrapper.unmount()
  })
})
