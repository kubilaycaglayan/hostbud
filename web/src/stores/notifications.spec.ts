import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { NotificationPayload, NotificationSettings, ServerEvent } from '@/api/types'
import { stubFetch } from '@/test-utils'
import { useNotificationsStore } from './notifications'

const settings = (over: Partial<NotificationSettings> = {}): NotificationSettings => ({
  enabled: true, onDone: true, onAttention: true, onFinished: true, push: { available: false }, ...over,
})
const payload = (kind: NotificationPayload['kind'], key = `run:r1:${kind}`): NotificationPayload => ({
  v: 1, kind, key, project: 'app', position: 2, outcome: kind, url: '/queues/q1?item=i2', title: `app: item 2 ${kind}`, body: 'Item 2 reached its goal.',
})
const event = (notification?: NotificationPayload, action = 'item_done'): ServerEvent => ({
  type: 'queue.changed', machine: 'host', payload: { action, queueId: 'q1', notification },
})

let shown: { title: string; options: NotificationOptions; instance: { onclick: (() => void) | null } }[]

function fakeNotification(permission: NotificationPermission) {
  shown = []
  const requestPermission = vi.fn()
  class FakeNotification {
    static permission = permission
    static requestPermission = requestPermission
    onclick: (() => void) | null = null
    constructor(title: string, options: NotificationOptions) {
      shown.push({ title, options, instance: this })
    }
    close() {}
  }
  vi.stubGlobal('Notification', FakeNotification)
  return requestPermission
}

async function loadedStore(s: NotificationSettings) {
  stubFetch(() => ({ status: 200, body: s }))
  const store = useNotificationsStore()
  await store.load()
  return store
}

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

describe('in-app notifications', () => {
  it.each(['done', 'attention', 'finished'] as const)('shows %s with the server text and the dedupe key as tag', async (kind) => {
    const ask = fakeNotification('granted')
    const store = await loadedStore(settings())
    store.apply(event(payload(kind)))
    expect(shown).toHaveLength(1)
    expect(shown[0].title).toBe(`app: item 2 ${kind}`)
    expect(shown[0].options).toMatchObject({ body: 'Item 2 reached its goal.', tag: `run:r1:${kind}` })
    expect(ask).not.toHaveBeenCalled()
  })

  it('ignores events without a notification (intermediate states)', async () => {
    fakeNotification('granted')
    const store = await loadedStore(settings())
    store.apply(event(undefined, 'run_started'))
    store.apply({ type: 'run.changed', machine: 'host', payload: { runId: 'r1', itemId: 'i', queueId: 'q1', status: 'running' } })
    expect(shown).toHaveLength(0)
  })

  it('stays quiet with the switch off or the event not chosen', async () => {
    fakeNotification('granted')
    let store = await loadedStore(settings({ enabled: false }))
    store.apply(event(payload('done')))
    setActivePinia(createPinia())
    store = await loadedStore(settings({ onAttention: false }))
    store.apply(event(payload('attention')))
    expect(shown).toHaveLength(0)
  })

  it.each(['default', 'denied'] as const)('never asks for permission and shows nothing with %s', async (permission) => {
    const ask = fakeNotification(permission)
    const store = await loadedStore(settings())
    store.apply(event(payload('done')))
    expect(shown).toHaveLength(0)
    expect(ask).not.toHaveBeenCalled()
  })

  it('leaves a push-subscribed device to push', async () => {
    fakeNotification('granted')
    const store = await loadedStore(settings())
    store.pushSubscribed = true
    store.apply(event(payload('done')))
    expect(shown).toHaveLength(0)
  })

  it('never replays a key (reconnect resync, a duplicate event)', async () => {
    fakeNotification('granted')
    const store = await loadedStore(settings())
    store.apply(event(payload('done')))
    store.apply({ type: 'snapshot', machines: [], sessions: {} })
    store.apply(event(payload('done')))
    expect(shown).toHaveLength(1)
  })

  it('shows only the payload text: no instruction or path from the queue view', async () => {
    fakeNotification('granted')
    const store = await loadedStore(settings())
    const e = event(payload('done'))
    if (e.type === 'queue.changed') {
      e.payload.queue = { id: 'q1', projectId: 'p', name: 'Q', status: 'running', projectName: 'app', projectPath: '/home/dev/secret-path', items: [
        { id: 'i2', queueId: 'q1', position: 2, agent: 'claude', flags: '--secret-flag', instruction: '/goal secret instruction', status: 'done' },
      ] }
    }
    store.apply(e)
    expect(JSON.stringify(shown.map((n) => [n.title, n.options]))).not.toMatch(/secret/)
  })

  it('a click focuses the app and opens the item', async () => {
    fakeNotification('granted')
    const focus = vi.spyOn(window, 'focus').mockImplementation(() => undefined)
    const store = await loadedStore(settings())
    const opened: [string, string | null][] = []
    store.onOpen((q, i) => opened.push([q, i]))
    store.apply(event(payload('attention')))
    shown[0].instance.onclick?.()
    expect(focus).toHaveBeenCalled()
    expect(opened).toEqual([['q1', 'i2']])
  })

  it('a foreign URL in a payload opens nothing', async () => {
    const store = await loadedStore(settings())
    const opened: string[] = []
    store.onOpen((q) => opened.push(q))
    store.open({ ...payload('done'), url: 'https://evil.example/queues/x' })
    expect(opened).toEqual([])
  })
})

describe('revoked permission (V2-M3 T4)', () => {
  afterEach(() => window.localStorage.clear())

  it('marks this device revoked when the account is on, it was set up, and permission is gone', async () => {
    fakeNotification('default')
    window.localStorage.setItem('hostbud.notifications.device', 'on')
    const store = await loadedStore(settings())
    expect(store.revoked).toBe(true)
  })

  it('a device never set up is not revoked', async () => {
    fakeNotification('default')
    const store = await loadedStore(settings())
    expect(store.revoked).toBe(false)
  })

  it('granted is not revoked', async () => {
    fakeNotification('granted')
    window.localStorage.setItem('hostbud.notifications.device', 'on')
    const store = await loadedStore(settings())
    expect(store.revoked).toBe(false)
  })
})
