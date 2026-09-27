import { describe, expect, it } from 'vitest'
import { blurActiveFieldOnHide } from './pageFocus'

describe('blurActiveFieldOnHide', () => {
  it('blurs active text inputs when hidden and removes its listener', () => {
    const input = document.createElement('textarea')
    document.body.append(input)
    input.focus()
    const original = Object.getOwnPropertyDescriptor(document, 'visibilityState')
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    const dispose = blurActiveFieldOnHide(document)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(document.activeElement).not.toBe(input)
    input.focus()
    dispose()
    document.dispatchEvent(new Event('visibilitychange'))
    expect(document.activeElement).toBe(input)
    input.remove()
    if (original) Object.defineProperty(document, 'visibilityState', original)
    else Reflect.deleteProperty(document, 'visibilityState')
  })
})
