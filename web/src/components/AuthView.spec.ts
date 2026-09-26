import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AuthView from './AuthView.vue'
import { useAuthStore } from '@/stores/auth'
import { stubFetch } from '@/test-utils'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

async function fill(wrapper: ReturnType<typeof mount>, email: string, password: string) {
  await wrapper.get('input[type=email]').setValue(email)
  await wrapper.get('input[type=password]').setValue(password)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
}

describe('AuthView', () => {
  it('uses touch targets for account tabs and 16px controls', () => {
    const wrapper = mount(AuthView)
    expect(wrapper.findAll('[role=tab]').every((tab) => tab.classes().includes('touch-target'))).toBe(true)
    expect(wrapper.findAll('input').every((input) => input.classes().includes('text-base'))).toBe(true)
  })

  it('signs in and becomes authenticated', async () => {
    const calls = stubFetch((_m, path) =>
      path === '/api/auth/me' ? { status: 200, body: { email: 'person@example.com' } } : { status: 200, body: {} },
    )
    const wrapper = mount(AuthView)
    await fill(wrapper, 'person@example.com', 'correct horse battery')
    expect(calls[0]).toEqual({ method: 'POST', path: '/api/auth/login', body: { email: 'person@example.com', password: 'correct horse battery' } })
    expect(useAuthStore().status).toBe('authenticated')
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
  })

  it('shows the generic error on bad credentials', async () => {
    stubFetch(() => ({ status: 401, body: { error: 'invalid email or password' } }))
    const wrapper = mount(AuthView)
    await fill(wrapper, 'person@example.com', 'wrong password!')
    expect(wrapper.get('[role=alert]').text()).toContain('Invalid email or password.')
  })

  it('shows Retry-After when throttled', async () => {
    stubFetch(() => ({ status: 429, body: { error: 'too many attempts' }, headers: { 'Retry-After': '4' } }))
    const wrapper = mount(AuthView)
    await fill(wrapper, 'person@example.com', 'wrong password!')
    expect(wrapper.get('[role=alert]').text()).toContain('Try again in 4 seconds.')
  })

  it('checks the password length before registering', async () => {
    const calls = stubFetch(() => ({ status: 201, body: {} }))
    const wrapper = mount(AuthView, { attachTo: document.body })
    await wrapper.findAll('[role=tab]')[1].trigger('mousedown', { button: 0 })
    await wrapper.findAll('[role=tab]')[1].trigger('click')
    await flushPromises()
    expect(wrapper.get('button[type=submit]').text()).toBe('Create account')
    await fill(wrapper, 'person@example.com', 'short')
    expect(calls).toEqual([])
    expect(wrapper.get('[role=alert]').text()).toContain('at least 10 characters')
    wrapper.unmount()
  })

  it('shows the owner-approval hint when registration is refused', async () => {
    stubFetch(() => ({
      status: 400,
      body: { error: "registration isn't possible with this email address", hint: 'Registration needs an address the owner has approved.' },
    }))
    const wrapper = mount(AuthView, { attachTo: document.body })
    await wrapper.findAll('[role=tab]')[1].trigger('mousedown', { button: 0 })
    await flushPromises()
    await fill(wrapper, 'person@example.com', 'long enough pw')
    const alert = wrapper.get('[role=alert]').text()
    expect(alert).toContain("Registration isn't possible with this email address.")
    expect(alert).toContain('owner has approved')
    wrapper.unmount()
  })
})
