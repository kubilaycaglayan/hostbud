/**
 * macOS "natural text editing" keys for the terminal: the bytes a Mac
 * terminal (iTerm2's preset) sends for Option/Cmd editing shortcuts, which
 * readline, zsh and most line editors understand. Option is `altKey`, Cmd is
 * `metaKey`. Only these exact modifier combinations map; anything else
 * (Shift, Ctrl, two modifiers) is left to xterm and the browser.
 */
const ESC = '\x1b'

const OPTION: Record<string, string> = {
  Backspace: ESC + '\x7f', // delete the previous word
  ArrowLeft: ESC + 'b', // back a word
  ArrowRight: ESC + 'f', // forward a word
}

const CMD: Record<string, string> = {
  Backspace: '\x15', // Ctrl-U: delete to the start of the line
  ArrowLeft: '\x01', // Ctrl-A: start of the line
  ArrowRight: '\x05', // Ctrl-E: end of the line
}

type Keys = Pick<KeyboardEvent, 'key' | 'altKey' | 'metaKey' | 'ctrlKey' | 'shiftKey'>

/** The bytes to send for an editing shortcut, or undefined if it isn't one. */
export function editingKey(ev: Keys): string | undefined {
  if (ev.ctrlKey || ev.shiftKey || ev.altKey === ev.metaKey) return undefined
  return (ev.altKey ? OPTION : CMD)[ev.key]
}
