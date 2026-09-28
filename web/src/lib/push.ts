import { notificationsApi } from '@/api/client'

// V2-M3 Web Push on this device: subscribe with the server's VAPID key and
// register the subscription for the signed-in account, or remove it.

/** The VAPID public key (base64url) as the applicationServerKey bytes. */
export function applicationServerKey(base64url: string): Uint8Array<ArrayBuffer> {
  const b64 = base64url.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (base64url.length % 4)) % 4)
  const raw = atob(b64)
  const out = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

async function registration(): Promise<ServiceWorkerRegistration | undefined> {
  try {
    return await navigator.serviceWorker?.getRegistration()
  } catch {
    return undefined
  }
}

/** Whether this browser can subscribe to push (a service worker with
 * PushManager: desktop browsers, and iOS 16.4+ only as an installed app). */
export async function pushSupported(): Promise<boolean> {
  return Boolean((await registration())?.pushManager)
}

/** Subscribes this device and registers it for the account. */
export async function subscribeDevice(vapidPublicKey: string): Promise<void> {
  const reg = await registration()
  if (!reg?.pushManager) throw new Error('This browser has no push service here. On iPhone, add hostbud to the home screen first.')
  const sub = await reg.pushManager.getSubscription() ?? await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: applicationServerKey(vapidPublicKey) })
  const json = sub.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } }
  await notificationsApi.subscribe({ endpoint: json.endpoint ?? sub.endpoint, keys: { p256dh: json.keys?.p256dh ?? '', auth: json.keys?.auth ?? '' } })
}

/** Removes this device's subscription from the account and the browser
 * (sign-out, notifications off or blocked). Best effort. */
export async function unsubscribeDevice(): Promise<void> {
  const sub = await (await registration())?.pushManager?.getSubscription().catch(() => null)
  if (!sub) return
  try {
    await notificationsApi.unsubscribe(sub.endpoint)
  } catch {
    // The server copy goes when the push service reports it gone.
  }
  await sub.unsubscribe().catch(() => false)
}

/** This device's push endpoint, or null without a subscription. */
export async function pushEndpoint(): Promise<string | null> {
  const sub = await (await registration())?.pushManager?.getSubscription().catch(() => null)
  return sub?.endpoint ?? null
}
