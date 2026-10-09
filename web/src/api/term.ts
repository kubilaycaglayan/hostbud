// Client for /ws/term (internal/term): binary frames carry terminal bytes,
// JSON text frames carry control messages.

export interface TermSocket {
  binaryType: BinaryType
  bufferedAmount?: number
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
  onState: (s: TermState, exitCode?: number, disconnect?: { code: number | null; reason: string }) => void
  onDiagnostics?: (value: TerminalDiagnostics) => void
}

export interface TerminalDiagnostics {
  pingMs: number | null
  pingP95Ms: number | null
  inputAckMs: number | null
  inputAckP95Ms: number | null
  ptyWriteMs: number | null
  ptyWriteP95Ms: number | null
  remoteSSHMs: number | null
  remoteSSHP95Ms: number | null
  remoteSSHCommandMs: number | null
  remoteSSHCommandP95Ms: number | null
  remoteSSHOK: boolean | null
  bufferedBytes: number
  inputCount: number
  pendingInputs: number
  outputBytes: number
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
export const DIAGNOSTICS_PING_EVERY_MS = 2_000
export const DIAGNOSTICS_SSH_PROBE_EVERY_MS = 10_000
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
  private remotePinger: unknown = null
  private silence: unknown = null
  private diagnosticsEnabled = false
  private nextProbeID = 0
  private pendingPings = new Map<number, number>()
  private pendingInputs = new Map<number, number>()
  private pendingRemote = new Map<number, number>()
  private pingSamples: number[] = []
  private inputSamples: number[] = []
  private writeSamples: number[] = []
  private remoteSamples: number[] = []
  private remoteCommandSamples: number[] = []
  private diagnostics: TerminalDiagnostics = { pingMs: null, pingP95Ms: null, inputAckMs: null, inputAckP95Ms: null, ptyWriteMs: null, ptyWriteP95Ms: null, remoteSSHMs: null, remoteSSHP95Ms: null, remoteSSHCommandMs: null, remoteSSHCommandP95Ms: null, remoteSSHOK: null, bufferedBytes: 0, inputCount: 0, pendingInputs: 0, outputBytes: 0 }
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
      this.pinger = timers.setInterval(() => this.sendPing(), this.diagnosticsEnabled ? DIAGNOSTICS_PING_EVERY_MS : PING_EVERY_MS)
      this.set('open')
      if (this.diagnosticsEnabled) this.startRemoteProbes()
    }
    s.onmessage = (m) => {
      this.alive()
      if (typeof m.data !== 'string') {
        const bytes = new Uint8Array(m.data as ArrayBuffer)
        if (this.diagnosticsEnabled) this.diagnostics.outputBytes += bytes.length
        h.onData(bytes)
        return
      }
      let ctl: { type?: string; code?: number; id?: number; writeMs?: number; durationMs?: number; ok?: boolean }
      try {
        ctl = JSON.parse(m.data)
      } catch {
        return
      }
      if (ctl.type === 'pong' && ctl.id !== undefined) {
        const sentAt = this.pendingPings.get(ctl.id)
        if (sentAt !== undefined) {
          this.diagnostics.pingMs = performance.now() - sentAt
          this.pingP95Ms(this.diagnostics.pingMs)
          this.pendingPings.delete(ctl.id)
          this.publishDiagnostics()
        }
      }
      if (ctl.type === 'inputAck' && ctl.id !== undefined) {
        const sentAt = this.pendingInputs.get(ctl.id)
        if (sentAt !== undefined) {
          this.diagnostics.inputAckMs = performance.now() - sentAt
          this.diagnostics.ptyWriteMs = ctl.writeMs ?? null
          this.inputP95Ms(this.diagnostics.inputAckMs)
          if (ctl.writeMs !== undefined) this.writeP95Ms(ctl.writeMs)
          this.pendingInputs.delete(ctl.id)
          this.publishDiagnostics()
        }
      }
      if (ctl.type === 'remoteProbeAck' && ctl.id !== undefined) {
        const sentAt = this.pendingRemote.get(ctl.id)
        if (sentAt !== undefined) {
          this.diagnostics.remoteSSHMs = performance.now() - sentAt
          this.diagnostics.remoteSSHOK = ctl.ok === true
          this.diagnostics.remoteSSHP95Ms = this.addSample(this.remoteSamples, this.diagnostics.remoteSSHMs)
          if (ctl.durationMs !== undefined) {
            this.diagnostics.remoteSSHCommandMs = ctl.durationMs
            this.diagnostics.remoteSSHCommandP95Ms = this.addSample(this.remoteCommandSamples, ctl.durationMs)
          }
          this.pendingRemote.delete(ctl.id)
          this.publishDiagnostics()
        }
      }
      if (ctl.type === 'exit') this.set('exited', ctl.code ?? 0)
    }
    s.onclose = (event) => {
      this.stopTimers()
      if (this.state !== 'exited' && this.state !== 'disconnected') {
        this.set('disconnected', undefined, {
          code: event.code || null,
          reason: typeof event.reason === 'string' && event.reason.length > 0 ? event.reason.slice(0, 120) : 'No reason provided by peer',
        })
      }
    }
  }

  /** A frame arrived (or we just started): restart the silence countdown. */
  private alive() {
    if (this.silence !== null) this.timers.clearTimeout(this.silence)
    this.silence = this.timers.setTimeout(() => {
      this.silence = null
      // Hung: close without waiting for the (never coming) close event.
      this.close()
      this.set('disconnected', undefined, { code: null, reason: `No WebSocket frames received for ${SILENCE_LIMIT_MS / 1000} seconds` })
    }, SILENCE_LIMIT_MS)
  }

  private stopTimers() {
    if (this.pinger !== null) this.timers.clearInterval(this.pinger)
    if (this.remotePinger !== null) this.timers.clearInterval(this.remotePinger)
    if (this.silence !== null) this.timers.clearTimeout(this.silence)
    this.pinger = this.remotePinger = this.silence = null
  }

  private set(s: TermState, code?: number, disconnect?: { code: number | null; reason: string }) {
    this.state = s
    this.h.onState(s, code, disconnect)
  }

  private control(msg: object) {
    if (this.state === 'open') this.socket.send(JSON.stringify(msg))
  }

  private sendPing() {
    if (this.state !== 'open') return
    const id = ++this.nextProbeID
    this.pendingPings.set(id, performance.now())
    this.control({ type: 'ping', id })
  }

  private sendRemoteProbe() {
    if (this.state !== 'open') return
    const id = ++this.nextProbeID
    this.pendingRemote.set(id, performance.now())
    while (this.pendingRemote.size > 2) this.pendingRemote.delete(this.pendingRemote.keys().next().value!)
    this.control({ type: 'remoteProbe', id })
  }

  private startRemoteProbes() {
    if (this.remotePinger !== null) this.timers.clearInterval(this.remotePinger)
    this.sendRemoteProbe()
    this.remotePinger = this.timers.setInterval(() => this.sendRemoteProbe(), DIAGNOSTICS_SSH_PROBE_EVERY_MS)
  }

  private publishDiagnostics() {
    this.diagnostics.bufferedBytes = this.socket.bufferedAmount ?? 0
    this.diagnostics.pendingInputs = this.pendingInputs.size
    this.h.onDiagnostics?.({ ...this.diagnostics })
  }

  private addSample(samples: number[], value: number): number {
    samples.push(value)
    if (samples.length > 50) samples.shift()
    return [...samples].sort((a, b) => a - b)[Math.ceil(samples.length * 0.95) - 1]
  }

  private pingP95Ms(value: number) { this.diagnostics.pingP95Ms = this.addSample(this.pingSamples, value) }
  private inputP95Ms(value: number) { this.diagnostics.inputAckP95Ms = this.addSample(this.inputSamples, value) }
  private writeP95Ms(value: number) { this.diagnostics.ptyWriteP95Ms = this.addSample(this.writeSamples, value) }

  setDiagnostics(enabled: boolean) {
    const startingDiagnostics = enabled && !this.diagnosticsEnabled
    this.diagnosticsEnabled = enabled
    if (startingDiagnostics) {
      this.pendingInputs.clear()
      this.pendingPings.clear()
      this.pendingRemote.clear()
      this.pingSamples = []
      this.inputSamples = []
      this.writeSamples = []
      this.remoteSamples = []
      this.remoteCommandSamples = []
      this.diagnostics = { pingMs: null, pingP95Ms: null, inputAckMs: null, inputAckP95Ms: null, ptyWriteMs: null, ptyWriteP95Ms: null, remoteSSHMs: null, remoteSSHP95Ms: null, remoteSSHCommandMs: null, remoteSSHCommandP95Ms: null, remoteSSHOK: null, bufferedBytes: 0, inputCount: 0, pendingInputs: 0, outputBytes: 0 }
    }
    if (!enabled) {
      this.pendingInputs.clear()
      this.pendingPings.clear()
      this.pendingRemote.clear()
      if (this.remotePinger !== null) this.timers.clearInterval(this.remotePinger)
      this.remotePinger = null
    }
    if (this.pinger !== null) this.timers.clearInterval(this.pinger)
    this.pinger = this.state === 'open'
      ? this.timers.setInterval(() => this.sendPing(), enabled ? DIAGNOSTICS_PING_EVERY_MS : PING_EVERY_MS)
      : null
    if (enabled && this.state === 'open') this.startRemoteProbes()
    if (enabled) this.publishDiagnostics()
  }

  /** Keyboard input (xterm's onData); dropped unless open. */
  send(text: string) {
    if (this.state !== 'open') return
    if (this.diagnosticsEnabled) {
      const id = ++this.nextProbeID
      this.pendingInputs.set(id, performance.now())
      while (this.pendingInputs.size > 32) this.pendingInputs.delete(this.pendingInputs.keys().next().value!)
      this.control({ type: 'inputProbe', id })
    }
    this.socket.send(this.encoder.encode(text))
    if (this.diagnosticsEnabled) {
      this.diagnostics.inputCount++
      this.publishDiagnostics()
    }
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
export type SessionState = 'connecting' | 'open' | 'reconnecting' | 'exited' | 'disconnected' | 'limited'

export interface TermSessionOptions {
  /** The attach URL, built at each attempt (so it has the current size). */
  url: () => string
  onData: (bytes: Uint8Array) => void
  onDiagnostics?: (value: TerminalDiagnostics) => void
  onState: (s: SessionState, info: { attempt: number; exitCode?: number; lastDisconnect: { code: number | null; reason: string } | null }) => void
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
  /** A resize made before the attach opened; sent once it does. */
  private pendingSize: { cols: number; rows: number } | null = null
  private timer: unknown = null
  private stable: unknown = null
  private attempt = 0
  private closed = false
  private diagnosticsEnabled = false
  private lastDisconnect: { code: number | null; reason: string } | null = null
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
    this.o.onState(s, { attempt: this.attempt, exitCode, lastDisconnect: this.lastDisconnect })
  }

  private connect() {
    this.conn?.close()
    this.set(this.attempt === 0 ? 'connecting' : 'reconnecting')
    const conn = new TermConnection(
      this.o.url(),
      {
        onData: this.o.onData,
        onDiagnostics: this.o.onDiagnostics,
        onState: (s, code, disconnect) => {
          if (this.conn !== conn || this.closed) return
          if (s === 'open') {
            // The URL carried the size when it was built; a resize since
            // then was dropped (not open yet) and would leave tmux stale.
            if (this.pendingSize) conn.resize(this.pendingSize.cols, this.pendingSize.rows)
            this.pendingSize = null
            this.set('open')
            this.stable = this.timers.setTimeout(() => {
              this.stable = null
              this.attempt = 0
            }, STABLE_MS)
          } else if (s === 'exited' && code !== SSH_FAILED) {
            this.clearStable()
            this.set('exited', code)
          } else if (s !== 'connecting') void this.dropped(conn.opened, disconnect)
        },
      },
      this.o.createSocket,
      this.timers,
    )
    this.conn = conn
    conn.setDiagnostics(this.diagnosticsEnabled)
  }

  setDiagnostics(enabled: boolean) {
    this.diagnosticsEnabled = enabled
    this.conn?.setDiagnostics(enabled)
  }

  private clearStable() {
    if (this.stable !== null) this.timers.clearTimeout(this.stable)
    this.stable = null
  }

  private async dropped(opened: boolean, disconnect?: { code: number | null; reason: string }) {
    if (disconnect) this.lastDisconnect = disconnect
    this.clearStable()
    if (!this.o.isListed()) {
      this.set('disconnected')
      return
    }
    if (!opened) {
      try {
        const response = await fetch('/api/runtime/terminal-slots', { credentials: 'same-origin' })
        if (response.status === 429) {
          this.set('limited')
          return
        }
      } catch { /* a network failure still follows the normal reconnect path */ }
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
    if (this.state === 'open') this.conn?.resize(cols, rows)
    else this.pendingSize = { cols, rows }
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
