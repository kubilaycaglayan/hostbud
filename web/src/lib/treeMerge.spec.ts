import { describe, expect, it } from 'vitest'
import { emptyTreeState } from './tree'
import { mergeTree } from './treeMerge'

describe('merge independent tree edits', () => {
  it('preserves remote section order while merging a local name change', () => {
    const base = { ...emptyTreeState(), sections: [{ id: 'a', name: 'A', color: 'red' as const }, { id: 'b', name: 'B', color: 'blue' as const }] }
    const local = structuredClone(base)
    local.sections[0].name = 'Renamed'
    const remote = { ...structuredClone(base), sections: [...base.sections].reverse() }
    expect(mergeTree(base, local, remote).sections).toEqual([base.sections[1], { ...base.sections[0], name: 'Renamed' }])
  })
  it('does not resurrect a remotely deleted section or its assignments', () => {
    const base = { ...emptyTreeState(), sections: [{ id: 'a', name: 'A', color: 'red' as const }], projectSections: { p: 'a' } }
    const local = structuredClone(base)
    local.sections[0].name = 'Stale rename'
    local.collapsedSections = ['a']
    const result = mergeTree(base, local, emptyTreeState())
    expect(result.sections).toEqual([])
    expect(result.projectSections).toEqual({})
    expect(result.collapsedSections).toEqual([])
  })
  it('the pending local reorder wins a same-list conflict while retaining new rows', () => {
    const base = { ...emptyTreeState(), projects: ['a', 'b', 'c'] }
    const local = { ...base, projects: ['b', 'a', 'c'] }
    const remote = { ...base, projects: ['c', 'b', 'a', 'new'] }
    expect(mergeTree(base, local, remote).projects).toEqual(['b', 'a', 'c', 'new'])
  })
})
