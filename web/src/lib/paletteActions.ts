import type { SplitDir } from './layout'

export interface PaletteActionHandlers {
  newSession(): void
  newProjectSession(id: string): void
  browseFiles(): void
  queue(): void
  renameProject(id: string): void
  removeProject(id: string): void
  renameSession(name: string): void
  hideProject(id: string): void
  unhideProject(id: string): void
  hideSession(name: string): void
  unhideSession(name: string): void
  pinProject(id: string): void
  unpinProject(id: string): void
  killSession(name: string): void
  collapseAll(): void
  expandAll(): void
  setShowHidden(show: boolean): void
  split(dir: SplitDir): void
  closeTab(): void
  nextTab(): void
  previousTab(): void
  setTheme(mode: 'dark' | 'light' | 'system'): void
  shortcuts(): void
  signOut(): void
}

/** Runs a palette action through the same app/store handlers as its existing UI. */
export function dispatchPaletteAction(id: string, handlers: PaletteActionHandlers): boolean {
  if (id === 'split-right') { handlers.split('row'); return true }
  if (id === 'split-down') { handlers.split('column'); return true }
  const [action, ...rest] = id.split(':')
  const value = rest.join(':')
  switch (action) {
    case 'new-session': handlers.newSession(); break
    case 'new-project-session': handlers.newProjectSession(value); break
    case 'browse-files': handlers.browseFiles(); break
    case 'queue': handlers.queue(); break
    case 'rename-project': handlers.renameProject(value); break
    case 'remove-project': handlers.removeProject(value); break
    case 'rename-session': handlers.renameSession(value); break
    case 'hide-project': handlers.hideProject(value); break
    case 'unhide-project': handlers.unhideProject(value); break
    case 'hide-session': handlers.hideSession(value); break
    case 'unhide-session': handlers.unhideSession(value); break
    case 'pin-project': handlers.pinProject(value); break
    case 'unpin-project': handlers.unpinProject(value); break
    case 'kill-session': handlers.killSession(value); break
    case 'collapse-all': handlers.collapseAll(); break
    case 'expand-all': handlers.expandAll(); break
    case 'show-hidden': handlers.setShowHidden(true); break
    case 'hide-hidden': handlers.setShowHidden(false); break
    case 'close-tab': handlers.closeTab(); break
    case 'next-tab': handlers.nextTab(); break
    case 'previous-tab': handlers.previousTab(); break
    case 'theme-dark': handlers.setTheme('dark'); break
    case 'theme-light': handlers.setTheme('light'); break
    case 'theme-system': handlers.setTheme('system'); break
    case 'shortcuts': handlers.shortcuts(); break
    case 'sign-out': handlers.signOut(); break
    default: return false
  }
  return true
}
