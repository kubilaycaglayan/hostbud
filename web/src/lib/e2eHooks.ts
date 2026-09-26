// Test hooks for the e2e suite (docs/ARCHITECTURE.md §13.1). Callers guard
// every use with `import.meta.env.VITE_E2E === '1'` inline: Vite replaces it
// with a constant, so production builds drop this code entirely
// (scripts/check-dist.mjs fails a production build that still has it).
//
// Each open terminal registers under its pane id. The `session` argument
// picks a terminal by session name; without it, the focused pane of the
// active tab answers (the only terminal before tabs existed).

/** One terminal's hooks. */
export interface TermHooks {
  /** The terminal buffer as text (trailing blanks trimmed). */
  termText: () => string
  /** The terminal's size in cells. */
  termSize: () => { cols: number; rows: number }
  /** The terminal's selected text ('' without a selection). */
  termSelection: () => string
  /** The rows currently in view (after scrolling), as text. */
  termViewport: () => string
  /** Where the last on-screen occurrence of `needle` is drawn (page px). */
  termTextRect: (needle: string) => { x: number; y: number; width: number; height: number } | null
}

export interface PaneInfo {
  session: string
  active: boolean
  focused: boolean
}

type Rect = { x: number; y: number; width: number; height: number }

export interface HostbudHooks {
  termText: (session?: string) => string
  termSize: (session?: string) => { cols: number; rows: number }
  termSelection: (session?: string) => string
  termViewport: (session?: string) => string
  termTextRect: (needle: string, session?: string) => Rect | null
  /** The registered terminals (mounted panes), in layout order. */
  panes: () => PaneInfo[]
}

declare global {
  interface Window {
    __hostbud?: HostbudHooks
  }
}

interface Entry {
  hooks: TermHooks
  info: () => PaneInfo
}

const registry = new Map<string, Entry>()

function pick(session?: string): TermHooks | undefined {
  const entries = [...registry.values()]
  const e = session === undefined ? entries.find((x) => x.info().focused) : entries.find((x) => x.info().session === session)
  return e?.hooks
}

const hooks: HostbudHooks = {
  termText: (s) => pick(s)?.termText() ?? '',
  termSize: (s) => pick(s)?.termSize() ?? { cols: 0, rows: 0 },
  termSelection: (s) => pick(s)?.termSelection() ?? '',
  termViewport: (s) => pick(s)?.termViewport() ?? '',
  termTextRect: (needle, s) => pick(s)?.termTextRect(needle) ?? null,
  panes: () => [...registry.values()].map((e) => e.info()),
}

/** Registers a mounted terminal; `info` is read live (sessions get renamed,
 * focus moves). */
export function registerPane(id: string, termHooks: TermHooks, info: () => PaneInfo): void {
  registry.set(id, { hooks: termHooks, info })
  window.__hostbud = hooks
}

export function unregisterPane(id: string): void {
  registry.delete(id)
  if (registry.size === 0) delete window.__hostbud
}
