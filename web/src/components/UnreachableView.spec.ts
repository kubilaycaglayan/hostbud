import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UnreachableView from './UnreachableView.vue'
import { useAuthStore } from '@/stores/auth'
import { stubFetch } from '@/test-utils'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('UnreachableView', () => {
  it('says offline when navigator is offline and Try again retries to sign-in on 401', async () => {
    vi.stubGlobal('navigator', { onLine: false })
    stubFetch(() => ({ status: 401, body: { error: 'sign in required' } }))
    const auth = useAuthStore()
    auth.status = 'unreachable'
    const wrapper = mount(UnreachableView)
    expect(wrapper.text()).toContain("You're offline.")
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(auth.status).toBe('anonymous')
  })

  it('retries on online and visibility changes, then stops after success', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { email: 'person@example.com' } }))
    const auth = useAuthStore()
    auth.status = 'unreachable'
    const wrapper = mount(UnreachableView)
    window.dispatchEvent(new Event('online'))
    await flushPromises()
    expect(auth.status).toBe('authenticated')
    expect(calls).toHaveLength(1)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(calls).toHaveLength(1)
    wrapper.unmount()
  })

  it('backs off 1, 2, 4, 8, then caps at 15 seconds', async () => {
    const calls = stubFetch(() => ({ status: 503, body: { error: 'unavailable' } }))
    const auth = useAuthStore()
    auth.status = 'unreachable'
    const wrapper = mount(UnreachableView)
    for (const interval of [1_000, 2_000, 4_000, 8_000, 15_000, 15_000]) {
      await vi.advanceTimersByTimeAsync(interval)
      await flushPromises()
    }
    expect(calls).toHaveLength(6)
    expect(auth.status).toBe('unreachable')
    wrapper.unmount()
  })
})
