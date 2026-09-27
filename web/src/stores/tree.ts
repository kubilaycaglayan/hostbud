import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { getUIState, putUIState } from '@/api/client'
import { emptyTreeState, OTHER_GROUP, ordered, projectTree, validateTreeState, type TreeState } from '@/lib/tree'
import { useProjectsStore } from './projects'
import { useSessionsStore } from './sessions'
import { useMachinesStore } from './machines'
import { whenOnline } from './whenOnline'

const SAVE_RETRY_MS = 2000
type SessionRenamePosition = {
  group: string
  index: number
  hiddenKeys: string[]
  expandedKeys: string[]
}

export const useTreeStore = defineStore('tree', () => {
  const order = ref<TreeState>(emptyTreeState())
  const loaded = ref(false)
  const projectsStore = useProjectsStore()
  const sessionsStore = useSessionsStore()
  const machinesStore = useMachinesStore()
  let timer: ReturnType<typeof setTimeout> | undefined
  let cancelDeferred = () => {}
  let deferred = false // a save waits for the connection
  let generation = 0
  let quiet = false // applying a change that isn't the user's; don't save it

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

  /** Adopt the order another tab or device saved, unless this one has an
   * unsaved change (it wins, as the latest edit). */
  async function refresh() {
    if (!loaded.value || timer !== undefined || deferred) return
    const gen = generation
    const before = JSON.stringify(order.value)
    let saved: unknown
    try {
      saved = await getUIState('tree')
    } catch (error) {
      console.warn("hostbud: can't refresh the saved tree order", error)
      return
    }
    if (gen !== generation || timer !== undefined || deferred || JSON.stringify(order.value) !== before) return
    const valid = saved !== null ? validateTreeState(saved) : null
    if (!valid || JSON.stringify(valid) === before) return
    apply(valid)
    sync()
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
    if (keepalive) {
      putUIState('tree', order.value, { keepalive }).catch((error) => console.warn("hostbud: can't save the tree", error))
      return
    }
    sendWhenOnline(() => {
      const gen = generation
      putUIState('tree', order.value).catch((error) => {
        console.warn("hostbud: can't save the tree", error)
        // The server went away mid-save: try again shortly (persist waits
        // for the connection), unless a newer save is pending or the user
        // signed out.
        if (timer === undefined && gen === generation) timer = setTimeout(() => ((timer = undefined), persist()), SAVE_RETRY_MS)
      })
    })
  }

  /** Sends now, or once the events connection is back (whenOnline). */
  function sendWhenOnline(send: () => void) {
    cancelDeferred()
    let ran = false
    const cancel = whenOnline(() => {
      ran = true
      deferred = false
      cancelDeferred = () => {}
      send()
    })
    if (!ran) {
      deferred = true
      cancelDeferred = () => {
        deferred = false
        cancel()
      }
    }
  }

  function saveSoon() {
    if (!loaded.value || quiet) return
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      persist()
    }, 500)
  }
  watch(order, saveSoon, { deep: true, flush: 'sync' })

  /** Sends a pending save now while the page is being hidden or unloaded. */
  function flush() {
    if (timer === undefined && !deferred) return
    clearTimeout(timer)
    timer = undefined
    cancelDeferred()
    persist(true)
  }

  /** Append observed rows; prune saved keys only after authoritative loads. */
  function sync() {
    // Only the user's edits are saved. Rows appended or pruned here follow
    // from the live list, and every client derives them; saving them from a
    // tab that loaded earlier would overwrite the order another tab or
    // device saved since (a save sends the whole tree).
    const before = JSON.stringify(order.value)
    const state: TreeState = JSON.parse(before)
    for (const p of projectsStore.items) if (!state.projects.includes(p.id)) state.projects.push(p.id)

    const reachable = machinesStore.byId('host')?.status === 'ok'
    const projectIds = new Set(projectsStore.items.map((p) => p.id))
    const sessionList = sessionsStore.list('host')
    const sessionNames = new Set(sessionList.map((s) => s.name))
    const projection = projectTree(projectsStore.items, sessionList, state)
    if (projectsStore.loaded) {
      state.projects = state.projects.filter((id) => projectIds.has(id))
      state.pinned = state.pinned.filter((id) => projectIds.has(id))
      state.hidden.projects = state.hidden.projects.filter((id) => projectIds.has(id))
      state.collapsed = state.collapsed.filter((id) => id === OTHER_GROUP || projectIds.has(id))
    }
    const pinnedIds = new Set(state.pinned)
    state.projects = [
      ...state.projects.filter((id) => pinnedIds.has(id)),
      ...state.projects.filter((id) => !pinnedIds.has(id)),
    ]

    const savedGroups = projectsStore.loaded
      ? Object.fromEntries(Object.entries(state.sessions).filter(([id]) => id === OTHER_GROUP || projectIds.has(id)))
      : state.sessions
    const next: Record<string, string[]> = reachable && projectsStore.loaded ? {} : { ...savedGroups }
    const merge = (previous: string[], observed: string[]) => [...previous, ...observed.filter((name) => !previous.includes(name))]
    for (const group of projection.groups) {
      const id = group.project.id
      const observed = ordered(group.sessions, state.sessions[id] ?? [], (s) => s.name).map((s) => s.name)
      next[id] = reachable ? observed : merge(state.sessions[id] ?? [], observed)
    }
    const observedOther = ordered(projection.other, state.sessions.__other__ ?? [], (s) => s.name).map((s) => s.name)
    next.__other__ = reachable ? observedOther : merge(state.sessions.__other__ ?? [], observedOther)
    state.sessions = next

    if (reachable) {
      state.hidden.sessions = state.hidden.sessions.filter((key) => {
        const [machine, name] = key.split('/')
        return machine !== 'host' || sessionNames.has(name)
      })
      state.expanded = state.expanded.filter((key) => {
        const [machine, name] = key.split('/')
        return machine !== 'host' || sessionNames.has(name)
      })
    }
    if (JSON.stringify(state) !== before) apply(state)
  }

  function apply(state: TreeState) {
    quiet = true
    try { order.value = state } finally { quiet = false }
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
  function reorderProjectSection(ids: string[], pinned: boolean) {
    const pinnedSet = new Set(order.value.pinned)
    const sectionIds = order.value.projects.filter((id) => pinnedSet.has(id) === pinned)
    const visibleIds = new Set(ids)
    let next = 0
    const reordered = sectionIds.map((id) => visibleIds.has(id) ? ids[next++] : id)
    reordered.push(...ids.slice(next))
    const pinnedRows = pinned ? reordered : order.value.projects.filter((id) => pinnedSet.has(id))
    const unpinnedRows = pinned ? order.value.projects.filter((id) => !pinnedSet.has(id)) : reordered
    order.value.projects = [...pinnedRows, ...unpinnedRows]
    sync()
  }
  function pinProject(id: string) {
    if (order.value.pinned.includes(id)) return
    const rest = order.value.projects.filter((item) => item !== id)
    const pinned = [...order.value.pinned, id]
    const pinnedSet = new Set(pinned)
    order.value.pinned = pinned
    order.value.projects = [...rest.filter((item) => pinnedSet.has(item)), id, ...rest.filter((item) => !pinnedSet.has(item))]
  }
  function unpinProject(id: string) {
    if (!order.value.pinned.includes(id)) return
    order.value.pinned = order.value.pinned.filter((item) => item !== id)
    order.value.projects = [
      ...order.value.projects.filter((item) => order.value.pinned.includes(item)),
      ...order.value.projects.filter((item) => !order.value.pinned.includes(item) && item !== id),
      id,
    ]
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
  function renameSession(machine: string, from: string, to: string, position?: SessionRenamePosition) {
    const oldKey = `${machine}/${from}`
    const newKey = `${machine}/${to}`
    const replacePrefix = (key: string) => key === oldKey || key.startsWith(oldKey + '/')
      ? newKey + key.slice(oldKey.length)
      : key
    const sessions = Object.fromEntries(Object.entries(order.value.sessions).map(([group, names]) => {
      const renamed = names.map((name) => name === from ? to : name)
      // The inventory can report the new name before the rename request
      // resolves. A sync in that window may prune `from` and append `to`;
      // restore the position captured when editing began in that case.
      if (position?.group === group && !names.includes(from) && names.includes(to)) {
        const without = renamed.filter((name) => name !== to)
        without.splice(Math.min(position.index, without.length), 0, to)
        return [group, without]
      }
      return [group, renamed]
    }))
    const hidden = order.value.hidden.sessions.map(replacePrefix)
    const expanded = order.value.expanded.map(replacePrefix)
    if (position) {
      for (const key of position.hiddenKeys.map(replacePrefix)) if (!hidden.includes(key)) hidden.push(key)
      for (const key of position.expandedKeys.map(replacePrefix)) if (!expanded.includes(key)) expanded.push(key)
    }
    const next: TreeState = {
      ...order.value,
      sessions,
      hidden: { ...order.value.hidden, sessions: hidden },
      expanded,
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
    cancelDeferred()
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

  return { order, groups, loaded, load, refresh, sync, flush, reorderProjects, reorderProjectSection, pinProject, unpinProject, reorderSessions, renameSession, hideProject, unhideProject, hideSession, unhideSession, setShowHidden, toggleShowHidden, hiddenCount, setCollapsed, toggleCollapsed, setExpanded, reset }
})
