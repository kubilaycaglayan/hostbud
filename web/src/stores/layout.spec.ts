import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { MAX_PANES, panesOf } from '@/lib/layout'
import { stubFetch } from '@/test-utils'
import { SAVE_DEBOUNCE_MS, SAVE_RETRY_MS, useLayoutStore } from './layout'
import { useToastsStore } from './toasts'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const saved = (tabs: { session: string }[], active = 0) => ({
  version: 1,
  tabs: tabs.map((t, i) => ({ id: `t${i}`, root: { type: 'pane', id: `p${i}`, machine: 'host', session: t.session }, focusedPane: `p${i}` })),
  activeTab: `t${active}`,
})
const open = () => useLayoutStore().tabs.map((t) => panesOf(t.root).map((p) => p.session).join('+'))

async function loaded(stored: unknown = null) {
  stubFetch((method) =>
    method === 'GET' ? (stored === null ? { status: 404, body: { error: 'nothing saved yet' } } : { status: 200, body: stored }) : { status: 204 },
  )
  const layout = useLayoutStore()
  await layout.load()
  return layout
}

describe('load', () => {
  it('restores the saved tabs and active tab', async () => {
    const layout = await loaded(saved([{ session: 'a' }, { session: 'b' }], 1))
    expect(layout.loaded).toBe(true)
    expect(open()).toEqual(['a', 'b'])
    expect(layout.focused?.session).toBe('b')
  })

  it('nothing saved yet → empty', async () => {
    const layout = await loaded()
    expect(layout.loaded).toBe(true)
    expect(layout.tabs).toEqual([])
  })

  it('invalid data → empty, with a console warning', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const layout = await loaded({ version: 9 })
    expect(layout.tabs).toEqual([])
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('invalid saved layout'))
  })

  it('a sign-out while loading wins', async () => {
    stubFetch(() => ({ status: 200, body: saved([{ session: 'a' }]) }))
    const layout = useLayoutStore()
    const p = layout.load()
    layout.reset()
    await p
    expect(layout.loaded).toBe(false)
    expect(layout.tabs).toEqual([])
  })
})

describe('save', () => {
  it('is debounced and sends the whole layout', async () => {
    const layout = await loaded()
    vi.useFakeTimers()
    const calls = stubFetch(() => ({ status: 204 }))
    layout.open('host', 'a')
    await nextTick()
    vi.advanceTimersByTime(SAVE_DEBOUNCE_MS - 1)
    layout.open('host', 'b')
    await nextTick()
    vi.advanceTimersByTime(SAVE_DEBOUNCE_MS - 1)
    expect(calls).toEqual([])
    vi.advanceTimersByTime(1)
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ method: 'PUT', path: '/api/ui-state/layout' })
    const body = calls[0].body as { tabs: { root: { session: string } }[] }
    expect(body.tabs.map((t) => t.root.session)).toEqual(['a', 'b'])
  })

  it('loading saves nothing, even once the debounce has passed', async () => {
    vi.useFakeTimers()
    const calls = stubFetch((m) => (m === 'GET' ? { status: 200, body: saved([{ session: 'a' }]) } : { status: 204 }))
    await useLayoutStore().load()
    await nextTick()
    vi.advanceTimersByTime(SAVE_DEBOUNCE_MS * 2)
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([])
  })

  it('flush sends a pending save right away (keepalive), and only a pending one', async () => {
    const layout = await loaded()
    vi.useFakeTimers()
    const init: RequestInit[] = []
    vi.stubGlobal('fetch', vi.fn(async (_p: string, i: RequestInit) => (init.push(i), new Response(null, { status: 204 }))))
    layout.flush()
    expect(init).toEqual([])
    layout.open('host', 'a')
    layout.flush()
    expect(init).toHaveLength(1)
    expect(init[0]).toMatchObject({ method: 'PUT', keepalive: true })
    vi.advanceTimersByTime(SAVE_DEBOUNCE_MS * 2)
    expect(init).toHaveLength(1)
  })

  it('a failed save is tried again', async () => {
    const layout = await loaded()
    vi.useFakeTimers()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    let status = 502
    const calls = stubFetch(() => ({ status }))
    layout.open('host', 'a')
    await vi.advanceTimersByTimeAsync(SAVE_DEBOUNCE_MS)
    expect(calls).toHaveLength(1)
    status = 204
    await vi.advanceTimersByTimeAsync(SAVE_RETRY_MS)
    expect(calls).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(SAVE_RETRY_MS * 2)
    expect(calls).toHaveLength(2)
  })

  it('loading or signing out saves nothing', async () => {
    vi.useFakeTimers()
    const calls = stubFetch((m) => (m === 'GET' ? { status: 200, body: saved([{ session: 'a' }]) } : { status: 204 }))
    const layout = useLayoutStore()
    await layout.load()
    await nextTick()
    layout.open('host', 'b')
    await nextTick()
    layout.reset()
    await nextTick()
    vi.advanceTimersByTime(SAVE_DEBOUNCE_MS * 2)
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([])
  })
})

describe('tabs', () => {
  it('the limit shows a notice and opens nothing', async () => {
    const layout = await loaded()
    for (let i = 0; i < MAX_PANES; i++) expect(layout.open('host', `s${i}`)).toBe(true)
    expect(layout.open('host', 'extra')).toBe(false)
    expect(open()).not.toContain('extra')
    expect(useToastsStore().toasts.map((t) => t.title)).toEqual(['Too many terminals'])
  })

  it('killed from this UI: closes quietly', async () => {
    const layout = await loaded()
    layout.open('host', 'a')
    layout.open('host', 'b')
    layout.closeSession('host', 'a')
    expect(open()).toEqual(['b'])
    expect(useToastsStore().toasts).toEqual([])
  })
})

describe('sessions that end', () => {
  it('close their tabs with one notice', async () => {
    const layout = await loaded(saved([{ session: 'a' }, { session: 'b' }, { session: 'c' }]))
    layout.syncSessions('host', new Set(['b']))
    expect(open()).toEqual(['b'])
    expect(useToastsStore().toasts).toMatchObject([{ title: 'Sessions ended', message: 'Closed the terminals of a, c.' }])
    layout.syncSessions('host', new Set())
    expect(useToastsStore().toasts.at(-1)).toMatchObject({ title: 'Session b ended' })
  })

  it('before the saved layout is loaded, nothing happens', () => {
    const layout = useLayoutStore()
    layout.syncSessions('host', new Set())
    expect(useToastsStore().toasts).toEqual([])
  })

  it('a rename from this UI that the list shows first keeps the tab', async () => {
    const layout = await loaded(saved([{ session: 'a' }]))
    layout.expectRename('host', 'a', 'z')
    layout.syncSessions('host', new Set(['z']))
    expect(open()).toEqual(['z'])
    layout.renamed('host', 'a', 'z')
    expect(open()).toEqual(['z'])
    expect(useToastsStore().toasts).toEqual([])
  })

  it('a rename that lands after the list: the tab follows', async () => {
    const layout = await loaded(saved([{ session: 'a' }]))
    layout.expectRename('host', 'a', 'z')
    layout.renamed('host', 'a', 'z')
    layout.syncSessions('host', new Set(['z']))
    expect(open()).toEqual(['z'])
  })

  it('an abandoned rename no longer protects the tab', async () => {
    const layout = await loaded(saved([{ session: 'a' }]))
    layout.expectRename('host', 'a', 'z')
    layout.renameAbandoned('host', 'a')
    layout.syncSessions('host', new Set(['z']))
    expect(open()).toEqual([])
  })
})
