// Option/Alt-click moves the caret of the program's line editor (M8 T5).
//
// xterm's own `altClickMovesCursor` sends the whole arrow sequence at once,
// computed as if the text from the cursor to the click were one soft-wrapped
// line filling the terminal's full width (in the normal buffer, which hostbud
// always uses: scrollback.ts ignores tmux's alternate screen). That's wrong
// for multiline prompts (Codex, Claude Code: explicit newlines, prefixes,
// borders), tmux split panes and anything right of the text: the caret lands
// on unrelated lines. Here the move is closed-loop: send left/right arrows a
// row at a time, let the program redraw, read where its cursor really is and
// continue from there. Up/down are never sent (in a shell they recall
// history). The caret ends on the clicked cell, or as close as the editor
// lets it get (end of a shorter line; start of the editable text).

export interface Cell {
  /** 0-based column and viewport row. */
  x: number
  y: number
}

export interface CaretHost {
  cursor(): Cell
  /** The viewport row's text (trailing blanks may be trimmed). */
  rowText(y: number): string
  send(data: string): void
  /** Resolves once the program's reaction to the last send has been drawn
   * (or nothing came within a short wait). */
  settle(): Promise<void>
  /** True once the move should stop (another click, a key press, unmount). */
  cancelled(): boolean
}

type Dir = 'left' | 'right'

const before = (a: Cell, b: Cell) => a.y < b.y || (a.y === b.y && a.x < b.x)
const same = (a: Cell, b: Cell) => a.x === b.x && a.y === b.y

export function arrow(dir: Dir, applicationCursor: boolean): string {
  return `\x1b${applicationCursor ? 'O' : '['}${dir === 'right' ? 'C' : 'D'}`
}

/** Arrows likely needed from `cur` toward `target`: the exact distance on
 * the same row, otherwise just enough to leave the current row's text. */
export function estimate(cur: Cell, target: Cell, dir: Dir, rowText: string): number {
  if (cur.y === target.y) return Math.abs(target.x - cur.x)
  if (dir === 'right') return Math.max(1, rowText.trimEnd().length - cur.x + 1)
  const start = rowText.length - rowText.trimStart().length
  return Math.max(1, cur.x - start + 1)
}

/** Moves the caret toward `target`. Returns true when it ends on the
 * target's row (on the cell itself when the editor allows it). */
export async function moveCaret(host: CaretHost, target: Cell, applicationCursor = false, maxRounds = 60): Promise<boolean> {
  let lastDir: Dir | undefined
  let lastCount = 0
  let cap = Infinity
  for (let round = 0; round < maxRounds && !host.cancelled(); round++) {
    const cur = host.cursor()
    if (same(cur, target)) return true
    const dir: Dir = before(cur, target) ? 'right' : 'left'
    if (lastDir && dir !== lastDir) {
      // Stepping back and forth over a cell the editor can't reach (past the
      // end of a line): settle on the target's row.
      if (lastCount === 1) {
        if (cur.y === target.y) return true
        host.send(arrow(dir, applicationCursor))
        await host.settle()
        return host.cursor().y === target.y
      }
      cap = Math.max(1, Math.floor(lastCount / 2)) // overshot: damp
    }
    const count = Math.min(estimate(cur, target, dir, host.rowText(cur.y)), cap)
    host.send(arrow(dir, applicationCursor).repeat(count))
    await host.settle()
    // The editor didn't move: its text ends (or starts) before the target.
    if (same(host.cursor(), cur)) return cur.y === target.y
    lastDir = dir
    lastCount = count
  }
  return host.cursor().y === target.y
}

/** The viewport cell under a pointer, given the terminal's screen box. */
export function cellAt(point: { clientX: number; clientY: number }, screen: DOMRect, cols: number, rows: number): Cell {
  const clamp = (v: number, max: number) => Math.min(Math.max(v, 0), max - 1)
  return {
    x: clamp(Math.floor(((point.clientX - screen.left) / screen.width) * cols), cols),
    y: clamp(Math.floor(((point.clientY - screen.top) / screen.height) * rows), rows),
  }
}

/** Resolves once writes to the terminal have been quiet for `quietMs`, or
 * after `idleMs` if nothing arrives. */
export function settleAfterWrites(term: { onWriteParsed(fn: () => void): { dispose(): void } }, quietMs = 40, idleMs = 400): Promise<void> {
  return new Promise((resolve) => {
    const done = () => {
      sub.dispose()
      resolve()
    }
    let timer = setTimeout(done, idleMs)
    const sub = term.onWriteParsed(() => {
      clearTimeout(timer)
      timer = setTimeout(done, quietMs)
    })
  })
}
