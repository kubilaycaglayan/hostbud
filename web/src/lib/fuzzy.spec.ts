import { describe, expect, it } from 'vitest'
import { fuzzyFilter } from './fuzzy'

describe('fuzzyFilter', () => {
  it('matches case-insensitive subsequences and ranks word starts above mid-word matches', () => {
    const rows = [{ label: 'xfoobarbaz' }, { label: 'Foo Bar Baz' }]
    expect(fuzzyFilter('fbb', rows).map((row) => row.label)).toEqual(['Foo Bar Baz', 'xfoobarbaz'])
    expect(fuzzyFilter('FOO', [{ label: 'Foo' }])).toHaveLength(1)
  })

  it('prefers contiguous runs and uses a secondary field', () => {
    const rows = [{ label: 'a-b-c' }, { label: 'abc' }, { label: 'session', secondary: 'Garden Project' }]
    expect(fuzzyFilter('abc', rows).map((row) => row.label)).toEqual(['abc', 'a-b-c'])
    expect(fuzzyFilter('gp', rows)[0].label).toBe('session')
  })

  it('keeps stable ties, excludes misses, preserves empty-query order and caps results', () => {
    const rows = [{ label: 'ab' }, { label: 'ab' }, { label: 'other' }]
    expect(fuzzyFilter('ab', rows)).toEqual(rows.slice(0, 2))
    expect(fuzzyFilter('', rows, 2)).toEqual(rows.slice(0, 2))
    expect(fuzzyFilter('', Array.from({ length: 60 }, (_, i) => ({ label: String(i) })))).toHaveLength(50)
  })

  it('uses Unicode case folding', () => {
    expect(fuzzyFilter('straße', [{ label: 'Straße' }])).toHaveLength(1)
    expect(fuzzyFilter('ß', [{ label: 'ẞ' }])).toHaveLength(1)
  })
})
