import { describe, expect, it } from 'vitest'
import { clipboardKey, editingKey } from './terminalKeys'

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

describe('clipboardKey', () => {
  it.each([
    ['C', { ctrlKey: true, shiftKey: true }, false, 'copy'],
    ['C', { metaKey: true, shiftKey: true }, false, 'copy'],
    ['c', { metaKey: true }, true, 'copy'],
    ['V', { ctrlKey: true, shiftKey: true }, false, 'paste'],
    ['V', { metaKey: true, shiftKey: true }, false, 'paste'],
    ['v', { metaKey: true }, false, 'paste'],
  ] as const)('%s with %o (selection: %s) → %s', (k, mods, sel, action) => {
    expect(clipboardKey(key(k, mods), sel)).toBe(action)
  })

  it('Cmd+C without a selection does nothing', () => {
    expect(clipboardKey(key('c', { metaKey: true }), false)).toBeUndefined()
  })

  it('leaves Ctrl+C (interrupt) and Ctrl+V to the program, even with a selection', () => {
    expect(clipboardKey(key('c', { ctrlKey: true }), true)).toBeUndefined()
    expect(clipboardKey(key('v', { ctrlKey: true }), true)).toBeUndefined()
  })

  it('leaves plain keys and other combinations alone', () => {
    expect(clipboardKey(key('c'), true)).toBeUndefined()
    expect(clipboardKey(key('C', { shiftKey: true }), true)).toBeUndefined()
    expect(clipboardKey(key('x', { ctrlKey: true, shiftKey: true }), true)).toBeUndefined()
    expect(clipboardKey(key('c', { altKey: true }), true)).toBeUndefined()
    expect(clipboardKey(key('C', { ctrlKey: true, metaKey: true, shiftKey: true }), true)).toBeUndefined()
    expect(clipboardKey(key('C', { ctrlKey: true, altKey: true, shiftKey: true }), true)).toBeUndefined()
  })
})
