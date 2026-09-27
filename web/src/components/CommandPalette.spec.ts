import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import CommandPalette from './CommandPalette.vue'
import type { PaletteItem } from '@/lib/palette'

Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: () => {} })

const items: PaletteItem[] = [
  { id: 'session:acc-a', label: 'acc-a', group: 'Sessions', secondary: 'Garden', hidden: true },
  { id: 'window:acc-a:@1', label: 'editor', group: 'Windows', secondary: 'acc-a', detail: 'window 1' },
  { id: 'project:p1', label: 'Garden', group: 'Projects', secondary: '/home/dev/garden' },
  { id: 'action:new-session', label: 'New session', group: 'Actions', shortcut: 'N' },
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
    for (const heading of ['Sessions', 'Windows', 'Projects', 'Actions']) expect(dialog.textContent).toContain(heading)
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

  it('closes on Escape and emits the closed state', async () => {
    wrapper = mount(CommandPalette, { props: { open: true, items }, attachTo: document.body })
    await flushPromises()
    document.querySelector('input')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })
})
