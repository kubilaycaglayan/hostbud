import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { Machine, Project, Session } from '@/api/types'
import { useMachinesStore } from './machines'
import { useProjectsStore } from './projects'
import { useSessionsStore } from './sessions'
import { useTreeStore } from './tree'

// V2-M13 T4: the tree shows every machine's sessions.
const machine = (id: string, status: Machine['status'] = 'ok'): Machine => ({ id, label: id === 'host' ? 'Host' : 'Build box', source: id === 'host' ? 'host' : 'custom', status, os: '', home: '/home/dev', tmuxVersion: '', tmuxMissing: false })
const project = (id: string, path: string, machineId = 'host'): Project => ({ id, machineId, path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 1, created: '', activity: '' })

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => useTreeStore().reset())

function snapshot(machines: Machine[], sessions: Record<string, Session[]>) {
  const e = { type: 'snapshot' as const, machines, sessions }
  useMachinesStore().apply(e)
  useSessionsStore().apply(e)
}

describe('tree store with servers', () => {
  it('groups server sessions under the server project and orders them by ref', () => {
    const tree = useTreeStore()
    const projects = useProjectsStore()
    projects.loaded = true
    projects.remember(project('srv', '/home/dev/app', 's-abc'))
    snapshot([machine('host'), machine('s-abc')], { host: [session('local', '/home/dev/app')], 's-abc': [session('one', '/home/dev/app'), session('two', '/home/dev/app')] })
    tree.sync()
    expect(useSessionsStore().all.map((s) => `${s.machine ?? 'host'}:${s.name}`)).toEqual(['host:local', 's-abc:one', 's-abc:two'])
    expect(tree.order.sessions.srv).toEqual(['s-abc/one', 's-abc/two'])
    expect(tree.order.sessions.__other__).toEqual(['local'])
    tree.reorderSessions('srv', ['s-abc/two', 's-abc/one'])
    expect(tree.groups.groups[0].sessions.map((s) => s.name)).toEqual(['two', 'one'])

    tree.hideSession('s-abc', 'one')
    tree.reorderSessions('srv', ['s-abc/two'])
    expect(tree.order.sessions.srv).toEqual(['s-abc/two', 's-abc/one'])

    tree.renameSession('s-abc', 'two', 'deux')
    expect(tree.order.sessions.srv).toEqual(['s-abc/deux', 's-abc/one'])
  })

  it('keeps saved order while a server is unreachable and drops a removed server\'s keys', () => {
    const tree = useTreeStore()
    useProjectsStore().loaded = true
    snapshot([machine('host'), machine('s-abc', 'unreachable')], { host: [session('local', '/x')], 's-abc': [] })
    tree.order.sessions.__other__ = ['local', 's-abc/away']
    tree.order.hidden.sessions = ['s-abc/away']
    tree.sync()
    expect(tree.order.sessions.__other__).toEqual(['local', 's-abc/away'])
    expect(tree.order.hidden.sessions).toEqual(['s-abc/away'])

    useMachinesStore().apply({ type: 'machine.removed', machine: 's-abc', payload: { id: 's-abc' } })
    useSessionsStore().apply({ type: 'machine.removed', machine: 's-abc', payload: { id: 's-abc' } })
    tree.sync()
    expect(tree.order.sessions.__other__).toEqual(['local'])
    expect(tree.order.hidden.sessions).toEqual([])
  })
})
