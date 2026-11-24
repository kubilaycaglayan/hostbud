import { emptyTreeState, type TreeState } from './tree'

const equal = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T

/** Rebase only fields changed locally; remote deletion wins over stale edits. */
export function mergeTree(base: TreeState, local: TreeState, remote: TreeState): TreeState {
  function merge(b: unknown, l: unknown, r: unknown, path: string): unknown {
    if (equal(b, l)) return r
    if (Array.isArray(b) && Array.isArray(l) && Array.isArray(r)) {
      if (path === 'sections') {
        const ids = (rows: { id: string }[]) => rows.map((row) => row.id)
        const order = merge(ids(b), ids(l), ids(r), 'sectionOrder') as string[]
        return order.map((id) => {
          const old = b.find((v) => v.id === id)
          const here = l.find((v) => v.id === id)
          const there = r.find((v) => v.id === id)
          return old ? merge(old, here, there, 'section') : here ?? there
        }).filter(Boolean)
      }
      const removed = new Set(b.filter((v) => !l.includes(v)))
      const added = l.filter((v) => !b.includes(v))
      const surviving = r.filter((v) => !removed.has(v))
      const ordered = ['projects', 'sectionOrder'].includes(path) || path.startsWith('sessions.')
      if (!ordered) return [...new Set([...surviving, ...added])]
      return [...l.filter((v) => surviving.includes(v) || added.includes(v)), ...surviving.filter((v) => !l.includes(v))]
    }
    if (b && l && r && typeof b === 'object' && typeof l === 'object' && typeof r === 'object') {
      const before = b as Record<string, unknown>, here = l as Record<string, unknown>, there = r as Record<string, unknown>
      return Object.fromEntries([...new Set([...Object.keys(here), ...Object.keys(there)])].flatMap((key) => {
        if (key in before && (!(key in here) || !(key in there))) return []
        return [[key, merge(before[key], here[key], there[key], path ? `${path}.${key}` : key)]]
      }))
    }
    return l
  }
  const result = clone(merge(base, local, remote, '') as TreeState)
  const sections = new Set(result.sections.map((s) => s.id))
  result.collapsedSections = result.collapsedSections.filter((id) => sections.has(id))
  result.projectSections = Object.fromEntries(Object.entries(result.projectSections).filter(([, id]) => sections.has(id)))
  return { ...emptyTreeState(), ...result }
}
