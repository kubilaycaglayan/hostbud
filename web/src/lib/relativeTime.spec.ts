import { describe, expect, it } from 'vitest'
import { relativeTime, sessionSubtitle } from './relativeTime'

describe('relativeTime', () => {
  const now = Date.parse('2026-09-28T12:00:00Z')

  it('formats compact ages', () => {
    expect(relativeTime('2026-09-28T11:59:40Z', now)).toBe('now')
    expect(relativeTime('2026-09-28T11:56:00Z', now)).toBe('4m')
    expect(relativeTime('2026-09-28T09:00:00Z', now)).toBe('3h')
    expect(relativeTime('2026-09-26T11:00:00Z', now)).toBe('2d')
    expect(relativeTime('2026-09-28T12:05:00Z', now)).toBe('now')
  })

  it('hides unknown times', () => {
    expect(relativeTime('', now)).toBe('')
    expect(relativeTime('not a date', now)).toBe('')
    expect(relativeTime('0001-01-01T00:00:00Z', now)).toBe('')
    expect(relativeTime('1970-01-01T00:00:00Z', now)).toBe('')
  })
})

describe('sessionSubtitle', () => {
  it('drops leading agent glyphs', () => {
    expect(sessionSubtitle('✳ deploy the changes')).toBe('deploy the changes')
    expect(sessionSubtitle('⠐ indexing ~/docs')).toBe('indexing ~/docs')
    expect(sessionSubtitle('_ tmux probe shell')).toBe('tmux probe shell')
    expect(sessionSubtitle('rg -n "x"')).toBe('rg -n "x"')
    expect(sessionSubtitle(undefined)).toBe('')
    expect(sessionSubtitle('✳ ')).toBe('')
  })
})
