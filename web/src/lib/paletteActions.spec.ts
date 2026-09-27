import { describe, expect, it, vi } from 'vitest'
import { dispatchPaletteAction, type PaletteActionHandlers } from './paletteActions'

describe('dispatchPaletteAction', () => {
  it('routes each action to its existing handler with the right target and mode', () => {
    const handlers = Object.fromEntries([
      'newSession', 'newProjectSession', 'browseFiles', 'renameProject', 'removeProject', 'renameSession',
      'hideProject', 'unhideProject', 'hideSession', 'unhideSession', 'pinProject', 'unpinProject',
      'killSession', 'collapseAll', 'expandAll', 'setShowHidden', 'split', 'closeTab', 'nextTab',
      'previousTab', 'setTheme', 'shortcuts', 'signOut',
    ].map((name) => [name, vi.fn()])) as unknown as PaletteActionHandlers

    const actions = [
      ['new-session', 'newSession', []], ['new-project-session:p1', 'newProjectSession', ['p1']],
      ['browse-files', 'browseFiles', []], ['rename-project:p1', 'renameProject', ['p1']],
      ['remove-project:p1', 'removeProject', ['p1']],
      ['rename-session:acc-a', 'renameSession', ['acc-a']], ['hide-project:p1', 'hideProject', ['p1']],
      ['unhide-project:p1', 'unhideProject', ['p1']], ['hide-session:acc-a', 'hideSession', ['acc-a']],
      ['unhide-session:acc-a', 'unhideSession', ['acc-a']], ['pin-project:p1', 'pinProject', ['p1']],
      ['unpin-project:p1', 'unpinProject', ['p1']], ['kill-session:acc-a', 'killSession', ['acc-a']],
      ['collapse-all', 'collapseAll', []], ['expand-all', 'expandAll', []], ['show-hidden', 'setShowHidden', [true]],
      ['hide-hidden', 'setShowHidden', [false]], ['split-right', 'split', ['row']], ['split-down', 'split', ['column']],
      ['close-tab', 'closeTab', []], ['next-tab', 'nextTab', []], ['previous-tab', 'previousTab', []],
      ['theme-dark', 'setTheme', ['dark']], ['theme-light', 'setTheme', ['light']], ['theme-system', 'setTheme', ['system']],
      ['shortcuts', 'shortcuts', []], ['sign-out', 'signOut', []],
    ] as const
    for (const [id, name, args] of actions) {
      expect(dispatchPaletteAction(id, handlers)).toBe(true)
      expect(handlers[name]).toHaveBeenLastCalledWith(...args)
    }
    expect(dispatchPaletteAction('missing', handlers)).toBe(false)
  })
})
