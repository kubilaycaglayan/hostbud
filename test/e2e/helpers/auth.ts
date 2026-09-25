import { readFileSync } from 'node:fs'
import { randomBytes } from 'node:crypto'

// Where global setup saves the signed-in session every scenario starts with.
export const STORAGE_STATE = 'results/.auth.json'

/** A throwaway account (example.com addresses only). */
export function newAccount(prefix = 'e2e') {
  const id = randomBytes(4).toString('hex')
  return { email: `${prefix}-${id}@example.com`, password: `pw-${randomBytes(9).toString('hex')}` }
}

/** "hostbud_session=<token>" from the saved session, for raw requests. */
export function sessionCookieHeader(): string {
  const state = JSON.parse(readFileSync(STORAGE_STATE, 'utf8')) as { cookies: { name: string; value: string }[] }
  const c = state.cookies.find((x) => x.name === 'hostbud_session')
  if (!c) throw new Error('no hostbud_session cookie in the saved state')
  return `${c.name}=${c.value}`
}

// Throttling settings of the e2e app (compose.yml).
export const LOGIN_MAX_FAILURES = Number(process.env.E2E_LOGIN_MAX_FAILURES ?? 3)
export const IP_MAX_FAILURES = Number(process.env.E2E_IP_MAX_FAILURES ?? 6)
