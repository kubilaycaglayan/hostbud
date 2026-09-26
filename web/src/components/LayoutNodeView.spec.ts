import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { allPanes, panesOf, splitPane } from '@/lib/layout'
import { useLayoutStore } from '@/stores/layout'
import LayoutNodeView from './LayoutNodeView.vue'
import TabView from './TabView.vue'
import { NEW_SESSION_FOR_SPLIT } from './layoutKeys'

beforeEach(() => setActivePinia(createPinia()))

/** A loaded layout with one tab: row[a, column[b, c]], c focused. */
function nested() {
  const layout = useLayoutStore()
  layout.loaded = true
  layout.open('host', 'a')
  const id = (s: string) => allPanes(layout.layout).find((p) => p.session === s)!.id
  layout.layout = splitPane(layout.layout, id('a'), 'row', 'host', 'b').layout
  layout.layout = splitPane(layout.layout, id('b'), 'column', 'host', 'c').layout
  return { layout, id }
}

// xterm can't render in jsdom; TerminalView has its own spec.
const stubs = { TerminalView: true }
const terms = (w: ReturnType<typeof mount>) =>
  w.findAll('terminal-view-stub').map((t) => ({ session: t.attributes('session'), focused: t.attributes('focused') }))

describe('LayoutNodeView', () => {
  it('renders nested splits and marks the focused pane', () => {
    const { layout } = nested()
    const tab = layout.tabs[0]
    const w = mount(LayoutNodeView, { props: { node: tab.root, tab, active: true }, global: { stubs } })
    expect(w.findAll('[data-split]').map((x) => x.attributes('data-split'))).toEqual(['row', 'column'])
    expect(w.findAll('.splitpanes--horizontal')).toHaveLength(1) // the column
    expect(terms(w)).toEqual([
      { session: 'a', focused: 'false' },
      { session: 'b', focused: 'false' },
      { session: 'c', focused: 'true' },
    ])
    const stub = w.findAll('terminal-view-stub')[0]
    expect(stub.attributes()).toMatchObject({ paneindex: '1', panecount: '3', cansplit: 'true' }) // stub attributes are lowercased
  })

  it('pane events reach the layout: focus, split, close; New session… asks App', async () => {
    const { layout, id } = nested()
    const asked: unknown[] = []
    const tab = () => layout.tabs[0]
    const w = mount(LayoutNodeView, {
      props: { node: tab().root, tab: tab(), active: true },
      global: { stubs, provide: { [NEW_SESSION_FOR_SPLIT as symbol]: (p: string, d: string) => asked.push([p, d]) } },
    })
    const view = (i: number) => w.findAllComponents({ name: 'TerminalView' })[i]
    view(0).vm.$emit('focus')
    expect(layout.focused?.session).toBe('a')
    view(0).vm.$emit('split', 'column', 'd')
    expect(panesOf(tab().root).map((p) => p.session)).toEqual(['a', 'd', 'b', 'c'])
    view(0).vm.$emit('split', 'row', null)
    expect(asked).toEqual([[id('a'), 'row']])
    view(2).vm.$emit('close') // c (the render predates the split)
    expect(panesOf(tab().root).map((p) => p.session)).toEqual(['a', 'd', 'b'])
  })
})

describe('TabView on narrow screens', () => {
  it('shows only the focused pane, full size, and the switcher cycles it', async () => {
    const { layout } = nested()
    const w = mount(TabView, { props: { tab: layout.tabs[0], active: true, narrow: true }, global: { stubs } })
    expect(w.find('[data-split]').exists()).toBe(false)
    const shown = () =>
      w.findAll('terminal-view-stub').filter((t) => (t.element.parentElement as HTMLElement).style.display !== 'none')
    expect(shown().map((t) => t.attributes('session'))).toEqual(['c'])
    expect(shown()[0].attributes()).toMatchObject({ paneindex: '3', panecount: '3', narrow: 'true', active: 'true' })
    // Hidden panes don't take input or refit.
    expect(w.findAll('terminal-view-stub').map((t) => t.attributes('active'))).toEqual(['false', 'false', 'true'])
    w.findAllComponents({ name: 'TerminalView' })[2].vm.$emit('cyclePane')
    await w.setProps({ tab: layout.tabs[0] })
    expect(shown().map((t) => t.attributes('session'))).toEqual(['a'])
    // The layout itself is unchanged: still a split.
    expect(layout.tabs[0].root.type).toBe('split')
  })
})
