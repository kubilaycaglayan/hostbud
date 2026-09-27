// Typed client for hostbud's JSON API. Errors carry the server's
// {error, hint} shape (and Retry-After for 429s).

import type { Machine, Project, Session, TmuxWindows } from './types'

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

export async function request<T>(method: Method, path: string, body?: unknown, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    ...init,
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
  me: () => request<Me>('GET', '/api/auth/me', undefined, { signal: AbortSignal.timeout(8_000) }),
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

export const windowsApi = {
  list: (machine: string, name: string) => request<TmuxWindows>(
    'GET', `${sessionsPath(machine)}/${encodeURIComponent(name)}/windows`, undefined, { signal: AbortSignal.timeout(10_000) },
  ),
  select: (machine: string, name: string, window: string, pane?: string) => request<TmuxWindows>(
    'POST', `${sessionsPath(machine)}/${encodeURIComponent(name)}/select`, { window, ...(pane === undefined ? {} : { pane }) }, { signal: AbortSignal.timeout(10_000) },
  ),
}

export type CopyModeAction = 'enter' | 'scroll-up' | 'scroll-down' | 'page-up' | 'page-down' | 'top' | 'bottom' | 'exit'
export interface CopyModeState {
  inMode: boolean
  scrollPosition: number
  historySize: number
}

export const copyModeApi = {
  action: (machine: string, name: string, action: CopyModeAction, lines?: number) => request<CopyModeState>(
    'POST',
    `${sessionsPath(machine)}/${encodeURIComponent(name)}/copy-mode`,
    { action, ...(lines === undefined ? {} : { lines }) },
    { signal: AbortSignal.timeout(10_000) },
  ),
}

export interface FileEntry {
  name: string
  path: string
  kind: string
  size: number
  modifiedAt: string
  symlinkState?: string
}

export const filesystemApi = {
  home: (machine: string) => request<{ path: string }>('GET', `/api/machines/${encodeURIComponent(machine)}/fs/home`),
  list: (machine: string, path: string, hidden: boolean) => request<{ path: string; entries: FileEntry[] }>(
    'GET', `/api/machines/${encodeURIComponent(machine)}/fs?path=${encodeURIComponent(path)}&hidden=${hidden}`,
  ),
  stat: (machine: string, path: string) => request<FileEntry & { symlink: boolean }>(
    'GET', `/api/machines/${encodeURIComponent(machine)}/fs/stat?path=${encodeURIComponent(path)}`,
  ),
  mkdir: (machine: string, path: string, name: string) => request<{ path: string }>(
    'POST', `/api/machines/${encodeURIComponent(machine)}/fs/mkdir`, { path, name },
  ),
}

export const projectsApi = {
  list: (machine: string) => request<{ projects: Project[] }>('GET', `/api/projects?machine=${encodeURIComponent(machine)}`),
  recentCommands: (id: string) => request<{ commands: string[] }>('GET', `/api/projects/${encodeURIComponent(id)}/recent-commands`),
  create: (machineId: string, path: string, name: string) => request<Project>('POST', '/api/projects', { machineId, path, name }),
  rename: (id: string, name: string) => request<Project>('PATCH', `/api/projects/${encodeURIComponent(id)}`, { name }),
  createSession: (id: string, spec: { name?: string; startCommand?: string }) =>
    request<{ name: string }>('POST', `/api/projects/${encodeURIComponent(id)}/sessions`, spec),
}

/** UI state keys the server accepts (internal/api/uistate.go). */
export type UIStateKey = 'layout' | 'tree'

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

/** Saves the account's UI state (JSON, ≤ 64 KiB). `keepalive` lets the
 * request outlive the page (a save on pagehide). */
export function putUIState(key: UIStateKey, value: unknown, opts: { keepalive?: boolean } = {}): Promise<void> {
  return request<void>('PUT', `/api/ui-state/${key}`, value, opts.keepalive ? { keepalive: true } : {})
}
