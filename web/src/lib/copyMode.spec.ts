import { describe, expect, it, vi } from 'vitest'
import type { CopyModeAction, CopyModeState } from '@/api/client'
import { createCopyModeController, swipeDelta, type CopyModeTimers } from './copyMode'

const active: CopyModeState = { inMode: true, scrollPosition: 8, historySize: 120 }
const inactive: CopyModeState = { inMode: false, scrollPosition: 0, historySize: 120 }

function frameClock() {
  let now = 0
  let id = 0
  const frames = new Map<number, FrameRequestCallback>()
  const timers: CopyModeTimers = {
    now: () => now,
    setTimeout: (fn, ms) => setTimeout(fn, ms),
    clearTimeout: (timer) => clearTimeout(timer),
    requestAnimationFrame: (fn) => {
      const next = ++id
      frames.set(next, fn)
      return next
    },
    cancelAnimationFrame: (frame) => { frames.delete(frame) },
  }
  return {
    timers,
    setNow: (value: number) => { now = value },
    frame: () => {
      const next = frames.entries().next().value as [number, FrameRequestCallback] | undefined
      if (next) {
        frames.delete(next[0])
        next[1](now)
      }
    },
    frames,
  }
}

describe('copy mode controller', () => {
  it('updates state from enter and tmux can leave mode in a response', async () => {
    const call = vi.fn(async (action: CopyModeAction) => action === 'enter' ? active : inactive)
    const controller = createCopyModeController(call, vi.fn(), frameClock().timers)
    await controller.action('enter')
    expect(controller.inMode.value).toBe(true)
    expect(controller.scrollPosition.value).toBe(8)
    expect(controller.historySize.value).toBe(120)
    await controller.action('scroll-down', 500)
    expect(controller.inMode.value).toBe(false)
  })

  it('coalesces swipes, allows one request in flight and caps pending lines at 500', async () => {
    vi.useFakeTimers()
    const clock = frameClock()
    let release: ((state: CopyModeState) => void) | undefined
    const call = vi.fn((action: CopyModeAction, lines?: number) => {
      if (action === 'enter') return Promise.resolve(active)
      if (call.mock.calls.filter(([a]) => a !== 'enter').length === 1)
        return new Promise<CopyModeState>((resolve) => { release = resolve })
      return Promise.resolve({ ...active, scrollPosition: lines ?? 0 })
    })
    const controller = createCopyModeController(call, vi.fn(), clock.timers)
    await controller.action('enter')
    controller.swipe('up', 12)
    controller.swipe('up', 8)
    clock.frame()
    expect(call).toHaveBeenCalledTimes(2)
    expect(call.mock.calls[1]).toEqual(['scroll-up', 20])

    controller.swipe('up', 400)
    controller.swipe('up', 400)
    clock.frame()
    expect(call).toHaveBeenCalledTimes(2)
    release!(active)
    await Promise.resolve()
    await Promise.resolve()
    clock.setNow(50)
    await vi.advanceTimersByTimeAsync(50)
    clock.frame()
    await Promise.resolve()
    expect(call).toHaveBeenCalledTimes(3)
    expect(call.mock.calls[2]).toEqual(['scroll-up', 500])
  })

  it('leaves scroll mode before sending typed input and resets on pane changes', async () => {
    const call = vi.fn(async (action: CopyModeAction) => action === 'enter' ? active : inactive)
    const controller = createCopyModeController(call, vi.fn(), frameClock().timers)
    await controller.action('enter')
    const send = vi.fn()
    await controller.exitThen(send)
    expect(call).toHaveBeenLastCalledWith('exit', undefined)
    expect(controller.inMode.value).toBe(false)
    expect(send).toHaveBeenCalledOnce()

    await controller.action('enter')
    controller.reset()
    expect(controller.inMode.value).toBe(false)
    expect(controller.scrollPosition.value).toBe(0)
  })

  it('ignores an action queued before a pane reset', async () => {
    const clock = frameClock()
    let release: ((state: CopyModeState) => void) | undefined
    const call = vi.fn((action: CopyModeAction) => action === 'enter'
      ? Promise.resolve(active)
      : new Promise<CopyModeState>((resolve) => { release = resolve }))
    const controller = createCopyModeController(call, vi.fn(), clock.timers)
    await controller.action('enter')
    controller.swipe('up', 4)
    clock.frame()
    const queued = controller.action('exit')
    controller.reset()
    release!(active)
    expect(await queued).toBe(false)
    expect(controller.inMode.value).toBe(false)
    expect(call).toHaveBeenCalledTimes(2)
  })

  it('shows the API error and returns to the key bar', async () => {
    const error = new Error('tmux needs an upgrade')
    const onError = vi.fn()
    const controller = createCopyModeController(vi.fn().mockRejectedValue(error), onError, frameClock().timers)
    expect(await controller.action('enter')).toBe(false)
    expect(controller.inMode.value).toBe(false)
    expect(onError).toHaveBeenCalledWith(error)
  })
})

describe('copy-mode swipe geometry', () => {
  it('maps vertical distance to line count and direction', () => {
    expect(swipeDelta({ x: 10, y: 200 }, { x: 12, y: 120 }, 16)).toEqual({ direction: 'up', lines: 5 })
    expect(swipeDelta({ x: 10, y: 120 }, { x: 10, y: 200 }, 20)).toEqual({ direction: 'down', lines: 4 })
    expect(swipeDelta({ x: 10, y: 100 }, { x: 80, y: 95 }, 16)).toBeNull()
    expect(swipeDelta({ x: 0, y: 5000 }, { x: 0, y: 0 }, 1)?.lines).toBe(500)
  })
})
