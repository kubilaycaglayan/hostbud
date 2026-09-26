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
 * disconnected (the socket dropped without an exit frame, or went silent). */
export type TermState = 'connecting' | 'open' | 'exited' | 'disconnected'

export interface TermHandlers {
  onData: (bytes: Uint8Array) => void
  onState: (s: TermState, exitCode?: number) => void
}

/** Timer functions, injectable for tests. */
export interface Timers {
  setTimeout: (fn: () => void, ms: number) => unknown
  clearTimeout: (t: unknown) => void
  setInterval: (fn: () => void, ms: number) => unknown
  clearInterval: (t: unknown) => void
}

const realTimers: Timers = {
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (t) => clearTimeout(t as number),
  setInterval: (fn, ms) => setInterval(fn, ms),
  clearInterval: (t) => clearInterval(t as number),
}

/** The client pings this often; the server answers with a pong frame. */
export const PING_EVERY_MS = 10_000
/** No frame of any kind for this long ⇒ the connection is dead (a network
 * cut hangs TCP without a close). */
export const SILENCE_LIMIT_MS = 25_000

export function termURL(machine: string, session: string, cols: number, rows: number, loc: Location = location): string {
  const q = new URLSearchParams({ machine, session, cols: String(cols), rows: String(rows) })
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/ws/term?${q}`
}

/** One attach: a single WebSocket, from connecting to exited/disconnected. */
export class TermConnection {
  private socket: TermSocket
  private encoder = new TextEncoder()
  private pinger: unknown = null
  private silence: unknown = null
  state: TermState = 'connecting'
  /** Whether the socket ever opened (a failure before it did may mean the
   * sign-in session ended: the upgrade was refused). */
  opened = false

  constructor(
    url: string,
    private readonly h: TermHandlers,
    create: (url: string) => TermSocket = (u) => new WebSocket(u),
    private readonly timers: Timers = realTimers,
  ) {
    const s = create(url)
    s.binaryType = 'arraybuffer'
    this.socket = s
    this.alive() // also bounds a connect that never completes
    s.onopen = () => {
      this.opened = true
      this.alive()
      this.pinger = timers.setInterval(() => this.control({ type: 'ping' }), PING_EVERY_MS)
      this.set('open')
    }
    s.onmessage = (m) => {
      this.alive()
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
      this.stopTimers()
      if (this.state !== 'exited') this.set('disconnected')
    }
  }

  /** A frame arrived (or we just started): restart the silence countdown. */
  private alive() {
    if (this.silence !== null) this.timers.clearTimeout(this.silence)
    this.silence = this.timers.setTimeout(() => {
      this.silence = null
      // Hung: close without waiting for the (never coming) close event.
      this.close()
      this.set('disconnected')
    }, SILENCE_LIMIT_MS)
  }

  private stopTimers() {
    if (this.pinger !== null) this.timers.clearInterval(this.pinger)
    if (this.silence !== null) this.timers.clearTimeout(this.silence)
    this.pinger = this.silence = null
  }

  private set(s: TermState, code?: number) {
    this.state = s
    this.h.onState(s, code)
  }

  private control(msg: object) {
    if (this.state === 'open') this.socket.send(JSON.stringify(msg))
  }

  /** Keyboard input (xterm's onData); dropped unless open. */
  send(text: string) {
    if (this.state === 'open') this.socket.send(this.encoder.encode(text))
  }

  resize(cols: number, rows: number) {
    this.control({ type: 'resize', cols, rows })
  }

  /** Ends the attach (the tmux session keeps running). */
  close() {
    this.stopTimers()
    this.socket.onclose = null
    this.socket.onmessage = null
    this.socket.close()
  }
}

/**
 * What a terminal view shows: connecting (first attach), open, reconnecting
 * (the attach dropped; retrying by itself), exited (detach or the session
 * ended: no retry) or disconnected (gave up: the session is no longer
 * listed).
 */
export type SessionState = 'connecting' | 'open' | 'reconnecting' | 'exited' | 'disconnected'

export interface TermSessionOptions {
  /** The attach URL, built at each attempt (so it has the current size). */
  url: () => string
  onData: (bytes: Uint8Array) => void
  onState: (s: SessionState, info: { attempt: number; exitCode?: number }) => void
  /** Whether the session is still in the live list (no retry once it isn't). */
  isListed: () => boolean
  /** Called when an attempt fails before its socket opens; false (the
   * sign-in session ended) stops retrying and calls onSignedOut. */
  stillAuthorized: () => Promise<boolean>
  onSignedOut: () => void
  createSocket?: (url: string) => TermSocket
  timers?: Timers
  random?: () => number
  /** Window/document to watch for `online` and visibility changes. */
  win?: Pick<Window, 'addEventListener' | 'removeEventListener'>
  doc?: Pick<Document, 'addEventListener' | 'removeEventListener' | 'visibilityState'>
}

/** ssh's exit code for its own failures (host unreachable, a dead
 * ControlMaster): not tmux ending, so retried like a drop. */
export const SSH_FAILED = 255
/** The backoff starts over once an attach has stayed up this long (an
 * attach that opens and then fails in ssh must not retry every 0.5 s). */
export const STABLE_MS = 5_000

const BACKOFF_MS = [500, 1000, 2000, 4000, 8000]
const BACKOFF_MAX_MS = 10_000
const JITTER = 0.2

/** Delay before reconnect attempt n (1-based), ±20 % jitter. */
export function backoff(n: number, random: () => number = Math.random): number {
  const base = BACKOFF_MS[n - 1] ?? BACKOFF_MAX_MS
  return Math.round(base * (1 - JITTER + 2 * JITTER * random()))
}

/**
 * A terminal's attach that survives drops: it re-attaches with backoff until
 * it works again (no attempt cap), right away when the browser comes back
 * online or the page becomes visible. It never retries after an exit frame
 * (except ssh's own failure, SSH_FAILED),
 * after close(), for a session that left the live list, or once signed out.
 */
export class TermSession {
  private conn: TermConnection | null = null
  private timer: unknown = null
  private stable: unknown = null
  private attempt = 0
  private closed = false
  private readonly timers: Timers
  state: SessionState = 'connecting'

  constructor(private readonly o: TermSessionOptions) {
    this.timers = o.timers ?? realTimers
    ;(o.win ?? window).addEventListener('online', this.retryNow)
    ;(o.doc ?? document).addEventListener('visibilitychange', this.onVisible)
    this.connect()
  }

  private readonly onVisible = () => {
    if ((this.o.doc ?? document).visibilityState === 'visible') this.retryNow()
  }

  /** Retries at once if waiting between attempts. */
  readonly retryNow = () => {
    if (this.state !== 'reconnecting' || this.timer === null) return
    this.timers.clearTimeout(this.timer)
    this.timer = null
    this.connect()
  }

  private set(s: SessionState, exitCode?: number) {
    this.state = s
    this.o.onState(s, { attempt: this.attempt, exitCode })
  }

  private connect() {
    this.conn?.close()
    this.set(this.attempt === 0 ? 'connecting' : 'reconnecting')
    const conn = new TermConnection(
      this.o.url(),
      {
        onData: this.o.onData,
        onState: (s, code) => {
          if (this.conn !== conn || this.closed) return
          if (s === 'open') {
            this.set('open')
            this.stable = this.timers.setTimeout(() => {
              this.stable = null
              this.attempt = 0
            }, STABLE_MS)
          } else if (s === 'exited' && code !== SSH_FAILED) {
            this.clearStable()
            this.set('exited', code)
          } else if (s !== 'connecting') void this.dropped(conn.opened)
        },
      },
      this.o.createSocket,
      this.timers,
    )
    this.conn = conn
  }

  private clearStable() {
    if (this.stable !== null) this.timers.clearTimeout(this.stable)
    this.stable = null
  }

  private async dropped(opened: boolean) {
    this.clearStable()
    if (!this.o.isListed()) {
      this.set('disconnected')
      return
    }
    if (!opened && !(await this.o.stillAuthorized())) {
      this.close()
      this.o.onSignedOut()
      return
    }
    if (this.closed) return
    this.attempt++
    this.set('reconnecting')
    this.timer = this.timers.setTimeout(() => {
      this.timer = null
      if (!this.closed) this.connect()
    }, backoff(this.attempt, this.o.random))
  }

  /** Keyboard input: dropped while not attached (typing into a screen that
   * will look different by the time it arrives is worse than losing it). */
  send(text: string) {
    this.conn?.send(text)
  }

  resize(cols: number, rows: number) {
    this.conn?.resize(cols, rows)
  }

  /** Ends the attach and stops retrying. */
  close() {
    this.closed = true
    if (this.timer !== null) this.timers.clearTimeout(this.timer)
    this.timer = null
    this.clearStable()
    ;(this.o.win ?? window).removeEventListener('online', this.retryNow)
    ;(this.o.doc ?? document).removeEventListener('visibilitychange', this.onVisible)
    this.conn?.close()
    this.conn = null
  }
}
