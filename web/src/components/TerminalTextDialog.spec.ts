import { afterEach, describe, expect, it } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import TerminalTextDialog from './TerminalTextDialog.vue'

let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

async function open(props = {}) {
  wrapper = mount(TerminalTextDialog, {
    props: { mode: 'snapshot', open: false, ...props },
    global: { plugins: [createPinia()] }, attachTo: document.body,
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

describe('native terminal text view', () => {
  it('renders styled text safely and selects only the output with Ctrl+A', async () => {
    const text = 'older conversation\n<script>alert(1)</script>\n'
    await open({ snapshot: text + '\x1b[1;31mred\x1b[0m' })
    const reader = document.querySelector<HTMLElement>('[aria-label="Terminal text"]')!
    expect(reader.textContent).toBe(text + 'red')
    expect(reader.querySelector('script, canvas, textarea')).toBeNull()
    const styled = [...reader.querySelectorAll('span')].find((span) => span.textContent === 'red')!
    expect(styled.style.fontWeight).toBe('bold')
    expect(styled.style.color).not.toBe('')
    reader.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', ctrlKey: true, bubbles: true, cancelable: true }))
    expect(window.getSelection()?.toString()).toBe(text + 'red')
  })

  it('shows loading and a retryable failure rather than a partial screen', async () => {
    const w = await open({ loading: true })
    expect(document.querySelector('[role="status"]')?.textContent).toContain('Loading')
    expect(document.querySelector('[aria-label="Terminal text"]')).toBeNull()
    await w.setProps({ loading: false, error: 'Host unavailable' })
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('Host unavailable')
    ;[...document.querySelectorAll('button')].find((b) => b.textContent === 'Try again')!.click()
    expect(w.emitted('retry')).toHaveLength(1)
  })
})
