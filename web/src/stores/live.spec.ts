import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import type { Machine, Session } from '@/api/types'
import type { ServerEvent } from '@/api/types'
import { panesOf } from '@/lib/layout'
import { useLayoutStore } from './layout'
import { useLiveStore } from './live'
import { applyMachines, useMachinesStore } from './machines'
import { applySessions, useSessionsStore } from './sessions'

const m = (id: string, status: Machine['status'] = 'ok'): Machine => ({
  id, label: id, status, os: 'Linux', home: '/home/dev', tmuxVersion: '3.4', tmuxMissing: false,
})
const s = (name: string, attached = 0, windows = 1): Session => ({
  id: '$1', name, path: '/home/dev', attached, windows, created: '', activity: '',
})

beforeEach(() => setActivePinia(createPinia()))

describe('machines reducer', () => {
  it('replaces everything on a snapshot', () => {
    expect(applyMachines([m('old')], { type: 'snapshot', machines: [m('host')], sessions: {} })).toEqual([m('host')])
  })
  it('upserts on machine.status', () => {
    const next = applyMachines([m('host')], { type: 'machine.status', machine: 'host', payload: m('host', 'unreachable') })
    expect(next).toEqual([m('host', 'unreachable')])
    expect(applyMachines([], { type: 'machine.status', machine: 'x', payload: m('x') })).toEqual([m('x')])
  })
  it('ignores sessions.changed', () => {
    const before = [m('host')]
    expect(applyMachines(before, { type: 'sessions.changed', machine: 'host', payload: { sessions: [] } })).toBe(before)
  })
})

describe('sessions reducer', () => {
  it('replaces everything on a snapshot (resync)', () => {
    const next = applySessions({ host: [s('stale')], gone: [s('x')] }, { type: 'snapshot', machines: [], sessions: { host: [s('a')] } })
    expect(next).toEqual({ host: [s('a')] })
  })
  it('replaces one machine on sessions.changed', () => {
    const next = applySessions({ host: [s('a')], other: [s('b')] },
      { type: 'sessions.changed', machine: 'host', payload: { sessions: [s('a', 1, 3), s('c')] } })
    expect(next).toEqual({ host: [s('a', 1, 3), s('c')], other: [s('b')] })
  })
  it('removal: a session missing from sessions.changed is gone', () => {
    const next = applySessions({ host: [s('a'), s('b')] }, { type: 'sessions.changed', machine: 'host', payload: { sessions: [s('b')] } })
    expect(next.host.map((x) => x.name)).toEqual(['b'])
  })
})

describe('stores', () => {
  it('apply events and reset', () => {
    const machines = useMachinesStore()
    const sessions = useSessionsStore()
    const snap = { type: 'snapshot' as const, machines: [m('host')], sessions: { host: [s('a')] } }
    machines.apply(snap)
    sessions.apply(snap)
    expect(machines.byId('host')?.status).toBe('ok')
    expect(sessions.list('host').map((x) => x.name)).toEqual(['a'])
    expect(sessions.list('nope')).toEqual([])
    machines.reset()
    sessions.reset()
    expect(machines.machines).toEqual([])
    expect(sessions.byMachine).toEqual({})
  })
})

describe('ended sessions close their terminals', () => {
  // Feeds an event the way the live connection does.
  function feed(e: ServerEvent) {
    useMachinesStore().apply(e)
    useSessionsStore().apply(e)
    useLiveStore().closeEndedSessions(e)
  }
  function withTabs(...names: string[]) {
    const layout = useLayoutStore()
    layout.loaded = true
    for (const n of names) layout.open('host', n)
    return layout
  }
  const open = () => useLayoutStore().tabs.map((t) => panesOf(t.root)[0].session)

  it('a list from a reachable host drops panes of missing sessions', () => {
    withTabs('a', 'b')
    feed({ type: 'snapshot', machines: [m('host')], sessions: { host: [s('a'), s('b')] } })
    expect(open()).toEqual(['a', 'b'])
    feed({ type: 'sessions.changed', machine: 'host', payload: { sessions: [s('a')] } })
    expect(open()).toEqual(['a'])
  })

  it('an unlisted or unreachable host keeps them (empty list right after a restart)', () => {
    withTabs('a')
    feed({ type: 'snapshot', machines: [m('host', 'unknown')], sessions: { host: [] } })
    expect(open()).toEqual(['a'])
    // The first poll: status first, then the list.
    feed({ type: 'machine.status', machine: 'host', payload: m('host') })
    expect(open()).toEqual(['a'])
    feed({ type: 'sessions.changed', machine: 'host', payload: { sessions: [s('a')] } })
    feed({ type: 'machine.status', machine: 'host', payload: m('host', 'unreachable') })
    feed({ type: 'snapshot', machines: [m('host', 'unreachable')], sessions: { host: [] } })
    expect(open()).toEqual(['a'])
  })
})
