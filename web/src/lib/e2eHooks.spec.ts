import { afterEach, describe, expect, it } from 'vitest'
import { registerPane, unregisterPane, type TermHooks } from './e2eHooks'

const term = (text: string): TermHooks => ({
  termText: () => text,
  termSize: () => ({ cols: 80, rows: 24 }),
  termSelection: () => '',
  termViewport: () => '',
  termTextRect: () => null,
  termTheme: () => ({ background: '#000000', foreground: '#ffffff' }),
})

afterEach(() => {
  unregisterPane('p1')
  unregisterPane('p2')
})

describe('e2e hooks', () => {
  it('answer for the focused pane by default, or by session, and go away', () => {
    let focus = 'p1'
    registerPane('p1', term('one'), () => ({ session: 'a', active: true, focused: focus === 'p1' }))
    registerPane('p2', term('two'), () => ({ session: 'b', active: false, focused: focus === 'p2' }))
    expect(window.__hostbud?.termText()).toBe('one')
    expect(window.__hostbud?.termText('b')).toBe('two')
    expect(window.__hostbud?.termSize('a')).toEqual({ cols: 80, rows: 24 })
    expect(window.__hostbud?.termTheme('a')).toEqual({ background: '#000000', foreground: '#ffffff' })
    focus = 'p2'
    expect(window.__hostbud?.termText()).toBe('two')
    expect(window.__hostbud?.panes()).toEqual([
      { session: 'a', active: true, focused: false },
      { session: 'b', active: false, focused: true },
    ])
    expect(window.__hostbud?.termText('missing')).toBe('')
    unregisterPane('p1')
    expect(window.__hostbud).toBeDefined()
    unregisterPane('p2')
    expect(window.__hostbud).toBeUndefined()
  })

  it('this build is not an e2e build', () => {
    expect(import.meta.env.VITE_E2E).not.toBe('1')
  })
})
