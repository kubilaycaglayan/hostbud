/** Blur text inputs when a standalone PWA is sent to the background. */
export function blurActiveFieldOnHide(doc: Document): () => void {
  const onVisibility = () => {
    if (doc.visibilityState !== 'visible' && doc.activeElement instanceof HTMLElement) {
      const active = doc.activeElement
      if (active.matches('input, textarea, [contenteditable="true"], .xterm-helper-textarea')) active.blur()
    }
  }
  doc.addEventListener('visibilitychange', onVisibility)
  return () => doc.removeEventListener('visibilitychange', onVisibility)
}
