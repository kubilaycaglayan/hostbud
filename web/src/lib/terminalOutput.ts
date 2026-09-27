import type { CSSProperties } from 'vue'
import type { ITheme } from '@xterm/xterm'

export interface OutputRun { text: string; style: CSSProperties }
const paletteKeys = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'] as const

function color(index: number, theme: ITheme): string | undefined {
  if (!Number.isInteger(index) || index < 0 || index > 255) return undefined
  if (index < 16) return theme[paletteKeys[index]]
  if (index >= 232) {
    const level = 8 + (index - 232) * 10
    return `rgb(${level}, ${level}, ${level})`
  }
  const levels = [0, 95, 135, 175, 215, 255]
  const n = index - 16
  return `rgb(${levels[Math.floor(n / 36)]}, ${levels[Math.floor(n / 6) % 6]}, ${levels[n % 6]})`
}

/** Render only text and SGR attributes. Terminal control sequences never
 * execute in the browser, and output is interpolated by Vue, never HTML. */
export function outputRuns(output: string, theme: ITheme): OutputRun[] {
  const runs: OutputRun[] = []
  let foreground: string | undefined, background: string | undefined
  let bold = false, dim = false, italic = false, underline = false, strike = false, inverse = false, hidden = false
  const reset = () => {
    foreground = background = undefined
    bold = dim = italic = underline = strike = inverse = hidden = false
  }
  const append = (text: string) => {
    // Capture-pane is already rendered text; retain only printable text, tabs and newlines.
    // eslint-disable-next-line no-control-regex
    text = text.replace(/[\x00-\x08\x0b-\x1f\x7f]/g, '')
    if (!text) return
    runs.push({ text, style: {
      color: inverse ? background ?? theme.background : foreground,
      backgroundColor: inverse ? foreground ?? theme.foreground : background,
      fontWeight: bold ? 'bold' : undefined,
      fontStyle: italic ? 'italic' : undefined,
      opacity: dim ? 0.5 : undefined,
      visibility: hidden ? 'hidden' : undefined,
      textDecoration: [underline ? 'underline' : '', strike ? 'line-through' : ''].filter(Boolean).join(' ') || undefined,
    } })
  }
  // OSC strings (including hyperlinks) are discarded; only SGR CSI is interpreted.
  // eslint-disable-next-line no-control-regex
  const controls = /\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_]/g
  let end = 0
  for (const match of output.matchAll(controls)) {
    append(output.slice(end, match.index))
    end = match.index! + match[0].length
    if (!match[0].startsWith('\x1b[') || !match[0].endsWith('m')) continue
    const codes = match[0].slice(2, -1).split(';').map((n) => Number(n || 0))
    for (let i = 0; i < codes.length; i++) {
      const code = codes[i]
      if (code === 0) reset()
      else if (code === 1) bold = true
      else if (code === 2) dim = true
      else if (code === 3) italic = true
      else if (code === 4 || code === 21) underline = true
      else if (code === 7) inverse = true
      else if (code === 8) hidden = true
      else if (code === 9) strike = true
      else if (code === 22) bold = dim = false
      else if (code === 23) italic = false
      else if (code === 24) underline = false
      else if (code === 27) inverse = false
      else if (code === 28) hidden = false
      else if (code === 29) strike = false
      else if (code === 39) foreground = undefined
      else if (code === 49) background = undefined
      else if (code >= 30 && code <= 37) foreground = color(code - 30, theme)
      else if (code >= 90 && code <= 97) foreground = color(code - 90 + 8, theme)
      else if (code >= 40 && code <= 47) background = color(code - 40, theme)
      else if (code >= 100 && code <= 107) background = color(code - 100 + 8, theme)
      else if (code === 38 || code === 48) {
        let value: string | undefined
        if (codes[i + 1] === 5) { value = color(codes[i + 2], theme); i += 2 }
        else if (codes[i + 1] === 2) {
          const rgb = codes.slice(i + 2, i + 5)
          if (rgb.length === 3 && rgb.every((v) => Number.isInteger(v) && v >= 0 && v <= 255)) value = `rgb(${rgb.join(', ')})`
          i += 4
        }
        if (code === 38) foreground = value
        else background = value
      }
    }
  }
  append(output.slice(end))
  return runs
}
