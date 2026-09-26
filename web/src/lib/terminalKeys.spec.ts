import { describe, expect, it } from 'vitest'
import { editingKey } from './terminalKeys'

const key = (key: string, mods: Partial<Record<'altKey' | 'metaKey' | 'ctrlKey' | 'shiftKey', boolean>> = {}) => ({
  key,
  altKey: false,
  metaKey: false,
  ctrlKey: false,
  shiftKey: false,
  ...mods,
})

describe('editingKey', () => {
  it.each([
    ['Backspace', 'altKey', '\x1b\x7f'],
    ['ArrowLeft', 'altKey', '\x1bb'],
    ['ArrowRight', 'altKey', '\x1bf'],
    ['Backspace', 'metaKey', '\x15'],
    ['ArrowLeft', 'metaKey', '\x01'],
    ['ArrowRight', 'metaKey', '\x05'],
  ] as const)('%s with %s → %j', (k, mod, bytes) => {
    expect(editingKey(key(k, { [mod]: true }))).toBe(bytes)
  })

  it('leaves plain keys to xterm', () => {
    for (const k of ['Backspace', 'ArrowLeft', 'ArrowRight', 'a']) expect(editingKey(key(k))).toBeUndefined()
  })

  it('leaves other Option/Cmd keys alone (Cmd+C, Cmd+V, Option+Up)', () => {
    expect(editingKey(key('c', { metaKey: true }))).toBeUndefined()
    expect(editingKey(key('v', { metaKey: true }))).toBeUndefined()
    expect(editingKey(key('ArrowUp', { altKey: true }))).toBeUndefined()
  })

  it('leaves combinations with extra modifiers alone', () => {
    expect(editingKey(key('ArrowLeft', { metaKey: true, shiftKey: true }))).toBeUndefined() // select
    expect(editingKey(key('Backspace', { altKey: true, ctrlKey: true }))).toBeUndefined()
    expect(editingKey(key('Backspace', { altKey: true, metaKey: true }))).toBeUndefined()
    expect(editingKey(key('Backspace', { ctrlKey: true }))).toBeUndefined()
  })
})
