// Typed client for hostbud's JSON API. Errors carry the server's
// {error, hint} shape (and Retry-After for 429s).

import type { Machine, Session } from './types'

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly hint?: string,
    readonly retryAfter?: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let error = `request failed (${res.status})`
    let hint: string | undefined
    try {
      const data = (await res.json()) as { error?: string; hint?: string }
      if (data.error) error = data.error
      hint = data.hint || undefined
    } catch {
      // not JSON: keep the generic message
    }
    const retry = Number(res.headers.get('Retry-After'))
    throw new ApiError(res.status, error, hint, Number.isFinite(retry) && retry > 0 ? retry : undefined)
  }
  // Always drain the body (even for 204): Chromium reports a response whose
  // body is never read as an aborted request.
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}


export interface Me {
  email: string
}

export const authApi = {
  me: () => request<Me>('GET', '/api/auth/me'),
  login: (email: string, password: string) => request<void>('POST', '/api/auth/login', { email, password }),
  register: (email: string, password: string) =>
    request<void>('POST', '/api/auth/register', { email, password }),
  logout: () => request<void>('POST', '/api/auth/logout'),
}

export interface CreateSession {
  name?: string
  path?: string
  startCommand?: string
}

const sessionsPath = (machine: string) => `/api/machines/${encodeURIComponent(machine)}/sessions`

export const sessionsApi = {
  machines: () => request<{ machines: Machine[] }>('GET', '/api/machines'),
  list: (machine: string) => request<{ sessions: Session[] }>('GET', sessionsPath(machine)),
  create: (machine: string, spec: CreateSession) =>
    request<{ name: string }>('POST', sessionsPath(machine), spec),
  rename: (machine: string, from: string, to: string) =>
    request<{ name: string }>('PATCH', `${sessionsPath(machine)}/${encodeURIComponent(from)}`, { name: to }),
  /** Kills a session: callers must have the user's confirmation. */
  kill: (machine: string, name: string) =>
    request<void>('DELETE', `${sessionsPath(machine)}/${encodeURIComponent(name)}`),
}

/** UI state keys the server accepts (internal/api/uistate.go). */
export type UIStateKey = 'layout'

/** The account's saved UI state, or null if nothing is saved yet. The value
 * is whatever was stored: callers validate it. */
export async function getUIState<T = unknown>(key: UIStateKey): Promise<T | null> {
  try {
    return await request<T>('GET', `/api/ui-state/${key}`)
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }
}

/** Saves the account's UI state (JSON, ≤ 64 KiB). */
export function putUIState(key: UIStateKey, value: unknown): Promise<void> {
  return request<void>('PUT', `/api/ui-state/${key}`, value)
}
