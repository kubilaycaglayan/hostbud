import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import CommandPalette from './CommandPalette.vue'
import type { PaletteItem } from '@/lib/palette'

Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: () => {} })

const items: PaletteItem[] = [
  { id: 'session:acc-a', label: 'acc-a', group: 'Sessions', secondary: 'Garden', hidden: true },
  { id: 'window:acc-a:@1', label: 'editor', group: 'Windows', secondary: 'acc-a', detail: 'window 1' },
  { id: 'project:p1', label: 'Garden', group: 'Projects', secondary: '/home/dev/garden' },
  { id: 'action:new-session', label: 'New session', group: 'Create', shortcut: 'N' },
  { id: 'action:remove-project:p1', label: 'Remove project Garden', group: 'Destructive', secondary: '/home/dev/garden' },
]
let wrapper: ReturnType<typeof mount> | undefined

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

describe('CommandPalette', () => {
  it('renders grouped session, window, project and action sources with shortcut and hidden markers', async () => {
    wrapper = mount(CommandPalette, { props: { open: true, items }, attachTo: document.body })
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.getAttribute('aria-label')).toBe('Command palette')
    for (const heading of ['Sessions', 'Windows', 'Projects', 'Create', 'Destructive']) expect(dialog.textContent).toContain(heading)
    expect(dialog.querySelector('[data-palette-group="Destructive"] .palette-group-label')?.textContent).toContain('Destructive')
    expect(dialog.querySelector('[data-palette-group="Destructive"] .palette-item')?.getAttribute('data-palette-id')).toBe('action:remove-project:p1')
    expect(dialog.querySelector<HTMLElement>('[data-palette-group="Destructive"]')?.style.getPropertyValue('--palette-group-color')).toBe('var(--hb-danger)')
    expect(dialog.querySelector('[data-palette-group="Destructive"] .palette-item')?.classList.contains('palette-item')).toBe(true)
    for (const item of items) expect(dialog.textContent).toContain(item.label)
    expect(dialog.textContent).toContain('hidden')
    expect(dialog.textContent).toContain('window 1')
    expect(dialog.textContent).toContain('N')
    expect(document.querySelector('input[aria-label="Command palette"]')).toBeTruthy()
  })

  it('fuzzy filters by label or secondary text and emits the chosen item', async () => {
    wrapper = mount(CommandPalette, { props: { open: true, items }, attachTo: document.body })
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('input[aria-label="Command palette"]')!
    input.value = 'gar'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('Garden')
    expect(dialog.textContent).not.toContain('New session')

    input.value = 'edi'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const option = document.body.querySelector<HTMLElement>('[role="option"]')!
    option.click()
    await flushPromises()
    expect(wrapper.emitted('select')).toEqual([['window:acc-a:@1']])
  })

  it('runs the first match on Enter when nothing is highlighted', async () => {
    wrapper = mount(CommandPalette, { props: { open: true, items }, attachTo: document.body })
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('input[aria-label="Command palette"]')!
    input.value = 'new sess'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    // A pasted query filtered out the highlighted item (jsdom keeps Reka's
    // own highlight, so only the palette's first emit is checked).
    document.body.querySelector('[role="option"][data-highlighted]')?.removeAttribute('data-highlighted')
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('select')?.[0]).toEqual(['action:new-session'])
  })

  it('runs the highlighted match on Enter', async () => {
    wrapper = mount(CommandPalette, { props: { open: true, items }, attachTo: document.body })
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('input[aria-label="Command palette"]')!
    document.querySelectorAll('[role="option"][data-highlighted]').forEach((item) => item.removeAttribute('data-highlighted'))
    const option = document.querySelector<HTMLElement>('[role="option"][data-palette-id="project:p1"]')!
    option.setAttribute('data-highlighted', '')
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('select')?.[0]).toEqual(['project:p1'])
  })

  it('closes on Escape and emits the closed state', async () => {
    wrapper = mount(CommandPalette, { props: { open: true, items }, attachTo: document.body })
    await flushPromises()
    document.querySelector('input')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })
})
