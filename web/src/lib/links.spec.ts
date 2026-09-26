import { describe, expect, it, vi } from 'vitest'
import { hyperlinkHandler, isWebLink, openLink } from './links'

describe('links', () => {
  it('only http and https open', () => {
    for (const ok of ['http://example.com/', 'https://example.com/a?b=c#d', 'HTTPS://EXAMPLE.COM']) expect(isWebLink(ok)).toBe(true)
    for (const bad of ['javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,x', 'ssh://host', 'mailto:a@b.c', 'example.com', ''])
      expect(isWebLink(bad)).toBe(false)
  })

  it('opens in a new tab with noopener,noreferrer', () => {
    const open = vi.fn()
    expect(openLink('https://example.com/x', open)).toBe(true)
    expect(open).toHaveBeenCalledWith('https://example.com/x', '_blank', 'noopener,noreferrer')
    expect(openLink('javascript:alert(1)', open)).toBe(false)
    expect(open).toHaveBeenCalledTimes(1)
  })

  it('OSC 8: activate opens the target; hover shows it, leave hides it', () => {
    const open = vi.fn()
    const hovers: unknown[] = []
    const h = hyperlinkHandler((x) => hovers.push(x), open)
    const range = { start: { x: 1, y: 1 }, end: { x: 5, y: 1 } }
    const ev = new MouseEvent('mousemove', { clientX: 10, clientY: 20 })
    h.hover!(ev, 'https://example.com/real', range)
    h.leave!(ev, 'https://example.com/real', range)
    expect(hovers).toEqual([{ url: 'https://example.com/real', x: 10, y: 20 }, null])
    h.activate(ev, 'https://example.com/real', range)
    h.activate(ev, 'file:///etc/passwd', range)
    expect(open.mock.calls).toEqual([['https://example.com/real', '_blank', 'noopener,noreferrer']])
    expect(h.allowNonHttpProtocols).toBe(false)
  })
})
