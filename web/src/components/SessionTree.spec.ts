import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import type { Project, Session } from '@/api/types'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useTreeStore } from '@/stores/tree'
import SessionTree from './SessionTree.vue'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)
const project = (id: string, path: string): Project => ({ id, machineId: 'host', path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 3, created: '', activity: '' })

beforeEach(async () => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
  fetchMock.mockImplementation(async (url: string) => {
    const body = String(url) === '/api/projects'
      ? { id: 'saved', machineId: 'host', path: '/outside', name: 'outside' }
      : null
    return { ok: true, status: 200, headers: new Headers(), text: async () => body ? JSON.stringify(body) : '' }
  })
  useProjectsStore().remember(project('a', '/work/a'))
  useProjectsStore().remember(project('b', '/work/b'))
  useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a'), session('two', '/work/a'), session('loose', '/outside')] } })
  const tree = useTreeStore()
  await tree.load()
  tree.sync()
})

afterEach(() => useTreeStore().reset())

describe('SessionTree', () => {
  it('moves groups and session rows in the explicit order', async () => {
    const wrapper = mount(SessionTree)
    await wrapper.get('button[aria-label="Move project b up"]').trigger('click')
    expect(useTreeStore().groups.groups.map((g) => g.project.id)).toEqual(['b', 'a'])
    await wrapper.get('button[aria-label="Move session two up"]').trigger('click')
    expect(useTreeStore().groups.groups.find((g) => g.project.id === 'a')?.sessions.map((s) => s.name)).toEqual(['two', 'one'])
  })

  it('saves an unmatched session as project metadata without changing its session', async () => {
    const before = useSessionsStore().list('host').find((s) => s.name === 'loose')
    const wrapper = mount(SessionTree)
    await wrapper.get('button[aria-label="Save loose as project"]').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/projects', expect.objectContaining({ method: 'POST', body: JSON.stringify({ machineId: 'host', path: '/outside', name: '' }) }))
    expect(useTreeStore().groups.groups.some((group) => group.project.id === 'saved' && group.sessions.some((s) => s.name === 'loose'))).toBe(true)
    expect(useSessionsStore().list('host').find((s) => s.name === 'loose')).toEqual(before)
  })
})
