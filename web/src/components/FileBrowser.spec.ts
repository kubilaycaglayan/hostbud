import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import FileBrowser from './FileBrowser.vue'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)

describe('FileBrowser', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    fetchMock.mockReset()
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      const parsed = new URL(String(url), 'http://localhost')
      let body: unknown = {}
      if (parsed.pathname.endsWith('/fs/home')) body = { path: '/home/dev' }
      else if (parsed.pathname.endsWith('/fs')) body = { path: parsed.searchParams.get('path'), entries: [{ name: 'work', path: '/home/dev/work', kind: 'directory' }, { name: 'notes', path: '/home/dev/notes', kind: 'directory' }] }
      else if (parsed.pathname.endsWith('/fs/mkdir')) body = { path: '/home/dev/new-folder' }
      else if (parsed.pathname === '/api/projects' && init?.method === 'POST') body = { id: 'p1', machineId: 'host', path: '/home/dev/work', name: 'work' }
      else if (parsed.pathname === '/api/projects' && init?.method === 'GET') body = { projects: [] }
      else if (parsed.pathname.endsWith('/sessions')) body = { name: 'work' }
      return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify(body) }
    })
  })

  it('navigates by breadcrumb, filters autocomplete and switches hidden entries', async () => {
    const wrapper = mount(FileBrowser, { props: { machine: 'host' } })
    await flushPromises()
    expect(wrapper.get('[aria-label="Breadcrumbs"]').text()).toContain('home')
    await wrapper.get('#browser-path').setValue('/work')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(1)
    await wrapper.get('[role="option"]').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('path=%2Fhome%2Fdev%2Fwork'), expect.anything())
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await flushPromises()
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes('hidden=true'))).toBe(true)
  })

  it('validates folder names and creates a project session at the saved project path', async () => {
    const wrapper = mount(FileBrowser, { props: { machine: 'host' } })
    await flushPromises()
    await wrapper.get('#folder-name').setValue('../bad')
    await wrapper.get('#folder-name').element.closest('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.text()).toContain('Use a single folder name')
    await wrapper.get('#folder-name').setValue('new-folder')
    await wrapper.get('#folder-name').element.closest('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/machines/host/fs/mkdir', expect.objectContaining({ method: 'POST' }))
    await wrapper.findAll('button').find((button) => button.text().includes('Open as project'))?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('New session here'))?.trigger('click')
    await wrapper.get('[aria-label="New session here"] form').trigger('submit')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/sessions', expect.objectContaining({ method: 'POST' }))
    expect(wrapper.emitted('created')?.[0]).toEqual(['work'])
  })
})
