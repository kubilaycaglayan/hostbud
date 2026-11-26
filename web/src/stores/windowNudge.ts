import { defineStore } from 'pinia'
import { ref } from 'vue'
import { windowNudgeApi } from '@/api/client'
import type { ServerEvent, WindowNudge } from '@/api/types'
import { describeError } from './toasts'

export const useWindowNudgeStore = defineStore('windowNudge', () => {
  const settings = ref<WindowNudge | null>(null)
  const loadError = ref('')
  const saving = ref(false)

  async function load() {
    try { settings.value = await windowNudgeApi.get(); loadError.value = '' }
    catch (error) { loadError.value = describeError(error).message }
  }
  async function save(value: Pick<WindowNudge, 'enabled' | 'machineId' | 'directory' | 'provider' | 'schedule'> & { skipFirst?: boolean }) {
    saving.value = true
    try { settings.value = await windowNudgeApi.save(value); loadError.value = ''; return true }
    catch (error) { loadError.value = describeError(error).message; return false }
    finally { saving.value = false }
  }
  function apply(event: ServerEvent) {
    if (event.type === 'snapshot') void load()
    if (event.type === 'window_nudge.changed') settings.value = event.payload
  }
  return { settings, loadError, saving, load, save, apply }
})
