import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import KillProjectSessionsDialog from './KillProjectSessionsDialog.vue'
import { stubFetch } from '@/test-utils'
import type { Project } from '@/api/types'

const project: Project = { id: 'p1', machineId: 'host', path: '/home/dev/work/app', name: 'App', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }
const $ = (selector: string) => document.body.querySelector(selector) as HTMLElement | null
const button = (label: string) => [...document.body.querySelectorAll('button')].find((b) => b.textContent?.trim() === label)!

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('KillProjectSessionsDialog', () => {
  it('asks twice before killing, and Cancel on either step makes no request', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    const wrapper = mount(KillProjectSessionsDialog, { attachTo: document.body, props: { machine: 'host', project, sessions: ['a', 'b'] } })
    await flushPromises()
    expect($('[role="alertdialog"]')?.textContent).toContain('Kill all in App?')
    expect($('[role="alertdialog"]')?.textContent).toContain('its 2 sessions, including hidden ones')
    button('Continue…').click()
    await flushPromises()
    expect(calls).toEqual([])
    expect(wrapper.emitted('cancel')).toBeUndefined()
    expect($('[role="alertdialog"]')?.textContent).toContain('Really kill 2 sessions?')
    expect($('[role="alertdialog"]')?.textContent).toContain('a, b')
    button('Cancel').click()
    await flushPromises()
    expect(calls).toEqual([])
    expect(wrapper.emitted('cancel')).toEqual([[]])
    wrapper.unmount()
  })

  it('kills every session after the second confirmation, continuing past a failure', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { killed: ['b'], failed: [{ name: 'a', error: 'boom' }] } }))
    const wrapper = mount(KillProjectSessionsDialog, { attachTo: document.body, props: { machine: 'host', project, sessions: ['a', 'b'] } })
    await flushPromises()
    button('Continue…').click()
    await flushPromises()
    button('Kill 2 sessions').click()
    await flushPromises()
    // One request for all of them: the server refreshes the list once.
    expect(calls).toEqual([{ method: 'POST', path: '/api/machines/host/sessions/kill', body: { names: ['a', 'b'] } }])
    expect(wrapper.emitted('killed')).toEqual([['b']])
    expect(wrapper.emitted('done')).toBeUndefined()
    expect($('[role="alert"]')?.textContent).toContain("Couldn't kill a. Boom.")
    wrapper.unmount()
  })

  it('counts every session as failed when the request itself fails', async () => {
    stubFetch(() => ({ status: 503, body: { error: 'host unreachable', hint: 'Check ssh.' } }))
    const wrapper = mount(KillProjectSessionsDialog, { attachTo: document.body, props: { machine: 'host', project, sessions: ['a', 'b'] } })
    await flushPromises()
    button('Continue…').click()
    await flushPromises()
    button('Kill 2 sessions').click()
    await flushPromises()
    expect(wrapper.emitted('killed')).toBeUndefined()
    expect(wrapper.emitted('done')).toBeUndefined()
    expect($('[role="alert"]')?.textContent).toContain("Couldn't kill a, b. Host unreachable. Check ssh.")
    wrapper.unmount()
  })

  it('reports done when every kill succeeds', async () => {
    stubFetch(() => ({ status: 200, body: { killed: ['a'], failed: [] } }))
    const wrapper = mount(KillProjectSessionsDialog, { attachTo: document.body, props: { machine: 'host', project, sessions: ['a'] } })
    await flushPromises()
    button('Continue…').click()
    await flushPromises()
    button('Kill 1 session').click()
    await flushPromises()
    expect(wrapper.emitted('killed')).toEqual([['a']])
    expect(wrapper.emitted('done')).toEqual([[]])
    wrapper.unmount()
  })
})
