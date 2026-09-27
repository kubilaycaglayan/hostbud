import { describe, expect, it } from 'vitest'
import { outputRuns } from './terminalOutput'
import { darkTerminalTheme } from './theme'

describe('native terminal output', () => {
  it('preserves multiline Unicode and literal markup without producing HTML', () => {
    const text = 'older conversation\n  indented café 🐈\n<script>alert(1)</script>'
    expect(outputRuns(text, darkTerminalTheme).map((r) => r.text).join('')).toBe(text)
  })
  it('preserves SGR colors and formatting and resets them', () => {
    const runs = outputRuns('\x1b[1;3;4;31mred\x1b[0m normal\x1b[38;2;12;34;56;48;5;196mrgb', darkTerminalTheme)
    expect(runs[0]).toMatchObject({ text: 'red', style: { color: darkTerminalTheme.red, fontWeight: 'bold', fontStyle: 'italic', textDecoration: 'underline' } })
    expect(runs[1].style.color).toBeUndefined()
    expect(runs[1].style.fontWeight).toBeUndefined()
    expect(runs[2].style).toMatchObject({ color: 'rgb(12, 34, 56)', backgroundColor: 'rgb(255, 0, 0)' })
  })
  it('discards terminal controls and hyperlink destinations while retaining labels', () => {
    expect(outputRuns('before\x1b[2J\x1b]8;;https://example.com\x07label\x1b]8;;\x07after', darkTerminalTheme).map((r) => r.text).join('')).toBe('beforelabelafter')
  })
})
