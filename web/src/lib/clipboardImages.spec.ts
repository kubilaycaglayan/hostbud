import { describe, expect, it, vi } from 'vitest'
import { clipboardImages, insertWord, relativePath } from './clipboardImages'

describe('clipboardImages', () => {
  it('extracts image files while ignoring text and non-image files', () => {
    const photo = new File(['image'], 'photo.png', { type: 'image/png' })
    const getAsFile = vi.fn(() => photo)
    const event = { clipboardData: { items: [
      { kind: 'string', type: 'text/plain', getAsFile },
      { kind: 'file', type: 'application/pdf', getAsFile },
      { kind: 'file', type: 'image/png', getAsFile },
    ], files: [] } } as unknown as ClipboardEvent
    expect(clipboardImages(event)).toEqual([photo])
    expect(getAsFile).toHaveBeenCalledOnce()
  })

  it('falls back to clipboard files and leaves text-only pastes empty', () => {
    const photo = new File(['image'], 'photo.png', { type: 'image/png' })
    const pdf = new File(['pdf'], 'file.pdf', { type: 'application/pdf' })
    expect(clipboardImages({ clipboardData: { items: [], files: [photo, pdf] } } as unknown as ClipboardEvent)).toEqual([photo])
    expect(clipboardImages({ clipboardData: { items: [{ kind: 'string', type: 'text/plain' }], files: [] } } as unknown as ClipboardEvent)).toEqual([])
  })
})

describe('relativePath', () => {
  it('prefixes paths inside the base with ./ and walks up otherwise', () => {
    expect(relativePath('/home/dev/repo', '/home/dev/repo/shot.png')).toBe('./shot.png')
    expect(relativePath('/home/dev/repo/nested', '/home/dev/repo/shot.png')).toBe('../shot.png')
  })
})

describe('insertWord', () => {
  it('inserts at the caret as its own word and places the caret after it', () => {
    expect(insertWord('/goal fix', 9, 9, './a.png')).toEqual({ value: '/goal fix ./a.png', caret: 17 })
    expect(insertWord('see here', 3, 4, './a.png')).toEqual({ value: 'see ./a.png here', caret: 11 })
    expect(insertWord('', 0, 0, './a.png')).toEqual({ value: './a.png', caret: 7 })
  })
})
