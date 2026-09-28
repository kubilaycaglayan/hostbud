<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import AuthView from '@/components/AuthView.vue'
import UnreachableView from '@/components/UnreachableView.vue'
import CreateSessionDialog from '@/components/CreateSessionDialog.vue'
import FileBrowserDialog from '@/components/FileBrowserDialog.vue'
import ProjectSessionDialog from '@/components/ProjectSessionDialog.vue'
import TreePanel from '@/components/TreePanel.vue'
import ShortcutsDialog from '@/components/ShortcutsDialog.vue'
import CommandPalette from '@/components/CommandPalette.vue'
import RemoveProjectDialog from '@/components/RemoveProjectDialog.vue'
import KillSessionDialog from '@/components/KillSessionDialog.vue'
import QueuePanel from '@/components/QueuePanel.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import HostBanner from '@/components/HostBanner.vue'
import TabBar from '@/components/TabBar.vue'
import TabView from '@/components/TabView.vue'
import IconButton from '@/components/IconButton.vue'
import { NEW_SESSION_FOR_SPLIT } from '@/components/layoutKeys'
import type { Project } from '@/api/types'
import { panelOrder, type SplitDir } from '@/lib/layout'
import { useMediaQuery, COMPACT_QUERY } from '@/lib/media'
import ToastRegion from '@/components/ToastRegion.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLayoutStore } from '@/stores/layout'
import { useLiveStore } from '@/stores/live'
import { useMachinesStore } from '@/stores/machines'
import { useTreeStore } from '@/stores/tree'
import { useThemeStore } from '@/stores/theme'
import { useWindowsStore } from '@/stores/windows'
import { useProjectsStore } from '@/stores/projects'
import { useToastsStore } from '@/stores/toasts'
import { useSessionsStore } from '@/stores/sessions'
import { FolderSearch, ListOrdered, PanelLeftClose, PanelLeftOpen, Search, SquareTerminal, UserRound } from 'lucide-vue-next'
import { isEditableTarget, isTerminalTarget, isTreeTarget, matchingShortcut, shortcutLabels, shortcutPlatform, shortcuts } from '@/lib/shortcuts'
import { projectTree, sessionKey, windowKey } from '@/lib/tree'
import { dispatchPaletteAction } from '@/lib/paletteActions'
import { buildPaletteItems } from '@/lib/palette'

const app = useAppStore()
const auth = useAuthStore()
const layout = useLayoutStore()
const live = useLiveStore()
const machines = useMachinesStore()
const tree = useTreeStore()
const theme = useThemeStore()
const windows = useWindowsStore()
const projects = useProjectsStore()
const toasts = useToastsStore()
const sessions = useSessionsStore()

// v1 has one machine: the host.
const MACHINE = 'host'
const host = computed(() => machines.byId(MACHINE))

const creating = ref(false)
const browsing = ref(false)
const queueOpen = ref(false) // V2-M1 Queue panel
const settingsOpen = ref(false) // V2-M2 Settings (the run cap)
const sessionProject = ref<Project | null>(null) // the project a New session here dialog is for
const killing = ref(false)
const drawerOpen = ref(false)
const accountOpen = ref(false)
const swipeStart = ref<{ x: number; y: number } | null>(null)
const target = ref('') // the session the kill confirmation is about
const removingProject = ref<Project | null>(null)
const shortcutsOpen = ref(false)
const paletteOpen = ref(false)
const paletteSplitDir = ref<SplitDir | null>(null)
let paletteActionSplitDir: SplitDir | null = null
let shortcutReturnFocus: HTMLElement | null = null
let paletteReturnFocus: HTMLElement | null = null
let focusTreeOnNextDrawerOpen = false
let focusTerminalOnNextDrawerClose = false
const themeChoices = [
  { mode: 'system', label: 'System' },
  { mode: 'dark', label: 'Dark' },
  { mode: 'dimmed', label: 'Dimmed' },
  { mode: 'solarized', label: 'Solarized' },
  { mode: 'light', label: 'Light' },
] as const
const coarsePointer = useMediaQuery('(pointer: coarse)')
let activeTreePanel: InstanceType<typeof TreePanel> | undefined
function setTreePanel(panel: unknown) {
  if (panel && typeof panel === 'object' && 'revealProject' in panel) activeTreePanel = panel as InstanceType<typeof TreePanel>
}

function shortcutHint(id: string) {
  const entry = shortcuts.find((item) => item.id === id)
  return entry ? shortcutLabels(entry, shortcutPlatform())[0] : undefined
}

const paletteItems = computed(() => {
  const projectGroups = tree.groups.groups
  const orderedSessions = [...projectGroups.flatMap((group) => group.sessions.map((session) => ({ session, group }))), ...tree.groups.other.map((session) => ({ session, group: undefined }))]
  return buildPaletteItems({
    sessions: orderedSessions.map(({ session, group }) => {
      const hidden = tree.order.hidden.sessions.includes(sessionKey(MACHINE, session.name)) || Boolean(group && tree.order.hidden.projects.includes(group.project.id))
      return { name: session.name, path: session.path, projectName: group?.project.name, hidden, windows: windows.bySession[sessionKey(MACHINE, session.name)] }
    }),
    projects: projectGroups.map(({ project }) => ({
      id: project.id,
      name: project.name,
      path: project.path,
      hidden: tree.order.hidden.projects.includes(project.id),
      pinned: tree.order.pinned.includes(project.id),
    })),
    showHidden: tree.order.showHidden,
    hasActiveTab: Boolean(layout.activeTab),
    selectingSplitTarget: Boolean(paletteSplitDir.value),
    shortcutHint,
  })
})
const removePreview = computed(() => {
  const project = removingProject.value
  if (!project) return { count: 0, destinations: 'Other sessions' }
  const affected = tree.groups.groups.find((group) => group.project.id === project.id)?.sessions ?? []
  const remaining = projects.items.filter((item) => item.id !== project.id)
  const projection = projectTree(remaining, affected, tree.order)
  const destinations = projection.groups
    .filter((group) => group.sessions.length > 0)
    .map((group) => `${group.project.name} (${group.sessions.length})`)
  if (projection.other.length) destinations.push(`Other sessions (${projection.other.length})`)
  return { count: affected.length, destinations: destinations.join(', ') || 'Other sessions' }
})
const removingProjectPath = computed(() => {
  const path = removingProject.value?.path ?? ''
  const homePath = host.value?.home ?? ''
  return homePath && (path === homePath || path.startsWith(homePath + '/')) ? '~' + path.slice(homePath.length) : path
})
function askKill(name: string) {
  target.value = name
  killing.value = true
}
function askRemoveProject(id: string) {
  const project = projects.items.find((item) => item.id === id)
  if (project) removingProject.value = project
}
function onProjectRemoved(id: string) {
  removingProject.value = null
  if (sessionProject.value?.id === id) {
    sessionProject.value = null
    toasts.push({ title: 'Project removed', message: 'The New session here dialog closed because its project was removed.', tone: 'info' })
  }
  projects.load(MACHINE).then(() => tree.sync()).catch((error) => console.warn("hostbud: can't refresh projects after removal", error))
}
function onKilled(name: string) {
  layout.closeSession(MACHINE, name)
}

/** Shows a session: its open tab, or a new one. */
function openSession(name: string) {
  drawerOpen.value = false
  if (layout.open(MACHINE, name)) app.showTerminal()
}

function openAtWindow(name: string, window: string, pane?: string) {
  drawerOpen.value = false
  if (!windows.openAt(MACHINE, name, window, pane)) return
  app.showTerminal()
}

/** A list row's "Open in split": beside the active tab's focused pane. */
function openInSplit(name: string, dir: SplitDir) {
  drawerOpen.value = false
  if (layout.splitFocused(dir, MACHINE, name)) app.showTerminal()
}

// "New session…" from a pane's split picker: the created session opens in a
// new pane beside it. The sidebar's New session opens a tab.
const splitTarget = ref<{ pane: string; dir: SplitDir } | null>(null)
provide(NEW_SESSION_FOR_SPLIT, (pane, dir) => {
  splitTarget.value = { pane, dir }
  creating.value = true
})
function newSession() {
  drawerOpen.value = false
  splitTarget.value = null
  creating.value = true
}
function newProjectSession(project: Project) {
  drawerOpen.value = false
  sessionProject.value = project
}

function browseFiles() {
  drawerOpen.value = false
  browsing.value = true
}

function openQueue() {
  drawerOpen.value = false
  queueOpen.value = true
}

function openSettings() {
  drawerOpen.value = false
  accountOpen.value = false
  settingsOpen.value = true
}

/** "Open session" in the Queue panel: the run's session in a tab (on a
 * phone, the single-terminal view). */
function openQueueSession(name: string) {
  queueOpen.value = false
  openSession(name)
  void nextTick(focusActiveTerminal)
}

function showTree() {
  if (compact.value && hasTabs.value) {
    drawerOpen.value = true
    return
  }
  if (!compact.value) app.toggleSidebar()
}

function onDrawerPointerDown(event: PointerEvent) {
  swipeStart.value = { x: event.clientX, y: event.clientY }
}
function onDrawerPointerUp(event: PointerEvent) {
  if (!swipeStart.value) return
  const dx = event.clientX - swipeStart.value.x
  const dy = event.clientY - swipeStart.value.y
  swipeStart.value = null
  if (dx <= -60 && Math.abs(dx) > Math.abs(dy)) drawerOpen.value = false
}
function onCreated(name: string) {
  browsing.value = false
  const t = splitTarget.value
  splitTarget.value = null
  if (t && layout.split(t.pane, t.dir, MACHINE, name)) app.showTerminal()
  else openSession(name)
  // Creating a session is an explicit request to start working in it. Wait
  // until its terminal is mounted before placing the cursor there.
  void nextTick(focusActiveTerminal)
}

function openShortcuts() {
  shortcutReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  shortcutsOpen.value = true
}

function closeShortcuts(open: boolean) {
  shortcutsOpen.value = open
  if (!open) void nextTick(() => shortcutReturnFocus?.focus())
}

function openPalette() {
  paletteReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  paletteSplitDir.value = null
  paletteOpen.value = true
}

function closePalette(open: boolean, restoreFocus = true) {
  paletteOpen.value = open
  if (!open && restoreFocus) void nextTick(() => paletteReturnFocus?.focus())
}

function revealTreeProject(id: string, rename = false) {
  if (compact.value && hasTabs.value) drawerOpen.value = true
  void nextTick(() => activeTreePanel?.revealProject(id, rename))
}

function revealTreeSession(name: string, rename = false) {
  if (compact.value && hasTabs.value) drawerOpen.value = true
  void nextTick(() => activeTreePanel?.revealSession(name, rename))
}

function collapseAll() {
  for (const project of projects.items.filter((item) => item.machineId === MACHINE)) tree.setCollapsed(project.id, true)
  tree.setCollapsed('__other__', true)
  for (const key of [...tree.order.expanded]) tree.setExpanded(key, false)
}

function expandAll() {
  for (const key of [...tree.order.collapsed]) tree.setCollapsed(key, false)
  for (const key of [...tree.order.expanded]) tree.setExpanded(key, false)
  const expanded = new Set<string>()
  for (const session of sessions.list(MACHINE)) {
    const key = sessionKey(MACHINE, session.name)
    expanded.add(key)
    const entry = windows.bySession[key]
    if (entry?.status === 'ok') {
      for (const item of entry.windows) {
        if (item.panes.length > 1) expanded.add(windowKey(MACHINE, session.name, item.id))
      }
    }
  }
  for (const key of expanded) tree.setExpanded(key, true)
  for (const session of sessions.list(MACHINE)) void windows.ensure(MACHINE, session.name)
}

function selectPaletteItem(id: string) {
  if (id === 'action:split-right' || id === 'action:split-down') {
    dispatchPaletteAction(id.slice('action:'.length), { ...paletteActionHandlers, split: (dir) => { paletteSplitDir.value = dir } })
    return
  }
  const splitDir = paletteSplitDir.value
  paletteSplitDir.value = null
  closePalette(false, false)
  if (id.startsWith('session:')) {
    const name = id.slice('session:'.length)
    if (splitDir) openInSplit(name, splitDir)
    else openSession(name)
    void nextTick(focusActiveTerminal)
    return
  }
  if (id.startsWith('window:')) {
    const encoded = id.slice('window:'.length)
    const separator = encoded.lastIndexOf(':')
    if (separator < 0) return
    openAtWindow(encoded.slice(0, separator), encoded.slice(separator + 1))
    void nextTick(focusActiveTerminal)
    return
  }
  if (id.startsWith('project:')) {
    revealTreeProject(id.slice('project:'.length))
    return
  }
  if (!id.startsWith('action:')) return
  paletteActionSplitDir = splitDir
  const actionId = id.slice('action:'.length)
  dispatchPaletteAction(actionId, paletteActionHandlers)
  const opensUi = ['new-session', 'browse-files', 'queue', 'settings', 'shortcuts', 'sign-out', 'close-tab', 'next-tab', 'previous-tab'].includes(actionId)
    || actionId.startsWith('new-project-session:')
    || actionId.startsWith('rename-project:')
    || actionId.startsWith('rename-session:')
    || actionId.startsWith('kill-session:')
    || actionId.startsWith('remove-project:')
  if (!opensUi) void nextTick(() => paletteReturnFocus?.focus())
  paletteActionSplitDir = null
}

const paletteActionHandlers = {
  queue: openQueue,
  settings: openSettings,
  newSession: () => {
    if (paletteActionSplitDir && layout.focused) {
      splitTarget.value = { pane: layout.focused.id, dir: paletteActionSplitDir }
      drawerOpen.value = false
      creating.value = true
    } else newSession()
  },
  newProjectSession: (id: string) => {
    const project = projects.items.find((item) => item.id === id)
    if (!project) return
    if (paletteActionSplitDir && layout.focused) {
      splitTarget.value = { pane: layout.focused.id, dir: paletteActionSplitDir }
      drawerOpen.value = false
      sessionProject.value = project
    } else newProjectSession(project)
  },
  browseFiles,
  renameProject: (id: string) => revealTreeProject(id, true),
  removeProject: askRemoveProject,
  renameSession: (name: string) => revealTreeSession(name, true),
  hideProject: (id: string) => tree.hideProject(id),
  unhideProject: (id: string) => tree.unhideProject(id),
  hideSession: (name: string) => tree.hideSession(MACHINE, name),
  unhideSession: (name: string) => tree.unhideSession(MACHINE, name),
  pinProject: (id: string) => tree.pinProject(id),
  unpinProject: (id: string) => tree.unpinProject(id),
  killSession: askKill,
  collapseAll,
  expandAll,
  setShowHidden: (show: boolean) => tree.setShowHidden(show),
  split: (dir: SplitDir) => { paletteSplitDir.value = dir },
  closeTab: () => { if (layout.activeTab) closeTab(layout.activeTab.id) },
  nextTab: () => { layout.cycleTab(1) },
  previousTab: () => { layout.cycleTab(-1) },
  setTheme: (mode: 'dark' | 'light' | 'solarized' | 'dimmed' | 'system') => { void theme.setMode(mode) },
  shortcuts: () => {
    shortcutReturnFocus = paletteReturnFocus
    shortcutsOpen.value = true
  },
  signOut: () => { void auth.logout() },
}

async function toggleTreeTerminalFocus() {
  if (isTreeTarget(document.activeElement)) {
    const closesCompactDrawer = compact.value && hasTabs.value
    if (closesCompactDrawer) focusTerminalOnNextDrawerClose = true
    drawerOpen.value = false
    app.showTerminal()
    if (closesCompactDrawer) return
    await nextTick()
    focusActiveTerminal()
    return
  }
  if (compact.value && hasTabs.value) {
    focusTreeOnNextDrawerOpen = true
    drawerOpen.value = true
    return
  }
  if (!compact.value && !app.sidebarOpen) app.toggleSidebar()
  await nextTick()
  focusTreeRow()
}

function focusTreeRow() {
  const rows = [...document.querySelectorAll<HTMLElement>('[role="treeitem"][data-tree-key]')]
  const selected = rows.find((row) => row.getAttribute('aria-selected') === 'true')
    ?? rows.find((row) => row.dataset.treeKey === (selectedSession.value ? `session:${selectedSession.value}` : ''))
  ;(selected ?? rows[0])?.focus()
}

function onDrawerOpenAutoFocus(event: Event) {
  if (!focusTreeOnNextDrawerOpen) return
  focusTreeOnNextDrawerOpen = false
  event.preventDefault()
  void nextTick(focusTreeRow)
}

function focusActiveTerminal() {
  document.querySelector<HTMLElement>('[data-focused="true"] .xterm-helper-textarea')?.focus()
}

function onDrawerCloseAutoFocus(event: Event) {
  if (!focusTerminalOnNextDrawerClose) return
  focusTerminalOnNextDrawerClose = false
  event.preventDefault()
  void nextTick(focusActiveTerminal)
}

function onShortcutKeydown(event: KeyboardEvent) {
  if (auth.status !== 'authenticated' || event.defaultPrevented || event.repeat) return
  const platform = shortcutPlatform()
  const global = matchingShortcut(event, platform, 'global')
  if (global) {
    if (global.id === 'help') openShortcuts()
    else if (global.id === 'next-tab') layout.cycleTab(1)
    else if (global.id === 'previous-tab') layout.cycleTab(-1)
    else if (global.id === 'last-tab') layout.toggleLastTab()
    else if (global.id === 'focus-tree-terminal') void toggleTreeTerminalFocus()
    else if (global.id === 'palette') openPalette()
    else return
    event.preventDefault()
    return
  }
  if (isTerminalTarget(event.target) || isEditableTarget(event.target)) return
  const outside = matchingShortcut(event, platform, 'outside-terminal')
  if (outside?.id === 'palette') {
    event.preventDefault()
    openPalette()
    return
  }
  if (outside?.id === 'help') {
    event.preventDefault()
    openShortcuts()
  }
}

const compact = useMediaQuery(COMPACT_QUERY)
const hasTabs = computed(() => layout.loaded && layout.tabs.length > 0)
// Tab panels stay in the order their tabs opened, so reordering tabs (M8 T4)
// never moves a mounted terminal's element: it keeps its focus and size.
let panelIds: string[] = []
const tabPanels = computed(() => {
  panelIds = panelOrder(panelIds, layout.tabs)
  const byId = new Map(layout.tabs.map((t) => [t.id, t]))
  return panelIds.map((id) => byId.get(id)!)
})
const sidebarExpanded = computed(() => compact.value && hasTabs.value ? drawerOpen.value : app.sidebarOpen)

function closeTab(id: string) {
  layout.closeTab(id)
  if (layout.tabs.length === 0) app.showList()
}

const selectedSession = computed(() => layout.focused?.session)

// Signed in: the saved layout first (before any terminal mounts), then live
// state; the server pushes every change.
watch(
  () => auth.status,
  async (s) => {
    if (s !== 'authenticated') {
      theme.signOut()
      live.stop()
      layout.reset()
      app.showList()
      return
    }
    await Promise.all([
      layout.load(),
      tree.load(),
      theme.load(),
      projects.load(MACHINE).catch((error) => console.warn("hostbud: can't load projects", error)),
    ])
    tree.sync()
    windows.restore()
    if (auth.status === 'authenticated') live.start()
  },
)

watch(
  () => projects.loaded && sessionProject.value !== null && !projects.items.some((item) => item.id === sessionProject.value?.id),
  (removed) => {
    if (!removed || !sessionProject.value) return
    sessionProject.value = null
    toasts.push({ title: 'Project removed', message: 'The New session here dialog closed because its project was removed.', tone: 'info' })
  },
)

// A reload right after a change still finds it saved.
const flushState = () => {
  layout.flush()
  tree.flush()
}
// Coming back to this tab or device picks up the order saved elsewhere.
const onVisibilityChange = () => {
  if (document.visibilityState === 'visible') void tree.refresh()
}
onMounted(() => {
  void auth.check()
  window.addEventListener('pagehide', flushState)
  document.addEventListener('visibilitychange', onVisibilityChange)
  window.addEventListener('keydown', onShortcutKeydown, true)
})
onUnmounted(() => {
  window.removeEventListener('pagehide', flushState)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('keydown', onShortcutKeydown, true)
  live.stop()
})
</script>

<template>
  <p
    v-if="auth.status === 'loading'"
    class="p-4 text-muted"
  >
    Loading…
  </p>
  <UnreachableView v-else-if="auth.status === 'unreachable'" />
  <main v-else-if="auth.status === 'server-error'" class="p-4" role="alert">
    {{ auth.serverError }} <button class="underline" type="button" @click="auth.check">Try again</button>
  </main>
  <AuthView v-else-if="auth.status === 'anonymous'" />
  <div
    v-else
    class="flex h-full flex-col"
  >
    <HostBanner :machine="host" />
    <header class="flex min-h-12 items-center gap-1 border-b border-border bg-surface px-2 sm:px-3">
      <IconButton
        v-if="!compact || hasTabs"
        :label="sidebarExpanded ? 'Hide sidebar' : 'Show sidebar'"
        aria-controls="sessions-sidebar"
        :aria-expanded="sidebarExpanded"
        @click="showTree"
      >
        <PanelLeftClose v-if="sidebarExpanded" :size="18" aria-hidden="true" />
        <PanelLeftOpen v-else :size="18" aria-hidden="true" />
      </IconButton>
      <h1 class="mx-1.5 font-bold text-accent">
        hostbud
      </h1>
      <!-- M8 T1: the app-wide actions follow the host name. -->
      <IconButton label="New session" @click="newSession">
        <SquareTerminal :size="18" aria-hidden="true" />
      </IconButton>
      <IconButton label="Browse files" @click="browseFiles">
        <FolderSearch :size="18" aria-hidden="true" />
      </IconButton>
      <IconButton label="Queue" @click="openQueue">
        <ListOrdered :size="18" aria-hidden="true" />
      </IconButton>
      <IconButton
        v-if="compact || coarsePointer"
        label="Command palette"
        @click="openPalette"
      >
        <Search :size="18" aria-hidden="true" />
      </IconButton>
      <div class="ml-auto min-w-0 text-sm text-muted">
        <details class="relative" :open="accountOpen" @toggle="accountOpen = ($event.target as HTMLDetailsElement).open" @keydown.escape="accountOpen = false">
          <!-- WebKit and the accessibility tree don't expose <summary> as a button everywhere. -->
          <summary role="button" aria-label="Account" :aria-expanded="accountOpen" class="flex min-h-11 cursor-pointer list-none items-center justify-center gap-1.5 rounded border border-border px-2 sm:px-3"><UserRound class="size-4 shrink-0" aria-hidden="true" /><span class="hidden sm:inline">Account</span></summary>
          <div class="absolute right-0 top-full z-30 mt-1 w-56 rounded border border-border bg-surface p-2 shadow-lg">
            <p class="truncate px-2 py-2" data-testid="account-email">{{ auth.email }}</p>
            <fieldset class="px-2 py-1" aria-label="Theme">
              <legend class="py-1 text-xs text-muted">Theme</legend>
              <label v-for="choice in themeChoices" :key="choice.mode" class="touch-target flex min-h-11 items-center gap-2">
                <input type="radio" name="theme" autocomplete="off" :value="choice.mode" :checked="theme.mode === choice.mode" class="theme-radio" @change="theme.setMode(choice.mode)">
                {{ choice.label }}
              </label>
            </fieldset>
            <button type="button" class="min-h-11 w-full rounded px-2 text-left" @click="openSettings">Settings</button>
            <button type="button" class="min-h-11 w-full rounded px-2 text-left" @click="auth.logout()">Sign out</button>
          </div>
        </details>
      </div>
    </header>
    <div class="flex min-h-0 flex-1">
      <aside
        v-if="!compact && app.sidebarOpen"
        id="sessions-sidebar"
        aria-label="Sessions"
        class="flex w-64 shrink-0 flex-col border-r border-border bg-surface p-2"
      >
        <TreePanel :ref="setTreePanel" :selected="selectedSession" :connection-state="live.state" @select="openSession" @select-window="openAtWindow" @split="openInSplit" @kill="askKill" @remove-project="askRemoveProject" @session-in-project="newProjectSession" @create="newSession" />
      </aside>
      <main v-if="compact && !hasTabs" id="sessions-sidebar" class="min-h-0 min-w-0 flex-1 overflow-y-auto bg-surface p-2">
        <TreePanel :ref="setTreePanel" :selected="selectedSession" :connection-state="live.state" @select="openSession" @select-window="openAtWindow" @split="openInSplit" @kill="askKill" @remove-project="askRemoveProject" @session-in-project="newProjectSession" @create="newSession" />
      </main>
      <main
        v-else
        class="flex min-h-0 min-w-0 flex-1 flex-col"
      >
        <template v-if="layout.loaded && layout.tabs.length > 0">
          <TabBar
            v-if="!compact"
            :tabs="layout.tabs"
            :active="layout.layout.activeTab"
            @activate="layout.activate"
            @close="closeTab"
            @reorder="layout.reorderTabs"
          />
          <!-- Inactive tabs stay mounted and attached (instant switching, and
               their scrollback keeps filling). -->
          <div
            v-for="t in tabPanels"
            v-show="t.id === layout.layout.activeTab"
            :id="`tabpanel-${t.id}`"
            :key="t.id"
            role="tabpanel"
            :aria-labelledby="`tab-${t.id}`"
            class="min-h-0 flex-1"
          >
            <TabView
              :tab="t"
              :active="t.id === layout.layout.activeTab"
              :narrow="compact"
              :tabs="layout.tabs"
              :active-tab="layout.layout.activeTab"
              @activate-tab="layout.activate"
              @close-tab="closeTab"
              @reorder-tabs="layout.reorderTabs"
            />
          </div>
        </template>
        <p
          v-else
          class="flex h-full flex-1 items-center justify-center text-muted"
        >
          Select a session to open a terminal.
        </p>
      </main>
    </div>
    <DialogRoot v-if="compact && hasTabs" v-model:open="drawerOpen">
      <DialogPortal>
        <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
        <DialogContent
          id="sessions-sidebar"
          class="fixed inset-y-0 left-0 z-50 flex w-[min(85vw,20rem)] flex-col border-r border-border bg-surface p-3 pt-[max(0.75rem,env(safe-area-inset-top))] pb-[max(0.75rem,env(safe-area-inset-bottom))] pl-[max(0.75rem,env(safe-area-inset-left))] text-fg shadow-xl"
          @pointerdown="onDrawerPointerDown"
          @pointerup="onDrawerPointerUp"
          @open-auto-focus="onDrawerOpenAutoFocus"
          @close-auto-focus="onDrawerCloseAutoFocus"
        >
          <div class="mb-2 flex items-center justify-between">
            <DialogTitle class="sr-only">Project tree</DialogTitle>
            <DialogClose as-child>
              <IconButton label="Hide sidebar">
                <PanelLeftClose :size="18" aria-hidden="true" />
              </IconButton>
            </DialogClose>
          </div>
          <DialogDescription class="sr-only">Choose a project or session.</DialogDescription>
          <TreePanel :ref="setTreePanel" :selected="selectedSession" :connection-state="live.state" @select="openSession" @select-window="openAtWindow" @split="openInSplit" @kill="askKill" @remove-project="askRemoveProject" @session-in-project="newProjectSession" @create="newSession" />
        </DialogContent>
      </DialogPortal>
    </DialogRoot>
    <CreateSessionDialog
      v-model:open="creating"
      :machine="MACHINE"
      :compact="compact"
      @created="onCreated"
    />
    <FileBrowserDialog
      v-model:open="browsing"
      :machine="MACHINE"
      :compact="compact"
      @created="onCreated"
    />
    <ProjectSessionDialog
      v-model:project="sessionProject"
      @created="onCreated"
    />
    <KillSessionDialog
      v-model:open="killing"
      :machine="MACHINE"
      :compact="compact"
      :session="target"
      @killed="onKilled"
    />
    <QueuePanel
      v-model:open="queueOpen"
      :compact="compact"
      :machine="MACHINE"
      @open-session="openQueueSession"
    />
    <SettingsDialog v-model:open="settingsOpen" :compact="compact" :machine="MACHINE" />
    <ShortcutsDialog :open="shortcutsOpen" @update:open="closeShortcuts" />
    <RemoveProjectDialog :project="removingProject" :session-count="removePreview.count" :destinations="removePreview.destinations" :display-path="removingProjectPath" @cancel="removingProject = null" @removed="onProjectRemoved" />
    <CommandPalette
      :open="paletteOpen"
      :items="paletteItems"
      :placeholder="paletteSplitDir ? 'Choose a session to split…' : undefined"
      @update:open="closePalette"
      @select="selectPaletteItem"
    />
  </div>
  <ToastRegion />
</template>
