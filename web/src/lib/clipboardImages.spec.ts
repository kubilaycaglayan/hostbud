import { describe, expect, it, vi } from 'vitest'
import { clipboardImages } from './clipboardImages'

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
