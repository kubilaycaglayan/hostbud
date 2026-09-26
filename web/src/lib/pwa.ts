export interface ServiceWorkerRegistrar {
  register(scriptURL: string, options: { scope: string }): Promise<ServiceWorkerRegistration>
}

export async function registerPWA(options: {
  production: boolean
  secureContext: boolean
  serviceWorker?: ServiceWorkerRegistrar
  debug?: (...args: unknown[]) => void
}) {
  if (!options.production || !options.secureContext || !options.serviceWorker) return
  try {
    await options.serviceWorker.register('/sw.js', { scope: '/' })
  } catch (error) {
    options.debug?.('hostbud: service worker registration failed', error)
  }
}
