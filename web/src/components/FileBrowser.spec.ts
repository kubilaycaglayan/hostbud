import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import FileBrowser from './FileBrowser.vue'

const fetchMock = vi.fn()
const createdFolders = new Set<string>()
let projectsList: { id: string; machineId: string; path: string; name: string; sortOrder: number; pinned: boolean; createdAt: string; updatedAt: string }[] = []
vi.stubGlobal('fetch', fetchMock)

describe('FileBrowser', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    fetchMock.mockReset()
    createdFolders.clear()
    projectsList = []
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      const parsed = new URL(String(url), 'http://localhost')
      let body: unknown = {}
      if (parsed.pathname.endsWith('/fs/home')) body = { path: '/home/dev' }
      else if (parsed.pathname.endsWith('/fs/stat')) body = { name: 'broken-link', path: '/home/dev/broken-link', kind: 'symlink', symlink: true, symlinkState: 'broken' }
      else if (parsed.pathname.endsWith('/fs') && parsed.searchParams.get('path') === '/home/dev/missing') {
        return { ok: false, status: 404, headers: new Headers(), json: async () => ({ error: 'path not found', hint: 'Check the path and try again.' }), text: async () => '' }
      } else if (parsed.pathname.endsWith('/fs') && parsed.searchParams.get('path') === '/home/dev/notes.txt') {
        return { ok: false, status: 400, headers: new Headers(), json: async () => ({ error: 'path is not a directory', hint: 'Choose an existing directory.' }), text: async () => '' }
      }
      else if (parsed.pathname.endsWith('/fs')) body = { path: parsed.searchParams.get('path'), entries: [
        { name: 'work', path: '/home/dev/work', kind: 'directory' },
        { name: 'notes', path: '/home/dev/notes', kind: 'directory' },
        { name: 'broken-link', path: '/home/dev/broken-link', kind: 'symlink', symlinkState: 'unresolved' },
        ...[...createdFolders]
          .filter((created) => created.startsWith(`${parsed.searchParams.get('path')}/`))
          .map((created) => ({ name: created.split('/').at(-1), path: created, kind: 'directory' })),
        ...(parsed.searchParams.get('hidden') === 'true' ? [{ name: '.hidden', path: '/home/dev/.hidden', kind: 'file' }] : []),
      ] }
      else if (parsed.pathname.endsWith('/fs/mkdir')) {
        const requestBody = JSON.parse(String(init?.body)) as { path: string; name: string }
        const child = `${requestBody.path}/${requestBody.name}`
        createdFolders.add(child)
        body = { path: child }
      }
      else if (parsed.pathname === '/api/projects' && init?.method === 'POST') body = { id: 'p1', machineId: 'host', path: '/home/dev/work', name: 'work' }
      else if (parsed.pathname === '/api/projects' && init?.method === 'GET') body = { projects: projectsList }
      else if (parsed.pathname === '/api/projects/p1/recent-commands') body = { commands: ['make test'] }
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
    expect(wrapper.findAll('button').some((button) => button.text() === 'new-folder/')).toBe(true)
    await wrapper.findAll('button').find((button) => button.text().includes('Open as project'))?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('New session here'))?.trigger('click')
    await flushPromises()
    const commandButton = wrapper.get('button[aria-label="Use recent command make test"]')
    expect(commandButton.text()).toBe('make test')
    expect(fetchMock.mock.calls.some(([url, init]) => String(url).endsWith('/sessions') && init?.method === 'POST')).toBe(false)
    await commandButton.trigger('click')
    expect((wrapper.get('[aria-label="New session here"] form').findAll('input')[1].element as HTMLInputElement).value).toBe('make test')
    expect(fetchMock.mock.calls.some(([url, init]) => String(url).endsWith('/sessions') && init?.method === 'POST')).toBe(false)
    await wrapper.get('[aria-label="New session here"] form').trigger('submit')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p1/sessions', expect.objectContaining({ method: 'POST', body: JSON.stringify({ name: undefined, startCommand: 'make test' }) }))
    expect(wrapper.emitted('created')?.[0]).toEqual(['work'])
  })

  it('shows hidden entries on request and resolves symlink state lazily', async () => {
    const wrapper = mount(FileBrowser, { props: { machine: 'host' } })
    await flushPromises()
    expect(wrapper.text()).toContain('broken-link (unresolved)')
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/fs/stat?path=%2Fhome%2Fdev%2Fbroken-link'))).toBe(false)
    await wrapper.get('button[aria-label="Check link broken-link"]').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/machines/host/fs/stat?path=%2Fhome%2Fdev%2Fbroken-link', expect.objectContaining({ method: 'GET' }))
    expect(wrapper.text()).toContain('broken-link (broken)')
    expect(wrapper.text()).not.toContain('.hidden')
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await flushPromises()
    expect(wrapper.text()).toContain('.hidden')
  })

  it('selects an existing project path without creating a duplicate', async () => {
    projectsList = [{ id: 'existing', machineId: 'host', path: '/home/dev/work', name: 'Saved work', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }]
    const wrapper = mount(FileBrowser, { props: { machine: 'host' } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'Open project')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Project: Saved work')
    expect(fetchMock.mock.calls.some(([url, init]) => String(url) === '/api/projects' && init?.method === 'POST')).toBe(false)
  })

  it('supports keyboard autocomplete and recovers from invalid paths in place', async () => {
    const wrapper = mount(FileBrowser, { props: { machine: 'host' } })
    await flushPromises()
    const input = wrapper.get('#browser-path')
    await input.setValue('/work')
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect((input.element as HTMLInputElement).value).toBe('/home/dev/work')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.get('[aria-label="Breadcrumbs"]').text()).toContain('work')

    await input.setValue('/home/dev/missing')
    await wrapper.get('#browser-path').element.closest('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Check the path')
    expect(wrapper.get('[aria-label="Breadcrumbs"]').text()).toContain('work')

    await input.setValue('/home/dev/notes.txt')
    await wrapper.get('#browser-path').element.closest('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Choose an existing directory')
    expect(wrapper.get('[aria-label="Breadcrumbs"]').text()).toContain('work')
  })
})
