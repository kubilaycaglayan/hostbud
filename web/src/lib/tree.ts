import type { Project, Session } from '@/api/types'

export const OTHER_GROUP = '__other__'
export const SECTION_COLORS = ['red', 'green', 'blue', 'yellow', 'orange', 'purple'] as const
export type SectionColor = typeof SECTION_COLORS[number]
export interface ProjectSection { id: string; name: string; color: SectionColor }
export interface TreeState {
  version: 3
  projects: string[]
  sessions: Record<string, string[]>
  pinned: string[]
  hidden: { projects: string[]; sessions: string[] }
  collapsed: string[]
  expanded: string[]
  showHidden: boolean
  sections: ProjectSection[]
  projectSections: Record<string, string>
}
export type TreeOrder = TreeState
export interface ProjectGroup { project: Project; sessions: Session[] }
export interface TreeProjection { groups: ProjectGroup[]; pinned: ProjectGroup[]; unpinned: ProjectGroup[]; other: Session[] }

export function emptyTreeState(): TreeState {
  return { version: 3, projects: [], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false, sections: [], projectSections: {} }
}
export const emptyTreeOrder = emptyTreeState

function cleanList(value: unknown): string[] | null {
  if (!Array.isArray(value) || value.length > 5000 || value.some((item) =>
    typeof item !== 'string' || !item || item.length > 512 || item === '__proto__' || item === 'constructor' || /[\u0000-\u001f\u007f]/u.test(item),
  )) return null
  return [...new Set(value)] as string[]
}

const sessionName = /^[A-Za-z0-9_-]{1,64}$/u
const machineID = /^[A-Za-z0-9_-]{1,64}$/u
function validSessionKey(value: string): boolean {
  const parts = value.split('/')
  return parts.length === 2 && machineID.test(parts[0]) && sessionName.test(parts[1])
}
function validExpandedKey(value: string): boolean {
  const parts = value.split('/')
  return validSessionKey(parts.slice(0, 2).join('/')) &&
    (parts.length === 2 || (parts.length === 3 && /^@[0-9]+$/u.test(parts[2])))
}

export function validateTreeState(value: unknown): TreeState | null {
  if (!value || typeof value !== 'object') return null
  const v = value as Record<string, unknown>
  if (v.version !== 1 && v.version !== 2 && v.version !== 3) return null
  if (!Array.isArray(v.projects) || !v.sessions || typeof v.sessions !== 'object' || Array.isArray(v.sessions)) return null
  const projects = cleanList(v.projects)
  if (!projects || projects.some((id) => id.includes('/'))) return null
  const sessions: Record<string, string[]> = {}
  for (const [group, list] of Object.entries(v.sessions)) {
    if (!group || group.length > 128 || group === '__proto__' || group === 'constructor' || (group !== OTHER_GROUP && group.includes('/'))) return null
    const names = cleanList(list)
    if (!names) return null
    if (names.some((name) => !sessionName.test(name))) return null
    Object.defineProperty(sessions, group, { value: names, writable: true, enumerable: true, configurable: true })
  }
  if (v.version === 1) return { ...emptyTreeState(), projects, sessions }
  if (!v.hidden || typeof v.hidden !== 'object' || Array.isArray(v.hidden)) return null
  const hidden = v.hidden as Record<string, unknown>
  const pinned = cleanList(v.pinned)
  const hiddenProjects = cleanList(hidden.projects)
  const hiddenSessions = cleanList(hidden.sessions)
  const collapsed = cleanList(v.collapsed)
  const expanded = cleanList(v.expanded)
  if (!pinned || !hiddenProjects || !hiddenSessions || !collapsed || !expanded || typeof v.showHidden !== 'boolean') return null
  if (hiddenSessions.some((key) => !validSessionKey(key)) ||
    expanded.some((key) => !validExpandedKey(key)) ||
    [...pinned, ...hiddenProjects, ...collapsed.filter((key) => key !== OTHER_GROUP)].some((id) => id.includes('/'))) return null
  if (v.version === 2) return { ...emptyTreeState(), projects, sessions, pinned, hidden: { projects: hiddenProjects, sessions: hiddenSessions }, collapsed, expanded, showHidden: v.showHidden }
  if (!Array.isArray(v.sections) || v.sections.length > 500 || !v.projectSections || typeof v.projectSections !== 'object' || Array.isArray(v.projectSections)) return null
  const sectionIDs = new Set<string>()
  const sections: ProjectSection[] = []
  for (const raw of v.sections) {
    if (!raw || typeof raw !== 'object') return null
    const section = raw as Record<string, unknown>
    if (typeof section.id !== 'string' || !/^[A-Za-z0-9_-]{1,64}$/u.test(section.id) || sectionIDs.has(section.id) ||
      typeof section.name !== 'string' || !section.name.trim() || section.name.length > 80 || /[\u0000-\u001f\u007f]/u.test(section.name) ||
      typeof section.color !== 'string' || !(SECTION_COLORS as readonly string[]).includes(section.color)) return null
    sectionIDs.add(section.id)
    sections.push({ id: section.id, name: section.name.trim(), color: section.color as SectionColor })
  }
  const projectSections: Record<string, string> = {}
  for (const [project, section] of Object.entries(v.projectSections as Record<string, unknown>)) {
    if (!project || project.length > 512 || project === '__proto__' || project === 'constructor' || typeof section !== 'string' || !sectionIDs.has(section)) return null
    Object.defineProperty(projectSections, project, { value: section, writable: true, enumerable: true, configurable: true })
  }
  return { version: 3, projects, sessions, pinned, hidden: { projects: hiddenProjects, sessions: hiddenSessions }, collapsed, expanded, showHidden: v.showHidden, sections, projectSections }
}
export const validateTreeOrder = validateTreeState

export function sessionKey(machineId: string, name: string): string { return machineId + '/' + name }
export function windowKey(machineId: string, name: string, id: string): string { return machineId + '/' + name + '/' + id }

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

export function canReorderProjectSections(from: string, to: string): boolean {
  return from === to
}

export function orderedProjectSections(sections: ProjectSection[], ids: string[]): ProjectSection[] {
  return ordered(sections, ids, (section) => section.id)
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

export function projectTree(projects: Project[], sessions: Session[], order: TreeState): TreeProjection {
  const orderedProjects = ordered(projects, order.projects, (p) => p.id)
  const placement = new Map(sessions.map((session) => [session.name, projectForSession(session, projects)?.id]))
  const groups = orderedProjects.map((project) => {
    const rows = sessions.filter((session) => placement.get(session.name) === project.id)
    return { project, sessions: ordered(rows, order.sessions[project.id] ?? [], (s) => s.name) }
  })
  const matched = new Set(groups.flatMap((group) => group.sessions.map((s) => s.name)))
  const otherRows = sessions.filter((s) => !matched.has(s.name))
  const pinned = new Set(order.pinned)
  return {
    groups,
    pinned: groups.filter((group) => pinned.has(group.project.id)),
    unpinned: groups.filter((group) => !pinned.has(group.project.id)),
    other: ordered(otherRows, order.sessions[OTHER_GROUP] ?? [], (s) => s.name),
  }
}
