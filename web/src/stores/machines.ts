import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Machine, ServerEvent } from '@/api/types'

/** Pure reducer: the machines after an event. */
export function applyMachines(machines: Machine[], e: ServerEvent): Machine[] {
  switch (e.type) {
    case 'snapshot':
      return e.machines
    case 'machine.status': {
      const i = machines.findIndex((m) => m.id === e.payload.id)
      if (i < 0) return [...machines, e.payload]
      return machines.map((m, j) => (j === i ? e.payload : m))
    }
    case 'machine.removed':
      return machines.filter((m) => m.id !== e.payload.id)
    default:
      return machines
  }
}

export const useMachinesStore = defineStore('machines', () => {
  const machines = ref<Machine[]>([])
  function apply(e: ServerEvent) {
    machines.value = applyMachines(machines.value, e)
  }
  function byId(id: string) {
    return machines.value.find((m) => m.id === id)
  }
  /** A machine's nickname for chips: its label, or its id while unknown. */
  function label(id: string) {
    return byId(id)?.label || id
  }
  /** Whether a machine is a server added in the UI, not the host. */
  function isServer(id: string) {
    return id !== 'host'
  }
  function reset() {
    machines.value = []
  }
  return { machines, apply, byId, label, isServer, reset }
})
