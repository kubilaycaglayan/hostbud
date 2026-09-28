import type { TmuxWindow } from '@/api/types'

export type PaletteGroup = 'Sessions' | 'Windows' | 'Projects' | 'Actions'
export interface PaletteItem {
  id: string
  label: string
  group: PaletteGroup
  secondary?: string
  detail?: string
  shortcut?: string
  hidden?: boolean
}

export interface PaletteSessionSource {
  name: string
  path: string
  projectName?: string
  hidden: boolean
  windows?: { status: string; windows: TmuxWindow[] }
}

export interface PaletteProjectSource {
  id: string
  name: string
  path: string
  hidden: boolean
  pinned: boolean
}

export interface PaletteData {
  sessions: PaletteSessionSource[]
  projects: PaletteProjectSource[]
  showHidden: boolean
  hasActiveTab: boolean
  selectingSplitTarget?: boolean
  shortcutHint(id: string): string | undefined
}

/** Stable tree-order source list for the command palette. */
export function buildPaletteItems(data: PaletteData): PaletteItem[] {
  const sessionItems: PaletteItem[] = data.sessions.map((session) => ({
    id: `session:${session.name}`,
    label: session.name,
    group: 'Sessions',
    secondary: session.projectName ?? session.path,
    hidden: session.hidden,
  }))
  const windowItems: PaletteItem[] = data.sessions.flatMap((session) => session.windows?.status === 'ok'
    ? session.windows.windows.map((window) => ({
      id: `window:${session.name}:${window.id}`,
      label: window.name || `Window ${window.index}`,
      group: 'Windows' as const,
      secondary: session.name,
      detail: `window ${window.index}`,
    }))
    : [])
  const projectItems: PaletteItem[] = data.projects.map((project) => ({
    id: `project:${project.id}`,
    label: project.name,
    group: 'Projects',
    secondary: project.path,
  }))
  const action = (id: string, label: string, secondary?: string, shortcutId = id): PaletteItem => ({
    id: `action:${id}`,
    label,
    group: 'Actions',
    secondary,
    shortcut: data.shortcutHint(shortcutId),
  })
  const actions: PaletteItem[] = [
    action('new-session', 'New session', undefined, 'tree-new-session'),
    action('browse-files', 'Browse files'),
    action('queue', 'Open queue panel'),
    action('settings', 'Open settings'),
    action('collapse-all', 'Collapse all'),
    action('expand-all', 'Expand all'),
    action(data.showHidden ? 'hide-hidden' : 'show-hidden', data.showHidden ? 'Hide hidden' : 'Show hidden'),
    action('split-right', 'Split right'),
    action('split-down', 'Split down'),
    ...(data.hasActiveTab ? [action('close-tab', 'Close tab')] : []),
    action('next-tab', 'Next tab'),
    action('previous-tab', 'Previous tab'),
    action('theme-dark', 'Theme: Dark'),
    action('theme-light', 'Theme: Light'),
    action('theme-system', 'Theme: System'),
    action('shortcuts', 'Keyboard shortcuts'),
    action('sign-out', 'Sign out'),
  ]
  for (const project of data.projects) {
    actions.push(action(`new-project-session:${project.id}`, `New session in ${project.name}`, project.path))
    actions.push(action(`rename-project:${project.id}`, `Rename ${project.name}`, project.path, 'tree-rename'))
    actions.push(action(`remove-project:${project.id}`, `Remove project ${project.name}`, project.path))
    actions.push(action(`${project.hidden ? 'unhide' : 'hide'}-project:${project.id}`, `${project.hidden ? 'Unhide' : 'Hide'} ${project.name}`, project.path, 'tree-hide'))
    actions.push(action(`${project.pinned ? 'unpin' : 'pin'}-project:${project.id}`, `${project.pinned ? 'Unpin' : 'Pin'} ${project.name}`, project.path, 'tree-pin'))
  }
  for (const session of data.sessions) {
    actions.push(action(`rename-session:${session.name}`, `Rename ${session.name}`, session.projectName ?? session.path, 'tree-rename'))
    actions.push(action(`${session.hidden ? 'unhide' : 'hide'}-session:${session.name}`, `${session.hidden ? 'Unhide' : 'Hide'} ${session.name}`, session.projectName ?? session.path, 'tree-hide'))
    actions.push(action(`kill-session:${session.name}`, `Kill ${session.name}`, session.projectName ?? session.path, 'tree-kill'))
  }
  const groups = data.selectingSplitTarget
    ? [...sessionItems, ...actions.filter((item) => item.id === 'action:new-session' || item.id.startsWith('action:new-project-session:'))]
    : [...sessionItems, ...windowItems, ...projectItems, ...actions]
  return groups
}
