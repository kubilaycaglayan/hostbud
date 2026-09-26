import { Terminal } from '@xterm/xterm'
import { describe, expect, it } from 'vitest'
import { keepScrollback, onlyAltScreen } from './scrollback'

/** A real (unopened) xterm; its parser and buffers work without a DOM. */
async function term(seq: string, keep = true) {
  const t = new Terminal({ cols: 20, rows: 5, scrollback: 100, allowProposedApi: true })
  if (keep) keepScrollback(t)
  await new Promise<void>((r) => t.write(seq, r))
  return t
}

function lines(t: Terminal): string[] {
  const b = t.buffer.active
  const out: string[] = []
  for (let i = 0; i < b.length; i++) out.push(b.getLine(i)?.translateToString(true) ?? '')
  return out
}

// What tmux does: enter the alternate screen, keep the bottom row for its
// status line (region rows 1-4), print lines at the region's bottom.
const ALT = '\x1b[?1049h'
const REGION = '\x1b[1;4r'
const numbered = (from: number, to: number) =>
  Array.from({ length: to - from + 1 }, (_, i) => `\x1b[4;1H\n\x1b[4;1Hline-${from + i}`).join('')

describe('keepScrollback', () => {
  it('the alternate-screen switch is ignored, so line feeds fill the scrollback', async () => {
    const t = await term(ALT + REGION + '\x1b[5;1Hstatus' + numbered(1, 10))
    expect(t.buffer.active.type).toBe('normal')
    expect(lines(t)).toContain('line-1')
    expect(lines(t).at(-1)).toBe('status') // the status row stays put
    expect(t.buffer.active.length).toBeGreaterThan(t.rows)
  })

  it("without it, xterm's alternate screen keeps nothing (why this exists)", async () => {
    const t = await term(ALT + REGION + numbered(1, 10), false)
    expect(t.buffer.active.type).toBe('alternate')
    expect(lines(t)).not.toContain('line-1')
  })

  it('CSI n S saves the lines it scrolls off a top-anchored region', async () => {
    const t = await term(REGION + '\x1b[1;1Ha\r\nb\r\nc\r\nd\x1b[5;1Hstatus\x1b[3S')
    expect(lines(t)).toEqual(['a', 'b', 'c', 'd', '', '', '', 'status'])
  })

  it('CSI S in a region below the top row keeps its default (nothing saved)', async () => {
    const t = await term('\x1b[2;4r\x1b[1;1Htop\r\nb\r\nc\r\nd\x1b[2S')
    expect(t.buffer.active.length).toBe(t.rows)
    expect(lines(t).slice(0, 2)).toEqual(['top', 'd'])
  })

  it('other private modes still work', async () => {
    const t = await term('\x1b[?25l')
    expect(onlyAltScreen([25])).toBe(false)
    expect(onlyAltScreen([1049])).toBe(true)
    expect(onlyAltScreen([1049, 25])).toBe(false)
    expect(onlyAltScreen([])).toBe(false)
    t.dispose()
  })
})
