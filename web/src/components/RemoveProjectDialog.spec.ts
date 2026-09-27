import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RemoveProjectDialog from './RemoveProjectDialog.vue'
import { stubFetch } from '@/test-utils'
import type { Project } from '@/api/types'

const project: Project = { id: 'p1', machineId: 'host', path: '/home/dev/work/app', name: 'App', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }
const $ = (selector: string) => document.body.querySelector(selector) as HTMLElement | null

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('RemoveProjectDialog', () => {
  it('explains the session move, path, and account-wide removal; Cancel makes no request', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    const wrapper = mount(RemoveProjectDialog, { attachTo: document.body, props: {
      project, sessionCount: 2, destinations: 'Work (1), Other sessions (1)', displayPath: '~/work/app',
    } })
    await flushPromises()
    const dialog = $('[role="alertdialog"]')!
    expect(dialog.textContent).toContain('Its 2 sessions keep running and move to Work (1), Other sessions (1).')
    expect(dialog.textContent).toContain("Files in ~/work/app aren't touched.")
    expect(dialog.textContent).toContain('This removes it for every account.')
    const cancel = [...dialog.querySelectorAll('button')].find((button) => button.textContent?.trim() === 'Cancel')!
    cancel.click()
    await flushPromises()
    expect(calls).toEqual([])
    expect(wrapper.emitted('cancel')).toEqual([[]])
    wrapper.unmount()
  })

  it('deletes only after confirmation and reports the project id', async () => {
    const calls = stubFetch((method, path) => method === 'DELETE' && path === '/api/projects/p1'
      ? { status: 204 }
      : { status: 200, body: {} })
    const wrapper = mount(RemoveProjectDialog, { attachTo: document.body, props: {
      project, sessionCount: 0, destinations: 'Other sessions', displayPath: '~/work/app',
    } })
    await flushPromises()
    expect($('[role="alertdialog"]')?.textContent).toContain('It has no sessions to move.')
    const confirm = [...document.body.querySelectorAll('button')].find((button) => button.textContent?.trim() === 'Remove project')!
    confirm.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'DELETE', path: '/api/projects/p1', body: undefined }])
    expect(wrapper.emitted('removed')).toEqual([['p1']])
    wrapper.unmount()
  })
})
