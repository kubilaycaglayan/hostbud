import { effectScope } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { COMPACT_QUERY, useMediaQuery } from './media'

const originalMatchMedia = window.matchMedia

afterEach(() => {
  if (originalMatchMedia) Object.defineProperty(window, 'matchMedia', { configurable: true, value: originalMatchMedia })
  else Reflect.deleteProperty(window, 'matchMedia')
})

describe('compact layout query', () => {
  it('matches portrait and landscape phones while leaving tablets and desktops wide', () => {
    expect(COMPACT_QUERY).toBe('(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)')
    const cases = [
      { width: 390, height: 844, coarse: true, compact: true },
      { width: 844, height: 390, coarse: true, compact: true },
      { width: 768, height: 1024, coarse: true, compact: false },
      { width: 1440, height: 900, coarse: false, compact: false },
    ]
    for (const device of cases) {
      Object.defineProperty(window, 'matchMedia', {
        configurable: true,
        value: vi.fn((query: string) => ({
          media: query,
          matches: device.width <= 767.84 || (device.coarse && device.height <= 511.84),
          addEventListener: vi.fn(),
          removeEventListener: vi.fn(),
        })),
      })
      const scope = effectScope()
      const value = scope.run(() => useMediaQuery(COMPACT_QUERY))!
      expect(value.value, `${device.width}x${device.height}`).toBe(device.compact)
      scope.stop()
    }
  })

  it('updates when the query changes without recreating its ref', () => {
    let listener: (() => void) | undefined
    let matches = true
    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn(() => ({
        media: COMPACT_QUERY,
        get matches() { return matches },
        addEventListener: (_: string, cb: () => void) => { listener = cb },
        removeEventListener: vi.fn(),
      })),
    })
    const scope = effectScope()
    const value = scope.run(() => useMediaQuery(COMPACT_QUERY))!
    const sameRef = value
    matches = true // portrait → landscape remains compact
    listener?.()
    expect(value).toBe(sameRef)
    matches = false
    listener?.()
    expect(value.value).toBe(false)
    scope.stop()
  })
})
