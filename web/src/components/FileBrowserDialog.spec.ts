import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FileBrowserDialog from './FileBrowserDialog.vue'

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('FileBrowserDialog', () => {
  it('uses bottom-sheet positioning in compact layout', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      let body: unknown = {}
      if (url.endsWith('/fs/home')) body = { path: '/home/dev' }
      else if (url.includes('/api/machines/host/fs?')) body = { path: '/home/dev', entries: [] }
      else if (url.endsWith('/api/projects?machine=host')) body = { projects: [] }
      return { ok: true, status: 200, headers: new Headers(), json: async () => body, text: async () => JSON.stringify(body) }
    }))
    const wrapper = mount(FileBrowserDialog, { props: { open: true, machine: 'host', compact: true }, attachTo: document.body, global: { plugins: [createPinia()] } })
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog.className).toContain('bottom-0')
    expect(dialog.className).toContain('w-full')
    expect([...dialog.querySelectorAll('button')].every((button) => button.classList.contains('touch-target'))).toBe(true)
    expect([...dialog.querySelectorAll('input:not([type="checkbox"])')].every((field) => field.classList.contains('text-base'))).toBe(true)
    wrapper.unmount()
  })
})
