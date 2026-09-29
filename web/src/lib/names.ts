// Same rule as the backend (internal/tmux.ValidateName).
export const SESSION_NAME_RE = /^[A-Za-z0-9_-]{1,64}$/

/** Whitespace in a typed session name becomes '-' ("new session" → "new-session"),
 *  like the backend (internal/tmux.NormalizeName). */
export function normalizeSessionName(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).join('-')
}

/** An inline validation message, or '' when the name is fine. */
export function sessionNameError(name: string, required = false): string {
  if (name === '') return required ? 'Enter a name.' : ''
  if (name.length > 64) return 'Use at most 64 characters.'
  if (!SESSION_NAME_RE.test(name)) return "Use only letters, digits, '-' and '_'."
  return ''
}

/** Project names are trimmed and limited by UTF-8 byte length on the server. */
export function projectNameError(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) return 'Enter a name.'
  if (new TextEncoder().encode(trimmed).byteLength > 255) return 'Use 255 bytes or fewer.'
  return ''
}

/** Any text as a session name base, like the backend (internal/session.SanitizeName). */
export function sanitizeSessionName(text: string): string {
  let name = text.replace(/[^A-Za-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '')
  if (name.length > 60) name = name.slice(0, 60).replace(/-+$/, '')
  return name || 'session'
}

/** The name the backend would derive for a directory: its last segment
 *  ("~" and "~/…" resolved against home; "/" → "root"). */
export function directorySessionName(dir: string, home = ''): string {
  let p = dir.trim()
  if (p === '' || p === '~') p = home
  else if (p.startsWith('~/')) p = `${home.replace(/\/+$/, '')}/${p.slice(2)}`
  const trimmed = p.replace(/\/+$/, '')
  if (p.startsWith('/') && trimmed === '') return 'root'
  return sanitizeSessionName(trimmed.slice(trimmed.lastIndexOf('/') + 1))
}

/** base, or base-1, base-2, … when taken (internal/session.uniqueName). */
export function uniqueSessionName(base: string, taken: Iterable<string>): string {
  const names = new Set(taken)
  if (!names.has(base)) return base
  for (let n = 1; ; n++) {
    const suffix = `-${n}`
    const name = base.slice(0, 64 - suffix.length) + suffix
    if (!names.has(name)) return name
  }
}
