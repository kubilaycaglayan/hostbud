import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import App from './App.vue'
import { useAppStore } from './stores/app'

describe('App shell', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('renders the sidebar and empty main area', () => {
    const wrapper = mount(App)
    expect(wrapper.get('aside[aria-label="Sessions"]').text()).toContain('hostbud')
    expect(wrapper.get('main').text()).toContain('Select a session')
  })

  it('hides the sidebar when toggled', async () => {
    const wrapper = mount(App)
    useAppStore().toggleSidebar()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('aside').exists()).toBe(false)
  })
})
