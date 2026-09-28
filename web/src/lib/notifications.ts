import type { NotificationPayload, NotificationSettings } from '@/api/types'

// V2-M3 in-app notifications: the browser Notification for a live
// queue.changed event. Text comes only from the server's payload.

export type Permission = NotificationPermission | 'unsupported'

export interface NotifyContext {
  settings: Pick<NotificationSettings, 'enabled' | 'onDone' | 'onAttention' | 'onFinished'> | null
  permission: Permission
  /** This device has a push subscription: push shows it instead. */
  pushSubscribed: boolean
  seen: ReadonlySet<string>
}

const chosen: Record<NotificationPayload['kind'], 'onDone' | 'onAttention' | 'onFinished'> = {
  done: 'onDone', attention: 'onAttention', finished: 'onFinished',
}

/** Whether a payload should show as an in-app notification. */
export function shouldNotify(p: NotificationPayload, ctx: NotifyContext): boolean {
  const s = ctx.settings
  if (!s?.enabled || !(p.kind in chosen) || !s[chosen[p.kind]]) return false
  if (ctx.permission !== 'granted' || ctx.pushSubscribed) return false
  return !ctx.seen.has(p.key)
}

/** An app path to open on click, or null (same-origin paths only). */
export function appPath(url: string, origin: string): string | null {
  if (!url.startsWith('/') || url.startsWith('//')) return null
  try {
    const u = new URL(url, origin)
    return u.origin === origin ? u.pathname + u.search : null
  } catch {
    return null
  }
}

/** The queue and item a notification's path points at (/queues/<id>?item=<id>). */
export function queueTarget(path: string): { queueId: string; itemId: string | null } | null {
  const u = new URL(path, 'http://hostbud.invalid')
  const m = /^\/queues\/([^/]+)$/.exec(u.pathname)
  if (!m) return null
  return { queueId: decodeURIComponent(m[1]), itemId: u.searchParams.get('item') }
}

/** The browser's notification permission ('unsupported' without the API). */
export function currentPermission(): Permission {
  return typeof Notification === 'undefined' ? 'unsupported' : Notification.permission
}
