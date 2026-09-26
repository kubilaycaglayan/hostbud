import { afterEach, describe, expect, it } from 'vitest'
import { trackAppHeight } from './appHeight'

class FakeVisualViewport extends EventTarget {
  height = 664
  offsetTop = 0
}

function fakeWindow(vv?: FakeVisualViewport) {
  return { visualViewport: vv, document } as unknown as Window
}

const prop = (name: string) => document.documentElement.style.getPropertyValue(name)

describe('trackAppHeight', () => {
  let stop = () => {}
  afterEach(() => stop())

  it('sets --app-height and --app-top from the visual viewport', () => {
    const vv = new FakeVisualViewport()
    stop = trackAppHeight(fakeWindow(vv))
    expect(prop('--app-height')).toBe('664px')
    expect(prop('--app-top')).toBe('0px')
  })

  it('follows the on-screen keyboard opening and the page scrolling under it', () => {
    const vv = new FakeVisualViewport()
    stop = trackAppHeight(fakeWindow(vv))
    vv.height = 330 // keyboard up
    vv.dispatchEvent(new Event('resize'))
    expect(prop('--app-height')).toBe('330px')
    vv.offsetTop = 120
    vv.dispatchEvent(new Event('scroll'))
    expect(prop('--app-top')).toBe('120px')
  })

  it('stops following and clears the variables', () => {
    const vv = new FakeVisualViewport()
    trackAppHeight(fakeWindow(vv))()
    expect(prop('--app-height')).toBe('')
    vv.height = 100
    vv.dispatchEvent(new Event('resize'))
    expect(prop('--app-height')).toBe('')
  })

  it('leaves the CSS fallback without the visualViewport API', () => {
    stop = trackAppHeight(fakeWindow(undefined))
    expect(prop('--app-height')).toBe('')
  })
})
