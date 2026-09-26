// Links in the terminal (docs/ARCHITECTURE.md §6): printed URLs (web-links
// addon) and OSC 8 hyperlinks (xterm's linkHandler). Only http(s) opens, in
// a new tab that gets no handle on hostbud.
import type { ILinkHandler } from '@xterm/xterm'

/** True for an http: or https: URL. */
export function isWebLink(uri: string): boolean {
  try {
    const { protocol } = new URL(uri)
    return protocol === 'http:' || protocol === 'https:'
  } catch {
    return false
  }
}

type Open = (url: string, target: string, features: string) => unknown

/** Opens an http(s) URL in a new tab (noopener, noreferrer); anything else
 * (javascript:, file:, data:, ssh:, …) is ignored. */
export function openLink(uri: string, open: Open = (u, t, f) => window.open(u, t, f)): boolean {
  if (!isWebLink(uri)) return false
  open(uri, '_blank', 'noopener,noreferrer')
  return true
}

/** Where to show an OSC 8 link's real target (page pixels), or null. */
export interface LinkHover {
  url: string
  x: number
  y: number
}

/**
 * xterm's handler for OSC 8 hyperlinks. Their text can differ from their
 * target, so hovering reports the target (shown as a tooltip).
 */
export function hyperlinkHandler(onHover: (h: LinkHover | null) => void, open?: Open): ILinkHandler {
  return {
    activate: (_ev, uri) => void openLink(uri, open),
    hover: (ev, uri) => onHover({ url: uri, x: ev.clientX, y: ev.clientY }),
    leave: () => onHover(null),
    allowNonHttpProtocols: false,
  }
}
