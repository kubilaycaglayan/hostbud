import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SessionPicker from './SessionPicker.vue'

describe('SessionPicker', () => {
  it('lists sessions; a pick or New session… closes it without refocusing the trigger', async () => {
    const w = mount(SessionPicker, { props: { label: 'Split right', icon: '◫', sessions: ['a', 'b'] }, attachTo: document.body })
    const trigger = w.get('button[aria-label="Split right"]')
    const open = async () => {
      await trigger.trigger('click')
      await flushPromises()
    }
    const button = (text: string) =>
      [...document.body.querySelectorAll<HTMLButtonElement>('[role=dialog] button')].find((b) => b.textContent?.trim() === text)!
    await open()
    expect(document.body.querySelector('[role=dialog]')?.getAttribute('aria-label')).toBe('Split right')
    button('b').click()
    await flushPromises()
    expect(w.emitted('pick')).toEqual([['b']])
    expect(document.body.querySelector('[role=dialog]')).toBeNull()
    expect(document.activeElement).not.toBe(trigger.element)
    await open()
    button('New session…').click()
    await flushPromises()
    expect(w.emitted('new')).toHaveLength(1)
    w.unmount()
  })
})
