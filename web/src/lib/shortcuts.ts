export type ShortcutScope = 'global' | 'outside-terminal' | 'tree'
export type ShortcutGroup = 'General' | 'Sessions' | 'Tree'
export type ShortcutPlatform = 'mac' | 'other'

export interface ShortcutBinding {
  scope: ShortcutScope
  platform?: ShortcutPlatform | 'all'
  key?: string
  code?: string
  ctrl?: boolean
  meta?: boolean
  alt?: boolean
  shift?: boolean | null
  label: string
}

export interface ShortcutEntry {
  id: string
  label: string
  group: ShortcutGroup
  bindings: ShortcutBinding[]
}

const chord = (scope: ShortcutScope, label: string, keys: Omit<ShortcutBinding, 'scope' | 'label'>): ShortcutBinding => ({ scope, label, ...keys })
const global = (label: string, keys: Omit<ShortcutBinding, 'scope' | 'label'>) => chord('global', label, keys)
const outside = (label: string, keys: Omit<ShortcutBinding, 'scope' | 'label'>) => chord('outside-terminal', label, keys)
const tree = (label: string, keys: Omit<ShortcutBinding, 'scope' | 'label'>) => chord('tree', label, keys)

export const shortcuts: ShortcutEntry[] = [
  { id: 'palette', label: 'Command palette', group: 'General', bindings: [global('⌘K', { key: 'k', meta: true, platform: 'mac' }), global('Ctrl+Shift+K', { key: 'k', ctrl: true, shift: true }), outside('Ctrl+K', { key: 'k', ctrl: true })] },
  { id: 'help', label: 'Keyboard shortcuts', group: 'General', bindings: [global('⌘/', { code: 'Slash', meta: true, shift: null, platform: 'mac' }), global('Ctrl+Shift+/', { code: 'Slash', ctrl: true, shift: true }), outside('?', { key: '?', shift: true })] },
  { id: 'focus-tree-terminal', label: 'Focus tree ↔ terminal', group: 'General', bindings: [global('⌘⇧E', { key: 'e', meta: true, shift: true, platform: 'mac' }), global('Ctrl+Shift+E', { key: 'e', ctrl: true, shift: true })] },
  { id: 'next-tab', label: 'Next open visible session', group: 'Sessions', bindings: [global('Ctrl+Shift+]', { code: 'BracketRight', ctrl: true, shift: true })] },
  { id: 'previous-tab', label: 'Previous open visible session', group: 'Sessions', bindings: [global('Ctrl+Shift+[', { code: 'BracketLeft', ctrl: true, shift: true })] },
  { id: 'last-tab', label: 'Switch to last session', group: 'Sessions', bindings: [global('Ctrl+Shift+D', { key: 'd', ctrl: true, shift: true }), global('Ctrl+⌘+D', { key: 'd', ctrl: true, meta: true, platform: 'mac' })] },
  { id: 'tree-up', label: 'Move focus up', group: 'Tree', bindings: [tree('↑', { key: 'ArrowUp' })] },
  { id: 'tree-down', label: 'Move focus down', group: 'Tree', bindings: [tree('↓', { key: 'ArrowDown' })] },
  { id: 'tree-expand', label: 'Expand row or move to child', group: 'Tree', bindings: [tree('→', { key: 'ArrowRight' })] },
  { id: 'tree-collapse', label: 'Collapse row or move to parent', group: 'Tree', bindings: [tree('←', { key: 'ArrowLeft' })] },
  { id: 'tree-home', label: 'Focus first row', group: 'Tree', bindings: [tree('Home', { key: 'Home' })] },
  { id: 'tree-end', label: 'Focus last row', group: 'Tree', bindings: [tree('End', { key: 'End' })] },
  { id: 'tree-open', label: 'Open row', group: 'Tree', bindings: [tree('Enter', { key: 'Enter' })] },
  { id: 'tree-reorder-up', label: 'Move row up', group: 'Tree', bindings: [tree('Alt+↑', { key: 'ArrowUp', alt: true })] },
  { id: 'tree-reorder-down', label: 'Move row down', group: 'Tree', bindings: [tree('Alt+↓', { key: 'ArrowDown', alt: true })] },
  { id: 'tree-rename', label: 'Rename row', group: 'Tree', bindings: [tree('F2', { key: 'F2' })] },
  { id: 'tree-kill', label: 'Kill session', group: 'Tree', bindings: [tree('Delete', { key: 'Delete' })] },
  { id: 'tree-hide', label: 'Hide or unhide row', group: 'Tree', bindings: [tree('H', { key: 'h', shift: null })] },
  { id: 'tree-pin', label: 'Pin or unpin project', group: 'Tree', bindings: [tree('P', { key: 'p', shift: null })] },
  { id: 'tree-new-session', label: 'New session here', group: 'Tree', bindings: [tree('N', { key: 'n', shift: null })] },
]

export function shortcutPlatform(platform?: string): ShortcutPlatform {
  const value = platform ?? (typeof navigator === 'undefined' ? '' : ((navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform || navigator.platform))
  return /mac|iphone|ipad|ipod/i.test(value) ? 'mac' : 'other'
}

export function matchesBinding(event: Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey'>, binding: ShortcutBinding): boolean {
  if (Boolean(binding.ctrl) !== Boolean(event.ctrlKey) || Boolean(binding.meta) !== Boolean(event.metaKey) || Boolean(binding.alt) !== Boolean(event.altKey)) return false
  if (binding.shift !== null && Boolean(binding.shift) !== Boolean(event.shiftKey)) return false
  if (binding.code) return event.code === binding.code
  return event.key.toLowerCase() === binding.key?.toLowerCase()
}

export function matchingShortcut(event: Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey'>, platform: ShortcutPlatform, scope?: ShortcutScope): ShortcutEntry | undefined {
  return shortcuts.find((entry) => entry.bindings.some((binding) => binding.scope !== 'tree' && (!scope || binding.scope === scope) && (binding.platform === undefined || binding.platform === 'all' || binding.platform === platform) && matchesBinding(event, binding)))
}

export function shouldInterceptGlobalShortcut(event: Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey'>, platform: ShortcutPlatform): boolean {
  return matchingShortcut(event, platform, 'global') !== undefined
}

export function matchingTreeShortcut(event: Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey'>, id: string): boolean {
  const entry = shortcuts.find((candidate) => candidate.id === id)
  return entry?.bindings.some((binding) => binding.scope === 'tree' && matchesBinding(event, binding)) ?? false
}

export function shortcutLabels(entry: ShortcutEntry, platform: ShortcutPlatform): string[] {
  return entry.bindings.filter((binding) => binding.platform === undefined || binding.platform === 'all' || binding.platform === platform).map((binding) => binding.label)
}

export function shortcutRegistryErrors(entries: ShortcutEntry[] = shortcuts): string[] {
  const errors: string[] = []
  const seen: { binding: ShortcutBinding }[] = []
  const reserved = new Set(['ctrl:KeyT', 'ctrl:KeyW', 'ctrl:KeyN', 'ctrl:Tab', 'ctrl+shift:KeyT', 'ctrl+shift:KeyW', 'ctrl+shift:KeyN', 'ctrl+shift:Tab', 'ctrl:PageUp', 'ctrl:PageDown', 'ctrl+shift:KeyC', 'ctrl+shift:KeyV', 'ctrl+shift:KeyF', 'meta+shift:KeyC', 'meta+shift:KeyV', 'meta+shift:KeyF', 'meta:KeyC', 'meta:KeyV'])
  for (const entry of entries) for (const binding of entry.bindings) {
    const mod = [binding.ctrl && 'ctrl', binding.meta && 'meta', binding.alt && 'alt', binding.shift === true && 'shift'].filter(Boolean).join('+') || 'plain'
    const key = binding.code ?? `Key${(binding.key ?? '').toUpperCase()}`
    if (binding.scope === 'global' && !(binding.meta || binding.ctrl && binding.shift) || binding.scope === 'global' && binding.alt) errors.push(`${entry.id}: unsafe terminal chord`)
    if (binding.scope === 'global' && reserved.has(`${mod}:${key}`)) errors.push(`${entry.id}: reserved or terminal chord`)
    if (seen.some(({ binding: prior }) => {
      const sameKey = (prior.code ?? prior.key?.toLowerCase()) === (binding.code ?? binding.key?.toLowerCase())
      const sameModifiers = Boolean(prior.ctrl) === Boolean(binding.ctrl) && Boolean(prior.meta) === Boolean(binding.meta) && Boolean(prior.alt) === Boolean(binding.alt) && (prior.shift === null || binding.shift === null || Boolean(prior.shift) === Boolean(binding.shift))
      const samePlatform = prior.platform === undefined || prior.platform === 'all' || binding.platform === undefined || binding.platform === 'all' || prior.platform === binding.platform
      return prior.scope === binding.scope && sameKey && sameModifiers && samePlatform
    })) errors.push(`${entry.id}: duplicate chord`)
    seen.push({ binding })
  }
  return errors
}

export function isEditableTarget(target: EventTarget | null): boolean {
  const element = target instanceof Element ? target : null
  return Boolean(element?.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"], [role="textbox"]'))
}

export function isTerminalTarget(target: EventTarget | null): boolean {
  const element = target instanceof Element ? target : null
  return Boolean(element?.closest('.xterm-helper-textarea'))
}

export function isTreeTarget(target: EventTarget | null): boolean {
  const element = target instanceof Element ? target : null
  return Boolean(element?.closest('[role="treeitem"][data-tree-key]'))
}

export function matchingShortcutForTarget(
  event: Pick<KeyboardEvent, 'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey'>,
  platform: ShortcutPlatform,
  target: EventTarget | null,
): ShortcutEntry | undefined {
  const globalEntry = matchingShortcut(event, platform, 'global')
  if (globalEntry) return globalEntry
  if (isTerminalTarget(target) || isEditableTarget(target)) return undefined
  if (isTreeTarget(target)) {
    const treeEntry = shortcuts.find((entry) => entry.bindings.some((binding) => binding.scope === 'tree' && (binding.platform === undefined || binding.platform === 'all' || binding.platform === platform) && matchesBinding(event, binding)))
    return treeEntry ?? matchingShortcut(event, platform, 'outside-terminal')
  }
  return matchingShortcut(event, platform, 'outside-terminal')
}
