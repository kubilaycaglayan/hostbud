import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  backoff,
  PING_EVERY_MS,
  SILENCE_LIMIT_MS,
  SSH_FAILED,
  STABLE_MS,
  TermConnection,
  TermSession,
  termURL,
  type SessionState,
  type TermSocket,
  type TermState,
} from './term'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

class FakeSocket implements TermSocket {
  binaryType: BinaryType = 'blob'
  onopen: TermSocket['onopen'] = null
  onmessage: TermSocket['onmessage'] = null
  onclose: TermSocket['onclose'] = null
  onerror: TermSocket['onerror'] = null
  sent: (string | Uint8Array)[] = []
  closed = false
  constructor(readonly url: string) {}
  send(d: string | BufferSource) {
    this.sent.push(typeof d === 'string' ? d : new Uint8Array(ArrayBuffer.isView(d) ? d.buffer : d))
  }
  close() {
    this.closed = true
  }
  open() {
    this.onopen?.({} as Event)
  }
  recv(data: string | ArrayBuffer) {
    this.onmessage?.({ data } as MessageEvent)
  }
}

function setup() {
  let socket!: FakeSocket
  const data: string[] = []
  const states: [TermState, number | undefined][] = []
  const conn = new TermConnection(
    'ws://x/ws/term',
    { onData: (b) => data.push(new TextDecoder().decode(b)), onState: (s, c) => states.push([s, c]) },
    (u) => (socket = new FakeSocket(u)),
  )
  return { conn, socket: () => socket, data, states }
}

describe('termURL', () => {
  it('encodes the session and size', () => {
    const loc = { protocol: 'http:', host: 'localhost:9055' } as Location
    expect(termURL('host', 'my-s', 120, 40, loc)).toBe('ws://localhost:9055/ws/term?machine=host&session=my-s&cols=120&rows=40')
    expect(termURL('host', 's', 80, 24, { protocol: 'https:', host: 'h.example.com' } as Location)).toMatch(/^wss:\/\/h\.example\.com\//)
  })
})

describe('TermConnection', () => {
  it('passes binary output and sends typed input as binary', () => {
    const { conn, socket, data } = setup()
    expect(socket().binaryType).toBe('arraybuffer')
    socket().open()
    socket().recv(new TextEncoder().encode('hello').buffer)
    expect(data).toEqual(['hello'])
    conn.send('ls\r')
    expect(new TextDecoder().decode(socket().sent[0] as Uint8Array)).toBe('ls\r')
  })

  it('sends resize as a JSON control frame', () => {
    const { conn, socket } = setup()
    conn.resize(100, 30) // not open yet: dropped
    socket().open()
    conn.resize(120, 40)
    expect(socket().sent).toEqual(['{"type":"resize","cols":120,"rows":40}'])
  })

  it('exit frame ⇒ exited (with code), not disconnected', () => {
    const { socket, states } = setup()
    socket().open()
    socket().recv('{"type":"exit","code":0}')
    socket().onclose?.({} as CloseEvent)
    expect(states).toEqual([['open', undefined], ['exited', 0]])
  })

  it('a dropped socket ⇒ disconnected', () => {
    const { socket, states } = setup()
    socket().open()
    socket().onclose?.({} as CloseEvent)
    expect(states.at(-1)).toEqual(['disconnected', undefined])
  })

  it('pings every 10 s while open', () => {
    const { socket } = setup()
    socket().open()
    vi.advanceTimersByTime(PING_EVERY_MS - 1)
    expect(socket().sent).toEqual([])
    vi.advanceTimersByTime(1)
    expect(socket().sent).toEqual(['{"type":"ping"}'])
    socket().recv('{"type":"pong"}')
    vi.advanceTimersByTime(PING_EVERY_MS)
    expect(socket().sent).toEqual(['{"type":"ping"}', '{"type":"ping"}'])
  })

  it('25 s without any frame ⇒ closed and disconnected (a hung connection)', () => {
    const { socket, states } = setup()
    socket().open()
    vi.advanceTimersByTime(SILENCE_LIMIT_MS - 1000)
    socket().recv('{"type":"pong"}') // any frame counts as alive
    vi.advanceTimersByTime(SILENCE_LIMIT_MS - 1)
    expect(states.at(-1)).toEqual(['open', undefined])
    vi.advanceTimersByTime(1)
    expect(socket().closed).toBe(true)
    expect(states.at(-1)).toEqual(['disconnected', undefined])
  })

  it('a connect that never completes also times out', () => {
    const { socket, states } = setup()
    vi.advanceTimersByTime(SILENCE_LIMIT_MS)
    expect(socket().closed).toBe(true)
    expect(states).toEqual([['disconnected', undefined]])
  })

  it('close ends the socket quietly', () => {
    const { conn, socket, states } = setup()
    socket().open()
    conn.close()
    expect(socket().closed).toBe(true)
    expect(states).toEqual([['open', undefined]])
  })
})

describe('backoff', () => {
  it('0.5 s, 1, 2, 4, 8, then every 10 s, ±20 %', () => {
    const mid = () => 0.5
    expect([1, 2, 3, 4, 5, 6, 7, 50].map((n) => backoff(n, mid))).toEqual([500, 1000, 2000, 4000, 8000, 10000, 10000, 10000])
    expect(backoff(1, () => 0)).toBe(400)
    expect(backoff(1, () => 1)).toBe(600)
    expect(backoff(9, () => 0)).toBe(8000)
    expect(backoff(9, () => 1)).toBe(12000)
  })
})

describe('TermSession', () => {
  function session(o: { listed?: () => boolean; authorized?: () => Promise<boolean> } = {}) {
    const sockets: FakeSocket[] = []
    const states: [SessionState, number][] = []
    const data: string[] = []
    const win = new EventTarget()
    const doc = Object.assign(new EventTarget(), { visibilityState: 'visible' as DocumentVisibilityState })
    const signedOut = vi.fn()
    const authorized = vi.fn(o.authorized ?? (async () => true))
    let size = 80
    const s = new TermSession({
      url: () => `ws://x/ws/term?cols=${size}`,
      onData: (b) => data.push(new TextDecoder().decode(b)),
      onState: (st, info) => states.push([st, info.attempt]),
      isListed: o.listed ?? (() => true),
      stillAuthorized: authorized,
      onSignedOut: signedOut,
      createSocket: (u) => {
        const f = new FakeSocket(u)
        sockets.push(f)
        return f
      },
      random: () => 0.5,
      win,
      doc,
    })
    return { s, sockets, states, data, win, doc, signedOut, authorized, resize: (n: number) => (size = n) }
  }
  const last = <T>(a: T[]) => a[a.length - 1]

  it('re-attaches after a drop with backoff, at the current size; the backoff resets once stable', async () => {
    const { sockets, states, resize } = session()
    sockets[0].open()
    resize(120)
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(last(states)).toEqual(['reconnecting', 1])
    await vi.advanceTimersByTimeAsync(499)
    expect(sockets).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets).toHaveLength(2)
    expect(sockets[1].url).toBe('ws://x/ws/term?cols=120')
    // Opens, then drops at once: the backoff goes on (1 s).
    sockets[1].open()
    expect(last(states)[0]).toBe('open')
    sockets[1].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(last(states)).toEqual(['reconnecting', 2])
    await vi.advanceTimersByTimeAsync(999)
    expect(sockets).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets).toHaveLength(3)
    // Up for 5 s: the next drop starts over at 0.5 s.
    sockets[2].open()
    await vi.advanceTimersByTimeAsync(STABLE_MS)
    sockets[2].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(last(states)).toEqual(['reconnecting', 1])
    await vi.advanceTimersByTimeAsync(500)
    expect(sockets).toHaveLength(4)
  })

  it('ssh failing (exit 255: host unreachable) is retried like a drop', async () => {
    const { sockets, states } = session()
    sockets[0].open()
    sockets[0].recv(`{"type":"exit","code":${SSH_FAILED}}`)
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(last(states)).toEqual(['reconnecting', 1])
    await vi.advanceTimersByTimeAsync(500)
    expect(sockets).toHaveLength(2)
  })

  it('retries at once when the browser comes back online or the page becomes visible', async () => {
    const { sockets, win, doc } = session()
    sockets[0].open()
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    win.dispatchEvent(new Event('online'))
    expect(sockets).toHaveLength(2)
    sockets[1].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    doc.visibilityState = 'hidden'
    doc.dispatchEvent(new Event('visibilitychange'))
    expect(sockets).toHaveLength(2)
    doc.visibilityState = 'visible'
    doc.dispatchEvent(new Event('visibilitychange'))
    expect(sockets).toHaveLength(3)
    // Not while attached.
    sockets[2].open()
    win.dispatchEvent(new Event('online'))
    expect(sockets).toHaveLength(3)
  })

  it('no retry after an exit frame', async () => {
    const { sockets, states } = session()
    sockets[0].open()
    sockets[0].recv('{"type":"exit","code":0}')
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets).toHaveLength(1)
    expect(last(states)).toEqual(['exited', 0])
  })

  it('no retry after close()', async () => {
    const { s, sockets, win } = session()
    sockets[0].open()
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    s.close()
    await vi.advanceTimersByTimeAsync(60_000)
    win.dispatchEvent(new Event('online'))
    expect(sockets).toHaveLength(1)
  })

  it('no retry for a session that left the live list', async () => {
    const { sockets, states } = session({ listed: () => false })
    sockets[0].open()
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets).toHaveLength(1)
    expect(last(states)).toEqual(['disconnected', 0])
  })

  it('a failure before the socket opens checks sign-in; signed out ⇒ stop and hand over', async () => {
    const { sockets, signedOut, authorized } = session({ authorized: async () => false })
    sockets[0].onclose?.({} as CloseEvent) // upgrade refused
    await vi.advanceTimersByTimeAsync(60_000)
    expect(authorized).toHaveBeenCalledTimes(1)
    expect(signedOut).toHaveBeenCalledTimes(1)
    expect(sockets).toHaveLength(1)
  })

  it("a drop after opening doesn't ask about sign-in", async () => {
    const { sockets, authorized } = session()
    sockets[0].open()
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(authorized).not.toHaveBeenCalled()
  })

  it('input is dropped while disconnected', async () => {
    const { s, sockets } = session()
    sockets[0].open()
    sockets[0].onclose?.({} as CloseEvent)
    await vi.advanceTimersByTimeAsync(0)
    s.send('lost')
    await vi.advanceTimersByTimeAsync(500)
    sockets[1].open()
    s.send('kept')
    expect(sockets[0].sent).toEqual([])
    expect(sockets[1].sent.map((d) => new TextDecoder().decode(d as Uint8Array))).toEqual(['kept'])
  })

  it('a hung connection (silence) re-attaches too', async () => {
    const { sockets, states } = session()
    sockets[0].open()
    await vi.advanceTimersByTimeAsync(SILENCE_LIMIT_MS)
    expect(sockets[0].closed).toBe(true)
    expect(last(states)).toEqual(['reconnecting', 1])
    await vi.advanceTimersByTimeAsync(500)
    expect(sockets).toHaveLength(2)
  })
})
