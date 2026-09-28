import { describe, expect, it } from 'vitest'
import type { ITheme } from '@xterm/xterm'
import { contrastRatio, darkTerminalTheme, dimmedTerminalTheme, lightTerminalTheme, solarizedTerminalTheme, TERMINAL_MIN_CONTRAST } from './theme'

// Long-lived terminal contrast (M8 T7).
const TEXT_KEYS = ['foreground', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white', 'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'] as const

describe('terminal palette contrast (M8 T7)', () => {
  it('computes WCAG contrast ratios', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 5)
    expect(contrastRatio('#777777', '#777777')).toBe(1)
  })

  it.each([
    ['dark', darkTerminalTheme],
    ['light', lightTerminalTheme],
    ['solarized 25', solarizedTerminalTheme],
    ['dimmed 70', dimmedTerminalTheme],
  ] as [string, ITheme][])('keeps every hostbud text color readable on the %s background', (_name, theme) => {
    for (const key of TEXT_KEYS) {
      expect(contrastRatio(theme[key]!, theme.background!), key).toBeGreaterThanOrEqual(4.5)
    }
  })

  it("reproduces the report: default text on a prompt background computed for the other theme", () => {
    // Codex paints its prompt with an explicit background derived at startup
    // from the (dark) default background, about 12 % toward white, and draws
    // its text in the default foreground, which the theme switch flips.
    const promptOnDark = '#2c2e32'
    expect(contrastRatio(promptOnDark, darkTerminalTheme.foreground!)).toBeGreaterThan(7)
    expect(contrastRatio(promptOnDark, lightTerminalTheme.foreground!)).toBeLessThan(1.5)
    // hostbud's side: xterm enforces at least WCAG AA per cell, against the
    // cell's actual background (xterm's ensureContrastRatio).
    expect(TERMINAL_MIN_CONTRAST).toBeGreaterThanOrEqual(4.5)
  })
})
