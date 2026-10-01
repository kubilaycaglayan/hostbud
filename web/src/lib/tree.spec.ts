import { describe, expect, it } from 'vitest'
import type { Project, Session } from '@/api/types'
import { canReorderProjectSections, emptyTreeState, move, orderedProjectSections, projectForSession, projectTree, SECTION_COLORS, validateTreeState, visibleOpenSessionNames } from './tree'

const project = (id: string, path: string, name = id): Project => ({ id, machineId: 'host', path, name, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 1, created: '', activity: '' })

describe('project session tree', () => {
  it('cycles only open sessions visible in the actual left-tree order', () => {
    const groups = projectTree(
      [project('a', '/a'), project('b', '/b'), project('c', '/c')],
      [session('a1', '/a'), session('a2', '/a'), session('b1', '/b'), session('c1', '/c'), session('other', '/else')],
      { ...emptyTreeState(), pinned: ['b'], sections: [{ id: 's', name: 'Section', color: 'blue' }], projectSections: { c: 's' } },
    )
    const open = new Set(['a1', 'a2', 'b1', 'c1', 'other', 'unopened'].map((name) => `host/${name}`))
    expect(visibleOpenSessionNames(groups.groups, groups.other, { ...emptyTreeState(), pinned: ['b'], sections: [{ id: 's', name: 'Section', color: 'blue' }], projectSections: { c: 's' } }, open)).toEqual(['b1', 'c1', 'a1', 'a2', 'other'])
    const collapsed = { ...emptyTreeState(), collapsed: ['a'], collapsedSections: ['s'], pinned: ['b'], sections: [{ id: 's', name: 'Section', color: 'blue' as const }], projectSections: { c: 's' } }
    expect(visibleOpenSessionNames(groups.groups, groups.other, collapsed, open)).toEqual(['b1', 'other'])
    const hidden = { ...emptyTreeState(), hidden: { projects: [], sessions: ['host/b1'] }, pinned: ['b'], sections: [{ id: 's', name: 'Section', color: 'blue' as const }], projectSections: { c: 's' } }
    expect(visibleOpenSessionNames(groups.groups, groups.other, hidden, open)).toEqual(['c1', 'a1', 'a2', 'other'])
    expect(visibleOpenSessionNames(groups.groups, groups.other, { ...hidden, showHidden: true }, open)).toContain('b1')
  })

  it('matches nested paths by longest component prefix and keeps siblings in Other', () => {
    const projects = [project('root', '/work'), project('nested', '/work/app'), project('sibling', '/work/application')]
    expect(projectForSession(session('exact', '/work/app'), projects)?.id).toBe('nested')
    expect(projectForSession(session('child', '/work/app/src'), projects)?.id).toBe('nested')
    expect(projectForSession(session('sibling', '/work/application'), projects)?.id).toBe('sibling')
    expect(projectForSession(session('unmatched', '/workbench'), projects)).toBeUndefined()
    const projection = projectTree(projects, [session('nested', '/work/app/src')], emptyTreeState())
    expect(projection.groups.find((g) => g.project.id === 'root')?.sessions).toEqual([])
    expect(projection.groups.find((g) => g.project.id === 'nested')?.sessions.map((s) => s.name)).toEqual(['nested'])
  })

  it('preserves explicit project/session order and appends newly observed rows', () => {
    const order = { ...emptyTreeState(), projects: ['b', 'a'], sessions: { a: ['s2', 's1'] } }
    const projection = projectTree([project('a', '/a'), project('b', '/b'), project('c', '/c')], [session('s1', '/a'), session('s2', '/a'), session('s3', '/a')], order)
    expect(projection.groups.map((g) => g.project.id)).toEqual(['b', 'a', 'c'])
    expect(projection.groups[1].sessions.map((s) => s.name)).toEqual(['s2', 's1', 's3'])
  })

  it('projects pinned and unpinned sections in manual order', () => {
    const order = { ...emptyTreeState(), projects: ['b', 'a', 'c'], pinned: ['a', 'b'] }
    const projection = projectTree([project('a', '/a'), project('b', '/b'), project('c', '/c')], [], order)
    expect(projection.pinned.map((group) => group.project.id)).toEqual(['b', 'a'])
    expect(projection.unpinned.map((group) => group.project.id)).toEqual(['c'])
  })

  it('keeps observed project order when there is no saved order', () => {
    const projection = projectTree([project('third', '/c'), project('first', '/a'), project('second', '/b')], [], emptyTreeState())
    expect(projection.groups.map((group) => group.project.id)).toEqual(['third', 'first', 'second'])
  })

  it('only permits project sorting within the same pin section', () => {
    expect(canReorderProjectSections('pinned', 'pinned')).toBe(true)
    expect(canReorderProjectSections('unpinned', 'unpinned')).toBe(true)
    expect(canReorderProjectSections('pinned', 'unpinned')).toBe(false)
  })

  it('orders project sections by saved ids while retaining unlisted sections', () => {
    const sections = [
      { id: 'first', name: 'First', color: 'blue' as const },
      { id: 'second', name: 'Second', color: 'green' as const },
      { id: 'third', name: 'Third', color: 'purple' as const },
    ]
    expect(orderedProjectSections(sections, ['third', 'first']).map((section) => section.id)).toEqual(['third', 'first', 'second'])
  })

  it('keeps nested projects with equal display names as distinct groups', () => {
    const projects = [project('parent', '/work', 'Work'), project('nested', '/work/app', 'Work')]
    const projection = projectTree(projects, [], emptyTreeState())
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

  it('upgrades the v1 order without changing its order', () => {
    expect(validateTreeState({ version: 1, projects: ['b', 'a'], sessions: { a: ['second', 'first'] } })).toEqual({
      ...emptyTreeState(), projects: ['b', 'a'], sessions: { a: ['second', 'first'] },
    })
  })

  it('upgrades the complete v2 tree while retaining all saved presentation state', () => {
    const value = {
      version: 2, projects: ['b', 'a'], sessions: { a: ['second', 'first'] }, pinned: ['b'],
      hidden: { projects: ['a'], sessions: ['host/hidden'] }, collapsed: ['b', '__other__'], expanded: ['host/first/@2'], showHidden: true,
    }
    expect(validateTreeState(value)).toEqual({ ...emptyTreeState(), ...value, version: 4 })
  })

  it('upgrades v3 sections without losing order or assignments', () => {
    const value = {
      version: 3, projects: ['a'], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: ['a'], expanded: [], showHidden: false,
      sections: [{ id: 'research', name: 'Research', color: 'blue' }], projectSections: { a: 'research' },
    }
    expect(validateTreeState(value)).toEqual({ ...emptyTreeState(), ...value, version: 4, collapsedSections: [] })
  })

  it('round-trips valid v2 state and dedupes saved lists', () => {
    const state = {
      ...emptyTreeState(), projects: ['a', 'a'], sessions: { a: ['two', 'two', 'one'] },
      pinned: ['a', 'a'], hidden: { projects: ['a', 'a'], sessions: ['host/hidden', 'host/hidden'] },
      collapsed: ['a', '__other__', 'a'], expanded: ['host/one', 'host/one', 'host/one/@7'], showHidden: true,
    }
    expect(validateTreeState(state)).toEqual({
      ...state, projects: ['a'], sessions: { a: ['two', 'one'] }, pinned: ['a'],
      hidden: { projects: ['a'], sessions: ['host/hidden'] }, collapsed: ['a', '__other__'],
      expanded: ['host/one', 'host/one/@7'],
    })
  })

  it('round-trips user project sections and rejects unsafe section values', () => {
    const sections = SECTION_COLORS.map((color, index) => ({ id: `color-${index}`, name: `Section ${index}`, color }))
    const state = { ...emptyTreeState(), sections, collapsedSections: ['color-2'], projectSections: Object.fromEntries(sections.map((section, index) => [`project-${index}`, section.id])) }
    expect(validateTreeState(state)).toEqual(state)
    expect(validateTreeState({ ...state, projectSections: { a: 'missing' } })).toBeNull()
    expect(validateTreeState({ ...state, sections: [{ id: '__proto__', name: 'Bad', color: 'red' }] })).toBeNull()
    expect(validateTreeState({ ...state, sections: [{ id: 'x', name: 'Bad', color: 'pink' }] })).toBeNull()
    expect(validateTreeState({ ...state, sections: [{ id: 'x', name: ' ', color: 'red' }] })).toBeNull()
    expect(validateTreeState({ ...state, collapsedSections: ['missing'] })).toBeNull()
  })

  it.each([
    { version: 3, projects: [], sessions: {} },
    { version: 2, projects: {}, sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false },
    { version: 2, projects: ['x'.repeat(513)], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false },
    { version: 2, projects: [], sessions: JSON.parse('{"__proto__":["safe"]}'), pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false },
    { version: 2, projects: [], sessions: {}, pinned: [], hidden: { projects: [], sessions: ['missing-slash'] }, collapsed: [], expanded: [], showHidden: false },
    { version: 2, projects: [], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: ['host/a/@x'], showHidden: false },
    { version: 2, projects: [], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: 1 },
  ])('rejects malformed state %#', (state) => {
    expect(validateTreeState(state)).toBeNull()
  })

  it('rejects oversize lists and preserves manual order when moving rows', () => {
    expect(validateTreeState({ ...emptyTreeState(), projects: Array.from({ length: 5001 }, (_, i) => 'p' + i) })).toBeNull()
    expect(move(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b'])
    expect(move(['a', 'b'], 8, 0)).toEqual(['a', 'b'])
  })
})
