// Keeps tmux's output in xterm's scrollback (docs/ARCHITECTURE.md §6).
//
// tmux draws in the terminal's alternate screen, and xterm keeps no
// scrollback there, so a browser terminal attached to tmux would have none
// to search or scroll. Two changes on the xterm side (tmux's config is the
// user's and stays untouched):
//  - the alternate-screen switches (DECSET/DECRST 1049, 1047, 47) are
//    ignored, so tmux draws on the normal screen, where lines scrolled off
//    the top go to the scrollback;
//  - "scroll up n lines" (CSI n S, which tmux uses for bursts of output)
//    saves the lines it scrolls off a region that starts at the top row, as
//    a line feed does, instead of dropping them.
// Lines tmux never sends (it skips what scrolls past between two screen
// updates) can't be recovered here; tmux copy mode has them.
import type { IDisposable, Terminal } from '@xterm/xterm'

const ALT_SCREEN_MODES = new Set([47, 1047, 1049])

/** True if every parameter of a DECSET/DECRST is an alternate-screen mode. */
export function onlyAltScreen(params: (number | number[])[]): boolean {
  return params.length > 0 && params.every((p) => typeof p === 'number' && ALT_SCREEN_MODES.has(p))
}

// xterm 6 internals used for CSI S (no public API scrolls into the
// scrollback). If they're not as expected, xterm's default runs instead.
interface Internals {
  _bufferService: { scroll(eraseAttr: unknown): void }
  _inputHandler: {
    _activeBuffer: { scrollTop: number; scrollBottom: number }
    _eraseAttrData(): unknown
    _dirtyRowTracker: { markRangeDirty(y1: number, y2: number): void }
  }
}

function internals(term: Terminal): Internals | null {
  const core = (term as unknown as { _core?: Partial<Internals> })._core
  const ih = core?._inputHandler
  if (
    typeof core?._bufferService?.scroll !== 'function' ||
    typeof ih?._eraseAttrData !== 'function' ||
    typeof ih._dirtyRowTracker?.markRangeDirty !== 'function' ||
    typeof ih._activeBuffer?.scrollTop !== 'number'
  )
    return null
  return core as Internals
}

export function keepScrollback(term: Terminal): IDisposable {
  const swallow = (params: (number | number[])[]) => onlyAltScreen(params)
  const handlers = [
    term.parser.registerCsiHandler({ prefix: '?', final: 'h' }, swallow),
    term.parser.registerCsiHandler({ prefix: '?', final: 'l' }, swallow),
    term.parser.registerCsiHandler({ final: 'S' }, (params) => {
      if (term.buffer.active.type !== 'normal') return false
      const core = internals(term)
      const buf = core?._inputHandler._activeBuffer
      if (!core || !buf || buf.scrollTop !== 0) return false
      const n = typeof params[0] === 'number' && params[0] > 0 ? params[0] : 1
      const erase = core._inputHandler._eraseAttrData()
      for (let i = 0; i < Math.min(n, term.rows); i++) core._bufferService.scroll(erase)
      core._inputHandler._dirtyRowTracker.markRangeDirty(buf.scrollTop, buf.scrollBottom)
      return true
    }),
  ]
  return { dispose: () => handlers.forEach((h) => h.dispose()) }
}
