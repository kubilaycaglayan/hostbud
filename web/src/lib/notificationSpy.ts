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
  }
}

export function installNotificationSpy(w: Window): void {
  const Real = (w as unknown as { Notification?: typeof Notification }).Notification
  const shown: ShownNotification[] = []
  const instances: { onclick: ((this: unknown, ev: Event) => unknown) | null }[] = []
  class SpyNotification {
    static get permission(): NotificationPermission {
      return Real?.permission ?? 'default'
    }
    static requestPermission(cb?: NotificationPermissionCallback): Promise<NotificationPermission> {
      return Real ? Real.requestPermission(cb) : Promise.resolve('denied')
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
  w.__clickNotification = (index) => instances[index]?.onclick?.call(instances[index], new Event('click'))
  Object.defineProperty(w, 'Notification', { configurable: true, writable: true, value: SpyNotification })
}
