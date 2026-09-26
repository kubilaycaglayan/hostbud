import { describe, expect, it } from 'vitest'
import { applyModifiers, createModifiers, keyBytes, sendKey, toggleModifier } from './keyBar'

describe('keyBar byte mapping', () => {
  it.each([
    ['Escape', '\x1b'], ['Tab', '\t'], ['ArrowLeft', '\x1b[D'], ['ArrowUp', '\x1b[A'], ['ArrowDown', '\x1b[B'], ['ArrowRight', '\x1b[C'],
    ['|', '|'], ['~', '~'], ['/', '/'], ['-', '-'],
  ] as const)('%s emits its normal key bytes', (key, bytes) => {
    expect(keyBytes(key, false)).toBe(bytes)
  })

  it.each([
    ['ArrowLeft', '\x1bOD'], ['ArrowUp', '\x1bOA'], ['ArrowDown', '\x1bOB'], ['ArrowRight', '\x1bOC'],
  ] as const)('%s follows application cursor mode', (key, bytes) => {
    expect(keyBytes(key, true)).toBe(bytes)
  })

  it.each([
    ['a', '\x01'], ['z', '\x1a'], ['@', '\x00'], [' ', '\x00'], ['[', '\x1b'], ['\\', '\x1c'], [']', '\x1d'], ['^', '\x1e'], ['_', '\x1f'], ['?', '\x7f'],
  ])('Control+%s maps to its control byte', (key, bytes) => {
    expect(keyBytes(key, false, true)).toBe(bytes)
  })

  it('maps Control on every letter', () => {
    for (let i = 0; i < 26; i++) expect(keyBytes(String.fromCharCode(97 + i), false, true)).toBe(String.fromCharCode(i + 1))
  })

  it('adds Alt prefixes and keeps unmapped Ctrl keys plain', () => {
    expect(keyBytes('b', false, false, true)).toBe('\x1bb')
    expect(keyBytes('|', false, true)).toBe('|')
    expect(keyBytes('ArrowUp', false, true)).toBe('\x1b[1;5A')
    expect(keyBytes('ArrowUp', false, false, true)).toBe('\x1b[1;3A')
    expect(keyBytes('ArrowUp', false, true, true)).toBe('\x1b\x1b[1;5A')
    expect(keyBytes('c', false, true, true)).toBe('\x1b\x03')
  })

  it('arms, locks on a double tap and toggles a locked modifier off', () => {
    const modifiers = createModifiers()
    toggleModifier(modifiers, 'ctrl', 1000)
    expect(modifiers.ctrl).toMatchObject({ armed: true, locked: false })
    toggleModifier(modifiers, 'ctrl', 1200)
    expect(modifiers.ctrl).toMatchObject({ armed: true, locked: true })
    sendKey('ArrowUp', false, modifiers)
    expect(modifiers.ctrl.armed).toBe(true)
    toggleModifier(modifiers, 'ctrl', 1400)
    expect(modifiers.ctrl).toMatchObject({ armed: false, locked: false })
  })

  it('applies one-shot modifiers to the next soft-keyboard character only', () => {
    const modifiers = createModifiers()
    toggleModifier(modifiers, 'ctrl', 1000)
    expect(applyModifiers('cxyz', modifiers)).toBe('\x03xyz')
    expect(applyModifiers('c', modifiers)).toBe('c')
    toggleModifier(modifiers, 'alt', 2000)
    expect(applyModifiers('bword', modifiers)).toBe('\x1bbword')
  })
})
