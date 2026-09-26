// Client for /ws/term (internal/term): binary frames carry terminal bytes,
// JSON text frames carry control messages.

export interface TermSocket {
  binaryType: BinaryType
  onopen: ((ev: Event) => void) | null
  onmessage: ((ev: MessageEvent) => void) | null
  onclose: ((ev: CloseEvent) => void) | null
  onerror: ((ev: Event) => void) | null
  send(data: string | BufferSource): void
  close(): void
}

/** open → exited (tmux attach ended: detach or the session exited) or
 * disconnected (the socket dropped without an exit frame). */
export type TermState = 'connecting' | 'open' | 'exited' | 'disconnected'

export interface TermHandlers {
  onData: (bytes: Uint8Array) => void
  onState: (s: TermState, exitCode?: number) => void
}

export function termURL(machine: string, session: string, cols: number, rows: number, loc: Location = location): string {
  const q = new URLSearchParams({ machine, session, cols: String(cols), rows: String(rows) })
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/ws/term?${q}`
}

export class TermConnection {
  private socket: TermSocket
  private encoder = new TextEncoder()
  state: TermState = 'connecting'

  constructor(
    url: string,
    private readonly h: TermHandlers,
    create: (url: string) => TermSocket = (u) => new WebSocket(u),
  ) {
    const s = create(url)
    s.binaryType = 'arraybuffer'
    this.socket = s
    s.onopen = () => this.set('open')
    s.onmessage = (m) => {
      if (typeof m.data !== 'string') {
        h.onData(new Uint8Array(m.data as ArrayBuffer))
        return
      }
      let ctl: { type?: string; code?: number }
      try {
        ctl = JSON.parse(m.data)
      } catch {
        return
      }
      if (ctl.type === 'exit') this.set('exited', ctl.code ?? 0)
    }
    s.onclose = () => {
      if (this.state !== 'exited') this.set('disconnected')
    }
  }

  private set(s: TermState, code?: number) {
    this.state = s
    this.h.onState(s, code)
  }

  /** Keyboard input (xterm's onData). */
  send(text: string) {
    if (this.state === 'open') this.socket.send(this.encoder.encode(text))
  }

  resize(cols: number, rows: number) {
    if (this.state === 'open') this.socket.send(JSON.stringify({ type: 'resize', cols, rows }))
  }

  /** Ends the attach (the tmux session keeps running). */
  close() {
    this.socket.onclose = null
    this.socket.close()
  }
}
