import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { getUIState, putUIState } from '@/api/client'
import { emptyTreeOrder, ordered, projectTree, validateTreeOrder, type TreeOrder } from '@/lib/tree'
import { useProjectsStore } from './projects'
import { useSessionsStore } from './sessions'
import { useMachinesStore } from './machines'

export const useTreeStore = defineStore('tree', () => {
  const order = ref<TreeOrder>(emptyTreeOrder())
  const loaded = ref(false)
  const projectsStore = useProjectsStore()
  const sessionsStore = useSessionsStore()
  const machinesStore = useMachinesStore()
  let timer: ReturnType<typeof setTimeout> | undefined
  let generation = 0

  const groups = computed(() => projectTree(projectsStore.items, sessionsStore.list('host'), order.value))

  async function load() {
    const gen = ++generation
    try {
      const saved = await getUIState('tree')
      if (gen !== generation) return
      order.value = (saved !== null && validateTreeOrder(saved)) || emptyTreeOrder()
    } catch (error) {
      console.warn("hostbud: can't load the saved tree order", error)
      if (gen !== generation) return
      order.value = emptyTreeOrder()
    }
    loaded.value = true
  }

  function saveSoon() {
    if (!loaded.value) return
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      putUIState('tree', order.value).catch((error) => {
        console.warn("hostbud: can't save the tree order", error)
      })
    }, 500)
  }
  watch(order, saveSoon, { deep: true })

  /** Append observed rows while retaining the user's existing order. */
  function sync() {
    for (const p of projectsStore.items) if (!order.value.projects.includes(p.id)) order.value.projects.push(p.id)

    const projection = projectTree(projectsStore.items, sessionsStore.list('host'), order.value)
    const reachable = machinesStore.byId('host')?.status === 'ok'
    const next: Record<string, string[]> = reachable ? {} : { ...order.value.sessions }
    const merge = (previous: string[], observed: string[]) => [...previous, ...observed.filter((name) => !previous.includes(name))]
    for (const group of projection.groups) {
      const id = group.project.id
      const observed = ordered(group.sessions, order.value.sessions[id] ?? [], (s) => s.name).map((s) => s.name)
      next[id] = reachable ? observed : merge(order.value.sessions[id] ?? [], observed)
    }
    const observedOther = ordered(projection.other, order.value.sessions.__other__ ?? [], (s) => s.name).map((s) => s.name)
    next.__other__ = reachable ? observedOther : merge(order.value.sessions.__other__ ?? [], observedOther)
    order.value.sessions = next
  }

  function reorderProjects(ids: string[]) {
    const allowed = new Set(projectsStore.items.map((p) => p.id))
    order.value.projects = ids.filter((id, i) => allowed.has(id) && ids.indexOf(id) === i)
    sync()
  }
  function reorderSessions(group: string, names: string[]) {
    const rows = group === '__other__' ? groups.value.other : groups.value.groups.find((g) => g.project.id === group)?.sessions ?? []
    const allowed = new Set(rows.map((s) => s.name))
    order.value.sessions[group] = names.filter((name, i) => allowed.has(name) && names.indexOf(name) === i)
    sync()
  }

  function reset() {
    generation++
    clearTimeout(timer)
    timer = undefined
    loaded.value = false
    order.value = emptyTreeOrder()
  }

  return { order, groups, loaded, load, sync, reorderProjects, reorderSessions, reset }
})
