import { onScopeDispose, ref, type Ref } from 'vue'

/** Whether a media query matches, kept up to date. Without matchMedia
 * (tests) it reports `fallback`. */
export function useMediaQuery(query: string, fallback = false): Ref<boolean> {
  const matches = ref(fallback)
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return matches
  const mq = window.matchMedia(query)
  matches.value = mq.matches
  const update = () => (matches.value = mq.matches)
  mq.addEventListener('change', update)
  onScopeDispose(() => mq.removeEventListener('change', update))
  return matches
}

/** Compact phones match in portrait and landscape without matching tablets. */
export const COMPACT_QUERY = '(max-width: 47.99rem), (pointer: coarse) and (max-height: 31.99rem)'
