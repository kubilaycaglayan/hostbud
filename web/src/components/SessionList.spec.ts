import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SessionList from './SessionList.vue'
import type { Session } from '@/api/types'

const s = (name: string, attached = 0, windows = 1): Session => ({
  id: '$1', name, path: '/home/dev', attached, windows, created: '', activity: '',
})

describe('SessionList', () => {
  it('shows an empty state, not an error', () => {
    const w = mount(SessionList, { props: { sessions: [] } })
    expect(w.text()).toContain('No tmux sessions yet.')
    expect(w.find('[role=alert]').exists()).toBe(false)
  })

  it('lists name, attached dot and window count', () => {
    const w = mount(SessionList, { props: { sessions: [s('acc-a', 1, 3), s('acc-b', 0, 1)] } })
    const items = w.findAll('li')
    expect(items).toHaveLength(2)
    expect(items[0].get('button').attributes('aria-label')).toBe('acc-a')
    expect(items[0].get('[role=img]').attributes('aria-label')).toBe('attached')
    expect(items[0].text()).toContain('3 windows')
    expect(items[1].get('[role=img]').attributes('aria-label')).toBe('detached')
    expect(items[1].text()).toContain('1 window')
    expect(items[1].text()).not.toContain('1 windows')
  })

  it('emits select and marks the selected session', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')], selected: 'b' } })
    expect(w.get('button[aria-label="b"]').attributes('aria-current')).toBe('true')
    expect(w.get('button[aria-label="a"]').attributes('aria-current')).toBeUndefined()
    await w.get('button[aria-label="a"]').trigger('click')
    expect(w.emitted('select')).toEqual([['a']])
  })

  it('offers rename and kill per session', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a')] } })
    await w.get('button[aria-label="Rename a"]').trigger('click')
    await w.get('button[aria-label="Kill a"]').trigger('click')
    expect(w.emitted('rename')).toEqual([['a']])
    expect(w.emitted('kill')).toEqual([['a']])
  })
})
