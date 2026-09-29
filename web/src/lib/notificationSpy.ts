// E2E builds only (main.ts guards the call with VITE_E2E): wraps the
// browser's Notification so scenarios can see what was shown
// (window.__notifications) and click it (window.__clickNotification).

export interface ShownNotification {
  title: string
  body: string
  tag: string
}

declare global {
  interface Window {
    __notifications?: ShownNotification[]
    __clickNotification?: (index: number) => void
    /** How often the page asked for notification permission. */
    __notificationPermissionRequests?: number
  }
}

export function installNotificationSpy(w: Window): void {
  const Real = (w as unknown as { Notification?: typeof Notification }).Notification
  const shown: ShownNotification[] = []
  const instances: { onclick: ((this: unknown, ev: Event) => unknown) | null }[] = []
  // Headless Chromium keeps Notification.permission at "denied" after the
  // test grants the permission, while the Permissions API (and
  // requestPermission) report the grant: a grant there wins. Otherwise the
  // browser's own value stands (headless denies what nobody granted).
  let granted: NotificationPermission | undefined
  const fromState = (state: PermissionState): NotificationPermission | undefined => (state === 'granted' ? 'granted' : undefined)
  void w.navigator?.permissions?.query({ name: 'notifications' }).then((status) => {
    granted = fromState(status.state)
    status.onchange = () => { granted = fromState(status.state) }
  }).catch(() => {})
  class SpyNotification {
    static get permission(): NotificationPermission {
      return granted ?? Real?.permission ?? 'default'
    }
    static async requestPermission(cb?: NotificationPermissionCallback): Promise<NotificationPermission> {
      w.__notificationPermissionRequests = (w.__notificationPermissionRequests ?? 0) + 1
      const answer = Real ? await Real.requestPermission(cb) : 'denied'
      granted = answer === 'granted' ? answer : undefined
      return answer
    }
    onclick: ((this: unknown, ev: Event) => unknown) | null = null
    readonly title: string
    readonly body: string
    readonly tag: string
    constructor(title: string, options: NotificationOptions = {}) {
      this.title = title
      this.body = options.body ?? ''
      this.tag = options.tag ?? ''
      shown.push({ title: this.title, body: this.body, tag: this.tag })
      instances.push(this)
    }
    close() {}
  }
  w.__notifications = shown
  w.__notificationPermissionRequests = 0
  w.__clickNotification = (index) => instances[index]?.onclick?.call(instances[index], new Event('click'))
  Object.defineProperty(w, 'Notification', { configurable: true, writable: true, value: SpyNotification })
}
