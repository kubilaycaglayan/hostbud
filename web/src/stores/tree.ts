import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { getUIState, putUIState } from '@/api/client'
import { emptyTreeState, OTHER_GROUP, ordered, projectTree, validateTreeState, type TreeState } from '@/lib/tree'
import { useProjectsStore } from './projects'
import { useSessionsStore } from './sessions'
import { useMachinesStore } from './machines'

export const useTreeStore = defineStore('tree', () => {
  const order = ref<TreeState>(emptyTreeState())
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
      const valid = saved !== null ? validateTreeState(saved) : null
      if (saved !== null && !valid) console.warn('hostbud: ignoring an invalid saved tree')
      order.value = valid || emptyTreeState()
    } catch (error) {
      console.warn("hostbud: can't load the saved tree order", error)
      if (gen !== generation) return
      order.value = emptyTreeState()
    }
    loaded.value = true
  }

  function persist(keepalive = false) {
    sync()
    clearTimeout(timer)
    timer = undefined
    const serialized = JSON.stringify(order.value)
    if (new TextEncoder().encode(serialized).byteLength > 60 * 1024) {
      console.warn('hostbud: saved tree exceeds 60 KiB; keeping the last saved value')
      return
    }
    putUIState('tree', order.value, { keepalive }).catch((error) => {
      console.warn("hostbud: can't save the tree", error)
    })
  }

  function saveSoon() {
    if (!loaded.value) return
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      persist()
    }, 500)
  }
  watch(order, saveSoon, { deep: true, flush: 'sync' })

  /** Sends a pending save now while the page is being hidden or unloaded. */
  function flush() {
    if (timer === undefined) return
    clearTimeout(timer)
    timer = undefined
    persist(true)
  }

  /** Append observed rows; prune saved keys only after authoritative loads. */
  function sync() {
    for (const p of projectsStore.items) if (!order.value.projects.includes(p.id)) order.value.projects.push(p.id)

    const reachable = machinesStore.byId('host')?.status === 'ok'
    const projectIds = new Set(projectsStore.items.map((p) => p.id))
    const sessionList = sessionsStore.list('host')
    const sessionNames = new Set(sessionList.map((s) => s.name))
    const projection = projectTree(projectsStore.items, sessionList, order.value)
    if (projectsStore.loaded) {
      order.value.projects = order.value.projects.filter((id) => projectIds.has(id))
      order.value.pinned = order.value.pinned.filter((id) => projectIds.has(id))
      order.value.hidden.projects = order.value.hidden.projects.filter((id) => projectIds.has(id))
      order.value.collapsed = order.value.collapsed.filter((id) => id === OTHER_GROUP || projectIds.has(id))
    }

    const savedGroups = projectsStore.loaded
      ? Object.fromEntries(Object.entries(order.value.sessions).filter(([id]) => id === OTHER_GROUP || projectIds.has(id)))
      : order.value.sessions
    const next: Record<string, string[]> = reachable && projectsStore.loaded ? {} : { ...savedGroups }
    const merge = (previous: string[], observed: string[]) => [...previous, ...observed.filter((name) => !previous.includes(name))]
    for (const group of projection.groups) {
      const id = group.project.id
      const observed = ordered(group.sessions, order.value.sessions[id] ?? [], (s) => s.name).map((s) => s.name)
      next[id] = reachable ? observed : merge(order.value.sessions[id] ?? [], observed)
    }
    const observedOther = ordered(projection.other, order.value.sessions.__other__ ?? [], (s) => s.name).map((s) => s.name)
    next.__other__ = reachable ? observedOther : merge(order.value.sessions.__other__ ?? [], observedOther)
    order.value.sessions = next

    if (reachable) {
      order.value.hidden.sessions = order.value.hidden.sessions.filter((key) => {
        const [machine, name] = key.split('/')
        return machine !== 'host' || sessionNames.has(name)
      })
      order.value.expanded = order.value.expanded.filter((key) => {
        const [machine, name] = key.split('/')
        return machine !== 'host' || sessionNames.has(name)
      })
    }
  }

  function reorderProjects(ids: string[]) {
    const allowed = new Set(projectsStore.items.map((p) => p.id))
    const requested = ids.filter((id, i) => allowed.has(id) && ids.indexOf(id) === i)
    if (!order.value.showHidden) {
      const hidden = new Set(order.value.hidden.projects)
      let index = 0
      order.value.projects = order.value.projects.map((id) => hidden.has(id) ? id : requested[index++] ?? id)
      order.value.projects.push(...requested.slice(index))
    } else order.value.projects = requested
    sync()
  }
  function reorderSessions(group: string, names: string[]) {
    const rows = group === '__other__' ? groups.value.other : groups.value.groups.find((g) => g.project.id === group)?.sessions ?? []
    const allowed = new Set(rows.map((s) => s.name))
    const requested = names.filter((name, i) => allowed.has(name) && names.indexOf(name) === i)
    if (!order.value.showHidden) {
      const hidden = new Set(order.value.hidden.sessions.filter((key) => key.startsWith('host/')).map((key) => key.slice('host/'.length)))
      const previous = order.value.sessions[group] ?? []
      let index = 0
      order.value.sessions[group] = previous.map((name) => hidden.has(name) ? name : requested[index++] ?? name)
      order.value.sessions[group].push(...requested.slice(index))
    } else order.value.sessions[group] = requested
    sync()
  }

  /** Re-key a session's saved position and expansion state as one tree update. */
  function renameSession(machine: string, from: string, to: string) {
    const oldKey = `${machine}/${from}`
    const newKey = `${machine}/${to}`
    const replacePrefix = (key: string) => key === oldKey || key.startsWith(oldKey + '/')
      ? newKey + key.slice(oldKey.length)
      : key
    const next: TreeState = {
      ...order.value,
      sessions: Object.fromEntries(Object.entries(order.value.sessions).map(([group, names]) => [
        group, names.map((name) => name === from ? to : name),
      ])),
      hidden: { ...order.value.hidden, sessions: order.value.hidden.sessions.map(replacePrefix) },
      expanded: order.value.expanded.map(replacePrefix),
    }
    order.value = next
  }

  function hideProject(id: string) {
    if (!order.value.hidden.projects.includes(id)) order.value.hidden.projects = [...order.value.hidden.projects, id]
  }
  function unhideProject(id: string) {
    order.value.hidden.projects = order.value.hidden.projects.filter((item) => item !== id)
  }
  function hideSession(machine: string, name: string) {
    const key = `${machine}/${name}`
    if (!order.value.hidden.sessions.includes(key)) order.value.hidden.sessions = [...order.value.hidden.sessions, key]
  }
  function unhideSession(machine: string, name: string) {
    const key = `${machine}/${name}`
    order.value.hidden.sessions = order.value.hidden.sessions.filter((item) => item !== key)
  }
  function setShowHidden(show: boolean) { order.value.showHidden = show }
  function toggleShowHidden() { setShowHidden(!order.value.showHidden) }
  const hiddenCount = computed(() => order.value.hidden.projects.length + order.value.hidden.sessions.length)

  function reset() {
    generation++
    clearTimeout(timer)
    timer = undefined
    loaded.value = false
    order.value = emptyTreeState()
  }

  function setCollapsed(key: string, collapsed: boolean) {
    order.value.collapsed = collapsed
      ? [...new Set([...order.value.collapsed, key])]
      : order.value.collapsed.filter((item) => item !== key)
  }

  function toggleCollapsed(key: string) {
    setCollapsed(key, !order.value.collapsed.includes(key))
  }

  function setExpanded(key: string, expanded: boolean) {
    order.value.expanded = expanded
      ? [...new Set([...order.value.expanded, key])]
      : order.value.expanded.filter((item) => item !== key)
  }

  return { order, groups, loaded, load, sync, flush, reorderProjects, reorderSessions, renameSession, hideProject, unhideProject, hideSession, unhideSession, setShowHidden, toggleShowHidden, hiddenCount, setCollapsed, toggleCollapsed, setExpanded, reset }
})
