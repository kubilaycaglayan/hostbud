import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { ServerEvent, Session } from '@/api/types'

export type SessionsByMachine = Record<string, Session[]>

/** Pure reducer: sessions per machine after an event. */
export function applySessions(state: SessionsByMachine, e: ServerEvent): SessionsByMachine {
  switch (e.type) {
    case 'snapshot': {
      // A machine not yet probed (right after an app restart) has an empty,
      // not authoritative, list. Leave it out until its first sessions.changed,
      // so a machine.status ok arriving first can't prune saved tree state.
      const unprobed = new Set(e.machines.filter((m) => m.status === 'unknown').map((m) => m.id))
      return Object.fromEntries(Object.entries(e.sessions).filter(([id]) => !unprobed.has(id)))
    }
    case 'sessions.changed':
      return { ...state, [e.machine]: e.payload.sessions }
    default:
      return state
  }
}

export const useSessionsStore = defineStore('sessions', () => {
  const byMachine = ref<SessionsByMachine>({})
  const pendingRenames = new Map<string, string>()
  function apply(e: ServerEvent) {
    byMachine.value = applySessions(byMachine.value, e)
    const machines = e.type === 'snapshot' ? Object.keys(byMachine.value) : e.type === 'sessions.changed' ? [e.machine] : []
    for (const machine of machines) {
      byMachine.value[machine] = (byMachine.value[machine] ?? []).map((session) => {
        const to = pendingRenames.get(`${machine}/${session.name}`)
        return to ? { ...session, name: to } : session
      })
    }
  }
  function list(machine: string): Session[] {
    return byMachine.value[machine] ?? []
  }
  function renameLocal(machine: string, from: string, to: string): Session | undefined {
    const rows = list(machine)
    const original = rows.find((session) => session.name === from)
    if (!original) return undefined
    byMachine.value = {
      ...byMachine.value,
      [machine]: rows.map((session) => session === original ? { ...session, name: to } : session),
    }
    return original
  }
  function beginRename(machine: string, from: string, to: string) {
    pendingRenames.set(`${machine}/${from}`, to)
    renameLocal(machine, from, to)
  }
  function finishRename(machine: string, from: string, to: string, succeeded: boolean) {
    pendingRenames.delete(`${machine}/${from}`)
    if (!succeeded) renameLocal(machine, to, from)
  }
  function reset() {
    byMachine.value = {}
    pendingRenames.clear()
  }
  return { byMachine, apply, list, renameLocal, beginRename, finishRename, reset }
})
