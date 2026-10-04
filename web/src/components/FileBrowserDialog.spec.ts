import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FileBrowserDialog from './FileBrowserDialog.vue'

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function stubListing(entries: unknown[] = []) {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    let body: unknown = {}
    if (url.endsWith('/fs/home')) body = { path: '/home/dev' }
    else if (url.includes('/api/machines/host/fs?')) body = { path: '/home/dev', entries }
    else if (url.endsWith('/api/projects?machine=*')) body = { projects: [] }
    return { ok: true, status: 200, headers: new Headers(), json: async () => body, text: async () => JSON.stringify(body) }
  }))
}

describe('FileBrowserDialog', () => {
  it('uses bottom-sheet positioning in compact layout', async () => {
    stubListing()
    const wrapper = mount(FileBrowserDialog, { props: { open: true, machine: 'host', compact: true }, attachTo: document.body, global: { plugins: [createPinia()] } })
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog.className).toContain('bottom-0')
    expect(dialog.className).toContain('w-full')
    // Sized by its content up to 85% of the screen, no fixed full height (M8 T3).
    expect(dialog.className).toContain('max-h-[85dvh]')
    expect(dialog.className).not.toMatch(/(^|\s)h-\[/)
    expect([...dialog.querySelectorAll('button')].every((button) => button.classList.contains('touch-target'))).toBe(true)
    expect([...dialog.querySelectorAll('input:not([type="checkbox"])')].every((field) => field.classList.contains('text-base'))).toBe(true)
    wrapper.unmount()
  })

  it('is compact on desktop: narrower, bounded, tight padding and rows, with phone-sized hit areas kept (M8 T3)', async () => {
    stubListing([
      { name: 'work', path: '/home/dev/work', kind: 'directory' },
      { name: 'notes.txt', path: '/home/dev/notes.txt', kind: 'file' },
    ])
    const wrapper = mount(FileBrowserDialog, { props: { open: true, machine: 'host' }, attachTo: document.body, global: { plugins: [createPinia()] } })
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog.className).toContain('w-[min(38rem,calc(100vw-2rem))]')
    expect(dialog.className).toContain('max-h-[min(40rem,85vh)]')
    expect(dialog.className).not.toContain('56rem')
    const header = dialog.firstElementChild as HTMLElement
    expect(header.className).toContain('px-3')
    expect(header.className).toContain('py-2')
    const browser = dialog.querySelector('[aria-label="File browser"]') as HTMLElement
    expect(browser.className).toContain('gap-2')
    expect(browser.className).toContain('p-2')
    const rows = [...dialog.querySelectorAll('[aria-label="Directory entries"] > li')]
    expect(rows).toHaveLength(2)
    for (const row of rows) {
      expect(row.className).toContain('py-0.5')
      expect(row.className).not.toContain('py-2')
    }
    // No control keeps the old 44 px minimum on desktop; every one still
    // grows to 44 px on touch screens (touch-target).
    expect(dialog.querySelectorAll('.min-h-11, .min-w-11')).toHaveLength(0)
    expect([...dialog.querySelectorAll('button')].every((button) => button.classList.contains('touch-target'))).toBe(true)
    expect(dialog.querySelector('input[type=checkbox]')!.closest('label')!.classList.contains('touch-target')).toBe(true)
    // Labels and actions are still there, and still wired.
    expect(dialog.querySelector('button[aria-label="Add work as project"]')).not.toBeNull()
    expect(dialog.querySelector('button[aria-label="Close file browser"]')).not.toBeNull()
    ;(dialog.querySelector('button[aria-label="Close file browser"]') as HTMLElement).click()
    await flushPromises()
    expect(wrapper.emitted('update:open')).toEqual([[false]])
    wrapper.unmount()
  })
})
