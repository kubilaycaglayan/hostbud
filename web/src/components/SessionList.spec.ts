import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SessionList from './SessionList.vue'
import type { Session } from '@/api/types'

const s = (name: string, attached = 0, windows = 1): Session => ({
  id: '$1', name, path: '/home/dev', attached, windows, created: '', activity: '',
})

describe('SessionList', () => {
  afterEach(() => vi.useRealTimers())
  it('shows an empty state, not an error', () => {
    const w = mount(SessionList, { props: { sessions: [] } })
    expect(w.text()).toContain('No tmux sessions yet.')
    expect(w.find('[role=alert]').exists()).toBe(false)
  })

  it('lists names and attached state without rendering window counts', () => {
    const w = mount(SessionList, { props: { sessions: [s('acc-a', 1, 3), s('acc-b', 0, 1), s('acc-c', 0, 0)] } })
    const items = w.findAll('li')
    expect(items).toHaveLength(3)
    expect(items[0].get('button').attributes('aria-label')).toBe('acc-a')
    expect(items[0].get('[role=img]').attributes('aria-label')).toBe('attached')
    expect(items[1].get('[role=img]').attributes('aria-label')).toBe('detached')
    expect(items.map((item) => item.text()).join(' ')).not.toMatch(/\b\d+ windows?\b/)
  })

  it('uses a drag handle for reorder and keeps the session row free of move arrows', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')], sortable: true } })
    expect(w.find('button[aria-label="Move session b up"]').exists()).toBe(false)
    expect(w.find('button[aria-label="Move session b down"]').exists()).toBe(false)
    expect(w.get('button[aria-label="Drag to reorder session b"]').attributes('title')).toBe('Drag to reorder sessions')
    expect(w.get('button[aria-label="Drag to reorder session b"]').classes()).toContain('touch-target')
    expect(w.get('button[aria-label="b"]').classes()).toContain('touch-target')
    expect(w.get('button[aria-label="Kill b"]').classes()).toContain('touch-target')
    expect(w.get('button[aria-label="Rename b"]').classes()).toContain('touch-target')
    expect(w.get('li').classes()).toContain('py-0.5')
  })

  it('keeps the session action group immediately after the title in kill/menu/rename order', () => {
    const w = mount(SessionList, { props: { sessions: [s('a')] } })
    const row = w.get('li')
    expect(row.get('[data-session-row]').attributes('aria-label')).toBe('a')
    const actions = row.get('[data-session-row]').element.nextElementSibling
    expect(actions?.tagName).toBe('SPAN')
    expect(actions?.classList.contains('gap-0.5')).toBe(true)
    expect(row.findAll('button').slice(-3).map((button) => button.attributes('aria-label'))).toEqual([
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

  it('opens the existing row menu after a long press and cancels when the finger moves', async () => {
    vi.useFakeTimers()
    const w = mount(SessionList, { props: { sessions: [s('a')] }, attachTo: document.body })
    const row = w.get('button[aria-label="a"]')
    const pointer = (type: string, x: number, y: number) => {
      const event = new Event(type, { bubbles: true })
      Object.defineProperties(event, { pointerType: { value: 'touch' }, clientX: { value: x }, clientY: { value: y } })
      return event
    }
    row.element.dispatchEvent(pointer('pointerdown', 20, 20))
    await vi.advanceTimersByTimeAsync(500)
    await w.vm.$nextTick()
    expect(document.body.querySelector('[role="menu"]')).not.toBeNull()
    row.element.dispatchEvent(pointer('pointerup', 20, 20))
    await row.trigger('click')
    expect(w.emitted('select')).toBeUndefined()
    w.unmount()

    const short = mount(SessionList, { props: { sessions: [s('b')] } })
    const shortRow = short.get('button[aria-label="b"]')
    shortRow.element.dispatchEvent(pointer('pointerdown', 20, 20))
    shortRow.element.dispatchEvent(pointer('pointermove', 31, 20))
    await vi.advanceTimersByTimeAsync(600)
    shortRow.element.dispatchEvent(pointer('pointerup', 31, 20))
    await shortRow.trigger('click')
    expect(short.emitted('select')).toEqual([['b']])
  })

  it('offers rename and kill per session', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a')] } })
    await w.get('button[aria-label="Rename a"]').trigger('click')
    await w.get('button[aria-label="Kill a"]').trigger('click')
    expect(w.emitted('rename')).toEqual([['a']])
    expect(w.emitted('kill')).toEqual([['a']])
  })

  it('offers Rename in the long-press tree menu', async () => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    const w = mount(SessionList, { props: { sessions: [s('a')], treeView: true }, attachTo: document.body })
    const row = w.get('button[aria-label="a"]')
    const down = new Event('pointerdown', { bubbles: true })
    Object.defineProperties(down, { pointerType: { value: 'touch' }, clientX: { value: 10 }, clientY: { value: 10 } })
    row.element.dispatchEvent(down)
    await vi.advanceTimersByTimeAsync(500)
    await w.vm.$nextTick()
    const rename = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    expect(rename).toBeTruthy()
    rename!.click()
    expect(w.emitted('rename')).toEqual([['a']])
    w.unmount()
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
