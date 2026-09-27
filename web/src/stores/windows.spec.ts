import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { Session, TmuxWindows } from '@/api/types'
import { ApiError, windowsApi } from '@/api/client'
import { useLayoutStore } from './layout'
import { useMachinesStore } from './machines'
import { useSessionsStore } from './sessions'
import { useToastsStore } from './toasts'
import { useTreeStore } from './tree'
import { useWindowsStore } from './windows'

const session = (name: string, windows = 2, activity = 'now'): Session => ({ id: '$' + name, name, path: '/work', attached: 0, windows, created: '', activity })
const listing: TmuxWindows = {
  windows: [
    { id: '@1', index: 0, name: 'shell', active: true, panes: [{ id: '%1', index: 0, active: true, command: 'bash', width: 80, height: 24 }] },
    { id: '@2', index: 1, name: 'editor', active: false, panes: [
      { id: '%2', index: 0, active: true, command: 'vim', width: 40, height: 24 },
      { id: '%3', index: 1, active: false, command: 'bash', width: 40, height: 24 },
    ] },
  ], truncated: false,
}

function setHostSessions(...rows: Session[]) {
  const machines = useMachinesStore()
  machines.apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: 'linux', home: '/home/dev', tmuxVersion: '3.4', tmuxMissing: false }], sessions: { host: rows } })
  useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: rows } })
}

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  useWindowsStore().reset()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('windows store', () => {
  it('fetches on expand, refreshes after collapse, and does not fetch while collapsed', async () => {
    vi.useFakeTimers()
    setHostSessions(session('work'))
    const list = vi.spyOn(windowsApi, 'list').mockResolvedValue(listing)
    const windows = useWindowsStore()
    windows.toggleSession('host', 'work')
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(1)
    expect(windows.bySession['host/work']).toMatchObject({ status: 'ok', windows: listing.windows })
    windows.toggleSession('host', 'work')
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 3, 'later')] } })
    await vi.advanceTimersByTimeAsync(300)
    expect(list).toHaveBeenCalledTimes(1)
    windows.toggleSession('host', 'work')
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('renders an error state and retries on demand', async () => {
    setHostSessions(session('work'))
    const list = vi.spyOn(windowsApi, 'list').mockRejectedValueOnce(new Error('Host unavailable')).mockResolvedValueOnce(listing)
    const windows = useWindowsStore()
    windows.toggleSession('host', 'work')
    await flushPromises()
    expect(windows.bySession['host/work']).toMatchObject({ status: 'error' })
    windows.refresh('host', 'work')
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    expect(windows.bySession['host/work']).toMatchObject({ status: 'ok' })
  })

  it('drops responses after collapse and removes state when the session ends', async () => {
    setHostSessions(session('work'))
    let resolve!: (result: TmuxWindows) => void
    vi.spyOn(windowsApi, 'list').mockImplementation(() => new Promise((r) => { resolve = r }))
    const windows = useWindowsStore()
    windows.toggleSession('host', 'work')
    windows.toggleSession('host', 'work')
    resolve(listing)
    await flushPromises()
    expect(windows.bySession['host/work']).toBeUndefined()

    windows.toggleSession('host', 'work')
    resolve(listing)
    await flushPromises()
    useSessionsStore().apply({ type: 'sessions.changed', machine: 'host', payload: { sessions: [] } })
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [] } })
    expect(windows.bySession['host/work']).toBeUndefined()
    expect(useTreeStore().order.expanded).not.toContain('host/work')
  })

  it('drops an in-flight response after the session is renamed', async () => {
    setHostSessions(session('old'))
    let resolve!: (result: TmuxWindows) => void
    vi.spyOn(windowsApi, 'list').mockImplementation(() => new Promise((r) => { resolve = r }))
    const windows = useWindowsStore()
    windows.toggleSession('host', 'old')
    useSessionsStore().apply({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('new')] } })
    useTreeStore().order.expanded = ['host/new']
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('new')] } })
    resolve(listing)
    await flushPromises()
    expect(windows.bySession['host/old']).toBeUndefined()
    expect(windows.bySession['host/new']).toBeUndefined()
  })

  it('debounces count and activity changes, ignores unrelated sessions and prunes ended sessions', async () => {
    vi.useFakeTimers()
    const work = session('work')
    const other = session('other')
    setHostSessions(work, other)
    const list = vi.spyOn(windowsApi, 'list').mockResolvedValue(listing)
    const windows = useWindowsStore()
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [work, other] } })
    windows.toggleSession('host', 'work')
    await flushPromises()
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 3), other] } })
    await vi.advanceTimersByTimeAsync(299)
    expect(list).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 3, 'later'), other] } })
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(3)
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 3, 'later'), session('other', 3)] } })
    await vi.advanceTimersByTimeAsync(300)
    expect(list).toHaveBeenCalledTimes(3)
    useSessionsStore().apply({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('other')] } })
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('other')] } })
    expect(windows.bySession['host/work']).toBeUndefined()
    expect(useTreeStore().order.expanded).not.toContain('host/work')
  })

  it('allows one in-flight refresh and coalesces subsequent changes', async () => {
    vi.useFakeTimers()
    setHostSessions(session('work'))
    let resolve!: (result: TmuxWindows) => void
    const list = vi.spyOn(windowsApi, 'list').mockImplementation(() => new Promise((r) => { resolve = r }))
    const windows = useWindowsStore()
    windows.toggleSession('host', 'work')
    await flushPromises()
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 3, 'first')] } })
    await vi.advanceTimersByTimeAsync(300)
    expect(list).toHaveBeenCalledTimes(1)
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 4, 'second')] } })
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 5, 'third')] } })
    resolve(listing)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(300)
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('opens through layout before selecting a window or pane', async () => {
    setHostSessions(session('work'))
    const order: string[] = []
    const layout = useLayoutStore()
    vi.spyOn(layout, 'open').mockImplementation(() => { order.push('open'); return true })
    vi.spyOn(windowsApi, 'select').mockImplementation(async () => {
      order.push('select')
      return listing
    })
    expect(useWindowsStore().openAt('host', 'work', '@2', '%3')).toBe(true)
    await flushPromises()
    expect(order).toEqual(['open', 'select'])
    expect(windowsApi.select).toHaveBeenCalledWith('host', 'work', '@2', '%3')
    expect(useWindowsStore().bySession['host/work']).toMatchObject({ status: 'ok' })
  })

  it('toasts on a vanished window and refreshes expanded rows', async () => {
    setHostSessions(session('work'))
    const list = vi.spyOn(windowsApi, 'list').mockResolvedValue(listing)
    vi.spyOn(windowsApi, 'select').mockRejectedValue(new ApiError(404, 'window not found'))
    const windows = useWindowsStore()
    windows.toggleSession('host', 'work')
    await flushPromises()
    await windows.select('host', 'work', '@99')
    await flushPromises()
    expect(useToastsStore().toasts[0]?.title).toBe('Window no longer available')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('restores only expanded sessions and prunes stale window keys on load', async () => {
    setHostSessions(session('work'), session('other'), session('collapsed'))
    const tree = useTreeStore()
    tree.order.expanded = ['host/work', 'host/work/@99', 'host/other', 'host/other/@9']
    const list = vi.spyOn(windowsApi, 'list').mockResolvedValue(listing)
    const windows = useWindowsStore()
    windows.restore()
    await flushPromises()
    expect(list).toHaveBeenCalledTimes(2)
    expect(list).toHaveBeenCalledWith('host', 'work')
    expect(list).toHaveBeenCalledWith('host', 'other')
    expect(tree.order.expanded).toEqual(['host/work', 'host/other'])
    windows.toggleWindow('host', 'work', '@2')
    expect(tree.order.expanded).toContain('host/work/@2')
  })

  it('does not schedule refresh timers unless a session is expanded', () => {
    vi.useFakeTimers()
    setHostSessions(session('work'))
    const windows = useWindowsStore()
    windows.applyEvent({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('work', 3, 'later')] } })
    expect(vi.getTimerCount()).toBe(0)
  })
})
