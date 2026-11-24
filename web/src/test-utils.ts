import { vi } from 'vitest'

export interface FakeResponse {
  status: number
  body?: unknown
  headers?: Record<string, string>
}

/** Stubs fetch with a router: (method, path, body) → response. */
export function stubFetch(route: (method: string, path: string, body: unknown) => FakeResponse) {
  const calls: { method: string; path: string; body: unknown }[] = []
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const wire = init?.body ? JSON.parse(String(init.body)) : undefined
    const body = (init?.headers as Record<string, string> | undefined)?.['X-Hostbud-Tree-Compare'] === '1' ? wire.value : wire
    calls.push({ method, path, body })
    const r = route(method, path, body)
    return new Response(r.body === undefined ? null : JSON.stringify(r.body), {
      status: r.status,
      headers: { 'Content-Type': 'application/json', ...r.headers },
    })
  })
  vi.stubGlobal('fetch', fn)
  return calls
}
