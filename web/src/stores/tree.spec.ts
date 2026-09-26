import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useProjectsStore } from './projects'
import { useSessionsStore } from './sessions'
import { useTreeStore } from './tree'
import { useMachinesStore } from './machines'
import { stubFetch } from '@/test-utils'
import type { Project, Session } from '@/api/types'

const project = (id: string, path: string): Project => ({ id, machineId: 'host', path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 2, created: '', activity: '' })

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  useTreeStore().reset()
  vi.useRealTimers()
})

describe('tree order store', () => {
  it('keeps saved session order before the host has a reachable session snapshot', async () => {
    stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { version: 1, projects: [], sessions: { __other__: ['saved-session'] } } }
      : { status: 204 })
    const tree = useTreeStore()
    await tree.load()
    useMachinesStore().apply({
      type: 'snapshot',
      machines: [{ id: 'host', label: 'Host', status: 'unknown', os: '', home: '', tmuxVersion: '', tmuxMissing: false }],
      sessions: { host: [] },
    })
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [] } })

    // App.vue calls sync after tree.load but before live.start has delivered
    // the first reachable inventory snapshot.
    tree.sync()
    expect(tree.order.sessions.__other__).toEqual(['saved-session'])
  })

  it.each(['unknown', 'unreachable'] as const)('keeps saved entries when machine status is %s', async (status) => {
    stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { version: 1, projects: [], sessions: { __other__: ['stale-session'] } } }
      : { status: 204 })
    const tree = useTreeStore()
    await tree.load()
    useMachinesStore().apply({
      type: 'snapshot',
      machines: [{ id: 'host', label: 'Host', status, os: '', home: '', tmuxVersion: '', tmuxMissing: false }],
      sessions: { host: [] },
    })
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [] } })
    tree.sync()
    expect(tree.order.sessions.__other__).toEqual(['stale-session'])
  })

  it('loads account order, appends observed rows, and persists project and group order', async () => {
    const calls = stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { version: 1, projects: ['b', 'a'], sessions: { a: ['second', 'first'] } } }
      : { status: 204 })
    const projects = useProjectsStore()
    projects.remember(project('a', '/a'))
    projects.remember(project('b', '/b'))
    projects.remember(project('c', '/c'))
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('first', '/a'), session('second', '/a'), session('new', '/a')] } })
    const tree = useTreeStore()
    await tree.load()
    vi.useFakeTimers()
    tree.sync()
    expect(tree.groups.groups.map((group) => group.project.id)).toEqual(['b', 'a', 'c'])
    expect(tree.groups.groups.find((group) => group.project.id === 'a')?.sessions.map((s) => s.name)).toEqual(['second', 'first', 'new'])
    tree.reorderProjects(['c', 'b', 'a'])
    tree.reorderSessions('a', ['new', 'first', 'second'])
    await vi.advanceTimersByTimeAsync(500)
    expect(calls).toContainEqual(expect.objectContaining({ method: 'PUT', path: '/api/ui-state/tree' }))
  })

  it('reset clears loaded order on sign-out', async () => {
    stubFetch((method, path) => path === '/api/ui-state/tree' ? { status: 200, body: { version: 1, projects: ['secret'], sessions: {} } } : { status: 204 })
    const tree = useTreeStore()
    await tree.load()
    expect(tree.order.projects).toEqual(['secret'])
    tree.reset()
    expect(tree.loaded).toBe(false)
    expect(tree.order.projects).toEqual([])
  })
})
