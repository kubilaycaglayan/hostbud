import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectSessionDialog from './ProjectSessionDialog.vue'
import { useToastsStore } from '@/stores/toasts'
import { stubFetch } from '@/test-utils'

const project = {
  id: 'project-1', machineId: 'host', name: 'Project', path: '/home/dev/project', sortOrder: 0, pinned: false,
  createdAt: '', updatedAt: '',
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.unstubAllGlobals()
})

describe('ProjectSessionDialog', () => {
  it('announces a numbered name when the typed name is taken', async () => {
    stubFetch((_method, path) => path.endsWith('/recent-commands')
      ? { status: 200, body: { commands: [] } }
      : { status: 201, body: { name: 'work-2' } })
    const wrapper = mount(ProjectSessionDialog, { props: { project } })
    await flushPromises()
    await wrapper.get('input').setValue('work')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(useToastsStore().toasts).toMatchObject([{
      title: 'Session name changed',
      message: 'Named "work-2": "work" was already taken.',
      tone: 'info',
    }])
    wrapper.unmount()
  })

  it('does not announce an auto-derived name when no name was typed', async () => {
    stubFetch((_method, path) => path.endsWith('/recent-commands')
      ? { status: 200, body: { commands: [] } }
      : { status: 201, body: { name: 'project-1' } })
    const wrapper = mount(ProjectSessionDialog, { props: { project } })
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(useToastsStore().toasts).toEqual([])
    wrapper.unmount()
  })
})
