import { defineStore } from 'pinia'
import { ref } from 'vue'
import { eventsURL, LiveConnection, type LiveOptions, type LiveState } from '@/api/live'
import { useAuthStore } from './auth'
import { useMachinesStore } from './machines'
import { useSessionsStore } from './sessions'

/** Owns the /ws/events connection and feeds the machines/sessions stores. */
export const useLiveStore = defineStore('live', () => {
  const state = ref<LiveState>('idle')
  let conn: LiveConnection | null = null

  function start(overrides: Partial<LiveOptions> = {}) {
    if (conn) return
    const machines = useMachinesStore()
    const sessions = useSessionsStore()
    const auth = useAuthStore()
    conn = new LiveConnection({
      url: eventsURL(),
      onEvent: (e) => {
        machines.apply(e)
        sessions.apply(e)
      },
      onState: (s) => (state.value = s),
      stillAuthorized: async () => {
        try {
          await auth.check()
        } catch {
          return true // hostbud itself is down: keep retrying
        }
        return auth.status === 'authenticated'
      },
      ...overrides,
    })
    conn.start()
  }

  function stop() {
    conn?.stop()
    conn = null
    useMachinesStore().reset()
    useSessionsStore().reset()
  }

  return { state, start, stop }
})
