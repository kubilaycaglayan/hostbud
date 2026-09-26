export type RequestRoute = 'network' | 'shell' | 'cache'

/** Classify a request without side effects; API and WebSocket paths never enter the shell cache. */
export function classifyRequest(request: Request, origin: string, precache: ReadonlySet<string>): RequestRoute {
  const url = new URL(request.url)
  if (request.method !== 'GET' || url.origin !== origin) return 'network'
  if (url.pathname === '/api' || url.pathname.startsWith('/api/') || url.pathname === '/ws' || url.pathname.startsWith('/ws/')) return 'network'
  if (request.mode === 'navigate') return 'shell'
  return precache.has(url.pathname) ? 'cache' : 'network'
}
