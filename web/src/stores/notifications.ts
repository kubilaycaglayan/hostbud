import { defineStore } from 'pinia'
import { ref } from 'vue'
import { notificationsApi } from '@/api/client'
import type { NotificationPayload, NotificationSettings, ServerEvent } from '@/api/types'
import { appPath, currentPermission, queueTarget, shouldNotify } from '@/lib/notifications'
import { deviceWasSetUp } from '@/lib/notificationDevice'
import { unsubscribeDevice } from '@/lib/push'

/** Whether this device has a Web Push subscription (then push shows the
 * notifications, not the page). */
export async function hasPushSubscription(): Promise<boolean> {
  try {
    const reg = await navigator.serviceWorker?.getRegistration()
    return Boolean(await reg?.pushManager?.getSubscription())
  } catch {
    return false
  }
}

/** V2-M3 in-app notifications: the account's settings and the live
 * queue.changed events that carry a notification. It never asks for
 * permission itself (only the Settings toggle does). */
export const useNotificationsStore = defineStore('notifications', () => {
  const settings = ref<NotificationSettings | null>(null)
  const pushSubscribed = ref(false)
  /** The account is on and this device was set up, but its permission was
   * taken back: its subscription was removed (Settings says so). */
  const revoked = ref(false)
  // Keys seen since the page loaded: a reconnect never replays one.
  const seen = new Set<string>()
  let opener: ((queueId: string, itemId: string | null) => void) | null = null

  async function load() {
    try {
      settings.value = await notificationsApi.settings()
    } catch {
      settings.value = null // off until it loads
    }
    pushSubscribed.value = await hasPushSubscription()
    // Revoked later: this device's subscription goes; other devices keep theirs.
    const permission = currentPermission()
    if (settings.value?.enabled && deviceWasSetUp() && permission !== 'granted' && permission !== 'unsupported') {
      await unsubscribeDevice()
      pushSubscribed.value = false
      revoked.value = true
    }
  }

  /** Where a click on a notification leads (App: the Queue panel). */
  function onOpen(fn: (queueId: string, itemId: string | null) => void) {
    opener = fn
  }

  function open(p: NotificationPayload) {
    const path = appPath(p.url, window.location.origin)
    const target = path ? queueTarget(path) : null
    if (target) opener?.(target.queueId, target.itemId)
  }

  /** Shows one notification on this page (Settings' test without push). */
  function showLocal(n: { title: string; body: string; tag: string }) {
    show({ v: 1, kind: 'done', key: n.tag, project: '', position: 0, outcome: 'test', url: '/', title: n.title, body: n.body })
  }

  function show(p: NotificationPayload) {
    const options: NotificationOptions = { body: p.body, tag: p.key, icon: '/icons/icon-192.png', data: { url: p.url } }
    try {
      const n = new Notification(p.title, options)
      n.onclick = () => {
        window.focus()
        open(p)
        n.close()
      }
    } catch {
      // Android Chrome only shows notifications from the service worker
      // (its notificationclick opens the item).
      void navigator.serviceWorker?.ready.then((reg) => reg.showNotification(p.title, options)).catch(() => undefined)
    }
  }

  /** Feeds one live server event. */
  function apply(e: ServerEvent) {
    if (e.type !== 'queue.changed' || !e.payload.notification) return
    const p = e.payload.notification
    const notify = shouldNotify(p, { settings: settings.value, permission: currentPermission(), pushSubscribed: pushSubscribed.value, seen })
    seen.add(p.key)
    if (notify) show(p)
  }

  function reset() {
    settings.value = null
    pushSubscribed.value = false
    revoked.value = false
  }

  return { settings, pushSubscribed, revoked, load, apply, onOpen, open, showLocal, reset }
})
