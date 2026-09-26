import type { Project, Session } from '@/api/types'

export const OTHER_GROUP = '__other__'
export interface TreeOrder {
  version: 1
  projects: string[]
  sessions: Record<string, string[]>
}
export interface ProjectGroup { project: Project; sessions: Session[] }
export interface TreeProjection { groups: ProjectGroup[]; other: Session[] }

export function emptyTreeOrder(): TreeOrder { return { version: 1, projects: [], sessions: {} } }

export function validateTreeOrder(value: unknown): TreeOrder | null {
  if (!value || typeof value !== 'object') return null
  const v = value as Record<string, unknown>
  if (v.version !== 1 || !Array.isArray(v.projects) || !v.sessions || typeof v.sessions !== 'object' || Array.isArray(v.sessions)) return null
  const cleanList = (list: unknown): string[] | null => {
    if (!Array.isArray(list) || list.length > 5000 || list.some((item) => typeof item !== 'string' || !item || item.length > 512)) return null
    return [...new Set(list)] as string[]
  }
  const projects = cleanList(v.projects)
  if (!projects) return null
  const sessions: Record<string, string[]> = {}
  for (const [group, list] of Object.entries(v.sessions)) {
    if (!group || group.length > 128 || group === '__proto__' || group === 'constructor') return null
    const names = cleanList(list)
    if (!names) return null
    Object.defineProperty(sessions, group, { value: names, writable: true, enumerable: true, configurable: true })
  }
  return { version: 1, projects, sessions }
}

export function ordered<T>(items: T[], ids: string[], key: (item: T) => string): T[] {
  const byId = new Map(items.map((item) => [key(item), item]))
  const out = ids.flatMap((id) => byId.has(id) ? [byId.get(id)!] : [])
  const seen = new Set(ids)
  return [...out, ...items.filter((item) => !seen.has(key(item)))]
}

export function move<T>(items: T[], from: number, to: number): T[] {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length || from === to) return items
  const next = [...items]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

function belongs(sessionPath: string, projectPath: string): boolean {
  const root = projectPath === '/' ? '/' : projectPath.replace(/\/+$/, '')
  return root === '/' ? sessionPath.startsWith('/') : sessionPath === root || sessionPath.startsWith(`${root}/`)
}

export function projectForSession(session: Session, projects: Project[]): Project | undefined {
  if (session.projectId) {
    const linked = projects.find((project) => project.id === session.projectId && project.machineId === 'host')
    if (linked) return linked
  }
  return projects.filter((p) => p.machineId === 'host')
    .filter((p) => belongs(session.path, p.path))
    .sort((a, b) => b.path.length - a.path.length || a.id.localeCompare(b.id))[0]
}

export function projectTree(projects: Project[], sessions: Session[], order: TreeOrder): TreeProjection {
  const orderedProjects = ordered(projects, order.projects, (p) => p.id)
  const placement = new Map(sessions.map((session) => [session.name, projectForSession(session, projects)?.id]))
  const groups = orderedProjects.map((project) => {
    const rows = sessions.filter((session) => placement.get(session.name) === project.id)
    return { project, sessions: ordered(rows, order.sessions[project.id] ?? [], (s) => s.name) }
  })
  const matched = new Set(groups.flatMap((group) => group.sessions.map((s) => s.name)))
  const otherRows = sessions.filter((s) => !matched.has(s.name))
  return { groups, other: ordered(otherRows, order.sessions[OTHER_GROUP] ?? [], (s) => s.name) }
}
