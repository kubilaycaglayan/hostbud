// The tab layout (docs/ARCHITECTURE.md §11): tabs of terminal panes, saved
// per account under the `layout` UI state key. Pure functions over plain
// data; stores/layout.ts owns the state, persistence and notices.

export interface Pane {
  type: 'pane'
  id: string
  machine: string
  session: string
}

/** Panes side by side (`row`) or stacked (`column`); `sizes` are percents
 * of the split, one per child, summing to 100. */
export interface Split {
  type: 'split'
  id: string
  dir: SplitDir
  sizes: number[]
  children: LayoutNode[]
}

export type SplitDir = 'row' | 'column'

/** A tab's content: one pane, or a tree of splits. */
export type LayoutNode = Pane | Split

export interface Tab {
  id: string
  root: LayoutNode
  focusedPane: string
}

export interface Layout {
  version: 1
  tabs: Tab[]
  activeTab: string | null
}

/** Open terminals in total, over every tab (each holds a WebSocket and an
 * ssh process). */
export const MAX_PANES = 16
/** Panes per tab. */
export const MAX_TAB_PANES = 4
/** The smallest pane, in percent of its split. */
export const MIN_PANE_SIZE = 10

export const emptyLayout = (): Layout => ({ version: 1, tabs: [], activeTab: null })

export const newId = (): string => crypto.randomUUID()

/** The node's panes, in order. */
export function panesOf(node: LayoutNode): Pane[] {
  return node.type === 'pane' ? [node] : node.children.flatMap(panesOf)
}

/** n equal shares of 100 (the last one takes the rounding). */
export function equalSizes(n: number): number[] {
  const each = Math.round((100 / n) * 100) / 100
  return Array.from({ length: n }, (_, i) => (i === n - 1 ? Math.round((100 - each * (n - 1)) * 100) / 100 : each))
}

/** Scales sizes to sum 100; bad ones (wrong count, not positive numbers)
 * become equal shares. */
export function normalizeSizes(sizes: unknown, n: number): number[] {
  if (!Array.isArray(sizes) || sizes.length !== n || !sizes.every((x) => typeof x === 'number' && Number.isFinite(x) && x > 0))
    return equalSizes(n)
  const sum = sizes.reduce((a: number, b: number) => a + b, 0)
  const scaled = sizes.map((x: number) => Math.round((x / sum) * 100 * 100) / 100)
  scaled[n - 1] = Math.round((100 - scaled.slice(0, -1).reduce((a, b) => a + b, 0)) * 100) / 100
  return scaled
}

/** Rebuilds a tree bottom-up: `f` maps each pane to a node, or null to
 * remove it. Removed panes give their space to their siblings (in
 * proportion); a split left with one child collapses to it, and a child
 * split of the same direction merges into its parent. */
function mapPanes(node: LayoutNode, f: (p: Pane) => LayoutNode | null): LayoutNode | null {
  if (node.type === 'pane') return f(node)
  const children: LayoutNode[] = []
  const sizes: number[] = []
  node.children.forEach((c, i) => {
    const next = mapPanes(c, f)
    if (!next) return
    if (next.type === 'split' && next.dir === node.dir) {
      // Same direction: its children become siblings, sharing its size.
      next.children.forEach((cc, j) => {
        children.push(cc)
        sizes.push((node.sizes[i] * next.sizes[j]) / 100)
      })
    } else {
      children.push(next)
      sizes.push(node.sizes[i])
    }
  })
  if (children.length === 0) return null
  if (children.length === 1) return children[0]
  return { ...node, children, sizes: normalizeSizes(sizes, children.length) }
}

export function allPanes(l: Layout): Pane[] {
  return l.tabs.flatMap((t) => panesOf(t.root))
}

export function activeTab(l: Layout): Tab | undefined {
  return l.tabs.find((t) => t.id === l.activeTab)
}

/** The focused pane of the active tab: "the selected session". */
export function focusedPane(l: Layout): Pane | undefined {
  const t = activeTab(l)
  return t && panesOf(t.root).find((p) => p.id === t.focusedPane)
}

const shows = (p: Pane, machine: string, session: string) => p.machine === machine && p.session === session

/** The first tab showing the session (in any of its panes). */
export function tabShowing(l: Layout, machine: string, session: string): Tab | undefined {
  return l.tabs.find((t) => panesOf(t.root).some((p) => shows(p, machine, session)))
}

export type OpenResult = 'focused' | 'opened' | 'full'

/**
 * Shows a session: focuses the tab (and pane) that already shows it, or
 * opens it in a new tab at the end. 'full' when that would exceed MAX_PANES.
 */
export function openSession(l: Layout, machine: string, session: string): { layout: Layout; result: OpenResult } {
  const existing = tabShowing(l, machine, session)
  if (existing) {
    const pane = panesOf(existing.root).find((p) => shows(p, machine, session))!
    const tabs = l.tabs.map((t) => (t === existing ? { ...t, focusedPane: pane.id } : t))
    return { layout: { ...l, tabs, activeTab: existing.id }, result: 'focused' }
  }
  if (allPanes(l).length >= MAX_PANES) return { layout: l, result: 'full' }
  const pane: Pane = { type: 'pane', id: newId(), machine, session }
  const tab: Tab = { id: newId(), root: pane, focusedPane: pane.id }
  return { layout: { ...l, tabs: [...l.tabs, tab], activeTab: tab.id }, result: 'opened' }
}

export function activate(l: Layout, tabId: string): Layout {
  return l.tabs.some((t) => t.id === tabId) ? { ...l, activeTab: tabId } : l
}

/** Closes a tab (its views detach; the sessions keep running). Closing the
 * active tab activates its right neighbor, or the left one at the end. */
export function closeTab(l: Layout, tabId: string): Layout {
  const i = l.tabs.findIndex((t) => t.id === tabId)
  if (i < 0) return l
  const tabs = l.tabs.filter((t) => t.id !== tabId)
  let active = l.activeTab
  if (active === tabId) active = (tabs[i] ?? tabs[i - 1])?.id ?? null
  return { ...l, tabs, activeTab: active }
}

/** Removes the panes a predicate matches; their space goes back to their
 * siblings. A tab left without panes closes; a tab whose focused pane went
 * gets the pane before it (or the first one) focused. */
export function removePanes(l: Layout, drop: (p: Pane) => boolean): Layout {
  let out = l
  for (const t of l.tabs) {
    const before = panesOf(t.root)
    if (!before.some(drop)) continue
    const root = mapPanes(t.root, (p) => (drop(p) ? null : p))
    if (!root) {
      out = closeTab(out, t.id)
      continue
    }
    const left = panesOf(root)
    let focus = t.focusedPane
    if (!left.some((p) => p.id === focus)) {
      const i = before.findIndex((p) => p.id === focus)
      focus = (before.slice(0, i).reverse().find((p) => !drop(p)) ?? left[0]).id
    }
    out = { ...out, tabs: out.tabs.map((x) => (x.id === t.id ? { ...x, root, focusedPane: focus } : x)) }
  }
  return out
}

/** Closes one pane (its view detaches); the last pane closes its tab. */
export function closePane(l: Layout, paneId: string): Layout {
  return removePanes(l, (p) => p.id === paneId)
}

export function focusPane(l: Layout, tabId: string, paneId: string): Layout {
  const t = l.tabs.find((x) => x.id === tabId)
  if (!t || t.focusedPane === paneId || !panesOf(t.root).some((p) => p.id === paneId)) return l
  return { ...l, tabs: l.tabs.map((x) => (x === t ? { ...x, focusedPane: paneId } : x)) }
}

/** Focuses the tab's next pane (in reading order), wrapping around. */
export function cycleFocus(l: Layout, tabId: string): Layout {
  const t = l.tabs.find((x) => x.id === tabId)
  if (!t) return l
  const panes = panesOf(t.root)
  const i = panes.findIndex((p) => p.id === t.focusedPane)
  return focusPane(l, tabId, panes[(i + 1) % panes.length].id)
}

export type SplitResult = 'split' | 'tab-full' | 'full' | 'missing'

/**
 * Opens a session in a new pane next to `paneId` (right for `row`, below
 * for `column`) and focuses it. Inside a split of the same direction it
 * becomes a sibling and the space is shared equally; otherwise the pane is
 * replaced by a new split of the two.
 */
export function splitPane(
  l: Layout,
  paneId: string,
  dir: SplitDir,
  machine: string,
  session: string,
): { layout: Layout; result: SplitResult } {
  const tab = l.tabs.find((t) => panesOf(t.root).some((p) => p.id === paneId))
  if (!tab) return { layout: l, result: 'missing' }
  if (panesOf(tab.root).length >= MAX_TAB_PANES) return { layout: l, result: 'tab-full' }
  if (allPanes(l).length >= MAX_PANES) return { layout: l, result: 'full' }
  const pane: Pane = { type: 'pane', id: newId(), machine, session }

  const insert = (node: LayoutNode): LayoutNode => {
    if (node.type === 'pane') {
      if (node.id !== paneId) return node
      return { type: 'split', id: newId(), dir, sizes: equalSizes(2), children: [node, pane] }
    }
    const i = node.children.findIndex((c) => c.type === 'pane' && c.id === paneId)
    if (i >= 0 && node.dir === dir) {
      const children = [...node.children.slice(0, i + 1), pane, ...node.children.slice(i + 1)]
      return { ...node, children, sizes: equalSizes(children.length) }
    }
    return { ...node, children: node.children.map(insert) }
  }
  const tabs = l.tabs.map((t) => (t === tab ? { ...t, root: insert(t.root), focusedPane: pane.id } : t))
  return { layout: { ...l, tabs, activeTab: tab.id }, result: 'split' }
}

/** New sizes for a split (a divider was dragged). */
export function setSizes(l: Layout, splitId: string, sizes: number[]): Layout {
  const apply = (n: LayoutNode): LayoutNode => {
    if (n.type === 'pane') return n
    if (n.id === splitId) return { ...n, sizes: normalizeSizes(sizes, n.children.length) }
    return { ...n, children: n.children.map(apply) }
  }
  return { ...l, tabs: l.tabs.map((t) => ({ ...t, root: apply(t.root) })) }
}

/** A session was renamed: every pane showing it follows. */
export function renameSession(l: Layout, machine: string, from: string, to: string): Layout {
  const rename = (n: LayoutNode): LayoutNode =>
    n.type === 'split' ? { ...n, children: n.children.map(rename) } : shows(n, machine, from) ? { ...n, session: to } : n
  return { ...l, tabs: l.tabs.map((t) => ({ ...t, root: rename(t.root) })) }
}

/** Panes of `machine` whose session isn't in `live` (a fresh session list). */
export function missingPanes(l: Layout, machine: string, live: ReadonlySet<string>): Pane[] {
  return allPanes(l).filter((p) => p.machine === machine && !live.has(p.session))
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const isStr = (v: unknown): v is string => typeof v === 'string' && v !== ''

function validNode(v: unknown, depth = 0): LayoutNode | null {
  if (!isObj(v) || !isStr(v.id)) return null
  if (v.type === 'pane') {
    if (!isStr(v.machine) || !isStr(v.session)) return null
    return { type: 'pane', id: v.id, machine: v.machine, session: v.session }
  }
  if (v.type !== 'split' || (v.dir !== 'row' && v.dir !== 'column') || depth >= MAX_TAB_PANES) return null
  if (!Array.isArray(v.children) || v.children.length < 2) return null
  const children: LayoutNode[] = []
  for (const c of v.children) {
    const n = validNode(c, depth + 1)
    if (!n) return null
    children.push(n)
  }
  return { type: 'split', id: v.id, dir: v.dir, sizes: normalizeSizes(v.sizes, children.length), children }
}

/**
 * Checks a stored layout (the server doesn't interpret it). Bad shape or an
 * unknown version → null; tabs beyond MAX_PANES terminals are dropped; a
 * missing focus or active tab is repaired.
 */
export function validateLayout(v: unknown): Layout | null {
  if (!isObj(v) || v.version !== 1 || !Array.isArray(v.tabs)) return null
  const tabs: Tab[] = []
  let count = 0
  for (const raw of v.tabs) {
    if (!isObj(raw) || !isStr(raw.id)) return null
    const root = validNode(raw.root)
    if (!root) return null
    const panes = panesOf(root)
    if (panes.length > MAX_TAB_PANES) return null
    if (count + panes.length > MAX_PANES) break
    count += panes.length
    const focus = panes.some((p) => p.id === raw.focusedPane) ? (raw.focusedPane as string) : panes[0].id
    tabs.push({ id: raw.id, root, focusedPane: focus })
  }
  const active = tabs.some((t) => t.id === v.activeTab) ? (v.activeTab as string) : (tabs[0]?.id ?? null)
  return { version: 1, tabs, activeTab: active }
}
