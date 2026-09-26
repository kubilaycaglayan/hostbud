import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
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
})
