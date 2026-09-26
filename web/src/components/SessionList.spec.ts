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

  it('lists names and attached state without rendering window counts', () => {
    const w = mount(SessionList, { props: { sessions: [s('acc-a', 1, 3), s('acc-b', 0, 1)] } })
    const items = w.findAll('li')
    expect(items).toHaveLength(2)
    expect(items[0].get('button').attributes('aria-label')).toBe('acc-a')
    expect(items[0].get('[role=img]').attributes('aria-label')).toBe('attached')
    expect(items[1].get('[role=img]').attributes('aria-label')).toBe('detached')
    expect(items.map((item) => item.text()).join(' ')).not.toMatch(/\b\d+ windows?\b/)
  })

  it('reorders sessions with the touch-friendly move control', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')], sortable: true } })
    await w.get('button[aria-label="Move session b up"]').trigger('click')
    expect(w.emitted('reorder')).toEqual([[['b', 'a']]])
  })

  it('keeps the session action group immediately after the title in kill/menu/rename order', () => {
    const w = mount(SessionList, { props: { sessions: [s('a')] } })
    const row = w.get('li')
    expect(row.get('[data-session-row]').attributes('aria-label')).toBe('a')
    expect(row.get('span.flex.shrink-0.items-center.gap-1').findAll('button').map((button) => button.attributes('aria-label'))).toEqual([
      'Kill a', 'More actions for a', 'Rename a',
    ])
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

  it.each([
    ['Open in split right', 'row'],
    ['Open in split down', 'column'],
  ])('the row menu: %s', async (label, dir) => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')] }, attachTo: document.body })
    await w.get('button[aria-label="More actions for b"]').trigger('keydown', { key: 'Enter' })
    await new Promise((r) => setTimeout(r))
    const item = [...document.body.querySelectorAll<HTMLElement>('[role=menuitem]')].find((x) => x.textContent?.trim() === label)
    item!.click()
    expect(w.emitted('split')).toEqual([['b', dir]])
    w.unmount()
  })
})
