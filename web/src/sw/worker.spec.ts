import { describe, expect, it, vi } from 'vitest'
import { classifyRequest } from './routing'
import { activateWorker, handleFetch, installWorker } from './worker'

function fakeRequest(url: string, method = 'GET', mode: RequestMode = 'cors') {
  return { url: new URL(url, 'https://hostbud.example.test').toString(), method, mode } as Request
}

function fakeCaches(initial: Record<string, Record<string, Response>> = {}) {
  const records = new Map(Object.entries(initial).map(([name, entries]) => [name, new Map(Object.entries(entries))]))
  let putCalls = 0
  const store: Partial<CacheStorage> = {
    open: vi.fn(async (name: string) => {
      if (!records.has(name)) records.set(name, new Map())
      const values = records.get(name)!
      return {
        addAll: vi.fn(async (urls: string[]) => { for (const url of urls) values.set(url, new Response(url)) }),
        match: vi.fn(async (key: RequestInfo | URL) => values.get(typeof key === 'string' ? key : new URL(String(key), 'https://hostbud.example.test').pathname)),
        put: vi.fn(async (key: RequestInfo | URL, response: Response) => { putCalls++; values.set(String(key), response) }),
      } as unknown as Cache
    }),
    keys: vi.fn(async () => [...records.keys()]),
    delete: vi.fn(async (name: string) => records.delete(name)),
  }
  return { cacheStorage: store as CacheStorage, records, putCalls: () => putCalls }
}

describe('service worker request routing', () => {
  const precache = new Set(['/', '/assets/app-hash.js', '/manifest.webmanifest'])
  it.each([
    [fakeRequest('/', 'GET', 'navigate'), 'shell'],
    [fakeRequest('/sessions/a', 'GET', 'navigate'), 'shell'],
    [fakeRequest('/assets/app-hash.js'), 'cache'],
    [fakeRequest('/api/auth/me'), 'network'],
    [fakeRequest('/api/machines/host/sessions'), 'network'],
    [fakeRequest('/ws/events'), 'network'],
    [fakeRequest('/ws/term?session=a'), 'network'],
    [fakeRequest('https://elsewhere.example/api'), 'network'],
    [fakeRequest('/assets/app.js', 'POST'), 'network'],
    [fakeRequest('/unknown.css'), 'network'],
  ] as const)('classifies %s as %s', (request, expected) => {
    expect(classifyRequest(request, 'https://hostbud.example.test', precache)).toBe(expected)
  })
})

describe('service worker cache lifecycle', () => {
  it('installs exactly the provided shell URLs', async () => {
    const { cacheStorage, records } = fakeCaches()
    const urls = ['/', '/assets/a.js', '/manifest.webmanifest']
    await installWorker(cacheStorage, 'hostbud-shell-v1', urls)
    expect([...records.get('hostbud-shell-v1')!.keys()]).toEqual(urls)
  })

  it('deletes stale hostbud caches and preserves caches with another prefix', async () => {
    const { cacheStorage, records } = fakeCaches({ 'hostbud-shell-old': {}, 'hostbud-shell-current': {}, 'other-app-cache': {} })
    await activateWorker(cacheStorage, 'hostbud-shell-current')
    expect([...records.keys()]).toEqual(['hostbud-shell-current', 'other-app-cache'])
  })

  it('refreshes the navigation shell from the network and keeps precached assets cache-first', async () => {
    const shell = new Response('shell')
    const asset = new Response('asset')
    const { cacheStorage, records, putCalls } = fakeCaches({ 'hostbud-shell-v1': { '/': shell, '/assets/app.js': asset } })
    const fetcher = vi.fn(async () => new Response('fresh shell'))
    const navigation = await handleFetch(fakeRequest('/session/a', 'GET', 'navigate'), 'https://hostbud.example.test', cacheStorage, 'hostbud-shell-v1', ['/assets/app.js'], fetcher)
    expect(await navigation?.text()).toBe('fresh shell')
    expect(await handleFetch(fakeRequest('/assets/app.js'), 'https://hostbud.example.test', cacheStorage, 'hostbud-shell-v1', ['/assets/app.js'], fetcher)).toBe(asset)
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(records.get('hostbud-shell-v1')?.size).toBe(2)
    expect(putCalls()).toBe(1)
    expect(await records.get('hostbud-shell-v1')?.get('/')?.text()).toBe('fresh shell')
  })

  it('falls back to the cached app shell when a navigation request is offline', async () => {
    const shell = new Response('cached shell')
    const { cacheStorage } = fakeCaches({ 'hostbud-shell-v1': { '/': shell } })
    const fetcher = vi.fn(async () => { throw new TypeError('offline') })
    const response = await handleFetch(fakeRequest('/session/a', 'GET', 'navigate'), 'https://hostbud.example.test', cacheStorage, 'hostbud-shell-v1', [], fetcher)
    expect(response).toBe(shell)
    expect(fetcher).toHaveBeenCalledOnce()
  })

  it.each(['/api/auth/me', '/api/machines/host/sessions', '/ws/events', '/ws/term?session=x'])('leaves %s to the network with no runtime cache write', async (url) => {
    const { cacheStorage, records, putCalls } = fakeCaches({ 'hostbud-shell-v1': {} })
    const fetcher = vi.fn(async () => new Response('network'))
    expect(handleFetch(fakeRequest(url), 'https://hostbud.example.test', cacheStorage, 'hostbud-shell-v1', [], fetcher)).toBeNull()
    expect(fetcher).not.toHaveBeenCalled() // the browser performs the fetch when respondWith is omitted
    expect(records.get('hostbud-shell-v1')?.size).toBe(0)
    expect(putCalls()).toBe(0)
  })
})
