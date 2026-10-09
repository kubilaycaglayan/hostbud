// Terminal clipboard: copy the selection, paste as bracketed paste, and OSC 52
// writes from programs on the host (write-only; docs/ARCHITECTURE.md §6).
import { ClipboardAddon, type IClipboardProvider } from '@xterm/addon-clipboard'
import type { Terminal } from '@xterm/xterm'
import { useToastsStore } from '@/stores/toasts'

/** OSC 52 payloads larger than this (decoded) are ignored. */
export const OSC52_MAX_BYTES = 1 << 20

const BLOCKED = 'The browser blocked clipboard access.'

/** Copies the terminal's selection (kept in place); a toast if the browser
 * refuses (no permission, or not a secure context). */
export async function copySelection(term: Terminal): Promise<void> {
  const text = term.getSelection()
  if (!text) return
  const joined = text.replace(/[\r\n]/g, '')
  let copyText = text
  if (/[\r\n]/.test(text) && !/[ \t]/.test(text)) {
    try {
      const url = new URL(joined)
      if (url.protocol === 'http:' || url.protocol === 'https:') copyText = joined
    } catch {
      // Preserve ordinary multiline terminal selections.
    }
  }
  try {
    await navigator.clipboard.writeText(copyText)
  } catch {
    useToastsStore().push({ title: "Couldn't copy", message: BLOCKED })
  }
}

/** Pastes the clipboard through xterm, which brackets it (ESC[200~ … ESC[201~)
 * when the program asked for bracketed paste. */
export async function pasteClipboard(term: Terminal): Promise<void> {
  let text: string
  try {
    text = await navigator.clipboard.readText()
  } catch {
    useToastsStore().push({ title: "Couldn't paste", message: BLOCKED })
    return
  }
  term.paste(text)
}

/**
 * OSC 52 clipboard for programs on the host (tmux copy mode, vim, Claude
 * Code). Writes go to the browser clipboard; any selection parameter (tmux
 * sends an empty one) means the system clipboard. Reads never get here
 * (installOsc52 swallows them). The write isn't awaited, so a slow or
 * refused browser never stalls the terminal's parser.
 */
export class WriteOnlyClipboard implements IClipboardProvider {
  constructor(private readonly write: (text: string) => Promise<void> = (t) => navigator.clipboard.writeText(t)) {}

  readText(): string {
    return ''
  }

  writeText(_selection: string, text: string): void {
    // An empty text is a clear request or an undecodable payload: ignored,
    // like oversized ones.
    if (!text || new TextEncoder().encode(text).length > OSC52_MAX_BYTES) return
    this.write(text).catch((e: unknown) => console.debug('hostbud: OSC 52 write refused by the browser', e))
  }
}

/** True for an OSC 52 clipboard query (`52;<sel>;?`). */
export function isOsc52Query(data: string): boolean {
  return data.split(';')[1] === '?'
}

/**
 * Loads OSC 52 support into the terminal: writes reach the clipboard, and
 * queries are answered with nothing, so no program on the host can read the
 * browser's clipboard. The query guard is registered after the addon, so
 * xterm runs it first; returning true stops the addon from seeing it.
 */
export function installOsc52(term: Terminal, provider: IClipboardProvider = new WriteOnlyClipboard()): void {
  term.loadAddon(new ClipboardAddon(undefined, provider))
  term.parser.registerOscHandler(52, (data) => isOsc52Query(data))
}
