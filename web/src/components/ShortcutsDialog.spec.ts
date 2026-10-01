import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import ShortcutsDialog from './ShortcutsDialog.vue'
import { shortcuts, shortcutLabels, shortcutPlatform } from '@/lib/shortcuts'

let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

describe('ShortcutsDialog', () => {
  it('renders every registry entry under its group with current platform labels', async () => {
    wrapper = mount(ShortcutsDialog, { props: { open: true }, attachTo: document.body })
    await nextTick()
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog).toBeTruthy()
    expect(dialog.textContent).toContain('Keyboard shortcuts')
    for (const entry of shortcuts) {
      expect(dialog.textContent).toContain(entry.label)
      for (const label of shortcutLabels(entry, shortcutPlatform())) expect(dialog.textContent).toContain(label)
    }
    expect(dialog.textContent).toContain('Works in the terminal')
    expect(dialog.textContent).toContain("Shortcuts aren't customizable yet.")
    for (const group of ['General', 'Sessions', 'Tree']) expect(dialog.textContent).toContain(group)
  })

  it('closes on Escape and emits the closed state', async () => {
    wrapper = mount(ShortcutsDialog, { props: { open: true }, attachTo: document.body })
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })
})
