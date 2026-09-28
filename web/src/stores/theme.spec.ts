import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useThemeStore } from './theme'
import { darkTerminalTheme, dimmedTerminalTheme, lightTerminalTheme, solarizedTerminalTheme, resolveTheme, themeBootValue, validateTheme } from '@/lib/theme'

const mediaListeners = new Set<(event: MediaQueryListEvent) => void>()
let darkOS = false

beforeEach(() => {
  setActivePinia(createPinia())
  darkOS = false
  mediaListeners.clear()
  vi.stubGlobal('matchMedia', vi.fn(() => ({
    get matches() { return darkOS },
    media: '(prefers-color-scheme: dark)',
    addEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => mediaListeners.add(listener),
    removeEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => mediaListeners.delete(listener),
  })))
  document.head.innerHTML = '<meta name="theme-color" content="">'
  document.documentElement.removeAttribute('data-theme')
  localStorage.clear()
  vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
})

afterEach(() => vi.unstubAllGlobals())

function luminance(hex: string): number {
  const rgb = hex.slice(1).match(/../g)!.map((pair) => parseInt(pair, 16) / 255).map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722
}
function contrast(a: string, b: string): number {
  const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (high + 0.05) / (low + 0.05)
}

describe('theme', () => {
  it('resolves explicit modes and follows the OS only in System mode', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('solarized', true)).toBe('solarized')
    expect(resolveTheme('dimmed', false)).toBe('dimmed')
    expect(validateTheme({ version: 1, mode: 'light' })).toBe('light')
    expect(validateTheme({ version: 1, mode: 'solarized' })).toBe('solarized')
    expect(validateTheme({ version: 1, mode: 'dimmed' })).toBe('dimmed')
    expect(validateTheme({ version: 2, mode: 'light' })).toBe('system')
    expect(validateTheme({ version: 1, mode: 'unexpected' })).toBe('system')
  })

  it('applies root, native controls, status bar, and browser mirror on changes', async () => {
    const store = useThemeStore()
    await store.setMode('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#ffffff')
    expect(localStorage.getItem('hostbud.theme')).toBe('light')
    expect(fetch).toHaveBeenCalledWith('/api/ui-state/theme', expect.objectContaining({ method: 'PUT' }))
    await store.setMode('solarized')
    expect(document.documentElement.dataset.theme).toBe('solarized')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#bbc5b9')
    expect(localStorage.getItem('hostbud.theme')).toBe('solarized')
    await store.setMode('dimmed')
    expect(document.documentElement.dataset.theme).toBe('dimmed')
    expect(document.documentElement.style.colorScheme).toBe('dark')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#4c686a')
    expect(localStorage.getItem('hostbud.theme')).toBe('dimmed')
  })

  it('tracks system changes and removes the listener on sign-out', async () => {
    const store = useThemeStore()
    await store.load()
    expect(mediaListeners.size).toBe(1)
    darkOS = true
    for (const listener of mediaListeners) listener({ matches: true } as MediaQueryListEvent)
    expect(store.resolved).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#0f1115')
    await store.setMode('light')
    darkOS = true
    for (const listener of mediaListeners) listener({ matches: true } as MediaQueryListEvent)
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.querySelector('meta[name="theme-color"]')?.getAttribute('content')).toBe('#ffffff')
    const saved = localStorage.getItem('hostbud.theme')
    store.signOut()
    expect(mediaListeners.size).toBe(0)
    expect(localStorage.getItem('hostbud.theme')).toBe(saved)
  })

  it('resolves the boot mirror and falls back when storage throws', () => {
    expect(themeBootValue({ getItem: () => 'light' }, true)).toBe('light')
    expect(themeBootValue({ getItem: () => 'dark' }, false)).toBe('dark')
    expect(themeBootValue({ getItem: () => 'system' }, true)).toBe('dark')
    expect(themeBootValue({ getItem: () => 'solarized' }, true)).toBe('solarized')
    expect(themeBootValue({ getItem: () => 'dimmed' }, false)).toBe('dimmed')
    expect(themeBootValue({ getItem: () => 'invalid' }, true)).toBe('dark')
    expect(themeBootValue(null, false)).toBe('light')
    expect(themeBootValue({ getItem: () => { throw new Error('blocked') } }, true)).toBe('dark')
  })

  it('keeps terminal palettes and selection within their contrast targets', () => {
    for (const palette of [darkTerminalTheme, lightTerminalTheme, solarizedTerminalTheme, dimmedTerminalTheme]) {
      expect(contrast(palette.foreground!, palette.background!)).toBeGreaterThanOrEqual(4.5)
      for (const color of [palette.red, palette.green, palette.yellow, palette.blue, palette.magenta, palette.cyan,
        palette.brightRed, palette.brightGreen, palette.brightYellow, palette.brightBlue, palette.brightMagenta, palette.brightCyan])
        expect(contrast(color!, palette.background!)).toBeGreaterThanOrEqual(3)
      expect(contrast(palette.foreground!, palette.selectionBackground!)).toBeGreaterThanOrEqual(4.5)
    }
  })

})
