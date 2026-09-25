import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { useAppStore } from './stores/app'
import { stubFetch } from './test-utils'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

const signedIn = () =>
  stubFetch((method, path) =>
    path === '/api/auth/me' ? { status: 200, body: { email: 'person@example.com' } } : { status: method === 'POST' ? 204 : 200 },
  )

describe('App shell', () => {
  it('shows the sign-in screen without a session', async () => {
    stubFetch(() => ({ status: 401, body: { error: 'sign in required' } }))
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.find('input[type=password]').exists()).toBe(true)
  })

  it('renders the sidebar and empty main area when signed in', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('aside[aria-label="Sessions"]').text()).toContain('hostbud')
    expect(wrapper.get('aside').text()).toContain('person@example.com')
    expect(wrapper.get('main').text()).toContain('Select a session')
  })

  it('hides the sidebar when toggled', async () => {
    signedIn()
    const wrapper = mount(App)
    await flushPromises()
    useAppStore().toggleSidebar()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('aside').exists()).toBe(false)
  })

  it('signs out back to the sign-in screen', async () => {
    const calls = signedIn()
    const wrapper = mount(App)
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(calls.at(-1)).toMatchObject({ method: 'POST', path: '/api/auth/logout' })
    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.find('input[type=password]').exists()).toBe(true)
  })
})
