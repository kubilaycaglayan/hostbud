import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useProjectsStore } from './projects'
import { useSessionsStore } from './sessions'
import { useTreeStore } from './tree'
import { useMachinesStore } from './machines'
import { stubFetch } from '@/test-utils'
import type { Project, Session } from '@/api/types'
import { emptyTreeState } from '@/lib/tree'

const project = (id: string, path: string): Project => ({ id, machineId: 'host', path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 2, created: '', activity: '' })

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  useTreeStore().reset()
  vi.useRealTimers()
})

describe('tree order store', () => {
  it('stores project and session visibility per account and keeps hidden rows in saved order', () => {
    const tree = useTreeStore()
    useProjectsStore().remember(project('c', '/c'))
    useProjectsStore().remember(project('b', '/b'))
    useProjectsStore().remember(project('a', '/a'))
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/a'), session('two', '/a'), session('three', '/a')] } })
    tree.order.projects = ['a', 'b', 'c']
    tree.order.sessions.a = ['one', 'two', 'three']
    tree.hideProject('b')
    tree.hideSession('host', 'two')
    expect(tree.hiddenCount).toBe(2)
    tree.reorderProjects(['c', 'a'])
    tree.reorderSessions('a', ['three', 'one'])
    expect(tree.order.projects).toEqual(['c', 'b', 'a'])
    expect(tree.order.sessions.a).toEqual(['three', 'two', 'one'])
    tree.setShowHidden(true)
    expect(tree.order.showHidden).toBe(true)
    tree.unhideProject('b')
    tree.unhideSession('host', 'two')
    expect(tree.hiddenCount).toBe(0)
    expect(tree.order.projects).toEqual(['c', 'b', 'a'])
    expect(tree.order.sessions.a).toEqual(['three', 'two', 'one'])
  })

  it('pins and unpins projects at the end of their target section', () => {
    const tree = useTreeStore()
    useProjectsStore().remember(project('a', '/a'))
    useProjectsStore().remember(project('b', '/b'))
    useProjectsStore().remember(project('c', '/c'))
    tree.order.projects = ['a', 'b', 'c']
    tree.pinProject('b')
    expect(tree.order.pinned).toEqual(['b'])
    expect(tree.order.projects).toEqual(['b', 'a', 'c'])
    tree.pinProject('a')
    expect(tree.order.pinned).toEqual(['b', 'a'])
    expect(tree.order.projects).toEqual(['b', 'a', 'c'])
    tree.unpinProject('b')
    expect(tree.order.pinned).toEqual(['a'])
    expect(tree.order.projects).toEqual(['a', 'c', 'b'])
    tree.reorderProjectSection(['b', 'c'], false)
    expect(tree.order.projects).toEqual(['a', 'b', 'c'])
  })

  it('reorders project sections without changing their projects', () => {
    const tree = useTreeStore()
    const first = tree.createProjectSection('First', 'red')
    const second = tree.createProjectSection('Second', 'blue')
    tree.assignProjectSection('a', first.id)
    tree.reorderProjectSections([second.id, first.id])
    expect(tree.order.sections.map((section) => section.id)).toEqual([second.id, first.id])
    expect(tree.order.projectSections.a).toBe(first.id)
  })

  it('re-keys session ordering, hidden state, expansion and window keys in one update', () => {
    const tree = useTreeStore()
    tree.order.sessions = { group: ['before', 'old', 'after'], __other__: ['old'] }
    tree.order.hidden.sessions = ['host/old']
    tree.order.expanded = ['host/old', 'host/old/@2']
    const previous = tree.order
    tree.renameSession('host', 'old', 'new')
    expect(tree.order).not.toBe(previous)
    expect(tree.order.sessions).toEqual({ group: ['before', 'new', 'after'], __other__: ['new'] })
    expect(tree.order.hidden.sessions).toEqual(['host/new'])
    expect(tree.order.expanded).toEqual(['host/new', 'host/new/@2'])
  })

  it('restores a renamed session position if an inventory sync already appended the new name', () => {
    const tree = useTreeStore()
    // A poll can observe the remote rename before PATCH resolves, causing sync
    // to prune the old key and append the newly observed name.
    tree.order.sessions = { group: ['first', 'last', 'renamed'] }
    tree.renameSession('host', 'old', 'renamed', {
      group: 'group', index: 1,
      hiddenKeys: ['host/old'],
      expandedKeys: ['host/old', 'host/old/@2'],
    })
    expect(tree.order.sessions.group).toEqual(['first', 'renamed', 'last'])
    expect(tree.order.hidden.sessions).toEqual(['host/renamed'])
    expect(tree.order.expanded).toEqual(['host/renamed', 'host/renamed/@2'])
  })

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
    await vi.advanceTimersByTimeAsync(499)
    expect(calls.filter((call) => call.method === 'PUT' && call.path === '/api/ui-state/tree')).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(1)
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

  it('prunes stale project state only after the project inventory succeeds', async () => {
    const calls = stubFetch((method, path) => {
      if (path === '/api/ui-state/tree' && method === 'GET') return { status: 200, body: {
        version: 2, projects: ['removed', 'kept'], sessions: { removed: ['old'], kept: ['live'] },
        pinned: ['removed'], hidden: { projects: ['removed'], sessions: ['host/old'] },
        collapsed: ['removed'], expanded: ['host/old'], showHidden: true,
      } }
      if (path === '/api/projects?machine=host') return { status: 200, body: { projects: [project('kept', '/kept')] } }
      return { status: 204 }
    })
    const tree = useTreeStore()
    await tree.load()
    tree.sync()
    expect(tree.order.projects).toContain('removed')
    await useProjectsStore().load('host')
    tree.sync()
    expect(tree.order.projects).toEqual(['kept'])
    expect(tree.order.pinned).toEqual([])
    expect(tree.order.hidden.projects).toEqual([])
    expect(tree.order.collapsed).toEqual([])
    expect(tree.order.sessions).not.toHaveProperty('removed')
    expect(calls).toContainEqual(expect.objectContaining({ method: 'GET', path: '/api/projects?machine=host' }))
  })

  it('prunes ended host session keys only after a reachable snapshot', async () => {
    stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { ...emptyTreeState(), hidden: { projects: [], sessions: ['host/old'] }, expanded: ['host/old'] } }
      : { status: 204 })
    const tree = useTreeStore()
    await tree.load()
    const machines = useMachinesStore()
    const sessions = useSessionsStore()
    sessions.apply({ type: 'snapshot', machines: [], sessions: { host: [] } })
    machines.apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'unreachable', os: '', home: '', tmuxVersion: '', tmuxMissing: false }], sessions: { host: [] } })
    tree.sync()
    expect(tree.order.hidden.sessions).toEqual(['host/old'])
    expect(tree.order.expanded).toEqual(['host/old'])
    machines.apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: '', home: '', tmuxVersion: '', tmuxMissing: false }], sessions: { host: [] } })
    tree.sync()
    expect(tree.order.hidden.sessions).toEqual([])
    expect(tree.order.expanded).toEqual([])
  })

  it('keeps expanded and hidden keys when the host turns ok before its first session list (app restart)', async () => {
    stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { ...emptyTreeState(), hidden: { projects: [], sessions: ['host/kept-hidden'] }, expanded: ['host/kept'] } }
      : { status: 204 })
    const tree = useTreeStore()
    await tree.load()
    const machines = useMachinesStore()
    const sessions = useSessionsStore()
    const host = { id: 'host', label: 'Host', os: '', home: '', tmuxVersion: '', tmuxMissing: false }
    // After a restart the server snapshots the host before its first poll: status
    // unknown with an empty list. The poll then publishes status ok before the list.
    const snapshot = { type: 'snapshot' as const, machines: [{ ...host, status: 'unknown' as const }], sessions: { host: [] } }
    machines.apply(snapshot)
    sessions.apply(snapshot)
    tree.sync()
    const ok = { type: 'machine.status' as const, machine: 'host', payload: { ...host, status: 'ok' as const } }
    machines.apply(ok)
    sessions.apply(ok)
    tree.sync()
    expect(tree.order.expanded).toEqual(['host/kept'])
    expect(tree.order.hidden.sessions).toEqual(['host/kept-hidden'])
    const listed = { type: 'sessions.changed' as const, machine: 'host', payload: { sessions: [session('kept', '/k'), session('kept-hidden', '/k')] } }
    machines.apply(listed)
    sessions.apply(listed)
    tree.sync()
    expect(tree.order.expanded).toEqual(['host/kept'])
    expect(tree.order.hidden.sessions).toEqual(['host/kept-hidden'])
  })

  it('flushes a debounced change immediately and skips an oversized value', async () => {
    const calls = stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: emptyTreeState() }
      : { status: 204 })
    vi.useFakeTimers()
    const tree = useTreeStore()
    await tree.load()
    tree.setCollapsed('__other__', true)
    tree.flush()
    await flushPromises()
    expect(calls.filter((call) => call.method === 'PUT' && call.path === '/api/ui-state/tree')).toHaveLength(1)
    const putCalls = vi.mocked(fetch).mock.calls.filter(([path, init]) => path === '/api/ui-state/tree' && init?.method === 'PUT')
    expect(putCalls.at(-1)?.[1]).toEqual(expect.objectContaining({ keepalive: true }))
    tree.order.projects = ['x'.repeat(61 * 1024)]
    tree.flush()
    await flushPromises()
    expect(calls.filter((call) => call.method === 'PUT' && call.path === '/api/ui-state/tree')).toHaveLength(1)
  })

  it('prunes stale data before enforcing the serialized size limit', async () => {
    const calls = stubFetch((method, path) => {
      if (path === '/api/ui-state/tree' && method === 'GET') return { status: 200, body: emptyTreeState() }
      if (path === '/api/projects?machine=host') return { status: 200, body: { projects: [project('p0', '/kept')] } }
      return { status: 204 }
    })
    const tree = useTreeStore()
    await tree.load()
    await useProjectsStore().load('host')
    useMachinesStore().apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: '', home: '/home/dev', tmuxVersion: '', tmuxMissing: false }], sessions: { host: [] } })
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('actual', '/kept')] } })
    tree.order.projects = Array.from({ length: 500 }, (_, i) => `p${i}`)
    tree.order.sessions = Object.fromEntries(Array.from({ length: 500 }, (_, projectIndex) => [
      `p${projectIndex}`,
      Array.from({ length: 20 }, (_, sessionIndex) => (`s${sessionIndex}`).padEnd(64, 'x')),
    ]))
    tree.flush()
    await flushPromises()
    const saved = [...calls].reverse().find((call) => call.method === 'PUT' && call.path === '/api/ui-state/tree')?.body as { projects: string[]; sessions: Record<string, string[]> }
    expect(saved.projects).toEqual(['p0'])
    expect(saved.sessions).toEqual({ p0: ['actual'], __other__: [] })
    expect(new TextEncoder().encode(JSON.stringify(saved)).byteLength).toBeLessThan(60 * 1024)
  })

  it('never saves rows a live event appends or prunes, only user edits', async () => {
    const calls = stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { ...emptyTreeState(), sessions: { __other__: ['b', 'a'] } } }
      : { status: 204 })
    useMachinesStore().apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: '', home: '/home/dev', tmuxVersion: '', tmuxMissing: false }], sessions: { host: [] } })
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('a', '/x'), session('b', '/x')] } })
    const tree = useTreeStore()
    await tree.load()
    vi.useFakeTimers()
    tree.sync()
    useSessionsStore().apply({ type: 'sessions.changed', machine: 'host', payload: { sessions: [{ ...session('a', '/x'), attached: 1 }, session('b', '/x'), session('c', '/x')] } })
    tree.sync()
    expect(tree.groups.other.map((s) => s.name)).toEqual(['b', 'a', 'c'])
    useSessionsStore().apply({ type: 'sessions.changed', machine: 'host', payload: { sessions: [session('b', '/x'), session('c', '/x')] } })
    tree.sync()
    await vi.advanceTimersByTimeAsync(1000)
    expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(0)
    tree.reorderSessions('__other__', ['c', 'b'])
    await vi.advanceTimersByTimeAsync(1000)
    expect(calls.filter((call) => call.method === 'PUT').at(-1)?.body).toMatchObject({ sessions: { __other__: ['c', 'b'] } })
  })

  it('refresh adopts the order another device saved unless a local change is pending', async () => {
    let remote: string[] = ['a', 'b']
    const calls = stubFetch((method, path) => path === '/api/ui-state/tree' && method === 'GET'
      ? { status: 200, body: { ...emptyTreeState(), sessions: { __other__: remote } } }
      : { status: 204 })
    useMachinesStore().apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: '', home: '/home/dev', tmuxVersion: '', tmuxMissing: false }], sessions: { host: [] } })
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('a', '/x'), session('b', '/x')] } })
    const tree = useTreeStore()
    await tree.load()
    tree.sync()
    vi.useFakeTimers()
    remote = ['b', 'a']
    await tree.refresh()
    expect(tree.groups.other.map((s) => s.name)).toEqual(['b', 'a'])
    await vi.advanceTimersByTimeAsync(1000)
    expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(0)

    tree.reorderSessions('__other__', ['a', 'b'])
    remote = ['b', 'a']
    await tree.refresh()
    expect(tree.groups.other.map((s) => s.name)).toEqual(['a', 'b'])
  })
})
