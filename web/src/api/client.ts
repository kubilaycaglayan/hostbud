// Typed client for hostbud's JSON API. Errors carry the server's
// {error, hint} shape (and Retry-After for 429s).

import type { Capacity, DefaultPrompt, HostKey, Machine, NotificationSettings, ParallelQueues, Project, Queue, QueueItem, QueueList, Session, TmuxWindows } from './types'

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

let execTimeoutMs = 10_000
let sftpTimeoutMs = 10_000
let uploadTimeoutMs = 300_000

/** Applies the authenticated server's configured exec deadline to remote API requests. */
export function setExecTimeoutMs(value: number) {
  if (Number.isFinite(value) && value >= 2_000 && value <= 120_000) execTimeoutMs = value
}

export function setSftpTimeoutMs(value: number) {
  if (Number.isFinite(value) && value >= 2_000 && value <= 120_000) sftpTimeoutMs = value
}

export function setUploadTimeoutMs(value: number) {
  if (Number.isFinite(value) && value >= 30_000 && value <= 600_000) uploadTimeoutMs = value
}

function execSignal(): AbortSignal {
  return AbortSignal.timeout(execTimeoutMs + 5_000)
}

function sftpSignal(): AbortSignal {
  return AbortSignal.timeout(sftpTimeoutMs + 5_000)
}

function uploadSignal(): AbortSignal {
  return AbortSignal.timeout(uploadTimeoutMs + 5_000)
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export async function request<T>(method: Method, path: string, body?: unknown, init: RequestInit = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      ...init,
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (error) {
    if (error instanceof DOMException && ['AbortError', 'TimeoutError'].includes(error.name)) {
      throw new ApiError(504, "hostbud didn't answer", 'The host may be busy; hostbud will retry.')
    }
    throw error
  }
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

export const runtimeApi = {
  async configureExecTimeout() {
    try {
      const { execTimeoutMs: value, sftpTimeoutMs: sftpValue, uploadTimeoutMs: uploadValue } = await request<{ execTimeoutMs: number; sftpTimeoutMs: number; uploadTimeoutMs: number }>('GET', '/api/runtime/limits')
      setExecTimeoutMs(value)
      setSftpTimeoutMs(sftpValue)
      setUploadTimeoutMs(uploadValue)
    } catch {
      // Keep the shipped default if configuration discovery is unavailable.
    }
  },
}

export interface CreateSession {
  name?: string
  path?: string
  startCommand?: string
}

/** One request's outcome when killing several sessions. */
export interface KillSessionsResult {
  killed: string[]
  failed: { name: string; error: string; hint?: string }[]
}

const sessionsPath = (machine: string) => `/api/machines/${encodeURIComponent(machine)}/sessions`

export const sessionsApi = {
  machines: () => request<{ machines: Machine[] }>('GET', '/api/machines'),
  list: (machine: string) => request<{ sessions: Session[] }>('GET', sessionsPath(machine)),
  create: (machine: string, spec: CreateSession) =>
    request<{ name: string }>('POST', sessionsPath(machine), spec, { signal: execSignal() }),
  rename: (machine: string, from: string, to: string) =>
    request<{ name: string }>('PATCH', `${sessionsPath(machine)}/${encodeURIComponent(from)}`, { name: to }, { signal: execSignal() }),
  /** Kills a session: callers must have the user's confirmation. */
  kill: (machine: string, name: string) =>
    request<void>('DELETE', `${sessionsPath(machine)}/${encodeURIComponent(name)}`, undefined, { signal: execSignal() }),
  /** Kills several sessions in one request, refreshing the list once; callers must have the user's confirmation. */
  killMany: (machine: string, names: string[]) =>
    request<KillSessionsResult>('POST', `${sessionsPath(machine)}/kill`, { names }, { signal: execSignal() }),
}

/** A server to add with the host keys the owner confirmed (V2-M13). */
export interface AddServer {
  label: string
  host: string
  port: number
  user: string
  hostKeys: { type: string; key: string }[]
}

export const serversApi = {
  /** Fetches a server's host keys for the owner to compare; nothing is trusted yet. */
  scan: (host: string, port: number) =>
    request<{ hostKeys: HostKey[] }>('POST', '/api/machines/scan', { host, port }, { signal: AbortSignal.timeout(20_000) }),
  add: (server: AddServer) => request<Machine>('POST', '/api/machines', server),
  update: (id: string, server: AddServer) => request<Machine>('PATCH', `/api/machines/${encodeURIComponent(id)}`, server),
  rename: (id: string, label: string) => request<Machine>('PATCH', `/api/machines/${encodeURIComponent(id)}`, { label }),
  /** Forgets a server (its tmux keeps running): callers must have the user's confirmation. */
  remove: (id: string) => request<void>('DELETE', `/api/machines/${encodeURIComponent(id)}`),
}

export const windowsApi = {
  list: (machine: string, name: string) => request<TmuxWindows>(
    'GET', `${sessionsPath(machine)}/${encodeURIComponent(name)}/windows`, undefined, { signal: execSignal() },
  ),
  select: (machine: string, name: string, window: string, pane?: string) => request<TmuxWindows>(
    'POST', `${sessionsPath(machine)}/${encodeURIComponent(name)}/select`, { window, ...(pane === undefined ? {} : { pane }) }, { signal: execSignal() },
  ),
}

export const terminalOutputApi = {
  read: (machine: string, name: string) => request<{ output: string }>(
    'GET', `${sessionsPath(machine)}/${encodeURIComponent(name)}/output`, undefined, { signal: execSignal() },
  ),
}

export type CopyModeAction = 'enter' | 'scroll-up' | 'scroll-down' | 'wheel-up' | 'wheel-down' | 'page-up' | 'page-down' | 'top' | 'bottom' | 'exit'
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
    { signal: execSignal() },
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
  home: (machine: string) => request<{ path: string }>('GET', `/api/machines/${encodeURIComponent(machine)}/fs/home`, undefined, { signal: sftpSignal() }),
  list: (machine: string, path: string, hidden: boolean, signal?: AbortSignal) => request<{ path: string; entries: FileEntry[]; truncated?: boolean }>(
    'GET', `/api/machines/${encodeURIComponent(machine)}/fs?path=${encodeURIComponent(path)}&hidden=${hidden}`, undefined,
    { signal: signal ? AbortSignal.any([signal, sftpSignal()]) : sftpSignal() },
  ),
  stat: (machine: string, path: string) => request<FileEntry & { symlink: boolean }>(
    'GET', `/api/machines/${encodeURIComponent(machine)}/fs/stat?path=${encodeURIComponent(path)}`, undefined, { signal: sftpSignal() },
  ),
  mkdir: (machine: string, path: string, name: string) => request<{ path: string }>(
    'POST', `/api/machines/${encodeURIComponent(machine)}/fs/mkdir`, { path, name }, { signal: sftpSignal() },
  ),
  uploadPhoto: async (machine: string, directory: string, file: File, name = file.name) => {
    const url = `/api/machines/${encodeURIComponent(machine)}/fs/upload?directory=${encodeURIComponent(directory)}&name=${encodeURIComponent(name)}`
    let response: Response
    try {
      response = await fetch(url, {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/octet-stream' },
        // Pass the selected File itself. No canvas, decoder, FormData wrapper,
        // or re-encoding touches its bytes.
        body: file,
        signal: uploadSignal(),
      })
    } catch (error) {
      if (error instanceof DOMException && ['AbortError', 'TimeoutError'].includes(error.name)) {
        throw new ApiError(504, "hostbud didn't answer", 'The photo upload timed out; retry with the original photo.')
      }
      throw error
    }
    if (!response.ok) {
      let message = `request failed (${response.status})`
      let hint: string | undefined
      try {
        const data = await response.json() as { error?: string; hint?: string }
        message = data.error || message
        hint = data.hint || undefined
      } catch { /* keep generic error */ }
      throw new ApiError(response.status, message, hint)
    }
    return await response.json() as { path: string; size: number }
  },
  /** Upload with the first available numbered filename, preserving the File bytes. */
  async uploadPhotoUnique(machine: string, directory: string, file: File) {
    for (let suffix = 0; suffix <= 100; suffix++) {
      const name = suffix === 0 ? file.name : numberedFilename(file.name, suffix)
      try {
        return await this.uploadPhoto(machine, directory, file, name)
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 409 || !error.message.includes('already exists') || suffix === 100) throw error
      }
    }
    throw new Error('No available filename for this photo.')
  },
}

function numberedFilename(name: string, suffix: number): string {
  const dot = name.lastIndexOf('.')
  const hasExtension = dot > 0
  const stem = hasExtension ? name.slice(0, dot) : name
  const extension = hasExtension ? name.slice(dot) : ''
  return `${stem}-${suffix}${extension}`
}

export const projectsApi = {
  list: (machine: string) => request<{ projects: Project[] }>('GET', `/api/projects?machine=${encodeURIComponent(machine)}`),
  recentCommands: (id: string) => request<{ commands: string[] }>('GET', `/api/projects/${encodeURIComponent(id)}/recent-commands`),
  create: (machineId: string, path: string, name: string) => request<Project>('POST', '/api/projects', { machineId, path, name }),
  rename: (id: string, name: string) => request<Project>('PATCH', `/api/projects/${encodeURIComponent(id)}`, { name }),
  remove: (id: string) => request<void>('DELETE', `/api/projects/${encodeURIComponent(id)}`),
  createSession: (id: string, spec: { name?: string; startCommand?: string }) =>
    request<{ name: string }>('POST', `/api/projects/${encodeURIComponent(id)}/sessions`, spec, { signal: execSignal() }),
}

const q = (id: string) => encodeURIComponent(id)

/** The v2 queue API (/api/queues, /api/queue-items; V2-M2 the run cap). */
export const queuesApi = {
  list: () => request<QueueList>('GET', '/api/queues'),
  history: (limit = 100, offset = 0) => request<import('./types').QueueHistoryPage>('GET', `/api/queue-history?limit=${limit}&offset=${offset}`),
  capacity: (machine: string) => request<Capacity>('GET', `/api/machines/${q(machine)}/capacity`),
  setCapacity: (machine: string, maxConcurrentRuns: number | null) =>
    request<Capacity>('PUT', `/api/machines/${q(machine)}/capacity`, { maxConcurrentRuns }),
  setParallel: (machine: string, parallelQueues: boolean) =>
    request<ParallelQueues>('PUT', `/api/machines/${q(machine)}/parallel-queues`, { parallelQueues }),
  create: (projectId: string, name: string, afterRunId = '', afterSession = '') => request<Queue>('POST', '/api/queues', { projectId, name, afterRunId, afterSession }),
  rename: (id: string, name: string) => request<Queue>('PATCH', `/api/queues/${q(id)}`, { name }),
  /** "Start after" on an existing queue; both empty clears the link. */
  setLink: (id: string, afterRunId = '', afterSession = '') => request<Queue>('PUT', `/api/queues/${q(id)}/link`, { afterRunId, afterSession }),
  setLoop: (id: string, enabled: boolean, maxRuntime: string) => request<Queue>('PUT', `/api/queues/${q(id)}/loop`, { enabled, maxRuntime }),
  setDefaultPrompt: (id: string, prompt: DefaultPrompt) => request<Queue>('PUT', `/api/queues/${q(id)}/default-prompt`, prompt),
  remove: (id: string) => request<void>('DELETE', `/api/queues/${q(id)}`),
  addItem: (id: string, item: { agent: string; flags: string; instruction: string; executionMode?: 'agent' | 'session'; targetSession?: string; command?: string; verifyCommand?: string; requiresApproval?: boolean }) =>
    request<QueueItem>('POST', `/api/queues/${q(id)}/items`, item),
  updateItem: (id: string, item: { agent?: string; flags?: string; instruction?: string; executionMode?: 'agent' | 'session'; targetSession?: string; command?: string; verifyCommand?: string; requiresApproval?: boolean }) =>
    request<QueueItem>('PATCH', `/api/queue-items/${q(id)}`, item),
  removeItem: (id: string) => request<void>('DELETE', `/api/queue-items/${q(id)}`),
  reorder: (id: string, itemIds: string[]) => request<Queue>('PUT', `/api/queues/${q(id)}/order`, { itemIds }),
  // Starting a run checks the client and creates its session over ssh.
  start: (id: string, delay = '') => request<Queue>('POST', `/api/queues/${q(id)}/start`, delay ? { delay } : undefined, { signal: execSignal() }),
  pause: (id: string) => request<Queue>('POST', `/api/queues/${q(id)}/pause`),
  resume: (id: string) => request<Queue>('POST', `/api/queues/${q(id)}/resume`, undefined, { signal: execSignal() }),
  retry: (id: string) => request<Queue>('POST', `/api/queue-items/${q(id)}/retry`),
  skip: (id: string) => request<Queue>('POST', `/api/queue-items/${q(id)}/skip`),
  markDone: (id: string) => request<Queue>('POST', `/api/queue-items/${q(id)}/mark-done`),
  // V2-M4 completion gates.
  approve: (id: string) => request<Queue>('POST', `/api/queue-items/${q(id)}/approve`),
  reject: (id: string) => request<Queue>('POST', `/api/queue-items/${q(id)}/reject`),
  reverify: (id: string) => request<Queue>('POST', `/api/queue-items/${q(id)}/reverify`),
}

/** V2-M3 notifications: the caller's account only. */
export const notificationsApi = {
  settings: () => request<NotificationSettings>('GET', '/api/notifications/settings'),
  save: (prefs: Partial<Pick<NotificationSettings, 'enabled' | 'onDone' | 'onAttention' | 'onFinished'>>) =>
    request<NotificationSettings>('PUT', '/api/notifications/settings', prefs),
  subscribe: (sub: { endpoint: string; keys: { p256dh: string; auth: string } }) =>
    request<void>('POST', '/api/notifications/subscriptions', sub),
  test: (endpoint: string) => request<void>('POST', '/api/notifications/test', { endpoint }),
  unsubscribe: (endpoint: string, opts: { keepalive?: boolean } = {}) =>
    request<void>('DELETE', '/api/notifications/subscriptions', { endpoint }, opts.keepalive ? { keepalive: true } : {}),
}

/** V2-M5 read-only status; never includes provider credentials. */
export const supervisorApi = { status: () => request<import('./types').SupervisorStatus>('GET', '/api/supervisor') }

/** UI state keys the server accepts (internal/api/uistate.go). */
export type UIStateKey = 'layout' | 'tree' | 'theme'

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
