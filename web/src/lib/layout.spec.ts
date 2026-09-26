import { describe, expect, it } from 'vitest'
import {
  activate,
  allPanes,
  closePane,
  closeTab,
  cycleFocus,
  focusPane,
  MAX_TAB_PANES,
  normalizeSizes,
  setSizes,
  splitPane,
  emptyLayout,
  focusedPane,
  panesOf,
  MAX_PANES,
  missingPanes,
  openSession,
  removePanes,
  renameSession,
  validateLayout,
  type Layout,
  type LayoutNode,
  type Split,
} from './layout'

/** Opens sessions one after another on host. */
function withTabs(...names: string[]): Layout {
  let l = emptyLayout()
  for (const n of names) l = openSession(l, 'host', n).layout
  return l
}
const sessions = (l: Layout) => l.tabs.map((t) => panesOf(t.root).map((p) => p.session).join('+'))
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

describe('splits', () => {
  // A tab showing `a`, split as asked; returns the layout and a pane lookup.
  function tabOf(first = 'a') {
    const l = withTabs(first)
    const pane = (l2: Layout, s: string) => allPanes(l2).find((p) => p.session === s)!.id
    return { l, pane }
  }
  const tree = (n: LayoutNode): unknown =>
    n.type === 'pane' ? n.session : { [n.dir]: n.children.map(tree), sizes: n.sizes }
  const root = (l: Layout) => tree(l.tabs[0].root)

  it('split right, then right again: siblings share the space equally', () => {
    let { l, pane } = tabOf()
    const r = splitPane(l, pane(l, 'a'), 'row', 'host', 'b')
    expect(r.result).toBe('split')
    l = r.layout
    expect(root(l)).toEqual({ row: ['a', 'b'], sizes: [50, 50] })
    expect(active(l)).toBe('b')
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'c').layout
    expect(root(l)).toEqual({ row: ['a', 'c', 'b'], sizes: [33.33, 33.33, 33.34] })
  })

  it('the other direction nests a split', () => {
    let { l, pane } = tabOf()
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'b').layout
    l = splitPane(l, pane(l, 'b'), 'column', 'host', 'c').layout
    expect(root(l)).toEqual({ row: ['a', { column: ['b', 'c'], sizes: [50, 50] }], sizes: [50, 50] })
    expect(active(l)).toBe('c')
    expect(sessions(l)).toEqual(['a+b+c'])
  })

  it(`holds at most ${MAX_TAB_PANES} panes per tab`, () => {
    let { l, pane } = tabOf()
    for (const s of ['b', 'c', 'd']) l = splitPane(l, pane(l, 'a'), 'row', 'host', s).layout
    const r = splitPane(l, pane(l, 'a'), 'column', 'host', 'e')
    expect(r.result).toBe('tab-full')
    expect(r.layout).toBe(l)
    expect(splitPane(l, 'nope', 'row', 'host', 'e').result).toBe('missing')
  })

  it(`counts split panes toward the ${MAX_PANES}-terminal limit`, () => {
    let l = withTabs(...Array.from({ length: MAX_PANES - 1 }, (_, i) => `s${i}`))
    const first = allPanes(l)[0].id
    l = splitPane(l, first, 'row', 'host', 'x').layout
    expect(allPanes(l)).toHaveLength(MAX_PANES)
    expect(splitPane(l, first, 'row', 'host', 'y').result).toBe('full')
    expect(openSession(l, 'host', 'y').result).toBe('full')
  })

  it('closing a pane gives its space back; a split left with one child collapses', () => {
    let { l, pane } = tabOf()
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'b').layout
    l = splitPane(l, pane(l, 'b'), 'column', 'host', 'c').layout
    l = setSizes(l, (l.tabs[0].root as Split).id, [70, 30])
    l = closePane(l, pane(l, 'c'))
    // b's column collapsed to b; the focus went to the pane before c.
    expect(root(l)).toEqual({ row: ['a', 'b'], sizes: [70, 30] })
    expect(active(l)).toBe('b')
    l = closePane(l, pane(l, 'b'))
    expect(root(l)).toBe('a')
    l = closePane(l, pane(l, 'a'))
    expect(l.tabs).toEqual([])
  })

  it('removing panes rebalances in proportion, and merges a same-direction split', () => {
    let { l, pane } = tabOf()
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'b').layout
    l = splitPane(l, pane(l, 'b'), 'column', 'host', 'c').layout
    l = splitPane(l, pane(l, 'c'), 'row', 'host', 'd').layout
    // row[a, column[b, row[c, d]]] minus b → row[a, c, d]
    l = removePanes(l, (p) => p.session === 'b')
    expect(root(l)).toEqual({ row: ['a', 'c', 'd'], sizes: [50, 25, 25] })
    const all = (l.tabs[0].root as Split).sizes.reduce((x, y) => x + y)
    expect(all).toBe(100)
  })

  it('focus: set, cycled in reading order', () => {
    let { l, pane } = tabOf()
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'b').layout
    const t = l.tabs[0].id
    l = focusPane(l, t, pane(l, 'a'))
    expect(active(l)).toBe('a')
    expect(focusPane(l, t, 'nope')).toBe(l)
    expect(active(cycleFocus(l, t))).toBe('b')
    expect(active(cycleFocus(cycleFocus(l, t), t))).toBe('a')
  })

  it('opening a session shown in a split focuses that pane; rename reaches split panes', () => {
    let { l, pane } = tabOf()
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'b').layout
    l = openSession(l, 'host', 'a').layout
    expect(active(l)).toBe('a')
    expect(sessions(renameSession(l, 'host', 'b', 'z'))).toEqual(['a+z'])
  })

  it('divider sizes are kept summing to 100', () => {
    let { l, pane } = tabOf()
    l = splitPane(l, pane(l, 'a'), 'row', 'host', 'b').layout
    l = setSizes(l, (l.tabs[0].root as Split).id, [1, 3])
    expect((l.tabs[0].root as Split).sizes).toEqual([25, 75])
    expect(normalizeSizes([33.3, 33.3, 33.3], 3).reduce((x, y) => x + y)).toBe(100)
  })

  it('validates stored splits: bad sizes → equal shares; bad nodes or too many panes → rejected', () => {
    const split = (sizes: unknown, children: unknown[] = [p('a'), p('b')]) => ({
      version: 1,
      tabs: [{ id: 't', root: { type: 'split', id: 's', dir: 'row', sizes, children }, focusedPane: 'pa' }],
      activeTab: 't',
    })
    const p = (s: string) => ({ type: 'pane', id: `p${s}`, machine: 'host', session: s })
    const sizesOf = (v: unknown) => (validateLayout(v)!.tabs[0].root as Split).sizes
    expect(sizesOf(split([60, 40]))).toEqual([60, 40])
    expect(sizesOf(split([6, 4]))).toEqual([60, 40])
    for (const bad of [[50], [50, -1], [50, 'x'], 'nope', undefined]) expect(sizesOf(split(bad))).toEqual([50, 50])
    expect(validateLayout(split([50, 50], [p('a')]))).toBeNull() // one child
    expect(validateLayout(split([50, 50], [p('a'), { type: 'pane' }]))).toBeNull()
    expect(validateLayout({ ...split([]), tabs: [{ id: 't', root: { type: 'split', id: 's', dir: 'diag', sizes: [], children: [p('a'), p('b')] } }] })).toBeNull()
    expect(validateLayout(split([], ['a', 'b', 'c', 'd', 'e'].map(p)))).toBeNull()
  })
})
