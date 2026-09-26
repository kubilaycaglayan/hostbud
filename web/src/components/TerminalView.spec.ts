import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Fake xterm (hoisted: vi.mock factories run before the module body).
const h = vi.hoisted(() => {
  class FakeTerminal {
    cols = 80
    rows = 24
    written: string[] = []
    onDataFn: (d: string) => void = () => {}
    unicode = { activeVersion: '6' }
    buffer = { active: { length: 0, getLine: () => undefined } }
    constructor() {
      h.terms.push(this)
    }
    loadAddon(a: { activate?: (t: FakeTerminal) => void }) {
      a.activate?.(this)
    }
    open() {}
    focus() {}
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
  }
  const h = { terms: [] as FakeTerminal[], fitSize: { cols: 100, rows: 30 }, FakeTerminal }
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
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: class {} }))
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: class {} }))

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

  it('a dropped connection shows Disconnected with Reconnect', async () => {
    const w = await mountTerm()
    FakeWS.all[0].onopen?.({} as Event)
    FakeWS.all[0].onclose?.({} as CloseEvent)
    await flushPromises()
    expect(w.get('[role=status]').text()).toContain('Disconnected')
    expect(w.get('[role=status] button').text()).toBe('Reconnect')
  })

  it('offers a way back to the list (narrow screens)', async () => {
    const w = await mountTerm()
    await w.get('button[aria-label="Back to sessions"]').trigger('click')
    expect(w.emitted('back')).toHaveLength(1)
  })
})
