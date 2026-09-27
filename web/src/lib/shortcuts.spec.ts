import { describe, expect, it } from 'vitest'
import { isEditableTarget, isTerminalTarget, isTreeTarget, matchingShortcut, matchingShortcutForTarget, shortcutLabels, shortcutPlatform, shortcutRegistryErrors, shortcuts, shouldInterceptGlobalShortcut } from './shortcuts'

const key = (key: string, options: Partial<Pick<KeyboardEvent, 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey'>> = {}) => ({
  key,
  code: options.code ?? '',
  ctrlKey: options.ctrlKey ?? false,
  metaKey: options.metaKey ?? false,
  altKey: options.altKey ?? false,
  shiftKey: options.shiftKey ?? false,
})

describe('shortcut registry', () => {
  it('has a conflict-free and safe global set', () => {
    expect(shortcutRegistryErrors()).toEqual([])
  })

  it.each([
    ['plain Ctrl+K global', { ctrl: true, key: 'k' }, 'unsafe terminal chord'],
    ['Ctrl+Shift+C', { ctrl: true, shift: true, key: 'c' }, 'reserved or terminal chord'],
    ['Ctrl+T', { ctrl: true, key: 't' }, 'unsafe terminal chord'],
  ])('rejects %s', (_name, chord, expected) => {
    const fixture = [{ id: 'bad', label: 'bad', group: 'General' as const, bindings: [{ scope: 'global' as const, label: 'bad', ...chord }] }]
    expect(shortcutRegistryErrors(fixture)).toContain(`bad: ${expected}`)
  })

  it('rejects duplicate chords whose platform ranges overlap', () => {
    const fixture = [
      { id: 'one', label: 'one', group: 'General' as const, bindings: [{ scope: 'global' as const, key: 'x', ctrl: true, shift: true, label: 'x' }] },
      { id: 'two', label: 'two', group: 'General' as const, bindings: [{ scope: 'global' as const, key: 'x', ctrl: true, shift: true, platform: 'mac' as const, label: 'x' }] },
    ]
    expect(shortcutRegistryErrors(fixture)).toContain('two: duplicate chord')
    const shiftOverlap = [
      { id: 'plain', label: 'plain', group: 'Tree' as const, bindings: [{ scope: 'tree' as const, key: 'h', shift: null, label: 'H' }] },
      { id: 'shifted', label: 'shifted', group: 'Tree' as const, bindings: [{ scope: 'tree' as const, key: 'h', shift: true, label: 'Shift+H' }] },
    ]
    expect(shortcutRegistryErrors(shiftOverlap)).toContain('shifted: duplicate chord')
  })

  it('selects labels and bindings by platform', () => {
    const entry = shortcuts.find((candidate) => candidate.id === 'palette')!
    expect(shortcutLabels(entry, 'mac')).toEqual(['⌘K', 'Ctrl+Shift+K', 'Ctrl+K'])
    expect(shortcutLabels(entry, 'other')).toEqual(['Ctrl+Shift+K', 'Ctrl+K'])
    expect(shortcutPlatform('MacIntel')).toBe('mac')
    expect(shortcutPlatform('iPhone')).toBe('mac')
    expect(shortcutPlatform('Linux x86_64')).toBe('other')
    expect(matchingShortcut(key('k', { metaKey: true }), 'mac')?.id).toBe('palette')
    expect(matchingShortcut(key('k', { metaKey: true }), 'other')).toBeUndefined()
    expect(matchingShortcut(key('k', { ctrlKey: true, shiftKey: true }), 'other')?.id).toBe('palette')
    expect(matchingShortcut(key('d', { metaKey: true, shiftKey: true }), 'mac')).toBeUndefined()
  })

  it('intercepts global chords but leaves outside-terminal Ctrl+K and Alt+B to the terminal', () => {
    expect(shouldInterceptGlobalShortcut(key('k', { ctrlKey: true, shiftKey: true }), 'other')).toBe(true)
    expect(shouldInterceptGlobalShortcut(key('k', { ctrlKey: true }), 'other')).toBe(false)
    expect(shouldInterceptGlobalShortcut(key('b', { altKey: true }), 'other')).toBe(false)
  })
})

describe('shortcut scope targets', () => {
  it('recognizes terminal, text-entry and tree row targets', () => {
    document.body.innerHTML = '<div class="xterm"><textarea class="xterm-helper-textarea"></textarea></div><input id="field"><div contenteditable="true" id="edit"></div><div role="treeitem" data-tree-key="session:one" id="row"></div>'
    const terminal = document.querySelector('textarea')!
    const field = document.querySelector<HTMLInputElement>('#field')!
    const edit = document.querySelector('#edit')!
    const row = document.querySelector('#row')!
    expect(isTerminalTarget(terminal)).toBe(true)
    expect(isEditableTarget(field)).toBe(true)
    expect(isEditableTarget(edit)).toBe(true)
    expect(isTreeTarget(row)).toBe(true)
    expect(isEditableTarget(row)).toBe(false)
    expect(isTerminalTarget(row)).toBe(false)
    expect(matchingShortcutForTarget(key('?', { shiftKey: true }), 'other', row)?.id).toBe('help')
    expect(matchingShortcutForTarget(key('?', { shiftKey: true }), 'other', document.body)?.id).toBe('help')
    expect(matchingShortcutForTarget(key('?', { shiftKey: true }), 'other', field)).toBeUndefined()
    expect(matchingShortcutForTarget(key('?', { shiftKey: true }), 'other', terminal)).toBeUndefined()
    expect(matchingShortcutForTarget(key('n'), 'other', row)?.id).toBe('tree-new-session')
    expect(matchingShortcutForTarget(key('n'), 'other', document.body)).toBeUndefined()
    expect(matchingShortcutForTarget(key('k', { ctrlKey: true, shiftKey: true }), 'other', field)?.id).toBe('palette')
  })
})
