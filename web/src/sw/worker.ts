import { classifyRequest } from './routing'

export async function installWorker(cacheStorage: CacheStorage, cacheName: string, urls: string[]) {
  const cache = await cacheStorage.open(cacheName)
  await cache.addAll(urls)
}

export async function activateWorker(cacheStorage: CacheStorage, cacheName: string) {
  const names = await cacheStorage.keys()
  await Promise.all(names.filter((name) => name.startsWith('hostbud-shell-') && name !== cacheName).map((name) => cacheStorage.delete(name)))
}

/** Returns null for requests that must pass directly to the network. */
export function handleFetch(
  request: Request,
  origin: string,
  cacheStorage: CacheStorage,
  cacheName: string,
  precacheUrls: string[],
  fetcher: typeof fetch = fetch,
): Promise<Response> | null {
  const route = classifyRequest(request, origin, new Set(precacheUrls))
  if (route === 'network') return null
  const cacheUrl = route === 'shell' ? '/' : new URL(request.url).pathname
  return cacheStorage.open(cacheName).then(async (cache) => {
    const cached = await cache.match(cacheUrl)
    if (route === 'shell') {
      try {
        const response = await fetcher(request)
        if (response.ok) await cache.put(cacheUrl, response.clone())
        return response
      } catch (error) {
        if (cached) return cached
        throw error
      }
    }
    return cached ?? fetcher(request)
  })
}

// ---- V2-M3 Web Push ----

export interface PushShow {
  title: string
  options: NotificationOptions
}

/** The notification a push message shows: the server's allowlisted payload
 * (title, body; the dedupe key as tag, so a repeat replaces it). Anything
 * else shows nothing. */
export function pushNotification(text: string | null | undefined): PushShow | null {
  if (!text) return null
  let p: unknown
  try {
    p = JSON.parse(text)
  } catch {
    return null
  }
  if (!p || typeof p !== 'object') return null
  const { v, title, body, key, url } = p as Record<string, unknown>
  if (v !== 1 || typeof title !== 'string' || typeof body !== 'string' || typeof key !== 'string' || typeof url !== 'string') return null
  return { title, options: { body, tag: key, icon: '/icons/icon-192.png', badge: '/icons/icon-192.png', data: { url } } }
}

interface WindowClientLike {
  url: string
  focus(): Promise<unknown>
  postMessage(message: unknown): void
}

export interface ClientsLike {
  matchAll(options: { type: 'window'; includeUncontrolled: boolean }): Promise<readonly WindowClientLike[]>
  openWindow(url: string): Promise<unknown>
}

/** A same-origin app path from a notification's url, else the app root. */
export function notificationPath(url: unknown, origin: string): string {
  if (typeof url !== 'string' || !url.startsWith('/') || url.startsWith('//')) return '/'
  try {
    const u = new URL(url, origin)
    return u.origin === origin ? u.pathname + u.search : '/'
  } catch {
    return '/'
  }
}

/** notificationclick: focus an open hostbud window and tell it which item
 * to open, or open a new one on the item's path. */
export async function openFromNotification(clients: ClientsLike, origin: string, url: unknown): Promise<void> {
  const path = notificationPath(url, origin)
  const open = (await clients.matchAll({ type: 'window', includeUncontrolled: true })).filter((c) => new URL(c.url).origin === origin)
  if (open.length) {
    await open[0].focus()
    open[0].postMessage({ type: 'hostbud.open', path })
    return
  }
  await clients.openWindow(path)
}
