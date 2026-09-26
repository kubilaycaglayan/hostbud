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

/** Tailwind's `md` breakpoint: below it the app is in its narrow (phone)
 * layout. */
export const WIDE_QUERY = '(min-width: 48rem)'
