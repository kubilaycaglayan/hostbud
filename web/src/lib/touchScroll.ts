/** Touch swipes over the terminal scroll tmux (or the app in it), never
 * xterm's own buffer: xterm only holds what reached this browser since it
 * attached, with gaps where tmux or a full-screen app redrew in place.
 * A finger moving down reveals older output ('up'), like native scrolling.
 * Single-finger moves never reach xterm or the browser, so the page and
 * xterm's viewport stay put; taps and long presses are unaffected.
 *
 * Momentum (experimental, M8 T8): a flick keeps scrolling after release and
 * slows with friction; a flick in the same direction while it still moves adds
 * to its speed, so repeated flicks go fast. Touching stops it. Slow drags stay
 * 1:1. The caller coalesces the resulting lines into rate-limited requests. */
export const TOUCH_SCROLL_THRESHOLD_PX = 8
/** Release speeds below this (px/ms) are a drag, not a flick. */
export const FLING_MIN_VELOCITY = 0.3
/** Momentum speed cap (px/ms), also after stacked flicks. */
export const FLING_MAX_VELOCITY = 12
/** Friction: velocity decays by e every this many ms (iOS-like). */
export const FLING_TIME_CONSTANT_MS = 325
const FLING_STOP_VELOCITY = 0.02
const VELOCITY_WINDOW_MS = 100

export interface TouchScrollClock {
  now: () => number
  requestAnimationFrame: (fn: FrameRequestCallback) => number
  cancelAnimationFrame: (id: number) => void
}

const realClock: TouchScrollClock = {
  now: () => performance.now(),
  requestAnimationFrame: (fn) => requestAnimationFrame(fn),
  cancelAnimationFrame: (id) => cancelAnimationFrame(id),
}

export function attachTouchScroll(
  element: HTMLElement,
  cellHeight: () => number,
  scroll: (direction: 'up' | 'down', lines: number) => void,
  clock: TouchScrollClock = realClock,
): () => void {
  let gesture: { x: number; y: number; active: boolean; sent: number; samples: { t: number; y: number }[] } | null = null
  // Momentum state: velocity in px/ms (positive: finger down, older output),
  // pixels not yet emitted as whole lines, and the running frame.
  let velocity = 0
  let carry = 0
  let remainder = 0
  let frame: number | null = null
  let lastFrame = 0

  const emit = (px: number) => {
    remainder += px
    const lines = Math.trunc(remainder / Math.max(1, cellHeight()))
    if (!lines) return
    remainder -= lines * Math.max(1, cellHeight())
    scroll(lines > 0 ? 'up' : 'down', Math.abs(lines))
  }

  const stopMomentum = () => {
    if (frame !== null) clock.cancelAnimationFrame(frame)
    frame = null
    velocity = 0
    remainder = 0
  }

  const step = () => {
    frame = null
    const now = clock.now()
    const dt = Math.min(100, now - lastFrame)
    lastFrame = now
    emit(velocity * dt)
    velocity *= Math.exp(-dt / FLING_TIME_CONSTANT_MS)
    if (Math.abs(velocity) < FLING_STOP_VELOCITY) return stopMomentum()
    frame = clock.requestAnimationFrame(step)
  }

  const fling = (v: number) => {
    stopMomentum()
    velocity = Math.max(-FLING_MAX_VELOCITY, Math.min(FLING_MAX_VELOCITY, v))
    lastFrame = clock.now()
    frame = clock.requestAnimationFrame(step)
  }

  const start = (event: TouchEvent) => {
    const touch = event.touches[0]
    // Touching stops momentum; its speed carries into a same-direction flick.
    carry = velocity
    stopMomentum()
    gesture = event.touches.length === 1 && touch
      ? { x: touch.clientX, y: touch.clientY, active: false, sent: 0, samples: [] }
      : null
  }
  const move = (event: TouchEvent) => {
    const touch = event.touches[0]
    if (!gesture || event.touches.length !== 1 || !touch) return
    event.preventDefault()
    event.stopImmediatePropagation()
    const dx = touch.clientX - gesture.x
    const dy = touch.clientY - gesture.y
    if (!gesture.active) {
      if (Math.abs(dy) < TOUCH_SCROLL_THRESHOLD_PX || Math.abs(dy) <= Math.abs(dx)) return
      gesture.active = true
      gesture.y = touch.clientY
      gesture.samples = [{ t: clock.now(), y: touch.clientY }]
      return
    }
    const now = clock.now()
    gesture.samples.push({ t: now, y: touch.clientY })
    while (gesture.samples.length > 2 && now - gesture.samples[0]!.t > VELOCITY_WINDOW_MS) gesture.samples.shift()
    const total = Math.trunc(dy / Math.max(1, cellHeight()))
    const delta = total - gesture.sent
    if (!delta) return
    gesture.sent = total
    scroll(delta > 0 ? 'up' : 'down', Math.abs(delta))
  }
  const end = () => {
    const g = gesture
    gesture = null
    const stacked = carry
    carry = 0
    if (!g?.active || g.samples.length < 2) return
    const first = g.samples[0]!
    const last = g.samples[g.samples.length - 1]!
    // A finger resting before release is a drag, not a flick.
    if (clock.now() - last.t > VELOCITY_WINDOW_MS || last.t <= first.t) return
    const v = (last.y - first.y) / (last.t - first.t)
    if (Math.abs(v) < FLING_MIN_VELOCITY) return
    fling(Math.sign(v) === Math.sign(stacked) ? v + stacked : v)
  }
  const cancel = () => {
    gesture = null
    carry = 0
  }

  element.addEventListener('touchstart', start, { capture: true, passive: true })
  element.addEventListener('touchmove', move, { capture: true, passive: false })
  element.addEventListener('touchend', end, { capture: true })
  element.addEventListener('touchcancel', cancel, { capture: true })
  return () => {
    stopMomentum()
    element.removeEventListener('touchstart', start, true)
    element.removeEventListener('touchmove', move, true)
    element.removeEventListener('touchend', end, true)
    element.removeEventListener('touchcancel', cancel, true)
  }
}
