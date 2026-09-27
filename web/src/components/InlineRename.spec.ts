import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import InlineRename from './InlineRename.vue'

describe('InlineRename', () => {
  it('selects the current name, commits once on Enter followed by blur, and returns focus on cancel', async () => {
    const commit = vi.fn().mockResolvedValue(undefined)
    const wrapper = mount(InlineRename, { attachTo: document.body, props: { name: 'old', commit } })
    const input = wrapper.get('input')
    await flushPromises()
    expect(document.activeElement).toBe(input.element)
    expect((input.element as HTMLInputElement).selectionStart).toBe(0)
    expect((input.element as HTMLInputElement).selectionEnd).toBe('old'.length)
    expect(input.attributes()).toMatchObject({ autocapitalize: 'off', autocorrect: 'off', spellcheck: 'false' })
    await input.setValue('new')
    await input.trigger('keydown.enter')
    await input.trigger('blur')
    expect(commit).toHaveBeenCalledTimes(1)
    expect(commit).toHaveBeenCalledWith('new')
    wrapper.unmount()
  })

  it.each(['', '   ', 'old'])('cancels unchanged or empty value %j without a request', async (value) => {
    const commit = vi.fn()
    const wrapper = mount(InlineRename, { props: { name: 'old', commit } })
    await wrapper.get('input').setValue(value)
    await wrapper.get('input').trigger('keydown.enter')
    expect(commit).not.toHaveBeenCalled()
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('keeps the field open and associates an error with the input', async () => {
    const commit = vi.fn().mockRejectedValue(new Error('taken'))
    const wrapper = mount(InlineRename, { props: { name: 'old', commit } })
    await wrapper.get('input').setValue('taken')
    await wrapper.get('input').trigger('keydown.enter')
    await flushPromises()
    await wrapper.setProps({ error: 'That name is already in use.' })
    expect(wrapper.get('input').attributes('aria-describedby')).toBe('inline-rename-error')
    expect(wrapper.get('[role=alert]').text()).toContain('already in use')
    expect(wrapper.emitted('cancel')).toBeUndefined()
  })

  it('cancels on Escape', async () => {
    const wrapper = mount(InlineRename, { props: { name: 'old', commit: vi.fn() } })
    await wrapper.get('input').trigger('keydown.esc')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})
