import { describe, expect, it } from 'vitest'
import { TermConnection, termURL, type TermSocket, type TermState } from './term'

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

  it('close ends the socket quietly', () => {
    const { conn, socket, states } = setup()
    socket().open()
    conn.close()
    expect(socket().closed).toBe(true)
    expect(states).toEqual([['open', undefined]])
  })
})
