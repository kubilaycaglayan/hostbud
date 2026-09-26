import { describe, expect, it } from 'vitest'
import {
  activate,
  allPanes,
  closeTab,
  emptyLayout,
  focusedPane,
  MAX_PANES,
  missingPanes,
  openSession,
  removePanes,
  renameSession,
  validateLayout,
  type Layout,
} from './layout'

/** Opens sessions one after another on host. */
function withTabs(...names: string[]): Layout {
  let l = emptyLayout()
  for (const n of names) l = openSession(l, 'host', n).layout
  return l
}
const sessions = (l: Layout) => l.tabs.map((t) => t.root.session)
const active = (l: Layout) => focusedPane(l)?.session

describe('open', () => {
  it('a new session gets a new tab at the end, which becomes active', () => {
    const { layout, result } = openSession(withTabs('a'), 'host', 'b')
    expect(result).toBe('opened')
    expect(sessions(layout)).toEqual(['a', 'b'])
    expect(active(layout)).toBe('b')
    expect(layout.tabs[1].focusedPane).toBe(layout.tabs[1].root.id)
  })

  it('an open session focuses its tab instead of opening another', () => {
    const l = withTabs('a', 'b', 'c')
    const { layout, result } = openSession(l, 'host', 'a')
    expect(result).toBe('focused')
    expect(sessions(layout)).toEqual(['a', 'b', 'c'])
    expect(active(layout)).toBe('a')
  })

  it('the same name on another machine is another session', () => {
    const { result } = openSession(withTabs('a'), 'other', 'a')
    expect(result).toBe('opened')
  })

  it(`refuses a terminal beyond ${MAX_PANES}`, () => {
    const names = Array.from({ length: MAX_PANES }, (_, i) => `s${i}`)
    const l = withTabs(...names)
    expect(allPanes(l)).toHaveLength(MAX_PANES)
    const { layout, result } = openSession(l, 'host', 'one-more')
    expect(result).toBe('full')
    expect(layout).toBe(l)
    // Re-picking an open one still works at the limit.
    expect(openSession(l, 'host', 's3').result).toBe('focused')
  })
})

describe('activate and close', () => {
  it('activates a known tab only', () => {
    const l = withTabs('a', 'b')
    expect(active(activate(l, l.tabs[0].id))).toBe('a')
    expect(activate(l, 'nope')).toBe(l)
  })

  it('closing the active tab moves to its right neighbor, or the left one at the end', () => {
    let l = withTabs('a', 'b', 'c')
    l = activate(l, l.tabs[1].id)
    l = closeTab(l, l.tabs[1].id)
    expect(sessions(l)).toEqual(['a', 'c'])
    expect(active(l)).toBe('c')
    l = closeTab(l, l.tabs[1].id)
    expect(active(l)).toBe('a')
    l = closeTab(l, l.tabs[0].id)
    expect(l).toEqual(emptyLayout())
  })

  it('closing an inactive tab keeps the active one', () => {
    const l = withTabs('a', 'b', 'c')
    const next = closeTab(l, l.tabs[0].id)
    expect(sessions(next)).toEqual(['b', 'c'])
    expect(active(next)).toBe('c')
  })
})

describe('rename and missing sessions', () => {
  it('rename relabels every pane showing the session', () => {
    const l = renameSession(withTabs('a', 'b'), 'host', 'a', 'z')
    expect(sessions(l)).toEqual(['z', 'b'])
    expect(renameSession(l, 'other', 'b', 'y')).toEqual(l) // other machine: untouched
  })

  it('finds panes whose session is gone, and removes them', () => {
    const l = withTabs('a', 'b', 'c')
    const gone = missingPanes(l, 'host', new Set(['b']))
    expect(gone.map((p) => p.session)).toEqual(['a', 'c'])
    expect(missingPanes(l, 'other', new Set())).toEqual([])
    const next = removePanes(l, (p) => p.session !== 'b')
    expect(sessions(next)).toEqual(['b'])
    expect(active(next)).toBe('b')
  })
})

describe('validate', () => {
  it('round-trips a saved layout', () => {
    const l = activate(withTabs('a', 'b'), withTabs('a', 'b').tabs[0].id)
    const back = validateLayout(JSON.parse(JSON.stringify(l)))
    expect(back).toEqual(l)
  })

  it.each([
    ['null', null],
    ['an array', []],
    ['another version', { version: 2, tabs: [], activeTab: null }],
    ['tabs not a list', { version: 1, tabs: {}, activeTab: null }],
    ['a tab without id', { version: 1, tabs: [{ root: { type: 'pane', id: 'p', machine: 'host', session: 'a' } }] }],
    ['an unknown node', { version: 1, tabs: [{ id: 't', root: { type: 'grid' }, focusedPane: 'p' }] }],
    ['a pane without session', { version: 1, tabs: [{ id: 't', root: { type: 'pane', id: 'p', machine: 'host' } }] }],
  ])('rejects %s', (_, v) => {
    expect(validateLayout(v)).toBeNull()
  })

  it('repairs a missing focus or active tab, and trims beyond the limit', () => {
    const tabs = Array.from({ length: MAX_PANES + 2 }, (_, i) => ({
      id: `t${i}`,
      root: { type: 'pane', id: `p${i}`, machine: 'host', session: `s${i}` },
      focusedPane: i === 0 ? 'nope' : `p${i}`,
    }))
    const l = validateLayout({ version: 1, tabs, activeTab: 'gone' })!
    expect(l.tabs).toHaveLength(MAX_PANES)
    expect(l.tabs[0].focusedPane).toBe('p0')
    expect(l.activeTab).toBe('t0')
    expect(validateLayout({ version: 1, tabs: [], activeTab: 'x' })).toEqual(emptyLayout())
  })
})
