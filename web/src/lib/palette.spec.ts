import { describe, expect, it } from 'vitest'
import { buildPaletteItems } from './palette'

const data = (overrides: Partial<Parameters<typeof buildPaletteItems>[0]> = {}) => ({
  sessions: [
    { name: 'acc-a', path: '/home/dev/garden', projectName: 'Garden', hidden: true, windows: { status: 'ok', windows: [{ id: '@1', index: 1, name: 'editor', active: true, panes: [] }] } },
    { name: 'acc-b', path: '/home/dev', hidden: false, windows: { status: 'loading', windows: [] } },
  ],
  projects: [{ id: 'p1', name: 'Garden', path: '/home/dev/garden', hidden: false, pinned: true }],
  showHidden: true,
  hasActiveTab: true,
  shortcutHint: (id: string) => id === 'tree-hide' ? 'H' : undefined,
  ...overrides,
})

describe('buildPaletteItems', () => {
  it('lists tree-ordered sessions, loaded windows and projects with hidden and action markers', () => {
    const items = buildPaletteItems(data())
    expect(items.filter((item) => item.group === 'Sessions').map((item) => item.label)).toEqual(['acc-a', 'acc-b'])
    expect(items.find((item) => item.id === 'session:acc-a')?.hidden).toBe(true)
    expect(items.filter((item) => item.group === 'Windows').map((item) => item.label)).toEqual(['editor'])
    expect(items.find((item) => item.id === 'project:p1')?.secondary).toBe('/home/dev/garden')
    expect(items.find((item) => item.id === 'action:unhide-session:acc-a')?.shortcut).toBe('H')
    expect(items.some((item) => item.id === 'action:close-tab')).toBe(true)
  })

  it('includes every action, omits Close tab without an active tab and narrows split mode to destinations', () => {
    const base = data({ hasActiveTab: false, showHidden: false })
    const items = buildPaletteItems({
      ...base,
      sessions: base.sessions.map((session) => ({ ...session, hidden: false })),
      projects: base.projects.map((project) => ({ ...project, hidden: false, pinned: false })),
    })
    for (const id of [
      'action:new-session', 'action:new-project-session:p1', 'action:browse-files', 'action:rename-project:p1', 'action:remove-project:p1',
      'action:hide-project:p1', 'action:pin-project:p1', 'action:rename-session:acc-a', 'action:kill-session:acc-a',
      'action:collapse-all', 'action:expand-all', 'action:show-hidden', 'action:split-right', 'action:split-down',
      'action:next-tab', 'action:previous-tab', 'action:theme-dark', 'action:theme-light', 'action:theme-system',
      'action:shortcuts', 'action:sign-out',
    ]) expect(items.some((item) => item.id === id)).toBe(true)
    expect(items.some((item) => item.id === 'action:close-tab')).toBe(false)

    const split = buildPaletteItems(data({ selectingSplitTarget: true }))
    expect(split.every((item) => item.group === 'Sessions' || item.id === 'action:new-session' || item.id.startsWith('action:new-project-session:'))).toBe(true)
    expect(split.some((item) => item.group === 'Windows' || item.group === 'Projects')).toBe(false)
  })
})
