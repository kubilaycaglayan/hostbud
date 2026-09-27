import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, windowsApi } from '@/api/client'
import type { ServerEvent, Session, TmuxWindow, TmuxWindows } from '@/api/types'
import { sessionKey, windowKey } from '@/lib/tree'
import { useMachinesStore } from './machines'
import { useLayoutStore } from './layout'
import { useSessionsStore } from './sessions'
import { useToastsStore } from './toasts'
import { useTreeStore } from './tree'

export interface WindowsEntry {
  status: 'idle' | 'loading' | 'ok' | 'error'
  windows: TmuxWindow[]
  truncated: boolean
  error?: unknown
}

const emptyEntry = (): WindowsEntry => ({ status: 'idle', windows: [], truncated: false })
const REFRESH_DEBOUNCE_MS = 300

/** Lazily loads the target's tmux windows only for expanded sessions. */
export const useWindowsStore = defineStore('windows', () => {
  const bySession = ref<Record<string, WindowsEntry>>({})
  const sessions = useSessionsStore()
  const machines = useMachinesStore()
  const tree = useTreeStore()
  const toasts = useToastsStore()
  const observed = new Map<string, Pick<Session, 'windows' | 'activity'>>()
  const generations = new Map<string, number>()
  const inFlight = new Map<string, number>()
  const pending = new Set<string>()
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  let nextGeneration = 0

  function isExpanded(key: string) { return tree.order.expanded.includes(key) }
  function sessionExists(machine: string, name: string) {
    return sessions.list(machine).some((session) => session.name === name)
  }
  function current(key: string, generation: number, machine: string, name: string) {
    return generations.get(key) === generation && isExpanded(key) && sessionExists(machine, name)
  }
  function invalidate(key: string) { generations.set(key, ++nextGeneration) }

  function pruneWindowKeys(machine: string, name: string, windows: TmuxWindow[]) {
    const prefix = sessionKey(machine, name) + '/'
    const ids = new Set(windows.map((window) => window.id))
    tree.order.expanded = tree.order.expanded.filter((key) => {
      if (!key.startsWith(prefix)) return true
      return ids.has(key.slice(prefix.length))
    })
  }

  function accept(machine: string, name: string, result: TmuxWindows) {
    const key = sessionKey(machine, name)
    pruneWindowKeys(machine, name, result.windows)
    bySession.value[key] = { status: 'ok', windows: result.windows, truncated: result.truncated }
  }

  async function fetchList(machine: string, name: string) {
    const key = sessionKey(machine, name)
    const generation = ++nextGeneration
    generations.set(key, generation)
    inFlight.set(key, generation)
    bySession.value[key] = { ...(bySession.value[key] ?? emptyEntry()), status: 'loading', error: undefined }
    try {
      const result = await windowsApi.list(machine, name)
      if (current(key, generation, machine, name)) accept(machine, name, result)
      else if (generations.get(key) === generation) delete bySession.value[key]
    } catch (error) {
      if (current(key, generation, machine, name)) bySession.value[key] = { ...emptyEntry(), status: 'error', error }
    } finally {
      if (inFlight.get(key) === generation) {
        inFlight.delete(key)
        if (pending.delete(key) && isExpanded(key)) schedule(machine, name)
      }
    }
  }

  function schedule(machine: string, name: string) {
    const key = sessionKey(machine, name)
    if (!isExpanded(key)) return
    if (inFlight.has(key)) { pending.add(key); return }
    const old = timers.get(key)
    if (old) clearTimeout(old)
    timers.set(key, setTimeout(() => {
      timers.delete(key)
      void fetchList(machine, name)
    }, REFRESH_DEBOUNCE_MS))
  }

  function ensure(machine: string, name: string) {
    const key = sessionKey(machine, name)
    if (!isExpanded(key)) return Promise.resolve()
    const entry = bySession.value[key]
    if (entry?.status === 'ok' || entry?.status === 'loading') return Promise.resolve()
    return fetchList(machine, name)
  }

  function refresh(machine: string, name: string) {
    const key = sessionKey(machine, name)
    if (!isExpanded(key)) return
    if (inFlight.has(key)) { pending.add(key); return }
    const timer = timers.get(key)
    if (timer) clearTimeout(timer)
    timers.delete(key)
    void fetchList(machine, name)
  }

  function toggleSession(machine: string, name: string) {
    const key = sessionKey(machine, name)
    const expanded = !isExpanded(key)
    tree.setExpanded(key, expanded)
    if (expanded) refresh(machine, name)
    else {
      invalidate(key)
      const timer = timers.get(key)
      if (timer) clearTimeout(timer)
      timers.delete(key)
      pending.delete(key)
      if (inFlight.has(key)) delete bySession.value[key]
    }
  }

  function toggleWindow(machine: string, name: string, id: string) {
    const key = windowKey(machine, name, id)
    tree.setExpanded(key, !isExpanded(key))
  }

  /** Re-fetch only expanded sessions whose authoritative summary changed. */
  function applyEvent(event: ServerEvent) {
    if (event.type !== 'snapshot' && event.type !== 'sessions.changed') return
    const lists = event.type === 'snapshot' ? event.sessions : { [event.machine]: event.payload.sessions }
    for (const [machine, list] of Object.entries(lists)) {
      const reachable = (event.type === 'snapshot'
        ? event.machines.find((item) => item.id === machine)?.status
        : machines.byId(machine)?.status) === 'ok'
      if (!reachable) continue
      const liveNames = new Set(list.map((session) => session.name))
      for (const session of list) {
        const key = sessionKey(machine, session.name)
        const previous = observed.get(key)
        if (isExpanded(key) && (!previous || previous.windows !== session.windows || previous.activity !== session.activity)) {
          schedule(machine, session.name)
        }
        observed.set(key, { windows: session.windows, activity: session.activity })
      }
      const candidates = new Set([
        ...observed.keys(),
        ...Object.keys(bySession.value),
        ...tree.order.expanded.map((key) => key.split('/').slice(0, 2).join('/')),
      ])
      for (const key of candidates) {
        if (!key.startsWith(machine + '/') || liveNames.has(key.slice(machine.length + 1))) continue
        observed.delete(key)
        invalidate(key)
        const timer = timers.get(key)
        if (timer) clearTimeout(timer)
        timers.delete(key)
        pending.delete(key)
        delete bySession.value[key]
        const prefix = key + '/'
        tree.order.expanded = tree.order.expanded.filter((expandedKey) => expandedKey !== key && !expandedKey.startsWith(prefix))
      }
    }
  }

  function restore() {
    for (const key of tree.order.expanded) {
      const [machine, name] = key.split('/')
      if (!sessionExists(machine, name)) continue
      void ensure(machine, name)
    }
  }

  async function select(machine: string, name: string, window: string, pane?: string) {
    try {
      accept(machine, name, await windowsApi.select(machine, name, window, pane))
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        toasts.push({ title: 'Window no longer available', message: 'The window or pane ended. The list has been refreshed.' })
        refresh(machine, name)
      } else toasts.error('Could not select window', error)
    }
  }

  /** Opens the session using the established layout rules, then selects on tmux. */
  function openAt(machine: string, name: string, window: string, pane?: string) {
    if (!useLayoutStore().open(machine, name)) return false
    void select(machine, name, window, pane)
    return true
  }

  function reset() {
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    pending.clear()
    inFlight.clear()
    observed.clear()
    generations.clear()
    bySession.value = {}
  }

  return { bySession, ensure, refresh, toggleSession, toggleWindow, applyEvent, restore, select, openAt, reset }
})
