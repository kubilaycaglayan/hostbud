import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { ServerEvent, Session } from '@/api/types'

export type SessionsByMachine = Record<string, Session[]>

/** Pure reducer: sessions per machine after an event. */
export function applySessions(state: SessionsByMachine, e: ServerEvent): SessionsByMachine {
  switch (e.type) {
    case 'snapshot':
      return { ...e.sessions }
    case 'sessions.changed':
      return { ...state, [e.machine]: e.payload.sessions }
    default:
      return state
  }
}

export const useSessionsStore = defineStore('sessions', () => {
  const byMachine = ref<SessionsByMachine>({})
  function apply(e: ServerEvent) {
    byMachine.value = applySessions(byMachine.value, e)
  }
  function list(machine: string): Session[] {
    return byMachine.value[machine] ?? []
  }
  function reset() {
    byMachine.value = {}
  }
  return { byMachine, apply, list, reset }
})
