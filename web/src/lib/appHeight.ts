/**
 * Pins the app to the *visual* viewport: --app-height is its height and
 * --app-top its offset in the layout viewport. When a phone's on-screen
 * keyboard opens, the visual viewport shrinks (iOS doesn't resize the layout
 * viewport), so the app — and the terminal, through its ResizeObserver —
 * shrinks with it instead of hiding its bottom rows under the keyboard.
 * --app-pad-bottom drops the home-indicator inset when nothing is drawn over
 * the home indicator: while the keyboard is up (iOS keeps reporting the inset
 * although the keyboard covers that area), and in an iOS home-screen app whose
 * page stops short of the screen bottom (it can't paint there anyway).
 * Without the visualViewport API the CSS fallback (100dvh) applies.
 * Returns a function that stops tracking.
 */
export function trackAppHeight(win: Window = window): () => void {
  const vv = win.visualViewport
  if (!vv) return () => {}
  const root = win.document.documentElement
  const update = () => {
    // In layout pixels: a zoom (e.g. left over from a rotation, or a pinch)
    // shrinks vv.height but mustn't shrink the app; the keyboard still does.
    const height = vv.height * (vv.scale || 1)
    const full = standaloneScreenHeight(win)
    const keyboard = Math.max(win.innerHeight, full) - height > KEYBOARD_MIN_HEIGHT
    const shortOfScreen = full - win.innerHeight > SHORT_OF_SCREEN
    root.style.setProperty('--app-height', `${height}px`)
    root.style.setProperty('--app-top', `${vv.offsetTop}px`)
    if (keyboard || shortOfScreen) root.style.setProperty('--app-pad-bottom', '0px')
    else root.style.removeProperty('--app-pad-bottom')
  }
  update()
  vv.addEventListener('resize', update)
  vv.addEventListener('scroll', update)
  return () => {
    vv.removeEventListener('resize', update)
    vv.removeEventListener('scroll', update)
    root.style.removeProperty('--app-height')
    root.style.removeProperty('--app-top')
    root.style.removeProperty('--app-pad-bottom')
  }
}

/** Viewport shrinkage (CSS px) above which the on-screen keyboard is up. */
const KEYBOARD_MIN_HEIGHT = 150

/** How far (CSS px) the page may end above the screen bottom and still reach the home indicator. */
const SHORT_OF_SCREEN = 20

/** The screen's height in the current orientation for an iOS home-screen app, else 0. */
function standaloneScreenHeight(win: Window): number {
  if ((win.navigator as { standalone?: boolean } | undefined)?.standalone !== true) return 0
  const { width, height } = win.screen
  return win.innerWidth > win.innerHeight ? Math.min(width, height) : Math.max(width, height)
}
