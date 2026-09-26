import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Terminal } from '@xterm/xterm'
import KeyBar from './KeyBar.vue'
import { createModifiers } from '@/lib/keyBar'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function mountBar(coarse = true, focused = true) {
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    media: query,
    matches: coarse,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })))
  const input = vi.fn()
  const term = { modes: { applicationCursorKeysMode: false }, input } as unknown as Terminal
  const wrapper = mount(KeyBar, { props: { term, focused, modifiers: createModifiers() }, attachTo: document.body })
  return { wrapper, input, term }
}

function pointer(type: string) {
  return new Event(type, { bubbles: true, cancelable: true })
}

describe('KeyBar', () => {
  it('shows named keys in the required order and has touch targets', () => {
    const { wrapper } = mountBar()
    expect(wrapper.find('[aria-label="On-screen key bar"]').exists()).toBe(true)
    const names = wrapper.findAll('button').map((button) => button.attributes('aria-label'))
    expect(names).toEqual([
      'Escape', 'Tab', 'Control', 'Alt', 'Left arrow', 'Up arrow', 'Down arrow', 'Right arrow', 'Pipe', 'Tilde', 'Slash', 'Hyphen', 'Scroll history', 'Hide key bar',
    ])
    expect(wrapper.findAll('button').every((button) => button.classes().includes('touch-target'))).toBe(true)
  })

  it('is hidden for a fine pointer', () => {
    const { wrapper } = mountBar(false)
    expect(wrapper.find('[data-testid="key-bar"]').exists()).toBe(false)
  })

  it('is hidden when its terminal is not focused', () => {
    const { wrapper } = mountBar(true, false)
    expect(wrapper.find('[data-testid="key-bar"]').exists()).toBe(false)
  })

  it('sends on pointerdown without taking focus and keeps sticky modifier state', async () => {
    const { wrapper, input } = mountBar()
    const focused = document.createElement('textarea')
    document.body.append(focused)
    focused.focus()
    const escape = wrapper.get('button[aria-label="Escape"]')
    const down = pointer('pointerdown')
    escape.element.dispatchEvent(down)
    expect(down.defaultPrevented).toBe(true)
    expect(input).toHaveBeenCalledWith('\x1b', true)
    expect(document.activeElement).toBe(focused)
    const ctrl = wrapper.get('button[aria-label="Control"]')
    ctrl.element.dispatchEvent(pointer('pointerdown'))
    await wrapper.vm.$nextTick()
    expect(ctrl.attributes('aria-pressed')).toBe('true')
  })

  it('repeats arrows after 400ms at 80ms intervals and stops on pointerup', async () => {
    vi.useFakeTimers()
    const { wrapper, input } = mountBar()
    const up = wrapper.get('button[aria-label="Up arrow"]')
    up.element.dispatchEvent(pointer('pointerdown'))
    expect(input).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(400)
    expect(input).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(160)
    expect(input).toHaveBeenCalledTimes(4)
    up.element.dispatchEvent(pointer('pointerup'))
    await vi.advanceTimersByTimeAsync(200)
    expect(input).toHaveBeenCalledTimes(4)
  })

  it.each(['pointercancel', 'pointerleave'])('stops arrow repeats on %s', async (eventName) => {
    vi.useFakeTimers()
    const { wrapper, input } = mountBar()
    const up = wrapper.get('button[aria-label="Up arrow"]')
    up.element.dispatchEvent(pointer('pointerdown'))
    await vi.advanceTimersByTimeAsync(400)
    expect(input).toHaveBeenCalledTimes(2)
    up.element.dispatchEvent(pointer(eventName))
    await vi.advanceTimersByTimeAsync(200)
    expect(input).toHaveBeenCalledTimes(2)
  })

  it('collapses and expands to a labelled handle', async () => {
    const { wrapper } = mountBar()
    await wrapper.get('button[aria-label="Hide key bar"]').trigger('click')
    expect(wrapper.find('button[aria-label="Escape"]').exists()).toBe(false)
    await wrapper.get('button[aria-label="Show key bar"]').trigger('click')
    expect(wrapper.find('button[aria-label="Escape"]').exists()).toBe(true)
  })
})
