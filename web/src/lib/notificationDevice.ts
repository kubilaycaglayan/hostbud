import type { Permission } from './notifications'

// V2-M3 T4: what notifications can do on this device, for Settings.

export type DeviceState =
  | 'ios-install' // iPhone/iPad in a browser tab: install the app first
  | 'unsupported' // no Notification API
  | 'blocked' // permission denied
  | 'not-set-up' // not asked yet on this device
  | 'in-app' // granted, no push subscription: while hostbud is open
  | 'push' // granted and subscribed: also while hostbud is closed

export interface DeviceInfo {
  userAgent: string
  platform?: string
  maxTouchPoints?: number
  standalone: boolean
  notificationApi: boolean
  permission: Permission
  pushSubscribed: boolean
}

/** iPhone, iPad (also iPadOS reporting as a Mac with touch) or iPod. */
export function isIOS(info: Pick<DeviceInfo, 'userAgent' | 'platform' | 'maxTouchPoints'>): boolean {
  return /iPad|iPhone|iPod/.test(info.userAgent) || (info.platform === 'MacIntel' && (info.maxTouchPoints ?? 0) > 1)
}

export function deviceState(info: DeviceInfo): DeviceState {
  if (isIOS(info) && !info.standalone) return 'ios-install'
  if (!info.notificationApi || info.permission === 'unsupported') return 'unsupported'
  if (info.permission === 'denied') return 'blocked'
  if (info.permission === 'default') return 'not-set-up'
  return info.pushSubscribed ? 'push' : 'in-app'
}

/** The running app is the installed PWA (home screen). */
export function isStandalone(): boolean {
  const nav = navigator as Navigator & { standalone?: boolean }
  return nav.standalone === true || (typeof window.matchMedia === 'function' && window.matchMedia('(display-mode: standalone)').matches)
}

export function currentDevice(permission: Permission, pushSubscribed: boolean): DeviceInfo {
  return {
    userAgent: navigator.userAgent,
    platform: navigator.platform,
    maxTouchPoints: navigator.maxTouchPoints,
    standalone: isStandalone(),
    notificationApi: typeof Notification !== 'undefined',
    permission,
    pushSubscribed,
  }
}

export const DEVICE_TEXT: Record<DeviceState, string> = {
  'ios-install': 'On iPhone and iPad, notifications work only in the installed app: tap Share → Add to Home Screen, open hostbud from the home screen, then turn this on (iOS 16.4 or later).',
  unsupported: "This browser can't show notifications.",
  blocked: 'Notifications are blocked for hostbud in this browser. Allow them in the site settings (the icon next to the address bar), reload, then turn this on again.',
  'not-set-up': 'Not set up on this device yet.',
  'in-app': 'This device: notifications while hostbud is open.',
  push: 'This device: notifications also while hostbud is closed.',
}

// This device was set up for notifications (so a later "not granted" means
// the permission was revoked). Per browser; a missing store reads as no.
const DEVICE_KEY = 'hostbud.notifications.device'

export function deviceWasSetUp(): boolean {
  try {
    return window.localStorage.getItem(DEVICE_KEY) === 'on'
  } catch {
    return false
  }
}

export function markDeviceSetUp(on: boolean): void {
  try {
    if (on) window.localStorage.setItem(DEVICE_KEY, 'on')
    else window.localStorage.removeItem(DEVICE_KEY)
  } catch {
    // Private mode: the revoked message just won't show.
  }
}
