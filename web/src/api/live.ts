import type { ServerEvent } from './types'

// The /ws/events connection: reconnects with exponential backoff and relies
// on the server's snapshot (sent first on every connection) to resync. The
// server sends a heartbeat every 15 s; silence for SILENCE_LIMIT_MS means a
// hung connection (a network cut doesn't close it), which is then replaced.

/** No frame for this long ⇒ the connection is dead. */
export const SILENCE_LIMIT_MS = 40_000

export interface SocketLike {
  onopen: ((ev: Event) => void) | null
  onmessage: ((ev: MessageEvent) => void) | null
  onclose: ((ev: CloseEvent) => void) | null
  onerror: ((ev: Event) => void) | null
  close(): void
}

export type LiveState = 'idle' | 'connecting' | 'open' | 'reconnecting'

export interface LiveOptions {
  url: string
  onEvent: (e: ServerEvent) => void
  onState?: (s: LiveState) => void
  /** Called when a connection fails before its snapshot; resolve false
   * (e.g. the session ended) to stop reconnecting. */
  stillAuthorized?: () => Promise<boolean>
  createSocket?: (url: string) => SocketLike
  setTimer?: (fn: () => void, ms: number) => unknown
  clearTimer?: (t: unknown) => void
  minDelay?: number
  maxDelay?: number
  silenceLimit?: number
}

export class LiveConnection {
  private socket: SocketLike | null = null
  private timer: unknown = null
  private silence: unknown = null
  private attempt = 0
  private running = false
  private synced = false
  state: LiveState = 'idle'

  constructor(private readonly o: LiveOptions) {}

  start() {
    if (this.running) return
    this.running = true
    this.attempt = 0
    this.connect()
  }

  stop() {
    this.running = false
    if (this.timer !== null) (this.o.clearTimer ?? clearTimeout)(this.timer as number)
    this.timer = null
    this.stopSilence()
    const s = this.socket
    this.socket = null
    if (s) {
      s.onclose = null
      s.close()
    }
    this.setState('idle')
  }

  private stopSilence() {
    if (this.silence !== null) (this.o.clearTimer ?? clearTimeout)(this.silence as number)
    this.silence = null
  }

  /** Restarts the silence countdown for socket s. */
  private alive(s: SocketLike) {
    this.stopSilence()
    this.silence = (this.o.setTimer ?? setTimeout)(() => {
      this.silence = null
      if (this.socket !== s) return
      // Hung: drop it without waiting for a close event that won't come.
      s.onclose = null
      s.close()
      this.socket = null
      if (this.running) void this.retry(!this.synced)
    }, this.o.silenceLimit ?? SILENCE_LIMIT_MS)
  }

  /** Delay before reconnect attempt n (0-based): min·2^n, capped. */
  delay(n: number): number {
    const min = this.o.minDelay ?? 500
    return Math.min(this.o.maxDelay ?? 10_000, min * 2 ** n)
  }

  private setState(s: LiveState) {
    this.state = s
    this.o.onState?.(s)
  }

  private connect() {
    this.synced = false
    this.setState(this.attempt === 0 ? 'connecting' : 'reconnecting')
    const create = this.o.createSocket ?? ((url: string) => new WebSocket(url))
    const s = create(this.o.url)
    this.socket = s
    this.alive(s)
    s.onmessage = (m) => {
      this.alive(s)
      let e: ServerEvent
      try {
        e = JSON.parse(String(m.data))
      } catch {
        return
      }
      if (e.type === 'snapshot') {
        this.synced = true
        this.attempt = 0
        this.setState('open')
      }
      if (e.type !== 'heartbeat') this.o.onEvent(e)
    }
    s.onclose = () => {
      if (this.socket !== s) return
      this.stopSilence()
      this.socket = null
      if (this.running) void this.retry(!this.synced)
    }
  }

  private async retry(failedEarly: boolean) {
    if (failedEarly && this.o.stillAuthorized && !(await this.o.stillAuthorized())) {
      this.stop()
      return
    }
    if (!this.running) return
    this.setState('reconnecting')
    const d = this.delay(this.attempt++)
    this.timer = (this.o.setTimer ?? setTimeout)(() => {
      this.timer = null
      if (this.running) this.connect()
    }, d)
  }
}

export function eventsURL(loc: Location = location): string {
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/ws/events`
}
