import { activateWorker, handleFetch, installWorker, openFromNotification, pushNotification, type ClientsLike } from './worker'

declare const __HOSTBUD_CACHE_NAME__: string
declare const __HOSTBUD_PRECACHE_URLS__: string[]

interface LifecycleEvent { waitUntil(promise: Promise<unknown>): void }
interface FetchWorkerEvent extends LifecycleEvent { request: Request; respondWith(response: Promise<Response>): void }
interface PushWorkerEvent extends LifecycleEvent { data: { text(): string } | null }
interface NotificationWorkerEvent extends LifecycleEvent { notification: { data?: { url?: unknown }; close(): void } }
interface WorkerClients extends ClientsLike { claim(): Promise<void> }
interface WorkerScope {
  location: Location
  caches: CacheStorage
  clients: WorkerClients
  registration: { showNotification(title: string, options?: NotificationOptions): Promise<void> }
  skipWaiting(): Promise<void>
  addEventListener(type: 'install' | 'activate', listener: (event: LifecycleEvent) => void): void
  addEventListener(type: 'fetch', listener: (event: FetchWorkerEvent) => void): void
  addEventListener(type: 'push', listener: (event: PushWorkerEvent) => void): void
  addEventListener(type: 'notificationclick', listener: (event: NotificationWorkerEvent) => void): void
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

// V2-M3 Web Push: show the server's payload; a click opens its item.
scope.addEventListener('push', (event) => {
  const shown = pushNotification(event.data?.text())
  if (shown) event.waitUntil(scope.registration.showNotification(shown.title, shown.options))
})
scope.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil(openFromNotification(scope.clients, scope.location.origin, event.notification.data?.url))
})
