/**
 * Pins the app to the *visual* viewport: --app-height is its height and
 * --app-top its offset in the layout viewport. When a phone's on-screen
 * keyboard opens, the visual viewport shrinks (iOS doesn't resize the layout
 * viewport), so the app — and the terminal, through its ResizeObserver —
 * shrinks with it instead of hiding its bottom rows under the keyboard.
 * Without the visualViewport API the CSS fallback (100dvh) applies.
 * Returns a function that stops tracking.
 */
export function trackAppHeight(win: Window = window): () => void {
  const vv = win.visualViewport
  if (!vv) return () => {}
  const root = win.document.documentElement
  const update = () => {
    root.style.setProperty('--app-height', `${vv.height}px`)
    root.style.setProperty('--app-top', `${vv.offsetTop}px`)
  }
  update()
  vv.addEventListener('resize', update)
  vv.addEventListener('scroll', update)
  return () => {
    vv.removeEventListener('resize', update)
    vv.removeEventListener('scroll', update)
    root.style.removeProperty('--app-height')
    root.style.removeProperty('--app-top')
  }
}
