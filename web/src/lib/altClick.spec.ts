import { describe, expect, it } from 'vitest'
import { arrow, estimate, moveCaret, type CaretHost, type Cell } from './altClick'

// A line editor drawn on a terminal screen: logical lines, each wrapped into
// rows between a prefix and a suffix. Left at a line's start moves to the end
// of the previous line and right at its end to the next line's start, as in
// readline (one line), Codex and Claude Code (several). It never moves
// before the first line's start (a shell prompt, the output above it).
interface EditorShape {
  cols: number
  top: number // first row of the editable text
  lines: string[]
  firstPrefix: string // the first row of the first line (a prompt)
  prefix: string // every other row
  suffix: string // right border
  above?: string[] // output rows above the editor
}

class Editor implements CaretHost {
  line = 0
  col = 0
  sent: string[] = []
  constructor(private shape: EditorShape, caret: [number, number]) {
    ;[this.line, this.col] = caret
  }

  private layout() {
    const { cols, lines, firstPrefix, prefix, suffix, top } = this.shape
    const rows: string[] = []
    const where = new Map<string, Cell>()
    lines.forEach((text, li) => {
      let c = 0
      let first = true
      do {
        const pre = li === 0 && first ? firstPrefix : prefix
        const width = cols - pre.length - suffix.length
        const chunk = text.slice(c, c + width)
        for (let i = 0; i <= chunk.length && c + i <= text.length; i++) {
          if (i === chunk.length && chunk.length === width && c + i < text.length) break
          if (!where.has(`${li}:${c + i}`)) where.set(`${li}:${c + i}`, { x: pre.length + i, y: top + rows.length })
        }
        rows.push(pre + chunk.padEnd(width) + suffix)
        c += width
        first = false
      } while (c < text.length)
    })
    return { rows, where }
  }

  cursor(): Cell {
    return this.layoutFor(this.col, this.line)
  }

  /** Where the caret at a line/column is drawn. */
  layoutFor(col: number, line = 0): Cell {
    return this.layout().where.get(`${line}:${col}`)!
  }

  rowText(y: number): string {
    const above = this.shape.above ?? []
    if (y < this.shape.top) return above[y] ?? ''
    return this.layout().rows[y - this.shape.top] ?? ''
  }

  send(data: string) {
    this.sent.push(data)
    for (const m of data.matchAll(/\x1b[[O]([A-D])/g)) {
      const lines = this.shape.lines
      if (m[1] === 'C') {
        if (this.col < lines[this.line].length) this.col++
        else if (this.line < lines.length - 1) [this.line, this.col] = [this.line + 1, 0]
      } else if (m[1] === 'D') {
        if (this.col > 0) this.col--
        else if (this.line > 0) [this.line, this.col] = [this.line - 1, lines[this.line - 1].length]
      } else throw new Error('vertical arrow sent')
    }
  }

  settle() {
    return Promise.resolve()
  }

  cancelled() {
    return false
  }
}

const tui = (lines: string[], caret: [number, number]) =>
  new Editor({ cols: 40, top: 2, lines, firstPrefix: '│ > ', prefix: '│   ', suffix: ' │', above: ['codex', '╭──'] }, caret)

describe('Option-click caret placement (M8 T5)', () => {
  it('moves along one row exactly, in both directions', async () => {
    const shell = new Editor({ cols: 80, top: 5, lines: ['echo hello world'], firstPrefix: 'dev@server-a:~$ ', prefix: '', suffix: '' }, [0, 16])
    expect(await moveCaret(shell, { x: 16 + 5, y: 5 })).toBe(true)
    expect(shell.col).toBe(5)
    expect(shell.sent).toEqual([arrow('left', false).repeat(11)])
    expect(await moveCaret(shell, { x: 16 + 11, y: 5 })).toBe(true)
    expect(shell.col).toBe(11)
  })

  it('crosses soft-wrapped rows of a long shell line (beginning, middle, end)', async () => {
    const text = 'x'.repeat(50) + 'MIDDLE' + 'y'.repeat(120)
    const shell = new Editor({ cols: 80, top: 0, lines: [text], firstPrefix: '$ ', prefix: '', suffix: '' }, [0, text.length])
    for (const col of [0, 3, 78, 100, text.length - 1, 50]) {
      await moveCaret(shell, shell.layoutFor(col))
      expect(shell.col).toBe(col)
    }
  })

  it("reproduces the report: xterm's one-shot arrows land on an unrelated line of a multiline prompt", () => {
    // xterm 6 moveToCellSequence in the normal buffer: the cells between the
    // cursor and the click, counted as one wrapped line of full width.
    const editor = tui(['first line of the prompt', 'second', 'third line', 'end'], [3, 3])
    const cur = editor.cursor()
    const target = editor.layoutFor(12, 0)
    const cols = 40
    const cells = (cols - target.x) + (cur.y - target.y - 1) * cols + 1 + (cur.x - 1)
    editor.send(arrow('left', false).repeat(cells))
    expect([editor.line, editor.col]).not.toEqual([0, 12])
  })

  it('lands on the clicked line and column of a bordered multiline prompt', async () => {
    const lines = ['first line of the prompt', 'second', 'a third line that wraps around the box edge', 'end']
    const editor = tui(lines, [3, 3])
    for (const [line, col] of [[0, 0], [0, 12], [2, 40], [1, 3], [3, 0], [2, 5], [0, 24]] as const) {
      const target = editor.layoutFor(col, line)
      expect(await moveCaret(editor, target)).toBe(true)
      expect([editor.line, editor.col]).toEqual([line, col])
    }
    expect(editor.sent.join('')).not.toMatch(/\x1b[[O][AB]/)
  })

  it('clicking right of a shorter line puts the caret at its end', async () => {
    const editor = tui(['a long first line here', 'short', 'another long line here'], [0, 0])
    await moveCaret(editor, { x: 30, y: 3 })
    expect([editor.line, editor.col]).toEqual([1, 5])
    editor.line = 2
    editor.col = 10
    await moveCaret(editor, { x: 30, y: 3 })
    expect([editor.line, editor.col]).toEqual([1, 5])
  })

  it('never leaves the editable text: a click on the prompt or the output above stops at its start', async () => {
    const shell = new Editor({ cols: 80, top: 3, lines: ['ls -la'], firstPrefix: 'dev@server-a:~$ ', prefix: '', suffix: '', above: ['total 0', 'a', 'b'] }, [0, 6])
    expect(await moveCaret(shell, { x: 2, y: 3 })).toBe(true)
    expect(shell.col).toBe(0)
    expect(await moveCaret(shell, { x: 2, y: 0 })).toBe(false)
    expect(shell.col).toBe(0)
    expect(shell.sent.join('')).not.toMatch(/\x1b[[O][AB]/)
  })

  it('uses application-cursor arrows when the program asked for them', async () => {
    const shell = new Editor({ cols: 80, top: 0, lines: ['abc'], firstPrefix: '', prefix: '', suffix: '' }, [0, 3])
    await moveCaret(shell, { x: 1, y: 0 }, true)
    expect(shell.sent).toEqual(['\x1bOD\x1bOD'])
  })

  it('stops when cancelled (another click or a key press)', async () => {
    const shell = new Editor({ cols: 80, top: 0, lines: ['abc'], firstPrefix: '', prefix: '', suffix: '' }, [0, 3])
    shell.cancelled = () => true
    await moveCaret(shell, { x: 0, y: 0 })
    expect(shell.sent).toEqual([])
  })

  it('estimates one row at a time off the current row', () => {
    expect(estimate({ x: 4, y: 1 }, { x: 9, y: 1 }, 'right', '')).toBe(5)
    expect(estimate({ x: 4, y: 1 }, { x: 0, y: 3 }, 'right', '│ > abcdef     │')).toBe(13)
    expect(estimate({ x: 6, y: 3 }, { x: 0, y: 1 }, 'left', '   abcdef')).toBe(4)
    expect(estimate({ x: 0, y: 3 }, { x: 0, y: 1 }, 'left', 'abc')).toBe(1)
  })
})
