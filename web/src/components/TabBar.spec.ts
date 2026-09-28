import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { VueDraggable } from 'vue-draggable-plus'
import type { Tab } from '@/lib/layout'
import TabBar from './TabBar.vue'

const tab = (id: string, session: string): Tab => ({
  id,
  root: { type: 'pane', id: `p-${id}`, machine: 'host', session },
  focusedPane: `p-${id}`,
})
const tabs = [tab('1', 'a'), tab('2', 'b'), tab('3', 'c')]

function bar(active = '2') {
  return mount(TabBar, { props: { tabs, active }, attachTo: document.body })
}

describe('TabBar', () => {
  it('is a tablist with one tab per terminal; only the active one is selected and tabbable', () => {
    const w = bar()
    expect(w.get('[role=tablist]').attributes('aria-label')).toBe('Open terminals')
    const t = w.findAll('[role=tab]')
    expect(t.map((x) => x.text())).toEqual(['a', 'b', 'c'])
    expect(t.map((x) => x.attributes('aria-selected'))).toEqual(['false', 'true', 'false'])
    expect(t.map((x) => x.attributes('tabindex'))).toEqual(['-1', '0', '-1'])
    expect(t[1].attributes('aria-controls')).toBe('tabpanel-2')
    expect(t[1].element.parentElement?.classList.contains('bg-selected')).toBe(true)
    expect(t[1].classes()).toContain('text-selected-fg')
    expect(t[0].element.parentElement?.classList.contains('bg-selected')).toBe(false)
    expect(t.every((tab) => tab.classes().includes('touch-target'))).toBe(true)
    expect(w.findAll('[aria-label^="Close "]').every((button) => button.classes().includes('touch-target'))).toBe(true)
    w.unmount()
  })

  it('click activates; × and a middle click close', async () => {
    const w = bar()
    await w.findAll('[role=tab]')[0].trigger('click')
    expect(w.emitted('activate')).toEqual([['1']])
    await w.get('button[aria-label="Close c"]').trigger('click')
    await w.findAll('[role=tab]')[0].trigger('auxclick', { button: 1 })
    await w.findAll('[role=tab]')[1].trigger('auxclick', { button: 2 }) // right button: no
    expect(w.emitted('close')).toEqual([['3'], ['1']])
    w.unmount()
  })

  it('arrow keys, Home and End move between tabs (wrapping); Delete closes', async () => {
    const w = bar()
    const t = () => w.findAll('[role=tab]')
    await t()[1].trigger('keydown', { key: 'ArrowRight' })
    await t()[2].trigger('keydown', { key: 'ArrowRight' })
    await t()[0].trigger('keydown', { key: 'ArrowLeft' })
    await t()[1].trigger('keydown', { key: 'Home' })
    await t()[1].trigger('keydown', { key: 'End' })
    await t()[1].trigger('keydown', { key: 'a' })
    expect(w.emitted('activate')).toEqual([['3'], ['1'], ['3'], ['1'], ['3']])
    await t()[1].trigger('keydown', { key: 'Delete' })
    expect(w.emitted('close')).toEqual([['2']])
    w.unmount()
  })

  it('tabs themselves drag to a new order (no handle); a drop reorders without activating (M8 T4)', async () => {
    const w = bar()
    const list = w.getComponent(VueDraggable)
    expect(list.props('handle')).toBeUndefined()
    expect(list.props('forceFallback')).toBe(true)
    expect(list.props('delayOnTouchOnly')).toBe(true)
    expect(w.find('.drag-handle, [class*="drag-handle"], [aria-label*="Drag"]').exists()).toBe(false)
    const classes = w.findAll('[role=tab]').map((t) => t.classes().join(' '))

    list.vm.$emit('update:modelValue', [tabs[2], tabs[0], tabs[1]])
    await w.vm.$nextTick()
    expect(w.emitted('reorder')).toEqual([[['3', '1', '2']]])
    expect(w.emitted('activate')).toBeUndefined()
    expect(w.emitted('close')).toBeUndefined()
    // Appearance is unchanged: same classes, same selected tab.
    expect(w.findAll('[role=tab]').map((t) => t.classes().join(' '))).toEqual(classes)
    expect(w.findAll('[role=tab]').map((t) => t.attributes('aria-selected'))).toEqual(['false', 'true', 'false'])
    w.unmount()
  })

  it('shows and navigates tabs in the order it is given, compact too', async () => {
    const w = mount(TabBar, { props: { tabs: [tabs[2], tabs[0], tabs[1]], active: '1', compact: true }, attachTo: document.body })
    expect(w.findAll('[role=tab]').map((x) => x.text())).toEqual(['c', 'a', 'b'])
    await w.findAll('[role=tab]')[1].trigger('keydown', { key: 'ArrowRight' })
    await w.findAll('[role=tab]')[1].trigger('keydown', { key: 'Home' })
    expect(w.emitted('activate')).toEqual([['2'], ['3']])
    w.unmount()
  })
})
