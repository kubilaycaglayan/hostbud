// The tab layout (docs/ARCHITECTURE.md §11): tabs of terminal panes, saved
// per account under the `layout` UI state key. Pure functions over plain
// data; stores/layout.ts owns the state, persistence and notices.

export interface Pane {
  type: 'pane'
  id: string
  machine: string
  session: string
}

/** A tab's content. Splits arrive in M3 T8. */
export type LayoutNode = Pane

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

export const emptyLayout = (): Layout => ({ version: 1, tabs: [], activeTab: null })

export const newId = (): string => crypto.randomUUID()

/** The node's panes, in order. */
export function panesOf(node: LayoutNode): Pane[] {
  return [node]
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

/** Removes the panes a predicate matches; a tab left without panes closes. */
export function removePanes(l: Layout, drop: (p: Pane) => boolean): Layout {
  let out = l
  for (const t of l.tabs) {
    if (panesOf(t.root).some(drop)) out = closeTab(out, t.id)
  }
  return out
}

/** A session was renamed: every pane showing it follows. */
export function renameSession(l: Layout, machine: string, from: string, to: string): Layout {
  const rename = (n: LayoutNode): LayoutNode => (shows(n, machine, from) ? { ...n, session: to } : n)
  return { ...l, tabs: l.tabs.map((t) => ({ ...t, root: rename(t.root) })) }
}

/** Panes of `machine` whose session isn't in `live` (a fresh session list). */
export function missingPanes(l: Layout, machine: string, live: ReadonlySet<string>): Pane[] {
  return allPanes(l).filter((p) => p.machine === machine && !live.has(p.session))
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const isStr = (v: unknown): v is string => typeof v === 'string' && v !== ''

function validNode(v: unknown): LayoutNode | null {
  if (!isObj(v) || v.type !== 'pane' || !isStr(v.id) || !isStr(v.machine) || !isStr(v.session)) return null
  return { type: 'pane', id: v.id, machine: v.machine, session: v.session }
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
    if (count + panes.length > MAX_PANES) break
    count += panes.length
    const focus = panes.some((p) => p.id === raw.focusedPane) ? (raw.focusedPane as string) : panes[0].id
    tabs.push({ id: raw.id, root, focusedPane: focus })
  }
  const active = tabs.some((t) => t.id === v.activeTab) ? (v.activeTab as string) : (tabs[0]?.id ?? null)
  return { version: 1, tabs, activeTab: active }
}
