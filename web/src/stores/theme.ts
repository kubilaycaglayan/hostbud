import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getUIState, putUIState } from '@/api/client'
import { resolveTheme, validateTheme, type ThemeMode } from '@/lib/theme'

export const useThemeStore = defineStore('theme', () => {
  const mode = ref<ThemeMode>('system')
  const prefersDark = ref(typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: dark)').matches)
  const resolved = computed(() => resolveTheme(mode.value, prefersDark.value))
  let media: MediaQueryList | null = null
  let listener: (() => void) | null = null

  function apply() {
    const value = resolved.value
    document.documentElement.dataset.theme = value
    document.documentElement.style.colorScheme = value
    document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute('content', value === 'dark' ? '#0f1115' : '#ffffff')
    try { localStorage.setItem('hostbud.theme', mode.value) } catch { /* storage may be blocked */ }
  }

  function stop() {
    if (media && listener) media.removeEventListener('change', listener)
    media = null
    listener = null
  }

  function listen() {
    stop()
    if (typeof matchMedia === 'undefined') return
    media = matchMedia('(prefers-color-scheme: dark)')
    listener = () => {
      prefersDark.value = media?.matches ?? false
      if (mode.value === 'system') apply()
    }
    media.addEventListener('change', listener)
  }

  async function load() {
    listen()
    try { mode.value = validateTheme(await getUIState('theme')) } catch { mode.value = 'system' }
    apply()
  }

  async function setMode(next: ThemeMode) {
    mode.value = next
    apply()
    try { await putUIState('theme', { version: 1, mode: next }) } catch (error) { console.warn('hostbud: could not save theme', error) }
  }

  function signOut() {
    stop()
  }

  return { mode, resolved, apply, load, setMode, signOut }
})
