import { flushPromises, mount } from '@vue/test-utils'
import { SelectRoot } from 'reka-ui'
import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import DurationPicker from './DurationPicker.vue'

afterEach(() => { document.body.innerHTML = '' })

function mountPicker(seconds: number) {
  const model = { value: seconds }
  const w = mount(DurationPicker, {
    props: { label: 'Delay', modelValue: seconds, 'onUpdate:modelValue': (v: number) => { model.value = v; void w.setProps({ modelValue: v }) } },
    attachTo: document.body,
  })
  return { w, model }
}

describe('DurationPicker', () => {
  it('renders Reka UI selects for hours and minutes, not native selects', () => {
    const { w } = mountPicker(5400)
    expect(w.findAll('select')).toHaveLength(0)
    const triggers = w.findAll('[role="combobox"]')
    expect(triggers.map((t) => t.attributes('aria-label'))).toEqual(['Hours', 'Minutes'])
    expect(triggers.map((t) => t.text())).toEqual(['1', '30'])
  })

  it('offers at most 48 hours and caps the value there', async () => {
    const { w, model } = mountPicker(0)
    await w.find('[aria-label="Hours"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const options = [...document.body.querySelectorAll('[role="option"]')].map((o) => o.textContent?.trim())
    expect(options.at(-1)).toBe('48')
    expect(options).toHaveLength(49)
    // The popup scrolls inside a bounded height instead of overflowing the screen.
    expect(document.body.querySelector('[role="listbox"]')!.className).toMatch(/max-h-\[min\(16rem,/)
    const [h, m] = w.findAllComponents(SelectRoot)
    h.vm.$emit('update:modelValue', 48)
    await nextTick()
    m.vm.$emit('update:modelValue', 30)
    await nextTick()
    expect(model.value).toBe(48 * 3600)
  })

  it('shows a value set elsewhere above the cap as the cap', () => {
    const { w } = mountPicker(100 * 3600)
    expect(w.find('[aria-label="Hours"]').text()).toBe('48')
  })
})
