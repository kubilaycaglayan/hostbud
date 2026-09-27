import { afterEach, describe, expect, it } from 'vitest'
import { trackAppHeight } from './appHeight'

class FakeVisualViewport extends EventTarget {
  height = 664
  offsetTop = 0
}

function fakeWindow(vv?: FakeVisualViewport, opts: { innerHeight?: number; standalone?: boolean } = {}) {
  return {
    visualViewport: vv,
    document,
    innerWidth: 390,
    innerHeight: opts.innerHeight ?? 664,
    navigator: { standalone: opts.standalone },
    screen: { width: 390, height: 844 },
  } as unknown as Window
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
    expect(prop('--app-pad-bottom')).toBe('')
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

  it('drops the bottom safe-area padding only while the keyboard is up', () => {
    const vv = new FakeVisualViewport()
    stop = trackAppHeight(fakeWindow(vv))
    vv.height = 330
    vv.dispatchEvent(new Event('resize'))
    expect(prop('--app-pad-bottom')).toBe('0px')
    vv.height = 664
    vv.dispatchEvent(new Event('resize'))
    expect(prop('--app-pad-bottom')).toBe('')
  })

  it('drops the bottom padding in an iOS home-screen app whose page stops short of the screen', () => {
    const vv = new FakeVisualViewport()
    vv.height = 797
    stop = trackAppHeight(fakeWindow(vv, { innerHeight: 797, standalone: true }))
    expect(prop('--app-height')).toBe('797px')
    expect(prop('--app-pad-bottom')).toBe('0px')
  })

  it('keeps the bottom padding in an iOS home-screen app that reaches the screen bottom', () => {
    const vv = new FakeVisualViewport()
    vv.height = 844
    stop = trackAppHeight(fakeWindow(vv, { innerHeight: 844, standalone: true }))
    expect(prop('--app-height')).toBe('844px')
    expect(prop('--app-pad-bottom')).toBe('')
    vv.height = 430 // keyboard up
    vv.dispatchEvent(new Event('resize'))
    expect(prop('--app-height')).toBe('430px')
    expect(prop('--app-pad-bottom')).toBe('0px')
  })

  it('keeps the bottom padding outside a home-screen app', () => {
    const vv = new FakeVisualViewport()
    vv.height = 797
    stop = trackAppHeight(fakeWindow(vv, { innerHeight: 797 }))
    expect(prop('--app-height')).toBe('797px')
    expect(prop('--app-pad-bottom')).toBe('')
  })

  it('stops following and clears the variables', () => {
    const vv = new FakeVisualViewport()
    trackAppHeight(fakeWindow(vv))()
    expect(prop('--app-height')).toBe('')
    vv.height = 100
    vv.dispatchEvent(new Event('resize'))
    expect(prop('--app-height')).toBe('')
    expect(prop('--app-pad-bottom')).toBe('')
  })

  it('leaves the CSS fallback without the visualViewport API', () => {
    stop = trackAppHeight(fakeWindow(undefined))
    expect(prop('--app-height')).toBe('')
  })
})
