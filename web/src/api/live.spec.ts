import { describe, expect, it } from 'vitest'
import { LiveConnection, type LiveState, type SocketLike } from './live'
import type { ServerEvent } from './types'

class FakeSocket implements SocketLike {
  onopen: SocketLike['onopen'] = null
  onmessage: SocketLike['onmessage'] = null
  onclose: SocketLike['onclose'] = null
  onerror: SocketLike['onerror'] = null
  closed = false
  constructor(readonly url: string) {}
  close() {
    this.closed = true
  }
  send(e: object) {
    this.onmessage?.({ data: JSON.stringify(e) } as MessageEvent)
  }
  drop() {
    this.onclose?.({} as CloseEvent)
  }
}

const snapshot = { type: 'snapshot', machines: [], sessions: { host: [] } }
const tick = () => new Promise((r) => setTimeout(r, 0))

function setup(stillAuthorized = async () => true) {
  const sockets: FakeSocket[] = []
  const timers: { fn: () => void; ms: number }[] = []
  const events: ServerEvent[] = []
  const states: LiveState[] = []
  const conn = new LiveConnection({
    url: 'ws://localhost:9055/ws/events',
    onEvent: (e) => events.push(e),
    onState: (s) => states.push(s),
    stillAuthorized,
    createSocket: (url) => {
      const s = new FakeSocket(url)
      sockets.push(s)
      return s
    },
    setTimer: (fn, ms) => timers.push({ fn, ms }),
    clearTimer: () => {},
  })
  return { conn, sockets, timers, events, states }
}

describe('LiveConnection', () => {
  it('forwards the snapshot and events', () => {
    const { conn, sockets, events, states } = setup()
    conn.start()
    expect(sockets[0].url).toBe('ws://localhost:9055/ws/events')
    sockets[0].send(snapshot)
    sockets[0].send({ type: 'sessions.changed', machine: 'host', payload: { sessions: [] } })
    expect(events.map((e) => e.type)).toEqual(['snapshot', 'sessions.changed'])
    expect(states).toEqual(['connecting', 'open'])
  })

  it('reconnects with exponential backoff, capped, and resets after a snapshot', async () => {
    const { conn, sockets, timers } = setup()
    conn.start()
    sockets[0].send(snapshot)
    for (let i = 0; i < 7; i++) {
      sockets.at(-1)!.drop()
      await tick()
      timers.at(-1)!.fn()
    }
    expect(timers.map((t) => t.ms)).toEqual([500, 1000, 2000, 4000, 8000, 10000, 10000])
    // A snapshot means we're resynced: the next drop starts over.
    sockets.at(-1)!.send(snapshot)
    sockets.at(-1)!.drop()
    await tick()
    expect(timers.at(-1)!.ms).toBe(500)
  })

  it('resyncs from the snapshot of each new connection', async () => {
    const { conn, sockets, timers, events } = setup()
    conn.start()
    sockets[0].send(snapshot)
    sockets[0].drop()
    await tick()
    timers[0].fn()
    expect(sockets).toHaveLength(2)
    sockets[1].send({ ...snapshot, sessions: { host: [{ name: 'a' }] } })
    expect(events.filter((e) => e.type === 'snapshot')).toHaveLength(2)
    expect(conn.state).toBe('open')
  })

  it('stops when the session has ended', async () => {
    let checks = 0
    const { conn, sockets, timers } = setup(async () => {
      checks++
      return false
    })
    conn.start()
    sockets[0].drop() // refused before any snapshot (e.g. 401)
    await tick()
    expect(checks).toBe(1)
    expect(timers).toHaveLength(0)
    expect(conn.state).toBe('idle')
  })

  it('stop closes the socket and cancels reconnects', async () => {
    const { conn, sockets, timers } = setup()
    conn.start()
    sockets[0].send(snapshot)
    conn.stop()
    expect(sockets[0].closed).toBe(true)
    sockets[0].drop()
    await tick()
    expect(timers).toHaveLength(0)
  })
})
