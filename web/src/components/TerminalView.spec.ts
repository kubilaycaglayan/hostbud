import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '@/api/types'
import { useSessionsStore } from '@/stores/sessions'
import { useToastsStore } from '@/stores/toasts'

// Fake xterm (hoisted: vi.mock factories run before the module body).
const h = vi.hoisted(() => {
  class FakeTerminal {
    cols = 80
    rows = 24
    written: string[] = []
    onDataFn: (d: string) => void = () => {}
    unicode = { activeVersion: '6' }
    buffer = { active: { length: 0, getLine: () => undefined } }
    selection = ''
    modes = { mouseTrackingMode: 'none' }
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
    // Like xterm: input from the user goes out through onData.
    input(data: string) {
      this.onDataFn(data)
    }
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

async function mountTerm() {
  const w = mount(TerminalView, { props: { machine: 'host', session: 'acc-a' }, attachTo: document.body })
  await flushPromises()
  return w
}

describe('TerminalView', () => {
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

  it('prepares the hidden input for on-screen keyboards', async () => {
    await mountTerm()
    const input = h.terms[0].textarea!
    expect(input.getAttribute('autocorrect')).toBe('off')
    expect(input.getAttribute('autocapitalize')).toBe('off')
    expect(input.getAttribute('autocomplete')).toBe('off')
    expect(input.getAttribute('spellcheck')).toBe('false')
  })

  it('Show keyboard focuses the terminal (touch screens only)', async () => {
    const w = await mountTerm()
    const button = w.get('button[aria-label="Show keyboard"]')
    // Hidden unless the primary pointer is coarse (a touch screen).
    expect(button.classes()).toEqual(expect.arrayContaining(['hidden', 'pointer-coarse:inline-block']))
    const before = h.terms[0].focused
    await button.trigger('click')
    expect(h.terms[0].focused).toBe(before + 1)
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

  it('Option+click forces selection on macOS; OSC 52 is loaded', async () => {
    await mountTerm()
    expect(h.terms[0].options.macOptionClickForcesSelection).toBe(true)
    // The addon's handler, then the query guard registered after it.
    expect(h.terms[0].oscHandlers).toEqual([52, 52])
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
    await w.get('button[aria-label=Search]').trigger('click')
    await flushPromises()
    expect(w.find('[role=search]').exists()).toBe(true)
  })

  it('offers a way back to the list (narrow screens)', async () => {
    const w = await mountTerm()
    await w.get('button[aria-label="Back to sessions"]').trigger('click')
    expect(w.emitted('back')).toHaveLength(1)
  })
})
