import { ref } from 'vue'
import type { CopyModeAction, CopyModeState } from '@/api/client'

export type CopyModeCall = (action: CopyModeAction, lines?: number) => Promise<CopyModeState>
export interface CopyModeTimers {
  now: () => number
  setTimeout: (fn: () => void, ms: number) => ReturnType<typeof setTimeout>
  clearTimeout: (id: ReturnType<typeof setTimeout>) => void
  requestAnimationFrame: (fn: FrameRequestCallback) => number
  cancelAnimationFrame: (id: number) => void
}

export function swipeDelta(start: { x: number; y: number }, end: { x: number; y: number }, cellHeight: number) {
  const dx = end.x - start.x
  const dy = end.y - start.y
  if (Math.abs(dy) < 10 || Math.abs(dy) <= Math.abs(dx)) return null
  return {
    direction: dy < 0 ? 'up' as const : 'down' as const,
    lines: Math.max(1, Math.min(500, Math.round(Math.abs(dy) / Math.max(1, cellHeight)))),
  }
}

const realTimers: CopyModeTimers = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (id) => clearTimeout(id),
  requestAnimationFrame: (fn) => requestAnimationFrame(fn),
  cancelAnimationFrame: (id) => cancelAnimationFrame(id),
}

/** One copy-mode controller per mounted terminal pane. Swipe movement is
 * coalesced to one pending signed line count and sent at no more than 20/s. */
export function createCopyModeController(
  call: CopyModeCall,
  onError: (error: unknown) => void,
  timers: CopyModeTimers = realTimers,
) {
  const inMode = ref(false)
  const scrollPosition = ref(0)
  const historySize = ref(0)
  const busy = ref(false)
  let pendingDelta = 0
  let frame: number | null = null
  let throttle: ReturnType<typeof setTimeout> | null = null
  let lastStarted: number | null = null
  let generation = 0
  let inFlight: Promise<boolean> | null = null
  let actionTail: Promise<unknown> = Promise.resolve()

  function update(state: CopyModeState) {
    inMode.value = state.inMode
    scrollPosition.value = state.scrollPosition
    historySize.value = state.historySize
    if (!state.inMode) clearPending()
  }

  function clearPending() {
    pendingDelta = 0
    if (frame !== null) timers.cancelAnimationFrame(frame)
    if (throttle !== null) timers.clearTimeout(throttle)
    frame = throttle = null
  }

  function scheduleFrame() {
    if (frame !== null || pendingDelta === 0 || !inMode.value) return
    frame = timers.requestAnimationFrame(() => {
      frame = null
      flushSwipe()
    })
  }

  async function perform(action: CopyModeAction, lines?: number): Promise<boolean> {
    const version = generation
    busy.value = true
    const pending = (async () => {
      try {
        const state = await call(action, lines)
        if (version !== generation) return false
        update(state)
        return true
      } catch (error) {
        if (version === generation) {
          inMode.value = false
          scrollPosition.value = 0
          historySize.value = 0
          clearPending()
          onError(error)
        }
        return false
      } finally {
        if (version === generation) {
          busy.value = false
          inFlight = null
          scheduleFrame()
        }
      }
    })()
    inFlight = pending
    return pending
  }

  function action(action: CopyModeAction, lines?: number): Promise<boolean> {
    const version = generation
    const run = actionTail.then(async () => {
      if (version !== generation) return false
      if (inFlight) await inFlight
      if (version !== generation) return false
      if (action !== 'enter' && !inMode.value) return false
      return perform(action, lines)
    })
    actionTail = run.catch(() => undefined)
    return run
  }

  function flushSwipe() {
    if (busy.value || pendingDelta === 0 || !inMode.value) return
    const now = timers.now()
    if (lastStarted !== null && now - lastStarted < 50) {
      throttle = timers.setTimeout(() => {
        throttle = null
        scheduleFrame()
      }, 50 - (now - lastStarted))
      return
    }
    const lines = Math.min(500, Math.abs(pendingDelta))
    const direction = pendingDelta > 0 ? 'scroll-up' : 'scroll-down'
    pendingDelta += pendingDelta > 0 ? -lines : lines
    lastStarted = now
    void perform(direction, lines)
  }

  function swipe(direction: 'up' | 'down', lines: number) {
    if (!inMode.value || lines < 1) return
    const delta = Math.max(1, Math.min(500, Math.floor(lines))) * (direction === 'up' ? 1 : -1)
    pendingDelta = Math.max(-500, Math.min(500, pendingDelta + delta))
    scheduleFrame()
  }

  async function exitThen(send: () => void) {
    clearPending()
    await action('exit')
    send()
  }

  function reset() {
    generation++
    clearPending()
    inFlight = null
    lastStarted = null
    inMode.value = false
    scrollPosition.value = 0
    historySize.value = 0
    busy.value = false
  }

  return { inMode, scrollPosition, historySize, busy, action, swipe, exitThen, reset }
}
