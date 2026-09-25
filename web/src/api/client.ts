// Typed client for hostbud's JSON API. Errors carry the server's
// {error, hint} shape (and Retry-After for 429s).

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

type Method = 'GET' | 'POST' | 'PATCH' | 'DELETE'

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
