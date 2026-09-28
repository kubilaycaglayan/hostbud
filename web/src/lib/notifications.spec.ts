import { describe, expect, it } from 'vitest'
import type { NotificationPayload } from '@/api/types'
import { appPath, queueTarget, shouldNotify, type NotifyContext } from './notifications'

const payload = (kind: NotificationPayload['kind'], key = `run:r:${kind}`): NotificationPayload => ({
  v: 1, kind, key, project: 'app', position: 2, outcome: kind, url: '/queues/q?item=i', title: `app: item 2 ${kind}`, body: 'Item 2.',
})
const on = { enabled: true, onDone: true, onAttention: true, onFinished: true }
const ctx = (over: Partial<NotifyContext> = {}): NotifyContext => ({ settings: on, permission: 'granted', pushSubscribed: false, seen: new Set(), ...over })

describe('shouldNotify', () => {
  it.each(['done', 'attention', 'finished'] as const)('notifies %s when on and granted', (kind) => {
    expect(shouldNotify(payload(kind), ctx())).toBe(true)
  })
  it('stays quiet with the switch off or no settings', () => {
    expect(shouldNotify(payload('done'), ctx({ settings: { ...on, enabled: false } }))).toBe(false)
    expect(shouldNotify(payload('done'), ctx({ settings: null }))).toBe(false)
  })
  it.each([['done', 'onDone'], ['attention', 'onAttention'], ['finished', 'onFinished']] as const)('respects the %s choice', (kind, field) => {
    expect(shouldNotify(payload(kind), ctx({ settings: { ...on, [field]: false } }))).toBe(false)
  })
  it.each(['default', 'denied', 'unsupported'] as const)('needs granted permission, not %s', (permission) => {
    expect(shouldNotify(payload('done'), ctx({ permission }))).toBe(false)
  })
  it('leaves a push-subscribed device to push', () => {
    expect(shouldNotify(payload('done'), ctx({ pushSubscribed: true }))).toBe(false)
  })
  it('never shows a key twice', () => {
    expect(shouldNotify(payload('done', 'k'), ctx({ seen: new Set(['k']) }))).toBe(false)
  })
  it('ignores an unknown kind', () => {
    expect(shouldNotify({ ...payload('done'), kind: 'started' as never }, ctx())).toBe(false)
  })
})

describe('appPath and queueTarget', () => {
  it('accepts same-origin paths only', () => {
    expect(appPath('/queues/q?item=i', 'https://hostbud.example.com')).toBe('/queues/q?item=i')
    expect(appPath('https://evil.example/x', 'https://hostbud.example.com')).toBeNull()
    expect(appPath('//evil.example/x', 'https://hostbud.example.com')).toBeNull()
    expect(appPath('javascript:alert(1)', 'https://hostbud.example.com')).toBeNull()
  })
  it('reads the queue and item', () => {
    expect(queueTarget('/queues/queue_a?item=item_b')).toEqual({ queueId: 'queue_a', itemId: 'item_b' })
    expect(queueTarget('/queues/queue_a')).toEqual({ queueId: 'queue_a', itemId: null })
    expect(queueTarget('/sessions/x')).toBeNull()
  })
})
