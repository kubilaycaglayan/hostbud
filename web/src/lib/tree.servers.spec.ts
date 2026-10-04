import { describe, expect, it } from 'vitest'
import type { Project, Session } from '@/api/types'
import { emptyTreeState, parseSessionRef, projectForSession, projectTree, refOf, refOfKey, sessionRef, validateTreeState, visibleOpenSessionNames } from './tree'

// V2-M13 T4: sessions on servers added in the UI.
const project = (id: string, path: string, machineId = 'host'): Project => ({ id, machineId, path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string, machine?: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 1, created: '', activity: '', ...(machine ? { machine } : {}) })

describe('session refs', () => {
  it('keeps host names bare and qualifies other servers', () => {
    expect(sessionRef('host', 'work')).toBe('work')
    expect(sessionRef('s-abc', 'work')).toBe('s-abc/work')
    expect(parseSessionRef('work')).toEqual({ machine: 'host', name: 'work' })
    expect(parseSessionRef('s-abc/work')).toEqual({ machine: 's-abc', name: 'work' })
    expect(refOf(session('work', '/x'))).toBe('work')
    expect(refOf(session('work', '/x', 's-abc'))).toBe('s-abc/work')
    expect(refOfKey('host/work')).toBe('work')
    expect(refOfKey('s-abc/work')).toBe('s-abc/work')
  })

  it('places sessions only in projects on their own machine', () => {
    const projects = [project('host-app', '/home/dev/app'), project('srv-app', '/home/dev/app', 's-abc')]
    expect(projectForSession(session('a', '/home/dev/app'), projects)?.id).toBe('host-app')
    expect(projectForSession(session('b', '/home/dev/app/src', 's-abc'), projects)?.id).toBe('srv-app')
    expect(projectForSession(session('c', '/home/dev/app', 's-other'), projects)).toBeUndefined()
    // An explicit link to another machine's project doesn't count.
    expect(projectForSession({ ...session('d', '/elsewhere', 's-abc'), projectId: 'host-app' }, projects)).toBeUndefined()

    // The same name on two machines is two rows.
    const tree = projectTree(projects, [session('work', '/home/dev/app'), session('work', '/home/dev/app', 's-abc'), session('work', '/tmp', 's-other')], {
      ...emptyTreeState(), sessions: { 'srv-app': ['s-abc/work'] },
    })
    expect(tree.groups.find((g) => g.project.id === 'host-app')?.sessions.map(refOf)).toEqual(['work'])
    expect(tree.groups.find((g) => g.project.id === 'srv-app')?.sessions.map(refOf)).toEqual(['s-abc/work'])
    expect(tree.other.map(refOf)).toEqual(['s-other/work'])
  })

  it('saves server refs in the tree order and cycles them like host sessions', () => {
    const saved = validateTreeState({ ...emptyTreeState(), sessions: { __other__: ['work', 's-abc/work'] }, hidden: { projects: [], sessions: ['s-abc/hidden'] } })
    expect(saved?.sessions.__other__).toEqual(['work', 's-abc/work'])
    expect(validateTreeState({ ...emptyTreeState(), sessions: { __other__: ['a/b/c'] } })).toBeNull()
    expect(validateTreeState({ ...emptyTreeState(), sessions: { __other__: ['bad name/x'] } })).toBeNull()

    const groups = projectTree([], [session('one', '/x'), session('two', '/x', 's-abc')], emptyTreeState())
    expect(visibleOpenSessionNames(groups.groups, groups.other, emptyTreeState(), new Set(['host/one', 's-abc/two']))).toEqual(['one', 's-abc/two'])
    const hidden = { ...emptyTreeState(), hidden: { projects: [], sessions: ['s-abc/two'] } }
    expect(visibleOpenSessionNames(groups.groups, groups.other, hidden, new Set(['host/one', 's-abc/two']))).toEqual(['one'])
  })
})
