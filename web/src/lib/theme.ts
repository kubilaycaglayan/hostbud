import type { ITheme } from '@xterm/xterm'

export type ThemeMode = 'dark' | 'light' | 'system'
export type ResolvedTheme = 'dark' | 'light'

export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  return mode === 'system' ? (prefersDark ? 'dark' : 'light') : mode
}

export const darkTerminalTheme: ITheme = {
  background: '#0f1115', foreground: '#d7dae0', cursor: '#5fb3f9', selectionBackground: '#264f78',
  black: '#1b1d23', red: '#f47067', green: '#57ab5a', yellow: '#f0c674', blue: '#5fb3f9', magenta: '#d2a8ff', cyan: '#76d9e6', white: '#d7dae0',
  brightBlack: '#8b919c', brightRed: '#ff938a', brightGreen: '#7bd87e', brightYellow: '#ffe08a', brightBlue: '#80c8ff', brightMagenta: '#e3bcff', brightCyan: '#9af0f5', brightWhite: '#ffffff',
}

export const lightTerminalTheme: ITheme = {
  background: '#ffffff', foreground: '#1f2328', cursor: '#0969da', selectionBackground: '#b6d7ff',
  black: '#24292f', red: '#cf222e', green: '#1a7f37', yellow: '#7d4e00', blue: '#0550ae', magenta: '#8250df', cyan: '#055d72', white: '#6e7781',
  brightBlack: '#57606a', brightRed: '#a40e26', brightGreen: '#116329', brightYellow: '#633c01', brightBlue: '#033d8b', brightMagenta: '#6639ba', brightCyan: '#0550ae', brightWhite: '#1f2328',
}

/**
 * xterm raises (or lowers) a cell's foreground until it reaches this WCAG
 * contrast ratio against the cell's actual background (M8 T7; 4.5 is WCAG AA
 * and VS Code's terminal default). Long-lived clients keep colors they
 * computed for the old theme: Codex queries the default colors (OSC 10/11)
 * once and paints its prompt with an explicit background derived from them,
 * while its text uses the default foreground. After a System switch from
 * dark to light that text turned dark on the still-dark prompt; with this it
 * stays legible without hostbud overriding the client's own colors.
 */
export const TERMINAL_MIN_CONTRAST = 4.5

/** WCAG 2 contrast ratio between two #rrggbb colors. */
export function contrastRatio(a: string, b: string): number {
  const lum = (hex: string) => {
    const [r, g, bl] = [1, 3, 5].map((i) => {
      const c = parseInt(hex.slice(i, i + 2), 16) / 255
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * r + 0.7152 * g + 0.0722 * bl
  }
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

export const searchDecorations = {
  dark: { matchBackground: '#264f78', matchOverviewRuler: '#5fb3f9', activeMatchBackground: '#9a6700', activeMatchColorOverviewRuler: '#f0c674' },
  light: { matchBackground: '#b6d7ff', matchOverviewRuler: '#0969da', activeMatchBackground: '#ffdf5d', activeMatchColorOverviewRuler: '#9a6700' },
}

export function validateTheme(value: unknown): ThemeMode {
  if (!value || typeof value !== 'object') return 'system'
  const state = value as { version?: unknown; mode?: unknown }
  return state.version === 1 && (state.mode === 'dark' || state.mode === 'light' || state.mode === 'system') ? state.mode : 'system'
}

export function themeBootValue(storage: Pick<Storage, 'getItem'> | null, prefersDark: boolean): ResolvedTheme {
  try {
    const mode = storage?.getItem('hostbud.theme')
    return resolveTheme(mode === 'dark' || mode === 'light' || mode === 'system' ? mode : 'system', prefersDark)
  } catch {
    return resolveTheme('system', prefersDark)
  }
}
