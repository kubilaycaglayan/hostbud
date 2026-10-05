import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import type { Machine, Project, Session } from '@/api/types'
import { useMachinesStore } from '@/stores/machines'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useTreeStore } from '@/stores/tree'
import { stubFetch } from '@/test-utils'
import SessionTree from './SessionTree.vue'

// V2-M13 T4: other servers' projects and sessions in the tree.
const machines: Machine[] = [
  { id: 'host', label: 'Host', source: 'host', status: 'ok', os: '', home: '/home/dev', tmuxVersion: '', tmuxMissing: false },
  { id: 's-abc', label: 'Build box', source: 'custom', address: 'dev@server-a:22', status: 'ok', os: '', home: '/home/build', tmuxVersion: '', tmuxMissing: false },
]
const project = (id: string, path: string, machineId = 'host'): Project => ({ id, machineId, path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 1, created: '', activity: '' })

let calls: ReturnType<typeof stubFetch>
beforeEach(async () => {
  setActivePinia(createPinia())
  calls = stubFetch((method) => method === 'PATCH' ? { status: 200, body: { name: 'renamed' } } : { status: 404, body: { error: 'nothing saved yet' } })
  const projects = useProjectsStore()
  projects.loaded = true
  projects.remember(project('local', '/home/dev/app'))
  projects.remember(project('remote', '/home/build/app', 's-abc'))
  const e = { type: 'snapshot' as const, machines, sessions: { host: [session('work', '/home/dev/app')], 's-abc': [session('work', '/home/build/app/src'), session('loose', '/tmp')] } }
  useMachinesStore().apply(e)
  useSessionsStore().apply(e)
  const tree = useTreeStore()
  await tree.load()
  tree.sync()
})
afterEach(() => {
  useTreeStore().reset()
  vi.unstubAllGlobals()
})

describe('SessionTree with servers', () => {
  it('marks other servers\' projects and loose sessions with the nickname chip', () => {
    const w = mount(SessionTree)
    const remote = w.get('[data-tree-key="project:remote"]')
    expect(remote.attributes('aria-label')).toBe('remote, on Build box')
    expect(remote.get('[data-machine-chip]').text()).toBe('Build box')
    expect(remote.get('[data-machine-chip]').classes()).toEqual(expect.arrayContaining(['bg-danger', 'font-black', 'text-bg']))
    expect(remote.text()).toContain('~/app') // the server's own home
    expect(w.get('[data-tree-key="project:local"]').find('[data-machine-chip]').exists()).toBe(false)

    // Same session name on two machines: two rows, each under its machine's project.
    expect(w.get('[data-tree-key="project:local"]').find('[data-tree-key="session:work"]').exists()).toBe(true)
    expect(remote.find('[data-tree-key="session:s-abc/work"]').exists()).toBe(true)
    // In a server's project the project chip names the server; loose
    // sessions in Other sessions carry their own chip.
    expect(remote.find('[data-tree-key="session:s-abc/work"] [data-machine-chip]').exists()).toBe(false)
    const loose = w.get('[data-tree-key="session:s-abc/loose"]')
    expect(loose.get('[data-machine-chip]').text()).toBe('Build box')
    expect(loose.get('[data-machine-chip]').classes()).toEqual(expect.arrayContaining(['bg-danger', 'font-black', 'text-bg']))
    expect(loose.attributes('aria-label')).toContain('on Build box')
    w.unmount()
  })

  it('emits session refs and renames on the session\'s server', async () => {
    const w = mount(SessionTree, { props: { selected: 's-abc/work' } })
    expect(w.get('[data-tree-key="session:s-abc/work"]').attributes('aria-selected')).toBe('true')
    expect(w.get('[data-tree-key="session:work"]').attributes('aria-selected')).toBeUndefined()
    await w.get('[data-tree-key="session:s-abc/loose"] [data-session-row]').trigger('click')
    expect(w.emitted('select')?.at(-1)).toEqual(['s-abc/loose'])
    await w.get('[data-tree-key="session:s-abc/loose"]').trigger('keydown', { key: 'Delete' })
    expect(w.emitted('kill')?.at(-1)).toEqual(['s-abc/loose'])

    await w.get('[data-tree-key="session:s-abc/loose"]').trigger('keydown', { key: 'F2' })
    await flushPromises()
    const input = w.get('[data-tree-key="session:s-abc/loose"] input')
    await input.setValue('renamed')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(calls.find((c) => c.method === 'PATCH')).toMatchObject({ path: '/api/machines/s-abc/sessions/loose', body: { name: 'renamed' } })
    w.unmount()
  })
})
