import { describe, expect, it } from 'vitest'
import type { Project, Session } from '@/api/types'
import { emptyTreeOrder, move, projectForSession, projectTree, validateTreeOrder } from './tree'

const project = (id: string, path: string, name = id): Project => ({ id, machineId: 'host', path, name, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 1, created: '', activity: '' })

describe('project session tree', () => {
  it('matches nested paths by longest component prefix and keeps siblings in Other', () => {
    const projects = [project('root', '/work'), project('nested', '/work/app'), project('sibling', '/work/application')]
    expect(projectForSession(session('exact', '/work/app'), projects)?.id).toBe('nested')
    expect(projectForSession(session('child', '/work/app/src'), projects)?.id).toBe('nested')
    expect(projectForSession(session('sibling', '/work/application'), projects)?.id).toBe('sibling')
    expect(projectForSession(session('unmatched', '/workbench'), projects)).toBeUndefined()
    const projection = projectTree(projects, [session('nested', '/work/app/src')], emptyTreeOrder())
    expect(projection.groups.find((g) => g.project.id === 'root')?.sessions).toEqual([])
    expect(projection.groups.find((g) => g.project.id === 'nested')?.sessions.map((s) => s.name)).toEqual(['nested'])
  })

  it('preserves explicit project/session order and appends newly observed rows', () => {
    const order = { version: 1 as const, projects: ['b', 'a'], sessions: { a: ['s2', 's1'] } }
    const projection = projectTree([project('a', '/a'), project('b', '/b'), project('c', '/c')], [session('s1', '/a'), session('s2', '/a'), session('s3', '/a')], order)
    expect(projection.groups.map((g) => g.project.id)).toEqual(['b', 'a', 'c'])
    expect(projection.groups[1].sessions.map((s) => s.name)).toEqual(['s2', 's1', 's3'])
  })

  it('keeps observed project order when there is no saved order', () => {
    const projection = projectTree([project('third', '/c'), project('first', '/a'), project('second', '/b')], [], emptyTreeOrder())
    expect(projection.groups.map((group) => group.project.id)).toEqual(['third', 'first', 'second'])
  })

  it('keeps nested projects with equal display names as distinct groups', () => {
    const projects = [project('parent', '/work', 'Work'), project('nested', '/work/app', 'Work')]
    const projection = projectTree(projects, [], emptyTreeOrder())
    expect(projection.groups.map((group) => [group.project.id, group.project.path, group.project.name])).toEqual([
      ['parent', '/work', 'Work'],
      ['nested', '/work/app', 'Work'],
    ])
  })

  it('uses an explicit linked project before path-prefix matches', () => {
    const linked = session('linked', '/work/app')
    linked.projectId = 'root'
    expect(projectForSession(linked, [project('root', '/work'), project('nested', '/work/app')])?.id).toBe('root')
  })

  it('repairs malformed order data and moves rows without sorting them', () => {
    expect(validateTreeOrder({ ...emptyTreeOrder(), version: 2 })).toBeNull()
    expect(validateTreeOrder({ version: 1, projects: ['a', 'a'], sessions: { other: ['x', 'x'] } })).toEqual({ version: 1, projects: ['a'], sessions: { other: ['x'] } })
    expect(move(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b'])
    expect(move(['a', 'b'], 8, 0)).toEqual(['a', 'b'])
  })
})
