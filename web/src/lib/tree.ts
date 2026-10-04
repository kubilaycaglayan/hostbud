import type { Project, Session } from '@/api/types'

export const OTHER_GROUP = '__other__'
export const SECTION_COLORS = ['red', 'green', 'blue', 'yellow', 'orange', 'purple'] as const
export type SectionColor = typeof SECTION_COLORS[number]
export interface ProjectSection { id: string; name: string; color: SectionColor }
export interface TreeState {
  version: 4
  projects: string[]
  sessions: Record<string, string[]>
  pinned: string[]
  hidden: { projects: string[]; sessions: string[] }
  collapsed: string[]
  collapsedSections: string[]
  expanded: string[]
  showHidden: boolean
  sections: ProjectSection[]
  projectSections: Record<string, string>
}
export type TreeOrder = TreeState
export interface ProjectGroup { project: Project; sessions: Session[] }
export interface TreeProjection { groups: ProjectGroup[]; pinned: ProjectGroup[]; unpinned: ProjectGroup[]; other: Session[] }

export function emptyTreeState(): TreeState {
  return { version: 4, projects: [], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], collapsedSections: [], expanded: [], showHidden: false, sections: [], projectSections: {} }
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

// A saved session order holds host sessions by name and other servers'
// sessions as "machine/name" (V2-M13 session refs).
function validOrderEntry(value: string): boolean {
  return sessionName.test(value) || validSessionKey(value)
}

export function validateTreeState(value: unknown): TreeState | null {
  if (!value || typeof value !== 'object') return null
  const v = value as Record<string, unknown>
  if (v.version !== 1 && v.version !== 2 && v.version !== 3 && v.version !== 4) return null
  if (!Array.isArray(v.projects) || !v.sessions || typeof v.sessions !== 'object' || Array.isArray(v.sessions)) return null
  const projects = cleanList(v.projects)
  if (!projects || projects.some((id) => id.includes('/'))) return null
  const sessions: Record<string, string[]> = {}
  for (const [group, list] of Object.entries(v.sessions)) {
    if (!group || group.length > 128 || group === '__proto__' || group === 'constructor' || (group !== OTHER_GROUP && group.includes('/'))) return null
    const names = cleanList(list)
    if (!names) return null
    if (names.some((name) => !validOrderEntry(name))) return null
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
  const collapsedSections = v.version === 4 ? cleanList(v.collapsedSections) : []
  if (!collapsedSections || collapsedSections.some((id) => !sectionIDs.has(id))) return null
  return { version: 4, projects, sessions, pinned, hidden: { projects: hiddenProjects, sessions: hiddenSessions }, collapsed, collapsedSections, expanded, showHidden: v.showHidden, sections, projectSections }
}
export const validateTreeOrder = validateTreeState

export function sessionKey(machineId: string, name: string): string { return machineId + '/' + name }

/** The machine a session runs on (the host unless set). */
export function sessionMachine(session: { machine?: string }): string { return session.machine ?? 'host' }

/** A session's identity in the tree and app (V2-M13): its name on the host,
 * "machine/name" on another server, so host-only saved state stays valid. */
export function sessionRef(machine: string, name: string): string { return machine === 'host' ? name : machine + '/' + name }
export function refOf(session: { machine?: string; name: string }): string { return sessionRef(sessionMachine(session), session.name) }
export function parseSessionRef(ref: string): { machine: string; name: string } {
  const i = ref.indexOf('/')
  return i < 0 ? { machine: 'host', name: ref } : { machine: ref.slice(0, i), name: ref.slice(i + 1) }
}
/** The ref of a "machine/name" hidden or expanded key. */
export function refOfKey(key: string): string {
  const { machine, name } = parseSessionRef(key)
  return sessionRef(machine, name)
}
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
  const machine = sessionMachine(session)
  if (session.projectId) {
    const linked = projects.find((project) => project.id === session.projectId && project.machineId === machine)
    if (linked) return linked
  }
  return projects.filter((p) => p.machineId === machine)
    .filter((p) => belongs(session.path, p.path))
    .sort((a, b) => b.path.length - a.path.length || a.id.localeCompare(b.id))[0]
}

export function projectTree(projects: Project[], sessions: Session[], order: TreeState): TreeProjection {
  const orderedProjects = ordered(projects, order.projects, (p) => p.id)
  const placement = new Map(sessions.map((session) => [refOf(session), projectForSession(session, projects)?.id]))
  const groups = orderedProjects.map((project) => {
    const rows = sessions.filter((session) => placement.get(refOf(session)) === project.id)
    return { project, sessions: ordered(rows, order.sessions[project.id] ?? [], refOf) }
  })
  const matched = new Set(groups.flatMap((group) => group.sessions.map(refOf)))
  const otherRows = sessions.filter((s) => !matched.has(refOf(s)))
  const pinned = new Set(order.pinned)
  return {
    groups,
    pinned: groups.filter((group) => pinned.has(group.project.id)),
    unpinned: groups.filter((group) => !pinned.has(group.project.id)),
    other: ordered(otherRows, order.sessions[OTHER_GROUP] ?? [], refOf),
  }
}

/** Session refs in the order and visibility of the left tree, restricted to
 * open terminal views (openNames holds sessionKey values). Collapsed
 * project/section descendants are omitted. */
export function visibleOpenSessionNames(
  groups: ProjectGroup[],
  other: Session[],
  order: TreeState,
  openNames: ReadonlySet<string>,
): string[] {
  const visibleGroups = groups.filter((group) => order.showHidden || !order.hidden.projects.includes(group.project.id))
  const rows = (group: ProjectGroup) => group.sessions.filter((session) =>
    (order.showHidden || !order.hidden.sessions.includes(sessionKey(sessionMachine(session), session.name))) && openNames.has(sessionKey(sessionMachine(session), session.name)),
  )
  const orderedGroups = [
    ...visibleGroups.filter((group) => order.pinned.includes(group.project.id) && !order.projectSections[group.project.id]),
    ...order.sections.flatMap((section) => [
      ...visibleGroups.filter((group) => order.projectSections[group.project.id] === section.id && order.pinned.includes(group.project.id)),
      ...visibleGroups.filter((group) => order.projectSections[group.project.id] === section.id && !order.pinned.includes(group.project.id)),
    ]),
    ...visibleGroups.filter((group) => !order.pinned.includes(group.project.id) && !order.projectSections[group.project.id]),
  ]
  const names = orderedGroups.flatMap((group) => {
    const section = order.projectSections[group.project.id]
    if ((section && order.collapsedSections.includes(section)) || order.collapsed.includes(group.project.id)) return []
    return rows(group).map(refOf)
  })
  const visibleOther = other.filter((session) =>
    (order.showHidden || !order.hidden.sessions.includes(sessionKey(sessionMachine(session), session.name))) && openNames.has(sessionKey(sessionMachine(session), session.name)),
  )
  if (visibleOther.length && !order.collapsed.includes(OTHER_GROUP)) names.push(...visibleOther.map(refOf))
  return names
}
