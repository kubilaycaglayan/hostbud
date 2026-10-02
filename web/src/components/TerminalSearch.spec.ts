import { flushPromises, mount } from '@vue/test-utils'
import type { SearchAddon } from '@xterm/addon-search'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TerminalSearch from './TerminalSearch.vue'
import { useThemeStore } from '@/stores/theme'

afterEach(() => {
  document.body.innerHTML = ''
})

function fakeSearch() {
  let listener: (e: { resultIndex: number; resultCount: number }) => void = () => {}
  const dispose = vi.fn()
  return {
    findNext: vi.fn(() => true),
    findPrevious: vi.fn(() => true),
    clearDecorations: vi.fn(),
    onDidChangeResults: (fn: typeof listener) => {
      listener = fn
      return { dispose }
    },
    fire: (resultIndex: number, resultCount: number) => listener({ resultIndex, resultCount }),
    dispose,
  }
}

async function mountSearch(initial = '') {
  const search = fakeSearch()
  const w = mount(TerminalSearch, {
    props: { search: search as unknown as SearchAddon, initial },
    global: { plugins: [createPinia()] },
    attachTo: document.body,
  })
  await flushPromises()
  return { w, search, field: w.get('input[aria-label=Find]') }
}

const lastOpts = (fn: ReturnType<typeof vi.fn>) => fn.mock.calls.at(-1)![1]

describe('TerminalSearch', () => {
  it('keeps the option checkboxes inside touch-sized labels', async () => {
    const { w } = await mountSearch()
    for (const checkbox of w.findAll('input[type=checkbox]')) {
      expect(checkbox.element.parentElement?.classList.contains('touch-target')).toBe(true)
      expect(checkbox.element.parentElement?.classList.contains('min-h-11')).toBe(true)
    }
  })

  it('updates search decorations when the theme changes', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const theme = useThemeStore()
    theme.mode = 'dark'
    const search = fakeSearch()
    mount(TerminalSearch, { props: { search: search as unknown as SearchAddon, initial: 'x' }, global: { plugins: [pinia] }, attachTo: document.body })
    expect(lastOpts(search.findNext).decorations).toMatchObject({ matchOverviewRuler: '#5fb3f9' })
    theme.mode = 'light'
    await flushPromises()
    expect(lastOpts(search.findNext).decorations).toMatchObject({ matchOverviewRuler: '#0969da' })
  })

  it('searches as you type, with the case and regex options', async () => {
    const { w, search, field } = await mountSearch()
    expect(document.activeElement).toBe(field.element)
    expect(w.text()).toContain('received since attaching') // the scope hint
    await field.setValue('foo')
    expect(search.findNext).toHaveBeenLastCalledWith('foo', expect.objectContaining({ caseSensitive: false, regex: false, incremental: true }))
    expect(lastOpts(search.findNext).decorations).toBeDefined()
    search.clearDecorations.mockClear()
    await w.get('label:nth-of-type(1) input').setValue(true) // Match case
    expect(lastOpts(search.findNext)).toMatchObject({ caseSensitive: true, regex: false })
    expect(search.clearDecorations).toHaveBeenCalled() // re-highlight with the new options
    await w.findAll('input[type=checkbox]')[1].setValue(true) // Regex
    expect(lastOpts(search.findNext)).toMatchObject({ caseSensitive: true, regex: true })
  })

  it('Enter = next, Shift+Enter = previous, and the buttons', async () => {
    const { w, search, field } = await mountSearch('x')
    search.findNext.mockClear()
    await field.trigger('keydown', { key: 'Enter' })
    expect(search.findNext).toHaveBeenLastCalledWith('x', expect.objectContaining({ incremental: false }))
    await field.trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(search.findPrevious).toHaveBeenCalledTimes(1)
    await w.get('button[aria-label="Previous match"]').trigger('click')
    await w.get('button[aria-label="Next match"]').trigger('click')
    expect(search.findPrevious).toHaveBeenCalledTimes(2)
    expect(search.findNext).toHaveBeenCalledTimes(2)
  })

  it('shows the match count', async () => {
    const { w, search } = await mountSearch('x')
    const results = () => w.get('[data-testid=search-results]').text()
    search.fire(2, 12)
    await flushPromises()
    expect(results()).toBe('3 of 12')
    search.fire(-1, 0)
    await flushPromises()
    expect(results()).toBe('No results')
  })

  it('an invalid regex says so instead of throwing', async () => {
    const { w, search, field } = await mountSearch()
    await w.findAll('input[type=checkbox]')[1].setValue(true)
    await field.setValue('(')
    expect(w.get('[data-testid=search-results]').text()).toBe('Invalid pattern')
    expect(search.findNext).not.toHaveBeenCalledWith('(', expect.anything())
    expect(search.clearDecorations).toHaveBeenCalled()
  })

  it('Escape clears the highlights and closes', async () => {
    const { w, search, field } = await mountSearch('x')
    await field.trigger('keydown', { key: 'Escape' })
    expect(search.clearDecorations).toHaveBeenCalled()
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
    expect(search.dispose).toHaveBeenCalled()
  })

  it('opens pre-filled (the selection) and searches it at once', async () => {
    const { search, field } = await mountSearch('needle')
    expect((field.element as HTMLInputElement).value).toBe('needle')
    expect(search.findNext).toHaveBeenCalledWith('needle', expect.anything())
  })
})
