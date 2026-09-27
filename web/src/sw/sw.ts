import { activateWorker, handleFetch, installWorker } from './worker'

declare const __HOSTBUD_CACHE_NAME__: string
declare const __HOSTBUD_PRECACHE_URLS__: string[]

interface LifecycleEvent { waitUntil(promise: Promise<unknown>): void }
interface FetchWorkerEvent extends LifecycleEvent { request: Request; respondWith(response: Promise<Response>): void }
interface WorkerClients { claim(): Promise<void> }
interface WorkerScope {
  location: Location
  caches: CacheStorage
  clients: WorkerClients
  skipWaiting(): Promise<void>
  addEventListener(type: 'install' | 'activate', listener: (event: LifecycleEvent) => void): void
  addEventListener(type: 'fetch', listener: (event: FetchWorkerEvent) => void): void
}

const scope = self as unknown as WorkerScope
scope.addEventListener('install', (event) => {
  event.waitUntil(Promise.all([
    installWorker(scope.caches, __HOSTBUD_CACHE_NAME__, __HOSTBUD_PRECACHE_URLS__),
    scope.skipWaiting(),
  ]))
})
scope.addEventListener('activate', (event) => {
  event.waitUntil(Promise.all([
    activateWorker(scope.caches, __HOSTBUD_CACHE_NAME__),
    scope.clients.claim(),
  ]))
})
scope.addEventListener('fetch', (event) => {
  const response = handleFetch(event.request, scope.location.origin, scope.caches, __HOSTBUD_CACHE_NAME__, __HOSTBUD_PRECACHE_URLS__)
  if (response) event.respondWith(response)
})
