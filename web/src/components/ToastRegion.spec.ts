import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import ToastRegion from './ToastRegion.vue'
import { useToastsStore } from '@/stores/toasts'

describe('ToastRegion', () => {
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('shows photo success at the top right in green and dismisses it after five seconds', async () => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    const wrapper = mount(ToastRegion, { attachTo: document.body })
    useToastsStore().push({ title: 'Photo added to repo', message: '../photo.png', tone: 'success', placement: 'top-right' }, 5_000)
    await wrapper.vm.$nextTick()

    const toast = wrapper.get('[role="status"]')
    expect(wrapper.get('[aria-label="Notifications"]').element.firstElementChild?.contains(toast.element)).toBe(true)
    expect(toast.classes()).toContain('border-ok')
    expect(toast.text()).toContain('../photo.png')

    vi.advanceTimersByTime(5_000)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
