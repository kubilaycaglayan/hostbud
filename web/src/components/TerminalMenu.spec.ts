import { flushPromises, mount } from '@vue/test-utils'
import type { Terminal } from '@xterm/xterm'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TerminalMenu from './TerminalMenu.vue'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function fakeTerm(selection = '', mouseTrackingMode = 'none') {
  return {
    hasSelection: () => selection !== '',
    getSelection: () => selection,
    modes: { mouseTrackingMode },
    paste: vi.fn(),
    selectAll: vi.fn(),
    focus: vi.fn(),
  }
}

async function mountMenu(term: ReturnType<typeof fakeTerm>) {
  mount(TerminalMenu, {
    props: { term: term as unknown as Terminal },
    slots: { default: '<div data-testid="terminal">term</div>' },
    attachTo: document.body,
  })
  await flushPromises()
}

async function rightClick(init: MouseEventInit = {}) {
  const el = document.querySelector('[data-testid=terminal]')!
  el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 5, clientY: 5, ...init }))
  await flushPromises()
}

const item = (name: string) =>
  [...document.querySelectorAll('[role=menuitem]')].find((x) => x.textContent?.trim() === name) as HTMLElement
const menu = () => document.querySelector('[role=menu]')

async function choose(name: string) {
  item(name).click()
  await flushPromises()
}

describe('TerminalMenu', () => {
  it('Copy is disabled without a selection', async () => {
    await mountMenu(fakeTerm(''))
    await rightClick()
    expect(menu()?.getAttribute('aria-label')).toBe('Terminal menu')
    expect(item('Copy').hasAttribute('data-disabled')).toBe(true)
    expect(item('Paste').hasAttribute('data-disabled')).toBe(false)
  })

  it('Copy writes the selection', async () => {
    const writeText = vi.fn(async () => {})
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const t = fakeTerm('selected')
    await mountMenu(t)
    await rightClick()
    expect(item('Copy').hasAttribute('data-disabled')).toBe(false)
    await choose('Copy')
    expect(writeText).toHaveBeenCalledWith('selected')
  })

  it('Paste calls term.paste with the clipboard text, then refocuses the terminal', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { readText: async () => 'line 1\nline 2' } })
    const t = fakeTerm()
    await mountMenu(t)
    await rightClick()
    await choose('Paste')
    expect(t.paste).toHaveBeenCalledWith('line 1\nline 2')
    expect(t.focus).toHaveBeenCalled()
  })

  it('Select all', async () => {
    const t = fakeTerm()
    await mountMenu(t)
    await rightClick()
    await choose('Select all')
    expect(t.selectAll).toHaveBeenCalled()
  })

  it('a program capturing the mouse gets plain right-clicks; Shift or Option opens the menu', async () => {
    await mountMenu(fakeTerm('', 'vt200'))
    await rightClick()
    expect(menu()).toBeNull()
    await rightClick({ shiftKey: true })
    expect(menu()).not.toBeNull()
  })

  it('Option+right-click (macOS) also opens it over a mouse-capturing program', async () => {
    await mountMenu(fakeTerm('', 'any'))
    await rightClick({ altKey: true })
    expect(menu()).not.toBeNull()
  })
})
