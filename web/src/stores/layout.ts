import { defineStore } from 'pinia'
import { computed, ref, shallowRef, watch } from 'vue'
import { getUIState, putUIState } from '@/api/client'
import * as L from '@/lib/layout'
import { useToastsStore } from './toasts'

/** Saves wait this long after the last change. */
export const SAVE_DEBOUNCE_MS = 500

/**
 * The open tabs (lib/layout.ts), loaded on sign-in before any terminal
 * mounts and saved (debounced) on every change.
 */
export const useLayoutStore = defineStore('layout', () => {
  const layout = shallowRef<L.Layout>(L.emptyLayout())
  const loaded = ref(false)
  const toasts = useToastsStore()
  // Renames in flight from this UI ("machine/from" → to): the list may show
  // the new name before the rename request returns.
  const renames = new Map<string, string>()
  let timer: ReturnType<typeof setTimeout> | undefined
  let generation = 0

  const tabs = computed(() => layout.value.tabs)
  const activeTab = computed(() => L.activeTab(layout.value))
  const focused = computed(() => L.focusedPane(layout.value))

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
    loaded.value = true
  }

  /** Signed out: forget the layout without saving. */
  function reset() {
    generation++
    clearTimeout(timer)
    timer = undefined
    renames.clear()
    loaded.value = false
    layout.value = L.emptyLayout()
  }

  function save() {
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      putUIState('layout', layout.value).catch((e) => console.warn("hostbud: can't save the layout", e))
    }, SAVE_DEBOUNCE_MS)
  }

  // Changes made after loading are saved; the load itself isn't.
  watch(layout, () => {
    if (loaded.value) save()
  })

  /** Shows a session (existing tab, or a new one). False when at the limit. */
  function open(machine: string, session: string): boolean {
    const { layout: next, result } = L.openSession(layout.value, machine, session)
    if (result === 'full') {
      toasts.push({
        title: 'Too many terminals',
        message: `At most ${L.MAX_PANES} terminals can be open at once. Close a tab first.`,
      })
      return false
    }
    layout.value = next
    return true
  }

  function activate(tabId: string) {
    layout.value = L.activate(layout.value, tabId)
  }

  function closeTab(tabId: string) {
    layout.value = L.closeTab(layout.value, tabId)
  }

  /** Closes every view of a session without a notice (killed from this UI). */
  function closeSession(machine: string, session: string) {
    layout.value = L.removePanes(layout.value, (p) => p.machine === machine && p.session === session)
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
    focused,
    load,
    reset,
    open,
    activate,
    closeTab,
    closeSession,
    expectRename,
    renamed,
    renameAbandoned,
    syncSessions,
  }
})
