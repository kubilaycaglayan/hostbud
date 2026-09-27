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
