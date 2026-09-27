import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { Project, ServerEvent } from '@/api/types'
import { useProjectsStore } from './projects'
import { useTreeStore } from './tree'
import { useSessionsStore } from './sessions'

const project = (id: string, path: string): Project => ({ id, machineId: 'host', path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })

beforeEach(() => setActivePinia(createPinia()))

describe('project events', () => {
  it('applies duplicate upserts idempotently and updates tree placement', () => {
    const projects = useProjectsStore()
    const event: ServerEvent = { type: 'projects.changed', machine: 'host', payload: { action: 'upsert', project: project('app', '/work/app') } }
    projects.apply(event)
    projects.apply(event)
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [{ id: '$one', name: 'one', path: '/work/app', attached: 0, windows: 1, created: '', activity: '' }] } })
    const tree = useTreeStore()
    tree.sync()
    expect(projects.items).toHaveLength(1)
    expect(tree.groups.groups).toHaveLength(1)
    expect(tree.groups.groups[0].sessions.map((session) => session.name)).toEqual(['one'])
    tree.reset()
  })

  it('removes a project on its event and immediately places linked sessions under the remaining longest path', () => {
    const projects = useProjectsStore()
    const parent = project('parent', '/work')
    const child = project('child', '/work/app')
    projects.items = [parent, child]
    projects.loaded = true
    useSessionsStore().apply({
      type: 'snapshot', machines: [],
      sessions: { host: [{ id: '$one', name: 'one', path: '/work/app', projectId: 'child', attached: 0, windows: 1, created: '', activity: '' }] },
    })
    const tree = useTreeStore()
    tree.order.projects = ['parent', 'child']
    projects.apply({ type: 'projects.changed', machine: 'host', payload: { action: 'deleted', project: child } })
    tree.sync()
    expect(projects.items.map((item) => item.id)).toEqual(['parent'])
    expect(tree.groups.groups.find((group) => group.project.id === 'parent')?.sessions.map((item) => item.name)).toEqual(['one'])
    expect(tree.order.projects).toEqual(['parent'])
  })
})
