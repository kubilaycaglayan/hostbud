import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ScrollBar from './ScrollBar.vue'

describe('ScrollBar', () => {
  it('maps controls to copy-mode actions and displays the tmux position', async () => {
    const wrapper = mount(ScrollBar, { props: { scrollPosition: 18, historySize: 220 } })
    expect(wrapper.get('[role="status"]').text()).toBe('Line 202 of 220')
    for (const [name, action, lines] of [
      ['Top', 'top', undefined], ['Page up', 'page-up', undefined], ['Line up', 'scroll-up', 1],
      ['Line down', 'scroll-down', 1], ['Page down', 'page-down', undefined], ['Bottom', 'bottom', undefined], ['Done', 'exit', undefined],
    ] as const) {
      await wrapper.get(`button[aria-label="${name}"]`).trigger('click')
      expect(wrapper.emitted('action')?.at(-1)).toEqual(name === 'Done' ? [action] : [action, lines])
    }
    expect(wrapper.findAll('button').every((button) => button.classes().includes('touch-target'))).toBe(true)
  })
})
