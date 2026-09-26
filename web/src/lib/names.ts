// Same rule as the backend (internal/tmux.ValidateName).
export const SESSION_NAME_RE = /^[A-Za-z0-9_-]{1,64}$/

/** An inline validation message, or '' when the name is fine. */
export function sessionNameError(name: string, required = false): string {
  if (name === '') return required ? 'Enter a name.' : ''
  if (name.length > 64) return 'Use at most 64 characters.'
  if (!SESSION_NAME_RE.test(name)) return "Use only letters, digits, '-' and '_'."
  return ''
}
