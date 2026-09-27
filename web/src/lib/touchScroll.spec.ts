import { describe, expect, it, vi } from 'vitest'
import { attachTouchScroll, FLING_MAX_VELOCITY, type TouchScrollClock } from './touchScroll'

function touch(type: string, points: { x: number; y: number }[]) {
  const event = new Event(type, { cancelable: true, bubbles: true }) as TouchEvent
  Object.defineProperty(event, 'touches', { value: points.map((p) => ({ clientX: p.x, clientY: p.y })) })
  return event
}

function fakeClock() {
  let now = 0
  let next = 1
  const frames = new Map<number, FrameRequestCallback>()
  const clock: TouchScrollClock = {
    now: () => now,
    requestAnimationFrame: (fn) => { frames.set(next, fn); return next++ },
    cancelAnimationFrame: (id) => { frames.delete(id) },
  }
  return {
    clock,
    advance: (ms: number) => { now += ms },
    /** Runs frames 16 ms apart until momentum stops (or a limit). */
    run(limit = 1000) {
      let count = 0
      while (frames.size && count++ < limit) {
        now += 16
        const [id, fn] = [...frames][0]!
        frames.delete(id)
        fn(now)
      }
      return count
    },
    pending: () => frames.size,
  }
}

/** A swipe of `dy` px over `ms` in 10 steps, then release. */
function swipe(el: HTMLElement, c: ReturnType<typeof fakeClock>, dy: number, ms: number, from = 200) {
  el.dispatchEvent(touch('touchstart', [{ x: 50, y: from }]))
  el.dispatchEvent(touch('touchmove', [{ x: 50, y: from + Math.sign(dy) * 10 }]))
  for (let i = 1; i <= 10; i++) {
    c.advance(ms / 10)
    el.dispatchEvent(touch('touchmove', [{ x: 50, y: from + Math.sign(dy) * 10 + (dy * i) / 10 }]))
  }
  el.dispatchEvent(touch('touchend', []))
}

const total = (scroll: ReturnType<typeof vi.fn>) =>
  scroll.mock.calls.reduce((sum, [dir, lines]) => sum + (dir === 'up' ? lines : -lines), 0)

describe('attachTouchScroll', () => {
  it('turns a vertical swipe into line scrolls, finger down revealing older output', () => {
    const c = fakeClock()
    const host = document.createElement('div')
    const child = document.createElement('div')
    host.append(child)
    const xtermMove = vi.fn()
    child.addEventListener('touchmove', xtermMove)
    const scroll = vi.fn()
    const dispose = attachTouchScroll(host, () => 10, scroll, c.clock)
    child.dispatchEvent(touch('touchstart', [{ x: 50, y: 100 }]))
    const small = touch('touchmove', [{ x: 50, y: 104 }])
    child.dispatchEvent(small)
    expect(small.defaultPrevented).toBe(true)
    expect(scroll).not.toHaveBeenCalled()
    child.dispatchEvent(touch('touchmove', [{ x: 50, y: 110 }])) // passes the threshold
    c.advance(200)
    child.dispatchEvent(touch('touchmove', [{ x: 50, y: 135 }]))
    expect(scroll).toHaveBeenLastCalledWith('up', 2)
    c.advance(200)
    child.dispatchEvent(touch('touchmove', [{ x: 50, y: 100 }]))
    expect(scroll).toHaveBeenLastCalledWith('down', 3)
    expect(xtermMove).not.toHaveBeenCalled()
    c.advance(200)
    child.dispatchEvent(touch('touchend', []))
    expect(c.pending()).toBe(0) // slow drag: no momentum
    dispose()
    const after = touch('touchmove', [{ x: 50, y: 300 }])
    child.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })

  it('ignores horizontal moves and multi-finger gestures', () => {
    const host = document.createElement('div')
    const scroll = vi.fn()
    attachTouchScroll(host, () => 10, scroll, fakeClock().clock)
    host.dispatchEvent(touch('touchstart', [{ x: 0, y: 0 }]))
    host.dispatchEvent(touch('touchmove', [{ x: 80, y: 20 }]))
    host.dispatchEvent(touch('touchmove', [{ x: 90, y: 30 }]))
    host.dispatchEvent(touch('touchstart', [{ x: 0, y: 0 }, { x: 40, y: 0 }]))
    const pinch = touch('touchmove', [{ x: 0, y: 90 }, { x: 40, y: 90 }])
    host.dispatchEvent(pinch)
    expect(pinch.defaultPrevented).toBe(false)
    expect(scroll).not.toHaveBeenCalled()
  })

  it('keeps scrolling after a flick and slows to a stop', () => {
    const c = fakeClock()
    const host = document.createElement('div')
    const scroll = vi.fn()
    attachTouchScroll(host, () => 10, scroll, c.clock)
    swipe(host, c, 100, 50) // 2 px/ms
    const dragged = total(scroll)
    expect(dragged).toBe(10)
    const frames = c.run()
    expect(frames).toBeGreaterThan(10)
    expect(frames).toBeLessThan(1000)
    const coasted = total(scroll) - dragged
    // 2 px/ms with a 325 ms time constant coasts about 650 px = 65 lines.
    expect(coasted).toBeGreaterThan(50)
    expect(coasted).toBeLessThan(70)
    expect(scroll.mock.calls.every(([dir]) => dir === 'up')).toBe(true)
  })

  it('stops momentum on touch and stacks same-direction flicks', () => {
    const c = fakeClock()
    const host = document.createElement('div')
    const scroll = vi.fn()
    attachTouchScroll(host, () => 10, scroll, c.clock)
    swipe(host, c, -100, 50)
    c.advance(16)
    host.dispatchEvent(touch('touchstart', [{ x: 50, y: 200 }]))
    host.dispatchEvent(touch('touchend', []))
    expect(c.pending()).toBe(0) // a tap stops it

    const single = vi.fn()
    const c2 = fakeClock()
    const other = document.createElement('div')
    attachTouchScroll(other, () => 10, single, c2.clock)
    swipe(other, c2, -100, 50)
    c2.run()

    const stacked = vi.fn()
    const c3 = fakeClock()
    const third = document.createElement('div')
    attachTouchScroll(third, () => 10, stacked, c3.clock)
    swipe(third, c3, -100, 50)
    swipe(third, c3, -100, 50)
    c3.run()
    // Two stacked flicks coast further than one flick twice.
    expect(-total(stacked)).toBeGreaterThan(-2 * total(single))
    expect(stacked.mock.calls.every(([dir]) => dir === 'down')).toBe(true)
  })

  it('caps the speed of stacked flicks', () => {
    const c = fakeClock()
    const host = document.createElement('div')
    const scroll = vi.fn()
    attachTouchScroll(host, () => 10, scroll, c.clock)
    for (let i = 0; i < 20; i++) swipe(host, c, 400, 20)
    const before = total(scroll)
    c.run()
    // Coasting is at most v_max × time constant (+5% for 16 ms frame steps).
    expect(total(scroll) - before).toBeLessThanOrEqual(Math.ceil((FLING_MAX_VELOCITY * 325 * 1.05) / 10))
  })
})
