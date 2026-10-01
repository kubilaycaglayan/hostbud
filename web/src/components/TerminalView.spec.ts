import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '@/api/types'
import { filesystemApi } from '@/api/client'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useTreeStore } from '@/stores/tree'
import { useToastsStore } from '@/stores/toasts'
import { useThemeStore } from '@/stores/theme'

// Fake xterm (hoisted: vi.mock factories run before the module body).
const h = vi.hoisted(() => {
  class FakeTerminal {
    cols = 80
    rows = 24
    written: string[] = []
    onDataFn: (d: string) => void = () => {}
    unicode = { activeVersion: '6' }
    buffer: { active: Record<string, unknown> } = { active: { length: 0, getLine: () => undefined } }
    selection = ''
    selectionRange: [number, number, number] | null = null
    scrollLines = vi.fn()
    selectionChanged: () => void = () => {}
    select = vi.fn((column: number, row: number, length: number) => {
      this.selectionRange = [column, row, length]
      this.selection = 'copy-marker'
      this.selectionChanged()
    })
    modes = { mouseTrackingMode: 'none', applicationCursorKeysMode: false }
    element?: HTMLElement
    writeParsed: (() => void)[] = []
    onWriteParsed(fn: () => void) {
      this.writeParsed.push(fn)
      return { dispose: () => this.writeParsed.splice(this.writeParsed.indexOf(fn), 1) }
    }
    oscHandlers: number[] = []
    parser = {
      registerOscHandler: (id: number) => this.oscHandlers.push(id),
      registerCsiHandler: () => ({ dispose() {} }),
    }
    constructor(readonly options: Record<string, unknown>) {
      h.terms.push(this)
    }
    hasSelection() {
      return this.selection !== ''
    }
    getSelection() {
      return this.selection
    }
    loadAddon(a: { activate?: (t: FakeTerminal) => void }) {
      a.activate?.(this)
    }
    textarea?: HTMLTextAreaElement
    focused = 0
    // Like xterm: the hidden input the keyboard types into.
    open(el: HTMLElement) {
      this.textarea = document.createElement('textarea')
      el.appendChild(this.textarea)
      this.element = el
    }
    focus() {
      this.focused++
    }
    reset() {
      this.written = []
    }
    dispose() {}
    write(d: Uint8Array) {
      this.written.push(new TextDecoder().decode(d))
    }
    onData(fn: (d: string) => void) {
      this.onDataFn = fn
    }
    keyHandler: (ev: KeyboardEvent) => boolean = () => true
    attachCustomKeyEventHandler(fn: (ev: KeyboardEvent) => boolean) {
      this.keyHandler = fn
    }
    onSelectionChange(fn: () => void) {
      this.selectionChanged = fn
      return { dispose() {} }
    }
    // Like xterm: input from the user goes out through onData.
    input(data: string) {
      this.onDataFn(data)
    }
    paste = vi.fn((data: string) => this.onDataFn(data))
  }
  const h = {
    terms: [] as FakeTerminal[],
    fitSize: { cols: 100, rows: 30 },
    FakeTerminal,
    linkClick: (() => {}) as (ev: MouseEvent, uri: string) => void,
  }
  return h
})
type FakeTerminal = InstanceType<typeof h.FakeTerminal>

vi.mock('@xterm/xterm', () => ({ Terminal: h.FakeTerminal }))
vi.mock('@xterm/xterm/css/xterm.css', () => ({}))
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    t?: FakeTerminal
    activate(t: FakeTerminal) {
      this.t = t
    }
    fit() {
      this.t!.cols = h.fitSize.cols
      this.t!.rows = h.fitSize.rows
    }
  },
}))
vi.mock('@xterm/addon-webgl', () => ({ WebglAddon: class { onContextLoss() {} } }))
vi.mock('@xterm/addon-web-links', () => ({
  WebLinksAddon: class {
    constructor(handler: (ev: MouseEvent, uri: string) => void) {
      h.linkClick = handler
    }
  },
}))
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: class {} }))
vi.mock('@xterm/addon-search', () => ({
  SearchAddon: class {
    findNext = vi.fn(() => true)
    findPrevious = vi.fn(() => true)
    clearDecorations = vi.fn()
    onDidChangeResults() {
      return { dispose() {} }
    }
  },
}))
vi.mock('@xterm/addon-clipboard', () => ({
  ClipboardAddon: class {
    activate(t: FakeTerminal) {
      t.parser.registerOscHandler(52)
    }
  },
}))

import TerminalView from './TerminalView.vue'

class FakeWS {
  static all: FakeWS[] = []
  binaryType = ''
  onopen: ((e: Event) => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onclose: ((e: CloseEvent) => void) | null = null
  onerror = null
  sent: (string | Uint8Array)[] = []
  closed = false
  constructor(readonly url: string) {
    FakeWS.all.push(this)
  }
  send(d: string | Uint8Array) {
    this.sent.push(d)
  }
  close() {
    this.closed = true
  }
}

let resizeCallback: () => void = () => {}

beforeEach(() => {
  setActivePinia(createPinia())
  h.terms = []
  h.fitSize = { cols: 100, rows: 30 }
  FakeWS.all = []
  vi.stubGlobal('WebSocket', FakeWS)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(cb: () => void) {
        resizeCallback = cb
      }
      observe() {}
      disconnect() {}
    },
  )
})
afterEach(() => vi.unstubAllGlobals())

async function mountTerm(props: { active?: boolean; focused?: boolean } = {}) {
  const w = mount(TerminalView, { props: { machine: 'host', session: 'acc-a', ...props }, attachTo: document.body })
  await flushPromises()
  return w
}

async function clickMenuItem(text: string) {
  const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((el) => el.textContent?.trim() === text)
  expect(item).toBeTruthy()
  item!.click()
  await flushPromises()
}

describe('TerminalView', () => {
  it('emphasizes the focused session and shows its directory beside the name', async () => {
    useProjectsStore().remember({ id: 'project-a', machineId: 'host', path: '/home/dev/bright-work', name: 'Bright work', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
    useSessionsStore().$patch({ byMachine: { host: [{ id: '$1', name: 'acc-a', path: '/home/dev/bright-work', attached: 0, windows: 1, created: '', activity: '' }] } })
    const tree = useTreeStore()
    tree.order.projects = ['project-a']
    tree.order.sections = [{ id: 'section-a', name: 'Research', color: 'purple' }]
    tree.order.projectSections = { 'project-a': 'section-a' }
    const w = await mountTerm({ focused: true })
    const name = w.get('[data-terminal-session-name]')
    const directory = w.get('[data-terminal-directory]')
    const icon = w.get('[data-terminal-directory-icon]')
    expect(name.text()).toBe('acc-a')
    expect(name.classes()).toContain('text-accent')
    expect(name.attributes('style')).toContain('var(--hb-section-purple)')
    expect(directory.text()).toBe('bright-work')
    expect(directory.attributes('title')).toBe('/home/dev/bright-work')
    expect(icon.element.nextElementSibling).toBe(name.element)
    expect(name.element.parentElement?.contains(directory.element)).toBe(true)
    w.unmount()
  })

  it('updates a mounted xterm palette without reconnecting', async () => {
    const w = await mountTerm()
    const terminal = h.terms[0]
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    const clients = FakeWS.all.length
    useThemeStore().mode = 'light'
    await w.vm.$nextTick()
    expect(terminal.options.theme).toMatchObject({ background: '#ffffff', foreground: '#1f2328' })
    expect(FakeWS.all).toHaveLength(clients)
  })

  it('keeps a long-lived terminal attached and legible across System theme changes (M8 T7)', async () => {
    const w = await mountTerm()
    const terminal = h.terms[0]
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    ws.onmessage?.({ data: new TextEncoder().encode('\x1b[48;2;44;46;50m Ask the prompt \x1b[0m').buffer } as MessageEvent)
    const written = [...terminal.written]
    expect(terminal.options.minimumContrastRatio).toBe(4.5)
    const theme = useThemeStore()
    for (const [mode, background] of [['dark', '#0f1115'], ['light', '#ffffff'], ['solarized', '#bbc5b9'], ['dimmed', '#4c686a'], ['dark', '#0f1115']] as const) {
      theme.mode = mode
      await w.vm.$nextTick()
      expect(terminal.options.theme).toMatchObject({ background })
      // The same xterm and connection: nothing reset, re-attached or resent.
      expect(h.terms).toHaveLength(1)
      expect(FakeWS.all).toHaveLength(1)
      expect(ws.closed).toBe(false)
      expect(terminal.written).toEqual(written)
      expect(terminal.options.minimumContrastRatio).toBe(4.5)
    }
    w.unmount()
  })

  it('shows a terminal cap toast and offers a manual Retry now', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('', { status: 429 })))
    useSessionsStore().$patch({ byMachine: { host: [{ id: '$1', name: 'acc-a', path: '/home/dev', attached: 0, windows: 1, created: '', activity: '' }] } })
    const w = await mountTerm()
    FakeWS.all[0].onclose?.({} as CloseEvent)
    await flushPromises()
    expect(w.get('[role=status]').text()).toContain('Too many open terminals')
    expect(useToastsStore().toasts[0]).toMatchObject({ title: 'Too many open terminals', tone: 'error' })
    expect(FakeWS.all).toHaveLength(1)
    await w.get('[role=status] button').trigger('click')
    expect(FakeWS.all).toHaveLength(2)
    w.unmount()
  })

  it('shows the key bar on touch and applies modifiers to soft-keyboard input', async () => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ media: query, matches: query === '(pointer: coarse)', addEventListener: vi.fn(), removeEventListener: vi.fn() })))
    const w = await mountTerm()
    expect(w.find('[data-testid="key-bar"]').exists()).toBe(true)
    FakeWS.all[0].onopen?.({} as Event)
    const control = w.get('button[aria-label="Control"]')
    control.element.dispatchEvent(new Event('pointerdown', { bubbles: true, cancelable: true }))
    h.terms[0].input('c')
    const sent = FakeWS.all[0].sent.at(-1) as Uint8Array
    expect(new TextDecoder().decode(sent)).toBe('\x03')
  })

  it('switches between the key bar and scroll bar, and exits before forwarding typed input', async () => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ media: query, matches: query === '(pointer: coarse)', addEventListener: vi.fn(), removeEventListener: vi.fn() })))
    const requests: { action: string; lines?: number }[] = []
    vi.stubGlobal('fetch', vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body)) as { action: string; lines?: number }
      requests.push(body)
      const state = body.action === 'enter'
        ? { inMode: true, scrollPosition: 16, historySize: 150 }
        : { inMode: false, scrollPosition: 0, historySize: 150 }
      return new Response(JSON.stringify(state), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }))
    const w = await mountTerm()
    FakeWS.all[0].onopen?.({} as Event)
    w.get('button[aria-label="Scroll history"]').element.dispatchEvent(new Event('pointerdown', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(requests).toEqual([{ action: 'enter' }])
    expect(w.find('[data-testid="scroll-bar"]').exists()).toBe(true)
    expect(w.find('[data-testid="key-bar"]').exists()).toBe(false)

    h.terms[0].input('echo ok\r')
    await flushPromises()
    expect(requests.at(-1)).toEqual({ action: 'exit' })
    expect(new TextDecoder().decode(FakeWS.all[0].sent.at(-1) as Uint8Array)).toBe('echo ok\r')
    expect(w.find('[data-testid="scroll-bar"]').exists()).toBe(false)
    expect(w.find('[data-testid="key-bar"]').exists()).toBe(true)
  })

  it('returns to the key bar and shows the API hint when copy mode fails', async () => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({ media: query, matches: query === '(pointer: coarse)', addEventListener: vi.fn(), removeEventListener: vi.fn() })))
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'tmux needs 2.4', hint: 'Upgrade tmux on the host.' }), { status: 409 })))
    const w = await mountTerm()
    FakeWS.all[0].onopen?.({} as Event)
    w.get('button[aria-label="Scroll history"]').element.dispatchEvent(new Event('pointerdown', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(w.find('[data-testid="scroll-bar"]').exists()).toBe(false)
    expect(w.find('[data-testid="key-bar"]').exists()).toBe(true)
    expect(useToastsStore().toasts[0]).toMatchObject({ title: 'Could not scroll terminal history', message: 'Tmux needs 2.4.', hint: 'Upgrade tmux on the host.' })
  })

  it('marks terminal header controls as touch targets', async () => {
    const w = await mountTerm()
    expect(w.get('button[aria-label="Terminal actions"]').classes()).toContain('touch-target')
  })

  it('attaches with the fitted size and bridges bytes both ways', async () => {
    const w = await mountTerm()
    const ws = FakeWS.all[0]
    expect(ws.url).toBe('ws://localhost:3000/ws/term?machine=host&session=acc-a&cols=100&rows=30')
    ws.onopen?.({} as Event)
    ws.onmessage?.({ data: new TextEncoder().encode('prompt$ ').buffer } as MessageEvent)
    expect(h.terms[0].written).toEqual(['prompt$ '])
    h.terms[0].onDataFn('ls\r')
    expect(new TextDecoder().decode(ws.sent[0] as Uint8Array)).toBe('ls\r')
    expect(h.terms[0].unicode.activeVersion).toBe('11')
    expect(window.__hostbud).toBeUndefined() // not an e2e build
    w.unmount()
    expect(ws.closed).toBe(true)
  })

  it('ResizeObserver ⇒ fit ⇒ resize frame (only on change)', async () => {
    await mountTerm()
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    h.fitSize = { cols: 132, rows: 40 }
    resizeCallback()
    resizeCallback()
    expect(ws.sent).toEqual(['{"type":"resize","cols":132,"rows":40}'])
  })

  it('detaches while hidden and refits before reattaching when visible', async () => {
    const original = Object.getOwnPropertyDescriptor(document, 'visibilityState')
    try {
      const w = await mountTerm()
      const ws = FakeWS.all[0]
      ws.onopen?.({} as Event)
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
      document.dispatchEvent(new Event('visibilitychange'))
      expect(ws.closed).toBe(true)

      // Becoming the selected hostbud tab while the browser page is still
      // hidden must not create another tmux client.
      await w.setProps({ active: false })
      await w.setProps({ active: true })
      expect(FakeWS.all).toHaveLength(1)

      h.fitSize = { cols: 80, rows: 24 }
      resizeCallback()
      expect(FakeWS.all).toHaveLength(1)

      const beforeReattach = FakeWS.all.length
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
      document.dispatchEvent(new Event('visibilitychange'))
      expect(FakeWS.all.length).toBeGreaterThan(beforeReattach)
      expect(FakeWS.all.slice(beforeReattach).every((socket) => socket.url.includes('&cols=80&rows=24'))).toBe(true)
      const afterReattach = FakeWS.all.length
      document.dispatchEvent(new Event('visibilitychange'))
      expect(FakeWS.all).toHaveLength(afterReattach)
      w.unmount()
    } finally {
      if (original) Object.defineProperty(document, 'visibilityState', original)
      else Reflect.deleteProperty(document, 'visibilityState')
    }
  })

  it('exit ⇒ exit state with Reconnect, which re-attaches', async () => {
    const w = await mountTerm()
    FakeWS.all[0].onopen?.({} as Event)
    FakeWS.all[0].onmessage?.({ data: '{"type":"exit","code":0}' } as MessageEvent)
    await flushPromises()
    expect(w.get('[role=status]').text()).toContain('Session detached or ended.')
    await w.get('[role=status] button').trigger('click')
    expect(FakeWS.all).toHaveLength(2)
    await flushPromises()
    expect(w.find('[role=status]').exists()).toBe(false)
  })

  it('a dropped connection re-attaches by itself, with a Retry now strip meanwhile', async () => {
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [{ name: 'acc-a' } as Session] } })
    const w = await mountTerm()
    FakeWS.all[0].onopen?.({} as Event)
    FakeWS.all[0].onclose?.({} as CloseEvent)
    await flushPromises()
    expect(w.get('[role=status]').text()).toContain('Reconnecting… (attempt 1)')
    await w.get('[role=status] button').trigger('click') // Retry now
    expect(FakeWS.all).toHaveLength(2)
    FakeWS.all[1].onopen?.({} as Event)
    await flushPromises()
    expect(w.find('[role=status]').exists()).toBe(false)
  })

  it('a session that left the list shows Disconnected with Reconnect instead', async () => {
    const w = await mountTerm()
    FakeWS.all[0].onopen?.({} as Event)
    FakeWS.all[0].onclose?.({} as CloseEvent)
    await flushPromises()
    expect(w.get('[role=status]').text()).toContain('Disconnected: the session is gone.')
    expect(w.get('[role=status] button').text()).toBe('Reconnect')
  })

  it('detaches inactive tabs and refits on return without opening the keyboard', async () => {
    const w = await mountTerm()
    const t = h.terms[0]
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    await flushPromises()
    expect(t.focused).toBe(0)

    await w.setProps({ active: false })
    expect(ws.closed).toBe(true)
    h.fitSize = { cols: 5, rows: 2 } // what a hidden box would fit
    resizeCallback()
    expect(ws.sent).toEqual([])

    // Shown: it refits and reattaches, but focus stays closed until an explicit action.
    h.fitSize = { cols: 120, rows: 35 }
    await w.setProps({ active: true })
    await flushPromises()
    expect(t.focused).toBe(0)
    expect(FakeWS.all).toHaveLength(2)
    expect(FakeWS.all[1].url).toContain('&cols=120&rows=35')
    w.unmount()
  })

  it('selects the word under a long touch press so the mobile Copy action can use it', async () => {
    vi.useFakeTimers()
    const w = await mountTerm()
    const t = h.terms[0]
    const screen = document.createElement('div')
    screen.className = 'xterm-screen'
    screen.getBoundingClientRect = () => ({ left: 0, top: 0, right: 1000, bottom: 300, width: 1000, height: 300, x: 0, y: 0, toJSON: () => ({}) })
    w.get('[data-testid="terminal"]').element.appendChild(screen)
    const active = t.buffer.active as { viewportY: number; getLine: (row: number) => { translateToString: () => string; getCell: (column: number) => { getChars: () => string; getWidth: () => number } } }
    active.viewportY = 17
    active.getLine = (row) => {
      expect(row).toBe(18)
      const text = 'echo copy-marker'
      return {
        translateToString: () => text,
        getCell: (column) => ({ getChars: () => text[column] ?? '', getWidth: () => 1 }),
      }
    }

    const down = new Event('pointerdown', { bubbles: true, cancelable: true })
    Object.defineProperties(down, { pointerType: { value: 'touch' }, clientX: { value: 62 }, clientY: { value: 15 } })
    w.get('[data-testid="terminal"]').element.dispatchEvent(down)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(500)
    expect(t.select).toHaveBeenCalledWith(5, 18, 11)
    expect(w.find('button[aria-label="Terminal actions"]').exists()).toBe(true)
    w.unmount()
    vi.useRealTimers()
  })

  it('an unfocused pane stays unfocused when selected until Show keyboard', async () => {
    const w = await mountTerm({ focused: false })
    FakeWS.all[0].onopen?.({} as Event)
    await flushPromises()
    expect(h.terms[0].focused).toBe(0)
    await w.setProps({ focused: true })
    await flushPromises()
    expect(h.terms[0].focused).toBe(0)
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Show keyboard')
    expect(h.terms[0].focused).toBe(1)
    w.unmount()
  })

  it('focus inside the terminal is reported (it becomes the focused pane)', async () => {
    const w = await mountTerm({ focused: false })
    h.terms[0].textarea!.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    expect(w.emitted('focus')).toHaveLength(1)
    w.unmount()
  })

  it('split pickers only when the tab can take a pane', async () => {
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [{ name: 'acc-a' } as Session, { name: 'acc-b' } as Session] } })
    const w = await mountTerm()
    expect(w.find('[aria-label="Split right"]').exists()).toBe(false)
    await w.setProps({ canSplit: true })
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Split pane…')
    expect([...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].some((el) => el.textContent?.trim() === 'acc-b')).toBe(false)
    await clickMenuItem('Split right')
    expect([...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].map((el) => el.textContent?.trim())).toContain('acc-b')
    await clickMenuItem('acc-b')
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Split pane…')
    await clickMenuItem('Split down')
    await clickMenuItem('New session…')
    expect(w.emitted('split')).toEqual([['row', 'acc-b'], ['column', null]])
    // Narrow screens split from the list's row menu instead.
    await w.setProps({ narrow: true })
    expect(w.find('[aria-label="Split right"]').exists()).toBe(false)
    w.unmount()
  })

  it('narrow with several panes: a "Pane n of m" switcher; a focused split pane is outlined', async () => {
    const w = await mountTerm({ focused: true })
    expect(w.find('button[aria-label^="Pane "]').exists()).toBe(false)
    await w.setProps({ paneCount: 3, paneIndex: 2 })
    expect(w.get('section').classes()).toContain('outline-accent')
    await w.setProps({ narrow: true })
    expect(w.get('section').classes()).not.toContain('outline-accent')
    const b = w.get('button[aria-label^="Pane "]')
    expect(b.text()).toBe('Pane 2 of 3')
    await b.trigger('click')
    expect(w.emitted('cyclePane')).toHaveLength(1)
    w.unmount()
  })

  it('prepares the hidden input for on-screen keyboards', async () => {
    await mountTerm()
    const input = h.terms[0].textarea!
    expect(input.getAttribute('autocorrect')).toBe('off')
    expect(input.getAttribute('autocapitalize')).toBe('off')
    expect(input.getAttribute('autocomplete')).toBe('off')
    expect(input.getAttribute('spellcheck')).toBe('false')
  })

  it('menu actions open dictation and send text through xterm paste once', async () => {
    const w = await mountTerm()
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Dictation')
    const cancel = [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Cancel')!
    cancel.click()
    await flushPromises()
    expect(h.terms[0].paste).not.toHaveBeenCalled()
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Dictation')
    const editor = document.querySelector<HTMLTextAreaElement>('textarea[aria-label="Dictation text"]')!
    editor.value = 'echo dictated'
    editor.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: 'echo dictated' }))
    await flushPromises()
    ;[...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Send')!.click()
    await flushPromises()
    expect(h.terms[0].paste).toHaveBeenCalledOnce()
    expect(h.terms[0].paste).toHaveBeenCalledWith('echo dictated')
    await flushPromises()
    expect(document.querySelector('textarea[aria-label="Dictation text"]')).toBeNull()
  })

  it('menu opens a full-buffer text snapshot that stays unchanged', async () => {
    const fetchOutput = vi.fn(async () => new Response(JSON.stringify({ output: 'older history\n\x1b[31mstyled output\x1b[0m' }), { status: 200 }))
    vi.stubGlobal('fetch', fetchOutput)
    const w = await mountTerm()
    const buffer = h.terms[0].buffer.active as { length: number; getLine: (i: number) => unknown }
    buffer.length = 2
    buffer.getLine = (i: number) => ({ isWrapped: i === 1, translateToString: () => i === 0 ? 'scrollback' : ' continues' })
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('View terminal text')
    const snapshot = document.querySelector<HTMLElement>('[role="region"][aria-label="Terminal text"]')!
    expect(snapshot.textContent).toContain('older history\nstyled output')
    expect(fetchOutput).toHaveBeenCalledWith('/api/machines/host/sessions/acc-a/output', expect.objectContaining({ method: 'GET' }))
    expect(snapshot.closest('[role="dialog"]')?.querySelectorAll('button[aria-label="Close terminal view"]')).toHaveLength(1)
    expect(snapshot.closest('[role="dialog"]')?.querySelector('textarea')).toBeNull()
  })

  it('opens photo sending for the active session folder from the terminal menu', async () => {
    useProjectsStore().remember({ id: 'photo-project', machineId: 'host', path: '/home/dev/photo-repo', name: 'Photo repo', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
    useSessionsStore().$patch({ byMachine: { host: [{ id: '$1', name: 'acc-a', path: '/home/dev/photo-repo/subdir', projectId: 'photo-project', attached: 0, windows: 1, created: '', activity: '' }] } })
    const w = await mountTerm()
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Send photos to this repo')
    const input = document.querySelector<HTMLInputElement>('input[type="file"][accept="image/*,.heic,.heif,.dng"]')!
    const dialog = input.closest('[role="dialog"]')!
    expect(dialog.textContent).toContain('Send photos to this repo')
    expect(dialog.textContent).toContain('/home/dev/photo-repo')
    expect(input.getAttribute('accept')).toContain('image/*')
    w.unmount()
  })

  it('Cmd-V image paste uploads the original image to the active repo and never pastes binary data into tmux', async () => {
    useProjectsStore().remember({ id: 'photo-project', machineId: 'host', path: '/home/dev/photo-repo', name: 'Photo repo', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
    useSessionsStore().$patch({ byMachine: { host: [{ id: '$1', name: 'acc-a', path: '/home/dev/photo-repo/subdir', projectId: 'photo-project', attached: 0, windows: 1, created: '', activity: '' }] } })
    const upload = vi.spyOn(filesystemApi, 'uploadPhotoUnique').mockResolvedValue({ path: '/home/dev/photo-repo/pasted.png', size: 5 })
    const w = await mountTerm()
    const photo = new File([new Uint8Array([0, 255, 1, 2, 3])], 'pasted.png', { type: 'image/png' })
    const event = new Event('paste', { bubbles: true, cancelable: true }) as ClipboardEvent
    Object.defineProperty(event, 'clipboardData', { value: { items: [{ kind: 'file', type: 'image/png', getAsFile: () => photo }], files: [photo] } })
    h.terms[0].textarea!.dispatchEvent(event)
    await flushPromises()
    expect(event.defaultPrevented).toBe(true)
    expect(upload).toHaveBeenCalledWith('host', '/home/dev/photo-repo', photo)
    expect(h.terms[0].paste).toHaveBeenCalledOnce()
    expect(h.terms[0].paste).toHaveBeenCalledWith('../pasted.png')
    expect(useToastsStore().toasts[0]).toMatchObject({ title: 'Photo added to repo', message: '../pasted.png · 5 bytes; path pasted into terminal', tone: 'success', placement: 'top-right' })
    w.unmount()
  })

  it('backgrounding blurs the active terminal field and hidden tabs do not refocus', async () => {
    const descriptor = Object.getOwnPropertyDescriptor(document, 'visibilityState')
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    const w = await mountTerm({ focused: false })
    const t = h.terms[0]
    await w.setProps({ focused: true })
    await flushPromises()
    expect(t.focused).toBe(0)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    const before = t.focused
    await w.setProps({ focused: false })
    await w.setProps({ focused: true })
    await flushPromises()
    expect(t.focused).toBe(before)
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Show keyboard')
    expect(t.focused).toBe(before + 1)
    w.unmount()
    if (descriptor) Object.defineProperty(document, 'visibilityState', descriptor)
    else Reflect.deleteProperty(document, 'visibilityState')
  })

  it('Mac editing keys: sent once on keydown, browser default prevented', async () => {
    await mountTerm()
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    const t = h.terms[0]
    const down = new KeyboardEvent('keydown', { key: 'Backspace', metaKey: true, cancelable: true })
    expect(t.keyHandler(down)).toBe(false)
    expect(down.defaultPrevented).toBe(true)
    expect(t.keyHandler(new KeyboardEvent('keyup', { key: 'Backspace', metaKey: true }))).toBe(false)
    expect(ws.sent.map((d) => new TextDecoder().decode(d as Uint8Array))).toEqual(['\x15'])
    // Anything else goes to xterm as usual.
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'Backspace' }))).toBe(true)
    expect(ws.sent).toHaveLength(1)
  })

  it('keeps registered global shortcuts out of xterm while passing ordinary terminal chords through', async () => {
    await mountTerm()
    const t = h.terms[0]
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: ']', code: 'BracketRight', ctrlKey: true, shiftKey: true }))).toBe(false)
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'd', ctrlKey: true, shiftKey: true }))).toBe(false)
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, shiftKey: true }))).toBe(false)
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))).toBe(true)
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))).toBe(true)
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'b', altKey: true }))).toBe(true)
  })

  it('animates wheel notches in the scrollback without changing their step or direction (M8 T6)', async () => {
    const w = await mountTerm()
    const options = h.terms[0].options
    expect(options.smoothScrollDuration).toBe(100)
    // Rows per notch and direction stay xterm's defaults.
    expect(options.scrollSensitivity).toBeUndefined()
    expect(options.fastScrollSensitivity).toBeUndefined()
    // hostbud doesn't intercept the wheel itself: no scroll requests of its
    // own to duplicate or skip (copy-mode scrolling is tmux's, via the mouse).
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    w.get('[data-testid="terminal"]').element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, bubbles: true, cancelable: true }))
    await flushPromises()
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(FakeWS.all[0].sent).toEqual([])
    w.unmount()
  })

  it('scrolls tmux or the app through wheel actions on a touch swipe (M8 T9)', async () => {
    const w = await mountTerm()
    const fetchSpy = vi.fn(async () => new Response(JSON.stringify({ inMode: true, scrollPosition: 3, historySize: 50 }), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchSpy)
    const el = w.get('[data-testid="terminal"]').element
    const touch = (type: string, ys: number[]) => {
      const event = new Event(type, { bubbles: true, cancelable: true })
      Object.defineProperty(event, 'touches', { value: ys.map((y) => ({ clientX: 10, clientY: y })) })
      el.dispatchEvent(event)
      return event
    }
    touch('touchstart', [100])
    touch('touchmove', [110])
    const move = touch('touchmove', [158]) // 48 px at the 16 px fallback cell height
    expect(move.defaultPrevented).toBe(true)
    await vi.waitFor(() => expect(fetchSpy).toHaveBeenCalled())
    const [url, init] = fetchSpy.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toContain('/sessions/acc-a/copy-mode')
    expect(JSON.parse(String(init.body))).toEqual({ action: 'wheel-up', lines: 3 })
    expect(FakeWS.all[0].sent).toEqual([])
    touch('touchcancel', [])
    w.unmount()
  })

  it('Option+click forces selection on macOS; OSC 52 is loaded', async () => {
    await mountTerm()
    expect(h.terms[0].options.macOptionClickForcesSelection).toBe(true)
    // The addon's handler, then the query guard registered after it.
    expect(h.terms[0].oscHandlers).toEqual([52, 52])
  })

  describe('Option-click caret placement (M8 T5)', () => {
    // "$ hello world" on row 0 of a 100x30 terminal drawn 800x300 px (8x10
    // cells); the shell echoes each arrow by moving its cursor within the text.
    async function prompt() {
      const w = await mountTerm()
      const t = h.terms[0]
      const ws = FakeWS.all[0]
      ws.onopen?.({} as Event)
      const text = '$ hello world'
      const buffer = { cursorX: 13, cursorY: 0, baseY: 0, viewportY: 0, length: 1, getLine: (y: number) => (y === 0 ? { translateToString: () => text } : undefined) }
      t.buffer.active = buffer
      const screen = document.createElement('div')
      screen.className = 'xterm-screen'
      screen.getBoundingClientRect = () => ({ left: 0, top: 0, width: 800, height: 300 }) as DOMRect
      t.element!.appendChild(screen)
      const arrows: string[] = []
      ws.send = (d: string | Uint8Array) => {
        const data = typeof d === 'string' ? d : new TextDecoder().decode(d)
        arrows.push(data)
        for (const m of data.matchAll(/\x1b\[([CD])/g)) buffer.cursorX = Math.max(2, Math.min(13, buffer.cursorX + (m[1] === 'C' ? 1 : -1)))
        queueMicrotask(() => t.writeParsed.forEach((fn) => fn()))
      }
      const click = (col: number, init: MouseEventInit = {}, moveTo = col) => {
        w.get('[data-testid="terminal"]').element.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0, altKey: true, clientX: col * 8 + 3, clientY: 5, ...init }))
        window.dispatchEvent(new MouseEvent('mouseup', { button: 0, altKey: true, clientX: moveTo * 8 + 3, clientY: 5, ...init }))
      }
      return { w, t, buffer, arrows, click }
    }

    it("replaces xterm's one-shot move and lands on the clicked character", async () => {
      const { w, t, buffer, arrows, click } = await prompt()
      expect(t.options.altClickMovesCursor).toBe(false)
      click(4) // the first "l"
      await vi.waitFor(() => expect(buffer.cursorX).toBe(4))
      expect(arrows.join('')).toBe('\x1b[D'.repeat(9))
      click(12) // the "d"
      await vi.waitFor(() => expect(buffer.cursorX).toBe(12))
      click(0) // on the prompt: stops at the start of the text
      await vi.waitFor(() => expect(buffer.cursorX).toBe(2))
      expect(arrows.join('')).not.toMatch(/\x1b\[[AB]/)
      w.unmount()
    })

    it('leaves ordinary clicks, Option+drag selection and scrolled-back views alone', async () => {
      const { w, t, buffer, arrows, click } = await prompt()
      click(4, { altKey: false })
      click(4, {}, 9) // a drag
      t.selection = 'hello'
      click(4)
      t.selection = ''
      buffer.viewportY = -5
      click(4)
      await flushPromises()
      expect(arrows).toEqual([])
      expect(buffer.cursorX).toBe(13)
      w.unmount()
    })
  })

  it('copy keys write the selection, send no bytes and keep the selection', async () => {
    const writeText = vi.fn(async () => {})
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    await mountTerm()
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    const t = h.terms[0]
    t.selection = 'copied text'
    const down = new KeyboardEvent('keydown', { key: 'C', ctrlKey: true, shiftKey: true, cancelable: true })
    expect(t.keyHandler(down)).toBe(false)
    expect(down.defaultPrevented).toBe(true)
    expect(t.keyHandler(new KeyboardEvent('keyup', { key: 'C', ctrlKey: true, shiftKey: true }))).toBe(false)
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'c', metaKey: true }))).toBe(false)
    await flushPromises()
    expect(writeText.mock.calls).toEqual([['copied text'], ['copied text']])
    expect(t.selection).toBe('copied text')
    expect(ws.sent).toEqual([])
    // Ctrl+C stays the program's interrupt, selection or not.
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'c', ctrlKey: true }))).toBe(true)
  })

  it('paste keys send no bytes and keep the browser default (its paste event)', async () => {
    await mountTerm()
    const ws = FakeWS.all[0]
    ws.onopen?.({} as Event)
    const t = h.terms[0]
    for (const init of [
      { key: 'V', ctrlKey: true, shiftKey: true },
      { key: 'V', metaKey: true, shiftKey: true },
      { key: 'v', metaKey: true },
    ]) {
      const down = new KeyboardEvent('keydown', { ...init, cancelable: true })
      expect(t.keyHandler(down)).toBe(false)
      expect(down.defaultPrevented).toBe(false)
    }
    expect(ws.sent).toEqual([])
  })

  it('a refused copy shows a toast', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: () => Promise.reject(new Error('denied')) } })
    await mountTerm()
    const t = h.terms[0]
    t.selection = 'x'
    t.keyHandler(new KeyboardEvent('keydown', { key: 'C', ctrlKey: true, shiftKey: true }))
    await flushPromises()
    expect(useToastsStore().toasts.map((x) => x.title)).toEqual(["Couldn't copy"])
  })

  it('printed URLs open in a new tab (http/https only)', async () => {
    const open = vi.fn()
    vi.stubGlobal('open', open)
    await mountTerm()
    h.linkClick(new MouseEvent('click'), 'https://example.com/x')
    h.linkClick(new MouseEvent('click'), 'javascript:alert(1)')
    expect(open.mock.calls).toEqual([['https://example.com/x', '_blank', 'noopener,noreferrer']])
  })

  it('OSC 8 links: hovering shows the real target in a tooltip', async () => {
    const w = await mountTerm()
    const handler = h.terms[0].options.linkHandler as import('@xterm/xterm').ILinkHandler
    const range = { start: { x: 1, y: 1 }, end: { x: 5, y: 1 } }
    handler.hover!(new MouseEvent('mousemove', { clientX: 30, clientY: 40 }), 'https://example.com/real', range)
    await flushPromises()
    expect(w.get('[role=tooltip]').text()).toBe('https://example.com/real')
    handler.leave!(new MouseEvent('mouseleave'), 'https://example.com/real', range)
    await flushPromises()
    expect(w.find('[role=tooltip]').exists()).toBe(false)
  })

  it('Ctrl+Shift+F opens search pre-filled with the selection; Escape returns focus', async () => {
    const w = await mountTerm()
    const t = h.terms[0]
    t.selection = 'needle\nsecond line'
    const down = new KeyboardEvent('keydown', { key: 'F', ctrlKey: true, shiftKey: true, cancelable: true })
    expect(t.keyHandler(down)).toBe(false)
    expect(down.defaultPrevented).toBe(true)
    await flushPromises()
    const field = w.get('input[aria-label=Find]')
    expect((field.element as HTMLInputElement).value).toBe('needle')
    // Plain Ctrl+F is readline's.
    expect(t.keyHandler(new KeyboardEvent('keydown', { key: 'f', ctrlKey: true }))).toBe(true)
    const focused = t.focused
    await field.trigger('keydown', { key: 'Escape' })
    expect(w.find('[role=search]').exists()).toBe(false)
    expect(t.focused).toBe(focused + 1)
  })

  it('the 🔍 button opens search (the way in on phones)', async () => {
    const w = await mountTerm()
    await w.get('button[aria-label="Terminal actions"]').trigger('click')
    await flushPromises()
    await clickMenuItem('Search')
    expect(w.find('[role=search]').exists()).toBe(true)
  })

  it('does not replace the tree drawer control with a terminal back button', async () => {
    const w = await mountTerm()
    expect(w.find('button[aria-label="Back to sessions"]').exists()).toBe(false)
    expect(w.get('h2').text()).toBe('acc-a')
  })
})
