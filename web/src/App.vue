<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import AuthView from '@/components/AuthView.vue'
import UnreachableView from '@/components/UnreachableView.vue'
import CreateSessionDialog from '@/components/CreateSessionDialog.vue'
import FileBrowserDialog from '@/components/FileBrowserDialog.vue'
import ServersDialog from '@/components/ServersDialog.vue'
import ProjectSessionDialog from '@/components/ProjectSessionDialog.vue'
import TreePanel from '@/components/TreePanel.vue'
import ShortcutsDialog from '@/components/ShortcutsDialog.vue'
import CommandPalette from '@/components/CommandPalette.vue'
import RemoveProjectDialog from '@/components/RemoveProjectDialog.vue'
import KillProjectSessionsDialog from '@/components/KillProjectSessionsDialog.vue'
import KillSessionDialog from '@/components/KillSessionDialog.vue'
import QueuePanel from '@/components/QueuePanel.vue'
import SettingsDialog from '@/components/SettingsDialog.vue'
import HostBanner from '@/components/HostBanner.vue'
import TabView from '@/components/TabView.vue'
import IconButton from '@/components/IconButton.vue'
import { NEW_SESSION_FOR_SPLIT } from '@/components/layoutKeys'
import type { Project, Session } from '@/api/types'
import { panelOrder, panesOf, type SplitDir } from '@/lib/layout'
import { useMediaQuery, COMPACT_QUERY } from '@/lib/media'
import ToastRegion from '@/components/ToastRegion.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLayoutStore } from '@/stores/layout'
import { useLiveStore } from '@/stores/live'
import { useMachinesStore } from '@/stores/machines'
import { useNotificationsStore } from '@/stores/notifications'
import { useQueuesStore } from '@/stores/queues'
import { queueTarget } from '@/lib/notifications'
import { useTreeStore } from '@/stores/tree'
import { useThemeStore } from '@/stores/theme'
import { useWindowsStore } from '@/stores/windows'
import { useProjectsStore } from '@/stores/projects'
import { useToastsStore } from '@/stores/toasts'
import { useSessionsStore } from '@/stores/sessions'
import { EyeOff, FolderSearch, ListOrdered, PanelLeftClose, PanelLeftOpen, Search, Server, SquareTerminal, UserRound } from 'lucide-vue-next'
import { isEditableTarget, isTerminalTarget, isTreeTarget, matchingShortcut, shortcutLabels, shortcutPlatform, shortcuts } from '@/lib/shortcuts'
import { parseSessionRef, projectTree, refOf, sessionKey, sessionMachine, sessionRef, visibleOpenSessionNames, windowKey } from '@/lib/tree'
import { dispatchPaletteAction } from '@/lib/paletteActions'
import { buildPaletteItems } from '@/lib/palette'
import { projectsApi, queuesApi } from '@/api/client'
import { directorySessionName, uniqueSessionName } from '@/lib/names'

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
const notifications = useNotificationsStore()
const queuesStore = useQueuesStore()
const hasRunningQueue = computed(() => queuesStore.queues.some((queue) => queue.status === 'running'))

// The host: banner, queue and settings. Sessions and projects may also be
// on servers added in the UI (V2-M13); the tree and dialogs pass session
// refs (the name on the host, "machine/name" elsewhere).
const MACHINE = 'host'
const host = computed(() => machines.byId(MACHINE))
// The focused pane's session ref.
const selectedSession = computed(() => layout.focused ? sessionRef(layout.focused.machine, layout.focused.session) : undefined)

const creating = ref(false)
const browsing = ref(false)
const queueOpen = ref(false) // V2-M1 Queue panel
const settingsOpen = ref(false) // V2-M2 Settings (the run cap)
const serversOpen = ref(false) // V2-M13 Servers (other SSH targets)
const sessionProject = ref<Project | null>(null) // the project a New session here dialog is for
const killing = ref(false)
const drawerOpen = ref(false)
const accountOpen = ref(false)
const swipeStart = ref<{ x: number; y: number } | null>(null)
const target = ref('') // the session the kill confirmation is about
const removingProject = ref<Project | null>(null)
const killingProject = ref<Project | null>(null) // the project whose sessions the kill-all confirmation is about
const killingProjectSessions = ref<string[]>([])
const shortcutsOpen = ref(false)
const paletteOpen = ref(false)
// Focus mode (T29): the header toggle arms it; while armed, the mouse leaving
// the viewport for FOCUS_DELAY_MS shows the overlay, and the mouse coming back
// (or a click/Escape on the overlay) hides it. Only the toggle disarms it.
// Phones, touch devices and the installed PWA don't offer it and keep it off.
const FOCUS_DELAY_MS = 2000
const focusArmed = ref(false)
const focusMode = ref(false)
const focusOverlay = ref<HTMLButtonElement | null>(null)
let focusTimer: ReturnType<typeof setTimeout> | null = null
let focusReturnTarget: HTMLElement | null = null
function clearFocusTimer() {
  if (focusTimer) clearTimeout(focusTimer)
  focusTimer = null
}
function toggleFocusArmed() {
  if (focusArmed.value || !focusAvailable.value) disarmFocusMode()
  else focusArmed.value = true
}
function onViewportMouseLeave() {
  if (!focusArmed.value || !focusAvailable.value || focusMode.value || focusTimer) return
  focusTimer = setTimeout(showFocusOverlay, FOCUS_DELAY_MS)
}
function onViewportMouseEnter() {
  clearFocusTimer()
  hideFocusOverlay()
}
function showFocusOverlay() {
  focusTimer = null
  if (!focusArmed.value || !focusAvailable.value) return
  focusReturnTarget = document.activeElement instanceof HTMLElement ? document.activeElement : null
  focusMode.value = true
  void nextTick(() => focusOverlay.value?.focus())
}
function disarmFocusMode() {
  clearFocusTimer()
  focusArmed.value = false
  hideFocusOverlay()
}
function hideFocusOverlay() {
  if (!focusMode.value) return
  focusMode.value = false
  void nextTick(() => focusReturnTarget?.focus())
}
const recentSessionNames = ref<string[]>([])
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
      const key = sessionKey(sessionMachine(session), session.name)
      const hidden = tree.order.hidden.sessions.includes(key) || Boolean(group && tree.order.hidden.projects.includes(group.project.id))
      const label = sessionMachine(session) === 'host' ? undefined : `${session.name} (${machines.label(sessionMachine(session))})`
      return { name: refOf(session), label, path: session.path, projectName: group?.project.name, hidden, windows: windows.bySession[key] }
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
  const homePath = machines.byId(removingProject.value?.machineId ?? MACHINE)?.home ?? ''
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
function askKillProjectSessions(id: string) {
  const group = tree.groups.groups.find((item) => item.project.id === id)
  if (!group?.sessions.length) return
  // Snapshot the names: the list shrinks as the kills land. A project's
  // sessions all run on its machine.
  killingProjectSessions.value = group.sessions.map((session) => session.name)
  killingProject.value = group.project
}
function onProjectRemoved(id: string) {
  removingProject.value = null
  if (sessionProject.value?.id === id) {
    sessionProject.value = null
    toasts.push({ title: 'Project removed', message: 'The New session here dialog closed because its project was removed.', tone: 'info' })
  }
  projects.load().then(() => tree.sync()).catch((error) => console.warn("hostbud: can't refresh projects after removal", error))
}
/** The Queue panel kills its queue's sessions on the queue's machine (a ref). */
function onQueueSessionKilled(ref: string) {
  const { machine, name } = parseSessionRef(ref)
  layout.closeSession(machine, name)
}
function onKilledOn(machine: string, name: string) {
  layout.closeSession(machine, name)
}
const killTarget = computed(() => parseSessionRef(target.value))

/** Shows a session (a ref): its open tab, or a new one. */
function openSession(ref: string) {
  const { machine, name } = parseSessionRef(ref)
  if (drawerOpen.value) focusTerminalOnNextDrawerClose = true
  drawerOpen.value = false
  if (layout.open(machine, name)) {
    app.showTerminal()
    // The newly active xterm may not be in the DOM until Vue applies the
    // layout update. Focus it after that update so typing works immediately.
    void nextTick(focusActiveTerminal)
  }
}

function openAtWindow(ref: string, window: string, pane?: string) {
  const { machine, name } = parseSessionRef(ref)
  drawerOpen.value = false
  if (!windows.openAt(machine, name, window, pane)) return
  app.showTerminal()
}

/** A list row's "Open in split": beside the active tab's focused pane. */
function openInSplit(ref: string, dir: SplitDir) {
  const { machine, name } = parseSessionRef(ref)
  drawerOpen.value = false
  if (layout.splitFocused(dir, machine, name)) app.showTerminal()
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
function createProjectSession(project: Project) {
  drawerOpen.value = false
  const name = uniqueSessionName(directorySessionName(project.path), sessions.list(project.machineId).map((session) => session.name))
  void projectsApi.createSession(project.id, { name }).then((result) => {
    onCreated(sessionRef(project.machineId, result.name))
  }).catch((error) => toasts.error("Couldn't create the session", error))
}

function browseFiles() {
  drawerOpen.value = false
  browsing.value = true
}

function openServers() {
  drawerOpen.value = false
  serversOpen.value = true
}

function openQueue() {
  drawerOpen.value = false
  queueOpen.value = true
}

/** V2-M3: a notification's item (a click, or a push that opened
 * /queues/<id>?item=<id>): the Queue panel on that item. */
function openQueueItem(queueId: string, itemId: string | null) {
  drawerOpen.value = false
  queuesStore.focusItem(queueId, itemId)
  queueOpen.value = true
}
notifications.onOpen(openQueueItem)

/** A session row's "Create queue": a new queue on the session's directory
 * (saved as a project first if it isn't one), shown in the Queue panel. */
async function createQueueFor(session: Session) {
  const machine = sessionMachine(session)
  try {
    const project = projects.byPath(session.path, machine) ?? await projectsApi.create(machine, session.path, '')
    projects.remember(project)
    tree.sync()
    if (!queuesStore.loaded) await queuesStore.load()
    // Unnamed: "Milestones" for the first queue, else the server's "Queue n".
    const queue = await queuesApi.create(project.id, queuesStore.queues.length ? '' : 'Milestones')
    queuesStore.put(queue)
    openQueueItem(queue.id, null)
  } catch (error) {
    toasts.error("Couldn't create the queue", error)
  }
}

/** A push notification opened the app on an item's path: show the item and
 * go back to the app's own URL. */
function openLaunchTarget() {
  const target = queueTarget(window.location.pathname + window.location.search)
  if (!target) return
  window.history.replaceState(null, '', '/')
  openQueueItem(target.queueId, target.itemId)
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
  // The sidebar marks the session: expand its project if it was collapsed.
  void activeTreePanel?.showSession(name)
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
function onCreated(ref: string) {
  const { machine, name } = parseSessionRef(ref)
  browsing.value = false
  const t = splitTarget.value
  splitTarget.value = null
  if (t && layout.split(t.pane, t.dir, machine, name)) app.showTerminal()
  else openSession(ref)
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
  for (const project of projects.items) tree.setCollapsed(project.id, true)
  tree.setCollapsed('__other__', true)
  for (const key of [...tree.order.expanded]) tree.setExpanded(key, false)
}

function expandAll() {
  for (const key of [...tree.order.collapsed]) tree.setCollapsed(key, false)
  for (const key of [...tree.order.expanded]) tree.setExpanded(key, false)
  const expanded = new Set<string>()
  for (const session of sessions.all) {
    const machine = sessionMachine(session)
    const key = sessionKey(machine, session.name)
    expanded.add(key)
    const entry = windows.bySession[key]
    if (entry?.status === 'ok') {
      for (const item of entry.windows) {
        if (item.panes.length > 1) expanded.add(windowKey(machine, session.name, item.id))
      }
    }
  }
  for (const key of expanded) tree.setExpanded(key, true)
  for (const session of sessions.all) void windows.ensure(sessionMachine(session), session.name)
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
  hideSession: (ref: string) => tree.hideSession(parseSessionRef(ref).machine, parseSessionRef(ref).name),
  unhideSession: (ref: string) => tree.unhideSession(parseSessionRef(ref).machine, parseSessionRef(ref).name),
  pinProject: (id: string) => tree.pinProject(id),
  unpinProject: (id: string) => tree.unpinProject(id),
  killSession: askKill,
  collapseAll,
  expandAll,
  setShowHidden: (show: boolean) => tree.setShowHidden(show),
  split: (dir: SplitDir) => { paletteSplitDir.value = dir },
  closeTab: () => { if (layout.activeTab) closeTab(layout.activeTab.id) },
  nextTab: () => { cycleVisibleOpenSession(1) },
  previousTab: () => { cycleVisibleOpenSession(-1) },
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

// Switching layouts or sessions all moves input to the newly active pane
// after Vue reveals its terminal view.
watch(() => layout.activeTab?.id, (id, previous) => {
  if (id && previous && auth.status === 'authenticated') void nextTick(focusActiveTerminal)
})
watch(() => selectedSession.value, (session) => {
  if (session) recentSessionNames.value = [session, ...recentSessionNames.value.filter((name) => name !== session)]
})

function onDrawerCloseAutoFocus(event: Event) {
  if (!focusTerminalOnNextDrawerClose) return
  focusTerminalOnNextDrawerClose = false
  event.preventDefault()
  void nextTick(focusActiveTerminal)
}

function onShortcutKeydown(event: KeyboardEvent) {
  if (auth.status !== 'authenticated' || focusMode.value || event.defaultPrevented || event.repeat) return
  const platform = shortcutPlatform()
  const global = matchingShortcut(event, platform, 'global')
  if (global) {
    if (global.id === 'help') openShortcuts()
    else if (global.id === 'next-tab') cycleVisibleOpenSession(1)
    else if (global.id === 'previous-tab') cycleVisibleOpenSession(-1)
    else if (global.id === 'last-tab') toggleLastSession()
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

function cycleVisibleOpenSession(offset: 1 | -1) {
  const openNames = new Set(layout.tabs.flatMap((tab) => panesOf(tab.root).map((pane) => sessionKey(pane.machine, pane.session))))
  const names = visibleOpenSessionNames(tree.groups.groups, tree.groups.other, tree.order, openNames)
  if (names.length < 2) return false
  const current = selectedSession.value
  const index = current ? names.indexOf(current) : -1
  const next = index < 0
    ? (offset > 0 ? names[0] : names[names.length - 1])
    : names[(index + offset + names.length) % names.length]
  return activateOpenSession(next)
}

function activateOpenSession(ref: string) {
  const { machine, name: session } = parseSessionRef(ref)
  const candidates = layout.tabs.flatMap((tab) => panesOf(tab.root)
    .filter((pane) => pane.machine === machine && pane.session === session)
    .map((pane) => ({ tab, pane })))
  const target = candidates.find(({ tab }) => tab.id === layout.layout.activeTab) ?? candidates[0]
  if (!target) return false
  layout.activate(target.tab.id)
  layout.focusPane(target.tab.id, target.pane.id)
  app.showTerminal()
  drawerOpen.value = false
  void nextTick(focusActiveTerminal)
  return true
}

function toggleLastSession() {
  const current = selectedSession.value
  const openNames = new Set(layout.tabs.flatMap((tab) => panesOf(tab.root).map((pane) => sessionKey(pane.machine, pane.session))))
  const target = recentSessionNames.value.find((ref) => ref !== current && openNames.has(sessionKey(parseSessionRef(ref).machine, parseSessionRef(ref).name)))
  return target ? activateOpenSession(target) : false
}

const compact = useMediaQuery(COMPACT_QUERY)
const standaloneDisplay = useMediaQuery('(display-mode: standalone)')
const focusAvailable = computed(
  () => !compact.value && !coarsePointer.value && !standaloneDisplay.value && (navigator as Navigator & { standalone?: boolean }).standalone !== true,
)
watch(focusAvailable, (available) => {
  if (!available) disarmFocusMode()
})
const hasTabs = computed(() => layout.loaded && layout.tabs.length > 0)
// Keep terminal panel elements in their layout order so updating the active
// layout never moves a mounted terminal in the DOM.
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

const focusInitialTerminal = ref(true)

// Signed in: the saved layout first (before any terminal mounts), then live
// state; the server pushes every change.
watch(
  () => auth.status,
  async (s) => {
    if (s !== 'authenticated') {
      disarmFocusMode()
      recentSessionNames.value = []
      theme.signOut()
      live.stop()
      layout.reset()
      app.showList()
      return
    }
    focusInitialTerminal.value = true
    await Promise.all([
      layout.load(),
      tree.load(),
      theme.load(),
      projects.load().catch((error) => console.warn("hostbud: can't load projects", error)),
    ])
    tree.sync()
    windows.restore()
    if (auth.status === 'authenticated') {
      live.start()
      openLaunchTarget()
    }
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
// V2-M3: a push notification clicked while hostbud is open (the service
// worker focused this window and names the item).
const onWorkerMessage = (e: MessageEvent) => {
  const data = e.data as { type?: unknown; path?: unknown } | null
  if (data?.type !== 'hostbud.open' || typeof data.path !== 'string') return
  const target = queueTarget(data.path)
  if (target) openQueueItem(target.queueId, target.itemId)
}
onMounted(() => {
  void auth.check()
  navigator.serviceWorker?.addEventListener('message', onWorkerMessage)
  window.addEventListener('pagehide', flushState)
  document.addEventListener('visibilitychange', onVisibilityChange)
  window.addEventListener('keydown', onShortcutKeydown, true)
  document.documentElement.addEventListener('mouseleave', onViewportMouseLeave)
  document.documentElement.addEventListener('mouseenter', onViewportMouseEnter)
})
onUnmounted(() => {
  navigator.serviceWorker?.removeEventListener('message', onWorkerMessage)
  window.removeEventListener('pagehide', flushState)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('keydown', onShortcutKeydown, true)
  document.documentElement.removeEventListener('mouseleave', onViewportMouseLeave)
  document.documentElement.removeEventListener('mouseenter', onViewportMouseEnter)
  clearFocusTimer()
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
    <header class="flex min-h-12 flex-wrap items-center gap-1 border-b border-border bg-surface px-2 sm:flex-nowrap sm:px-3">
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
      <IconButton label="Queue" :emphasized="hasRunningQueue" @click="openQueue">
        <ListOrdered :size="18" aria-hidden="true" />
      </IconButton>
      <IconButton label="Add server" @click="openServers">
        <Server :size="18" aria-hidden="true" />
      </IconButton>
      <IconButton v-if="focusAvailable" label="Focus mode" :emphasized="focusArmed" :aria-pressed="focusArmed" @click="toggleFocusArmed">
        <EyeOff :size="18" aria-hidden="true" />
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
        <TreePanel :ref="setTreePanel" :selected="selectedSession" :connection-state="live.state" @select="openSession" @select-window="openAtWindow" @split="openInSplit" @kill="askKill" @remove-project="askRemoveProject" @kill-project-sessions="askKillProjectSessions" @session-in-project="newProjectSession" @create-session-in-project="createProjectSession" @create-queue="createQueueFor" @create="newSession" />
      </aside>
      <main v-if="compact && !hasTabs" id="sessions-sidebar" class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-surface p-2">
        <TreePanel :ref="setTreePanel" :selected="selectedSession" :connection-state="live.state" @select="openSession" @select-window="openAtWindow" @split="openInSplit" @kill="askKill" @remove-project="askRemoveProject" @kill-project-sessions="askKillProjectSessions" @session-in-project="newProjectSession" @create-session-in-project="createProjectSession" @create-queue="createQueueFor" @create="newSession" />
      </main>
      <main
        v-else
        class="flex min-h-0 min-w-0 flex-1 flex-col"
      >
        <template v-if="layout.loaded && layout.tabs.length > 0">
          <!-- Open terminal layouts remain mounted; inactive views detach. -->
          <div
            v-for="t in tabPanels"
            v-show="t.id === layout.layout.activeTab"
            :id="`tabpanel-${t.id}`"
            :key="t.id"
            role="group"
            aria-label="Terminal workspace"
            class="min-h-0 flex-1"
          >
            <TabView
              :tab="t"
              :active="t.id === layout.layout.activeTab"
              :narrow="compact"
              :focus-initial-terminal="focusInitialTerminal"
              @initial-focus="focusInitialTerminal = false"
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
        <DialogOverlay class="fixed inset-0 z-20 bg-overlay" />
        <DialogContent
          id="sessions-sidebar"
          class="fixed inset-y-0 left-0 z-30 flex w-[min(85vw,20rem)] flex-col border-r border-border bg-surface p-3 pt-[max(0.75rem,env(safe-area-inset-top))] pb-[max(0.75rem,env(safe-area-inset-bottom))] pl-[max(0.75rem,env(safe-area-inset-left))] text-fg shadow-xl"
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
          <TreePanel :ref="setTreePanel" :selected="selectedSession" :connection-state="live.state" @select="openSession" @select-window="openAtWindow" @split="openInSplit" @kill="askKill" @remove-project="askRemoveProject" @kill-project-sessions="askKillProjectSessions" @session-in-project="newProjectSession" @create-session-in-project="createProjectSession" @create-queue="createQueueFor" @create="newSession" />
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
      :machine="killTarget.machine"
      :compact="compact"
      :session="killTarget.name"
      @killed="onKilledOn(killTarget.machine, $event)"
    />
    <QueuePanel
      v-model:open="queueOpen"
      :compact="compact"
      :machine="MACHINE"
      @open-session="openQueueSession"
      @killed="onQueueSessionKilled"
    />
    <ServersDialog v-model:open="serversOpen" :compact="compact" />
    <SettingsDialog v-model:open="settingsOpen" :compact="compact" :machine="MACHINE" />
    <ShortcutsDialog :open="shortcutsOpen" @update:open="closeShortcuts" />
    <KillProjectSessionsDialog :machine="killingProject?.machineId ?? MACHINE" :project="killingProject" :sessions="killingProjectSessions" :compact="compact" @killed="onKilledOn(killingProject?.machineId ?? MACHINE, $event)" @done="killingProject = null" @cancel="killingProject = null" />
    <RemoveProjectDialog :project="removingProject" :session-count="removePreview.count" :destinations="removePreview.destinations" :display-path="removingProjectPath" @cancel="removingProject = null" @removed="onProjectRemoved" />
    <CommandPalette
      :open="paletteOpen"
      :items="paletteItems"
      :placeholder="paletteSplitDir ? 'Choose a session to split…' : undefined"
      @update:open="closePalette"
      @select="selectPaletteItem"
    />
    <button
      v-if="focusMode && focusAvailable"
      ref="focusOverlay"
      type="button"
      aria-label="Exit focus mode"
      class="focus-screen fixed inset-0 z-[100] flex h-[100dvh] w-screen items-center justify-center focus-visible:outline-2 focus-visible:outline-offset-[-4px]"
      @click="hideFocusOverlay"
      @keydown.esc.stop.prevent="hideFocusOverlay"
    >
      <span class="text-2xl font-medium tracking-[0.3em]">focus</span>
    </button>
  </div>
  <ToastRegion />
</template>
