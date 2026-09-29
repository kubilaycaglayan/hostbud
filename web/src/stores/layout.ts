import { defineStore } from 'pinia'
import { computed, ref, shallowRef, watch } from 'vue'
import { getUIState, putUIState } from '@/api/client'
import * as L from '@/lib/layout'
import { useToastsStore } from './toasts'
import { whenOnline } from './whenOnline'

/** Saves wait this long after the last change. */
export const SAVE_DEBOUNCE_MS = 500
/** A failed save is tried again after this long. */
export const SAVE_RETRY_MS = 5_000

/**
 * The open tabs (lib/layout.ts), loaded on sign-in before any terminal
 * mounts and saved (debounced) on every change.
 */
export const useLayoutStore = defineStore('layout', () => {
  const layout = shallowRef<L.Layout>(L.emptyLayout())
  const recentTabIds = ref<string[]>([])
  const loaded = ref(false)
  const toasts = useToastsStore()
  // Renames in flight from this UI ("machine/from" → to): the list may show
  // the new name before the rename request returns.
  const renames = new Map<string, string>()
  let timer: ReturnType<typeof setTimeout> | undefined
  let cancelDeferred = () => {}
  let deferred = false // a save waits for the connection
  let generation = 0

  const tabs = computed(() => layout.value.tabs)
  const activeTab = computed(() => L.activeTab(layout.value))
  const focused = computed(() => L.focusedPane(layout.value))

  function rememberActive() {
    const id = L.activeTab(layout.value)?.id
    if (id) recentTabIds.value = [id, ...recentTabIds.value.filter((candidate) => candidate !== id)]
  }

  function pruneRecentTabs() {
    recentTabIds.value = recentTabIds.value.filter((id) => tabs.value.some((tab) => tab.id === id))
    rememberActive()
  }

  /** Loads the saved layout. Invalid data falls back to an empty layout. */
  async function load() {
    const gen = ++generation
    let next = L.emptyLayout()
    try {
      const stored = await getUIState('layout')
      if (stored !== null) {
        const valid = L.validateLayout(stored)
        if (valid) next = valid
        else console.warn('hostbud: ignoring an invalid saved layout')
      }
    } catch (e) {
      console.warn("hostbud: can't load the saved layout", e)
    }
    if (gen !== generation) return // signed out meanwhile
    layout.value = next
    recentTabIds.value = []
    loaded.value = true
  }

  /** Signed out: forget the layout without saving. */
  function reset() {
    generation++
    clearTimeout(timer)
    timer = undefined
    cancelDeferred()
    renames.clear()
    loaded.value = false
    layout.value = L.emptyLayout()
    recentTabIds.value = []
  }

  function send(keepalive = false) {
    if (!keepalive) {
      // Wait for the events connection: a save during an app restart would
      // only fail (whenOnline).
      cancelDeferred()
      let ran = false
      const cancel = whenOnline(() => {
        ran = true
        cancelDeferred = () => {}
        deferred = false
        put(false)
      })
      if (!ran) {
        deferred = true
        cancelDeferred = () => ((deferred = false), cancel())
      }
      return
    }
    put(true)
  }

  function put(keepalive: boolean) {
    const gen = generation
    putUIState('layout', layout.value, { keepalive }).catch((e) => {
      console.warn("hostbud: can't save the layout", e)
      // hostbud restarting, say: try again unless a newer save is pending
      // or the user signed out.
      if (timer === undefined && gen === generation) timer = setTimeout(() => ((timer = undefined), send()), SAVE_RETRY_MS)
    })
  }

  function save() {
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      send()
    }, SAVE_DEBOUNCE_MS)
  }

  /** Sends a pending save now (the page is going away). */
  function flush() {
    if (timer === undefined && !deferred) return
    clearTimeout(timer)
    timer = undefined
    cancelDeferred()
    send(true)
  }

  // Changes made after loading are saved; the load itself isn't (a sync
  // watcher sees the layout change while `loaded` is still false).
  watch(
    layout,
    () => {
      if (loaded.value) save()
    },
    { flush: 'sync' },
  )

  /** Shows a session (existing tab, or a new one). */
  function open(machine: string, session: string): boolean {
    const { layout: next } = L.openSession(layout.value, machine, session)
    layout.value = next
    rememberActive()
    return true
  }

  /** Opens a session in a new pane beside `paneId` (M3 T8). False when a
   * limit refused it (with a notice). */
  function split(paneId: string, dir: L.SplitDir, machine: string, session: string): boolean {
    const { layout: next, result } = L.splitPane(layout.value, paneId, dir, machine, session)
    if (result === 'tab-full') {
      toasts.push({ title: 'Too many panes', message: `A tab holds at most ${L.MAX_TAB_PANES} panes. Open it in a new tab instead.` })
      return false
    }
    if (result === 'missing') return false
    layout.value = next
    rememberActive()
    return true
  }

  /** Splits the active tab's focused pane, or opens a tab if none is open. */
  function splitFocused(dir: L.SplitDir, machine: string, session: string): boolean {
    const pane = focused.value
    return pane ? split(pane.id, dir, machine, session) : open(machine, session)
  }

  function closePane(paneId: string) {
    layout.value = L.closePane(layout.value, paneId)
    pruneRecentTabs()
  }

  function focusPane(tabId: string, paneId: string) {
    layout.value = L.focusPane(layout.value, tabId, paneId)
  }

  function cycleFocus(tabId: string) {
    layout.value = L.cycleFocus(layout.value, tabId)
  }

  function setSizes(splitId: string, sizes: number[]) {
    layout.value = L.setSizes(layout.value, splitId, sizes)
  }

  function activate(tabId: string) {
    layout.value = L.activate(layout.value, tabId)
    rememberActive()
  }

  /** Custom tab order from a drag (M8 T4); saved like any other change. */
  function reorderTabs(ids: readonly string[]) {
    layout.value = L.reorderTabs(layout.value, ids)
  }

  function cycleTab(offset: number) {
    const current = activeTab.value
    if (!current || tabs.value.length < 2) return false
    const index = tabs.value.findIndex((tab) => tab.id === current.id)
    const next = tabs.value[(index + offset + tabs.value.length) % tabs.value.length]
    activate(next.id)
    return true
  }

  function toggleLastTab() {
    const current = activeTab.value?.id
    const valid = recentTabIds.value.filter((id) => tabs.value.some((tab) => tab.id === id))
    recentTabIds.value = valid
    const target = valid.find((id) => id !== current)
    if (!target) return false
    activate(target)
    return true
  }

  function closeTab(tabId: string) {
    layout.value = L.closeTab(layout.value, tabId)
    recentTabIds.value = recentTabIds.value.filter((id) => id !== tabId)
    rememberActive()
  }

  /** Closes every view of a session without a notice (killed from this UI). */
  function closeSession(machine: string, session: string) {
    layout.value = L.removePanes(layout.value, (p) => p.machine === machine && p.session === session)
    pruneRecentTabs()
  }

  function expectRename(machine: string, from: string, to: string) {
    renames.set(`${machine}/${from}`, to)
  }

  function renamed(machine: string, from: string, to: string) {
    renames.delete(`${machine}/${from}`)
    layout.value = L.renameSession(layout.value, machine, from, to)
  }

  function renameAbandoned(machine: string, from: string) {
    renames.delete(`${machine}/${from}`)
  }

  /**
   * A fresh session list for a machine: panes of sessions that no longer
   * exist close, with one notice. Only call it with an authoritative list
   * (the machine is reachable and was listed).
   */
  function syncSessions(machine: string, live: ReadonlySet<string>) {
    if (!loaded.value) return
    let next = layout.value
    for (const p of L.missingPanes(next, machine, live)) {
      const to = renames.get(`${machine}/${p.session}`)
      if (to !== undefined && live.has(to)) next = L.renameSession(next, machine, p.session, to)
    }
    const gone = [...new Set(L.missingPanes(next, machine, live).map((p) => p.session))]
    if (gone.length === 0) {
      if (next !== layout.value) layout.value = next
      return
    }
    layout.value = L.removePanes(next, (p) => p.machine === machine && gone.includes(p.session))
    pruneRecentTabs()
    toasts.push(
      gone.length === 1
        ? { title: `Session ${gone[0]} ended`, message: 'Its terminal was closed.' }
        : { title: 'Sessions ended', message: `Closed the terminals of ${gone.join(', ')}.` },
    )
  }

  return {
    layout,
    loaded,
    tabs,
    activeTab,
    recentTabIds,
    focused,
    load,
    reset,
    flush,
    open,
    split,
    splitFocused,
    closePane,
    focusPane,
    cycleFocus,
    setSizes,
    activate,
    reorderTabs,
    cycleTab,
    toggleLastTab,
    closeTab,
    closeSession,
    expectRename,
    renamed,
    renameAbandoned,
    syncSessions,
  }
})
