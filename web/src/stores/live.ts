import { defineStore } from 'pinia'
import { ref } from 'vue'
import { eventsURL, LiveConnection, type LiveOptions, type LiveState } from '@/api/live'
import type { ServerEvent } from '@/api/types'
import { useAuthStore } from './auth'
import { useLayoutStore } from './layout'
import { useMachinesStore } from './machines'
import { useSessionsStore } from './sessions'
import { useProjectsStore } from './projects'
import { useTreeStore } from './tree'

/** Owns the /ws/events connection and feeds the machines/sessions stores. */
export const useLiveStore = defineStore('live', () => {
  const state = ref<LiveState>('idle')
  let conn: LiveConnection | null = null

  function start(overrides: Partial<LiveOptions> = {}) {
    if (conn) return
    const machines = useMachinesStore()
    const sessions = useSessionsStore()
    const projects = useProjectsStore()
    const auth = useAuthStore()
    conn = new LiveConnection({
      url: eventsURL(),
      onEvent: (e) => {
        machines.apply(e)
        sessions.apply(e)
        projects.apply(e)
        useTreeStore().sync()
        closeEndedSessions(e)
      },
      onState: (s) => (state.value = s),
      stillAuthorized: () => auth.stillAuthorized(),
      ...overrides,
    })
    conn.start()
  }

  /** Closes the terminals of sessions that ended. Only a list from a
   * reachable host counts: before its first poll (right after an app
   * restart) or while unreachable, the list is empty or stale. */
  function closeEndedSessions(e: ServerEvent) {
    const ids = e.type === 'snapshot' ? Object.keys(e.sessions) : e.type === 'sessions.changed' ? [e.machine] : []
    const machines = useMachinesStore()
    const sessions = useSessionsStore()
    for (const id of ids) {
      if (machines.byId(id)?.status !== 'ok') continue
      useLayoutStore().syncSessions(id, new Set(sessions.list(id).map((s) => s.name)))
    }
  }

  function stop() {
    conn?.stop()
    conn = null
    useMachinesStore().reset()
    useSessionsStore().reset()
    useProjectsStore().reset()
    useTreeStore().reset()
  }

  return { state, start, stop, closeEndedSessions }
})
