<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { ChevronRight, Folder, GripVertical, Plus } from 'lucide-vue-next'
import type { Project, Session } from '@/api/types'
import { projectsApi, sessionsApi } from '@/api/client'
import { useMachinesStore } from '@/stores/machines'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'
import { describeError } from '@/stores/toasts'
import { normalizeSessionName, projectNameError, sessionNameError } from '@/lib/names'
import { useLayoutStore } from '@/stores/layout'
import type { SplitDir } from '@/lib/layout'
import type { ProjectGroup } from '@/lib/tree'
import { canReorderProjectSections, parseSessionRef, refOf, sessionKey, sessionMachine, sessionRef, windowKey } from '@/lib/tree'
import { SECTION_COLORS, type ProjectSection, type SectionColor } from '@/lib/tree'
import { matchingTreeShortcut } from '@/lib/shortcuts'
import SessionList from './SessionList.vue'
import ProjectTreeRow from './ProjectTreeRow.vue'

// selected and the session events are session refs (refOf): the name on the
// host, "machine/name" on another server (V2-M13).
const props = defineProps<{ selected?: string }>()
const emit = defineEmits<{
  select: [name: string]
  selectWindow: [name: string, window: string, pane?: string]
  split: [name: string, dir: SplitDir]
  kill: [name: string]
  createQueue: [session: Session]
  removeProject: [id: string]
  killProjectSessions: [id: string]
  sessionInProject: [project: Project]
  createSessionInProject: [project: Project]
  create: []
}>()
const tree = useTreeStore()
const projects = useProjectsStore()
const machines = useMachinesStore()
const windows = useWindowsStore()
const sessions = useSessionsStore()
const layout = useLayoutStore()
const root = ref<HTMLElement>()
const showHiddenButton = ref<HTMLButtonElement>()
const focusedKey = ref('')
const deferredFocusKey = ref('')
const busySession = ref('')
const error = ref<{ message: string; hint?: string } | null>(null)
const editingKey = ref('')
const editError = ref('')
const sectionEditor = ref<{ id?: string; name: string; color: SectionColor } | null>(null)
const sectionError = ref('')
const sectionDialogOpen = computed({ get: () => sectionEditor.value !== null, set: (open: boolean) => { if (!open) sectionEditor.value = null } })
const projectMenuId = ref('')
const longPressedProjectId = ref('')
const projectPointerStart = ref<{ x: number; y: number; id: string } | null>(null)
let projectLongPressTimer: ReturnType<typeof setTimeout> | undefined
function hiddenKey(session: Session) { return sessionKey(sessionMachine(session), session.name) }
const projectRows = computed(() => tree.groups.groups
    .filter((group) => tree.order.showHidden || !tree.order.hidden.projects.includes(group.project.id))
    .map((group) => ({
      ...group,
      id: group.project.id,
      sessions: tree.order.showHidden ? group.sessions : group.sessions.filter((session) => !tree.order.hidden.sessions.includes(hiddenKey(session))),
    })))
const selectedProjectId = computed(() => props.selected
  ? tree.groups.groups.find((group) => group.sessions.some((session) => refOf(session) === props.selected))?.project.id
  : undefined)
const selectedSectionId = computed(() => selectedProjectId.value ? tree.order.projectSections[selectedProjectId.value] : undefined)
const pinnedProjectRows = computed({
  get: () => projectRows.value.filter((group) => tree.order.pinned.includes(group.project.id) && !tree.order.projectSections[group.project.id]),
  set: (groups: (ProjectGroup & { id: string })[]) => tree.reorderProjectSection(groups.map((group) => group.project.id), true),
})
const unpinnedProjectRows = computed({
  get: () => projectRows.value.filter((group) => !tree.order.pinned.includes(group.project.id) && !tree.order.projectSections[group.project.id]),
  set: (groups: (ProjectGroup & { id: string })[]) => tree.reorderProjectSection(groups.map((group) => group.project.id), false),
})
const projectSectionRows = computed({
  get: () => tree.order.sections.map((section) => ({
    id: section.id,
    section,
    pinned: projectRows.value.filter((group) => tree.order.pinned.includes(group.project.id) && tree.order.projectSections[group.project.id] === section.id),
    unpinned: projectRows.value.filter((group) => !tree.order.pinned.includes(group.project.id) && tree.order.projectSections[group.project.id] === section.id),
  })),
  set: (rows: { id: string }[]) => tree.reorderProjectSections(rows.map((row) => row.id)),
})
const sectionColorValues: Record<SectionColor, string> = { red: 'var(--hb-section-red)', green: 'var(--hb-section-green)', blue: 'var(--hb-section-blue)', yellow: 'var(--hb-section-yellow)', orange: 'var(--hb-section-orange)', purple: 'var(--hb-section-purple)' }
function sectionStyle(section: ProjectSection) { return { '--section-accent': sectionColorValues[section.color] } }
function sectionCollapsed(id: string) { return tree.order.collapsedSections.includes(id) }
function sectionHasSelectedSession(id: string) { return sectionCollapsed(id) && selectedSectionId.value === id }
const otherRows = computed(() => tree.order.showHidden
  ? tree.groups.other
  : tree.groups.other.filter((session) => !tree.order.hidden.sessions.includes(hiddenKey(session))))
const totalRows = computed(() => tree.groups.groups.length + tree.groups.groups.reduce((count, group) => count + group.sessions.length, 0) + tree.groups.other.length)
const home = computed(() => machines.byId('host')?.home ?? '')
const visibleKeys = computed(() => {
  const keys: string[] = []
  const appendSessions = (rows: Session[]) => {
    for (const session of rows) {
      const sessionID = hiddenKey(session)
      keys.push('session:' + refOf(session))
      if (!tree.order.expanded.includes(sessionID)) continue
      for (const window of windows.bySession[sessionID]?.windows ?? []) {
        const winKey = windowKey(sessionMachine(session), session.name, window.id)
        keys.push('window:' + winKey)
        if (window.panes.length > 1 && tree.order.expanded.includes(winKey)) {
          keys.push(...window.panes.map((pane) => 'pane:' + winKey + '/' + pane.id))
        }
      }
    }
  }
  for (const group of projectRows.value) {
    const sectionId = tree.order.projectSections[group.project.id]
    if (sectionId && tree.order.collapsedSections.includes(sectionId)) continue
    keys.push('project:' + group.project.id)
    if (!tree.order.collapsed.includes(group.project.id)) {
      appendSessions(group.sessions)
    }
  }
  if (otherRows.value.length) {
    keys.push('other')
    if (!tree.order.collapsed.includes('__other__')) appendSessions(otherRows.value)
  }
  return keys
})
const activeFocusKey = computed(() => visibleKeys.value.includes(focusedKey.value) ? focusedKey.value : (visibleKeys.value[0] ?? ''))
onUnmounted(endProjectLongPress)

function canMoveProject(event: { from: HTMLElement; to: HTMLElement }) {
  return canReorderProjectSections(event.from.dataset.projectSection ?? '', event.to.dataset.projectSection ?? '') &&
    event.from.dataset.projectSectionId === event.to.dataset.projectSectionId
}
function reorderSectionBucket(sectionId: string, rows: (ProjectGroup & { id: string })[], pinned: boolean) {
  const current = tree.order.projects.filter((id) => tree.order.pinned.includes(id) === pinned)
  const members = new Set(current.filter((id) => tree.order.projectSections[id] === sectionId))
  const requested = rows.map((row) => row.project.id)
  let index = 0
  const next = current.map((id) => members.has(id) ? requested[index++] ?? id : id)
  tree.reorderProjectSection(next, pinned)
}

function editSection(section?: ProjectSection) {
  sectionError.value = ''
  sectionEditor.value = section ? { id: section.id, name: section.name, color: section.color } : { name: '', color: 'blue' }
}
function saveSection() {
  if (!sectionEditor.value) return
  try {
    if (sectionEditor.value.id) tree.updateProjectSection(sectionEditor.value.id, sectionEditor.value.name, sectionEditor.value.color)
    else tree.createProjectSection(sectionEditor.value.name, sectionEditor.value.color)
    sectionEditor.value = null
  } catch (e) { sectionError.value = e instanceof Error ? e.message : 'Could not save section.' }
}

watch(visibleKeys, (keys) => {
  if (deferredFocusKey.value && keys.includes(deferredFocusKey.value)) {
    const pending = deferredFocusKey.value
    deferredFocusKey.value = ''
    focusKey(pending)
  }
  if (focusedKey.value && !keys.includes(focusedKey.value) && deferredFocusKey.value !== focusedKey.value) {
    let fallback = ''
    if (focusedKey.value.startsWith('pane:')) {
      fallback = 'window:' + focusedKey.value.slice('pane:'.length).split('/').slice(0, 3).join('/')
    }
    if (focusedKey.value.startsWith('window:')) {
      const [machine, name] = focusedKey.value.slice('window:'.length).split('/')
      fallback = `session:${sessionRef(machine, name)}`
    }
    if (!keys.includes(fallback)) fallback = keys[0] ?? ''
    focusedKey.value = fallback ?? ''
    if (fallback) focusKey(fallback)
  }
}, { flush: 'post' })

function focusKey(key: string) {
  focusedKey.value = key
  deferredFocusKey.value = key
  void nextTick(() => {
    const target = [...(root.value?.querySelectorAll<HTMLElement>('[data-tree-key]') ?? [])].find((item) => item.dataset.treeKey === key)
    if (target) {
      if (deferredFocusKey.value === key) deferredFocusKey.value = ''
      target.focus()
    }
  })
}

async function revealProject(id: string, rename = false) {
  tree.setShowHidden(true)
  tree.setCollapsed(id, false)
  await nextTick()
  focusKey('project:' + id)
  if (rename) startRename('project:' + id)
}

async function revealSession(name: string, rename = false) {
  tree.setShowHidden(true)
  const group = tree.groups.groups.find((candidate) => candidate.sessions.some((session) => refOf(session) === name))
  tree.setCollapsed(group?.project.id ?? '__other__', false)
  await nextTick()
  focusKey('session:' + name)
  if (rename) startRename('session:' + name)
}

/** Shows a session that just opened elsewhere (a queue item's Open
 * session): its project expands and its selected row scrolls into view, but
 * focus stays on the terminal. */
async function showSession(name: string) {
  const group = tree.groups.groups.find((candidate) => candidate.sessions.some((session) => refOf(session) === name))
  const inOther = tree.groups.other.some((session) => refOf(session) === name)
  if (!group && !inOther) return
  const ref = parseSessionRef(name)
  if (tree.order.hidden.sessions.includes(sessionKey(ref.machine, ref.name)) || (group && tree.order.hidden.projects.includes(group.project.id))) tree.setShowHidden(true)
  tree.setCollapsed(group?.project.id ?? '__other__', false)
  await nextTick()
  const row = [...(root.value?.querySelectorAll<HTMLElement>('[data-tree-key]') ?? [])].find((item) => item.dataset.treeKey === `session:${name}`)
  row?.scrollIntoView?.({ block: 'nearest' })
}

defineExpose({ revealProject, revealSession, showSession })

function startProjectLongPress(event: PointerEvent, id: string) {
  if (event.pointerType !== 'touch') return
  clearTimeout(projectLongPressTimer)
  projectPointerStart.value = { x: event.clientX, y: event.clientY, id }
  projectLongPressTimer = setTimeout(() => {
    longPressedProjectId.value = id
    projectMenuId.value = id
  }, 500)
}

function moveProjectLongPress(event: PointerEvent) {
  const start = projectPointerStart.value
  if (start && Math.hypot(event.clientX - start.x, event.clientY - start.y) > 10) endProjectLongPress()
}

function endProjectLongPress() {
  clearTimeout(projectLongPressTimer)
  projectLongPressTimer = undefined
  projectPointerStart.value = null
}

function projectHeaderClick(id: string) {
  if (longPressedProjectId.value === id) {
    longPressedProjectId.value = ''
    return
  }
  focusKey('project:' + id)
  tree.toggleCollapsed(id)
}

function hideProject(id: string) {
  projectMenuId.value = ''
  const key = 'project:' + id
  const index = visibleKeys.value.indexOf(key)
  const hidden = tree.order.hidden.projects.includes(id)
  if (hidden) tree.unhideProject(id)
  else tree.hideProject(id)
  if (hidden) focusKey(key)
  else focusAfterHide(index)
}

async function toggleProjectPin(id: string) {
  // Pinning moves the row to another v-for section. Clear the controlled menu
  // before that move so its portal cannot remain open on the remounted row.
  projectMenuId.value = ''
  await nextTick()
  if (tree.order.pinned.includes(id)) tree.unpinProject(id)
  else tree.pinProject(id)
}

function hideSession(name: string, hidden: boolean) {
  const key = 'session:' + name
  const index = visibleKeys.value.indexOf(key)
  const ref = parseSessionRef(name)
  if (hidden) tree.unhideSession(ref.machine, ref.name)
  else tree.hideSession(ref.machine, ref.name)
  if (hidden) focusKey(key)
  else focusAfterHide(index)
}

function focusAfterHide(index: number) {
  void nextTick(() => {
    const keys = visibleKeys.value
    const neighbor = keys[Math.min(Math.max(index, 0), keys.length - 1)]
    if (neighbor) focusKey(neighbor)
    else showHiddenButton.value?.focus()
  })
}

function startRename(key: string) {
  editingKey.value = key
  editError.value = ''
}

function cancelRename(key: string) {
  if (editingKey.value !== key) return
  editingKey.value = ''
  editError.value = ''
  focusKey(key)
}

async function renameSession(fromRef: string, raw: string) {
  const { machine, name: from } = parseSessionRef(fromRef)
  const to = normalizeSessionName(raw)
  const toRef = sessionRef(machine, to)
  const invalid = sessionNameError(to, true)
  if (invalid) { editError.value = invalid; throw new Error(invalid) }
  if (to === from) { cancelRename('session:' + fromRef); return }
  // Refused before the optimistic rename: re-keying the tree onto a live
  // name would merge both rows, and the rollback couldn't tell them apart.
  if (sessions.list(machine).some((session) => session.name === to)) {
    editError.value = `a session named "${to}" already exists. Pick another name.`
    throw new Error(editError.value)
  }
  const position = tree.groups.groups
    .map((group) => ({ group: group.project.id, index: group.sessions.findIndex((session) => refOf(session) === fromRef) }))
    .concat([{ group: '__other__', index: tree.groups.other.findIndex((session) => refOf(session) === fromRef) }])
    .find((entry) => entry.index >= 0)
  const renamedKeyPrefix = `${machine}/${from}`
  const renameState = position ? {
    ...position,
    hiddenKeys: tree.order.hidden.sessions.filter((key) => key === renamedKeyPrefix || key.startsWith(renamedKeyPrefix + '/')),
    expandedKeys: tree.order.expanded.filter((key) => key === renamedKeyPrefix || key.startsWith(renamedKeyPrefix + '/')),
  } : undefined
  layout.expectRename(machine, from, to)
  sessions.beginRename(machine, from, to)
  tree.renameSession(machine, from, to, renameState)
  // The row keeps its place under the new key. Left on the old key, the
  // vanished-row fallback would pull focus from the selected terminal.
  if (focusedKey.value === 'session:' + fromRef) focusedKey.value = 'session:' + toRef
  if (deferredFocusKey.value === 'session:' + fromRef) deferredFocusKey.value = ''
  if (windows.bySession[`${machine}/${from}`]) {
    windows.bySession[`${machine}/${to}`] = windows.bySession[`${machine}/${from}`]
    delete windows.bySession[`${machine}/${from}`]
  }
  editingKey.value = ''
  editError.value = ''
  emit('select', fromRef)
  try {
    await sessionsApi.rename(machine, from, to)
    sessions.finishRename(machine, from, to, true)
    layout.renamed(machine, from, to)
  } catch (e) {
    sessions.finishRename(machine, from, to, false)
    tree.renameSession(machine, to, from)
    if (focusedKey.value === 'session:' + toRef) focusedKey.value = 'session:' + fromRef
    if (windows.bySession[`${machine}/${to}`]) {
      windows.bySession[`${machine}/${from}`] = windows.bySession[`${machine}/${to}`]
      delete windows.bySession[`${machine}/${to}`]
    }
    layout.renameAbandoned(machine, from)
    editingKey.value = 'session:' + fromRef
    editError.value = [describeError(e).message, describeError(e).hint].filter(Boolean).join(' ')
    throw e
  }
}

async function renameProject(id: string, raw: string) {
  const name = raw.trim()
  const invalid = projectNameError(name)
  if (invalid) {
    editError.value = invalid
    throw new Error(editError.value)
  }
  try {
    projects.remember(await projectsApi.rename(id, name))
    editingKey.value = ''
    editError.value = ''
    focusKey('project:' + id)
  } catch (e) {
    const detail = describeError(e)
    editError.value = [detail.message, detail.hint].filter(Boolean).join(' ')
    throw e
  }
}

function focusIn(event: FocusEvent) {
  const item = (event.target as HTMLElement).closest<HTMLElement>('[role="treeitem"][data-tree-key]')
  if (item?.dataset.treeKey) focusedKey.value = item.dataset.treeKey
}

function visibleItems(): HTMLElement[] {
  return [...(root.value?.querySelectorAll<HTMLElement>('[role="treeitem"][data-tree-key]') ?? [])]
}

function onTreeKeydown(event: KeyboardEvent) {
  const item = (event.target as HTMLElement).closest<HTMLElement>('[role="treeitem"][data-tree-key]')
  if (!item) return
  // Keep native button keys intact for row actions. Tree navigation belongs
  // to the treeitem itself; Tab is the only key we manage from its controls.
  if (event.target !== item && event.key !== 'Tab') return
  const key = item.dataset.treeKey ?? ''
  const kind = item.dataset.treeKind ?? ''
  const name = item.dataset.treeSession ?? (kind === 'session' ? key.slice('session:'.length) : '')
  const windowID = item.dataset.treeWindow ?? ''
  const items = visibleItems()
  const index = items.indexOf(item)
  if (event.key === 'Delete' && kind === 'project') {
    event.preventDefault()
    emit('removeProject', key.slice('project:'.length))
    return
  }
  if (matchingTreeShortcut(event, 'tree-rename') && (kind === 'project' || kind === 'session')) {
    event.preventDefault()
    startRename(key)
    return
  }
  if (matchingTreeShortcut(event, 'tree-hide') && (kind === 'project' || kind === 'session')) {
    event.preventDefault()
    if (kind === 'project') hideProject(key.slice('project:'.length))
    else {
      const name = key.slice('session:'.length)
      const ref = parseSessionRef(name)
      hideSession(name, tree.order.hidden.sessions.includes(sessionKey(ref.machine, ref.name)))
    }
    return
  }
  if (matchingTreeShortcut(event, 'tree-pin') && kind === 'project') {
    event.preventDefault()
    const id = key.slice('project:'.length)
    toggleProjectPin(id)
    focusKey(key)
    return
  }
  if (event.type === 'dblclick' && (kind === 'project' || kind === 'session') && window.matchMedia('(pointer: fine)').matches) {
    startRename(key)
    return
  }
  const moveFocus = (to: number) => {
    if (to >= 0 && to < items.length) focusKey(items[to].dataset.treeKey ?? '')
  }

  if (matchingTreeShortcut(event, 'tree-new-session')) {
    if (kind === 'other') emit('create')
    else {
      const projectId = kind === 'project' ? key.slice('project:'.length) : item.dataset.treeGroup
      const project = projectId ? projects.items.find((candidate) => candidate.id === projectId) : undefined
      if (project) emit('sessionInProject', project)
      else if (kind === 'session') emit('create')
    }
    if (kind === 'project' || kind === 'session' || kind === 'other') event.preventDefault()
    return
  }

  if (event.key === 'Tab') {
    const actions = [...item.querySelectorAll<HTMLButtonElement>('button:not([disabled])')]
    const current = actions.indexOf(event.target as HTMLButtonElement)
    if (current < 0) {
      if (!event.shiftKey && actions[0]) {
        event.preventDefault()
        actions[0].focus()
      }
      return
    }
    const to = current + (event.shiftKey ? -1 : 1)
    if (actions[to]) {
      event.preventDefault()
      actions[to].focus()
    } else if (event.shiftKey) {
      event.preventDefault()
      item.focus()
    } else if (items[index + 1]) {
      event.preventDefault()
      moveFocus(index + 1)
    }
    return
  }
  if (matchingTreeShortcut(event, 'tree-reorder-up') || matchingTreeShortcut(event, 'tree-reorder-down')) {
    event.preventDefault()
    const offset = matchingTreeShortcut(event, 'tree-reorder-up') ? -1 : 1
    if (kind === 'project') {
      const id = key.slice('project:'.length)
      const pinned = tree.order.pinned.includes(id)
      const sectionId = tree.order.projectSections[id]
      const bucket = projectSectionRows.value.find((entry) => entry.section.id === sectionId)
      const rows = bucket ? (pinned ? bucket.pinned : bucket.unpinned) : (pinned ? pinnedProjectRows.value : unpinnedProjectRows.value)
      const ids = rows.map((group) => group.project.id)
      const from = ids.indexOf(id)
      const to = from + offset
      if (to >= 0 && to < ids.length) {
        ids.splice(to, 0, ids.splice(from, 1)[0])
        tree.reorderProjectSection(ids, pinned)
        focusKey(key)
      }
    } else if (kind === 'session') {
      const group = item.dataset.treeGroup ?? '__other__'
      const rows = group === '__other__'
        ? tree.groups.other
        : tree.groups.groups.find((entry) => entry.project.id === group)?.sessions ?? []
      const names = rows.map(refOf)
      const from = names.indexOf(key.slice('session:'.length))
      const to = from + offset
      if (to >= 0 && to < names.length) {
        names.splice(to, 0, names.splice(from, 1)[0])
        tree.reorderSessions(group, names)
        focusKey(key)
      }
    }
    return
  }
  if (matchingTreeShortcut(event, 'tree-down')) { event.preventDefault(); moveFocus(index + 1); return }
  if (matchingTreeShortcut(event, 'tree-up')) { event.preventDefault(); moveFocus(index - 1); return }
  if (matchingTreeShortcut(event, 'tree-home')) { event.preventDefault(); moveFocus(0); return }
  if (matchingTreeShortcut(event, 'tree-end')) { event.preventDefault(); moveFocus(items.length - 1); return }
  if (matchingTreeShortcut(event, 'tree-expand')) {
    event.preventDefault()
    if ((kind === 'project' || kind === 'other') && item.getAttribute('aria-expanded') === 'false') {
      tree.setCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length), false)
    } else if (kind === 'session' && item.getAttribute('aria-expanded') === 'false') {
      windows.toggleSession(parseSessionRef(name).machine, parseSessionRef(name).name)
    } else if (kind === 'window' && item.getAttribute('aria-expanded') === 'false') {
      windows.toggleWindow(parseSessionRef(name).machine, parseSessionRef(name).name, windowID)
    } else if (item.getAttribute('aria-expanded') === 'true') moveFocus(index + 1)
    return
  }
  if (matchingTreeShortcut(event, 'tree-collapse')) {
    event.preventDefault()
    if ((kind === 'project' || kind === 'other') && item.getAttribute('aria-expanded') === 'true') {
      tree.setCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length), true)
    } else if (kind === 'session' && item.getAttribute('aria-expanded') === 'true') {
      windows.toggleSession(parseSessionRef(name).machine, parseSessionRef(name).name)
    } else if (kind === 'window' && item.getAttribute('aria-expanded') === 'true') {
      windows.toggleWindow(parseSessionRef(name).machine, parseSessionRef(name).name, windowID)
    } else {
      const parent = item.closest('[role="group"]')?.parentElement?.closest<HTMLElement>('[role="treeitem"][data-tree-key]')
      if (parent?.dataset.treeKey) focusKey(parent.dataset.treeKey)
    }
    return
  }
  if (matchingTreeShortcut(event, 'tree-open')) {
    event.preventDefault()
    if (kind === 'session') item.querySelector<HTMLButtonElement>('[data-session-row]')?.click()
    else if (kind === 'window') emit('selectWindow', name, windowID)
    else if (kind === 'pane') emit('selectWindow', name, windowID, item.dataset.treePane)
    else tree.toggleCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length))
    return
  }
  if (matchingTreeShortcut(event, 'tree-kill') && kind === 'session') {
    event.preventDefault()
    const name = key.slice('session:'.length)
    emit('kill', name)
  }
}

async function saveAsProject(session: Session) {
  busySession.value = refOf(session)
  error.value = null
  try {
    const machine = sessionMachine(session)
    const project = projects.byPath(session.path, machine) ?? await projectsApi.create(machine, session.path, '')
    projects.remember(project)
    tree.sync()
  } catch (e) { error.value = describeError(e) }
  finally { busySession.value = '' }
}
</script>

<template>
  <nav aria-label="Project and session tree" class="flex min-h-0 flex-1 flex-col">
    <button
      v-if="tree.hiddenCount > 0"
      ref="showHiddenButton"
      type="button"
      class="touch-target mb-2 min-h-11 rounded border border-border px-2 text-sm"
      :aria-pressed="tree.order.showHidden"
      @click="tree.toggleShowHidden()"
    >
      Show hidden ({{ tree.hiddenCount }})
    </button>
    <div
      ref="root"
      role="tree"
      aria-label="Projects and sessions"
      class="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto outline-none"
      @focusin="focusIn"
      @keydown="onTreeKeydown"
    >
    <p v-if="error" role="alert" class="text-danger">
      {{ error.message }}
    </p>
    <section v-if="pinnedProjectRows.length" role="group" aria-label="Pinned projects">
      <h3 class="px-1.5 pt-1 pb-0.5 text-[11px] font-semibold tracking-wider text-muted uppercase">Pinned</h3>
      <VueDraggable v-model="pinnedProjectRows" tag="ul" role="group" aria-label="Pinned projects list" data-project-section="pinned" data-project-section-id="__unsectioned-pinned" item-key="id" handle=".project-drag-handle" class="flex flex-col gap-1" :animation="150" :force-fallback="true" :fallback-on-body="true" :fallback-tolerance="4" :delay="250" :delay-on-touch-only="true" :touch-start-threshold="4" :group="{ name: 'project-sections', pull: true, put: true }" :on-move="canMoveProject">
        <ProjectTreeRow v-for="group in pinnedProjectRows" :key="group.id" :group="group" :selected="props.selected" :selection-proxy="tree.order.collapsed.includes(group.id) && selectedProjectId === group.id" :focused-key="activeFocusKey" :editing-key="editingKey" :edit-error="editError" :menu-open="projectMenuId === group.id" :hidden="tree.order.hidden.projects.includes(group.id)" :pinned="true" :sections="tree.order.sections" :collapsed="tree.order.collapsed.includes(group.id)" :home="home" :rename-project="renameProject" :rename-session="renameSession" @header-click="projectHeaderClick" @long-press-start="startProjectLongPress" @long-press-move="moveProjectLongPress" @long-press-end="endProjectLongPress" @menu-open="(open, id) => projectMenuId = open ? id : ''" @start-rename="startRename" @cancel-rename="cancelRename" @hide-project="hideProject" @remove-project="emit('removeProject', $event)" @kill-project-sessions="emit('killProjectSessions', $event)" @toggle-pin="toggleProjectPin" @assign-section="tree.assignProjectSection" @select="emit('select', $event)" @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)" @split="(name, dir) => emit('split', name, dir)" @hide-session="hideSession" @kill="emit('kill', $event)" @create-queue="emit('createQueue', $event)" @create-session-in-project="emit('createSessionInProject', $event)" @reorder-sessions="tree.reorderSessions" />
      </VueDraggable>
    </section>
    <VueDraggable v-model="projectSectionRows" tag="div" role="group" aria-label="Project sections" data-section-order-list class="flex flex-col gap-1" item-key="id" handle=".section-drag-handle" :animation="150" :force-fallback="true" :fallback-on-body="true" :fallback-tolerance="4" :delay="250" :delay-on-touch-only="true" :touch-start-threshold="4" :group="{ name: 'project-section-order' }">
    <section v-for="bucket in projectSectionRows" :key="bucket.section.id" role="group" :aria-label="bucket.section.name + ' section' + (sectionHasSelectedSession(bucket.section.id) ? ', contains selected session' : '')" :aria-expanded="!sectionCollapsed(bucket.section.id)" class="project-section-shell mx-px rounded-md border-l-2 border-r border-t border-b pb-1 pl-0 pr-0" :class="sectionHasSelectedSession(bucket.section.id) ? 'ring-1 ring-accent bg-selected' : ''" :style="sectionStyle(bucket.section)" :data-selected-session="sectionHasSelectedSession(bucket.section.id) || undefined" :data-project-section-id="bucket.section.id">
      <header class="flex min-h-7 items-center gap-1 px-1">
        <button type="button" class="section-color-button touch-target inline-flex min-h-7 min-w-6 shrink-0 items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent" :aria-label="(sectionCollapsed(bucket.section.id) ? 'Expand section ' : 'Collapse section ') + bucket.section.name" :aria-expanded="!sectionCollapsed(bucket.section.id)" title="Collapse or expand section" @click.stop="tree.toggleProjectSectionCollapsed(bucket.section.id)"><span class="h-2.5 w-2.5 rounded-full" style="background-color:var(--section-accent)" aria-hidden="true"></span></button>
        <span data-section-title class="section-title-label min-w-0 flex-1 truncate text-[11px] font-semibold tracking-wide uppercase">{{ bucket.section.name }}</span>
        <span v-if="sectionCollapsed(bucket.section.id)" data-section-project-count class="shrink-0 text-[10px] text-muted">({{ bucket.pinned.length + bucket.unpinned.length }})</span>
        <button type="button" class="touch-target rounded px-1 text-muted hover:bg-bg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent" :aria-label="'Edit section ' + bucket.section.name" title="Edit section" @click="editSection(bucket.section)">···</button>
        <button type="button" class="section-drag-handle touch-target inline-flex min-h-7 w-5 shrink-0 cursor-grab items-center justify-center rounded text-muted hover:bg-bg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent" :aria-label="'Drag to reorder section ' + bucket.section.name" title="Drag to reorder sections"><GripVertical :size="14" aria-hidden="true" /></button>
      </header>
      <template v-if="!sectionCollapsed(bucket.section.id)">
      <h4 v-if="bucket.pinned.length" class="px-1 pt-1 text-[10px] font-semibold tracking-wide text-muted uppercase">Pinned</h4>
      <VueDraggable v-if="bucket.pinned.length" :model-value="bucket.pinned" @update:model-value="reorderSectionBucket(bucket.section.id, $event, true)" tag="ul" role="group" :aria-label="bucket.section.name + ' pinned projects'" data-project-section="pinned" :data-project-section-id="bucket.section.id" item-key="id" handle=".project-drag-handle" class="flex flex-col gap-1" :animation="150" :force-fallback="true" :fallback-on-body="true" :fallback-tolerance="4" :delay="250" :delay-on-touch-only="true" :touch-start-threshold="4" :group="{ name: 'project-sections', pull: true, put: true }" :on-move="canMoveProject">
        <ProjectTreeRow v-for="group in bucket.pinned" :key="group.id" :group="group" :selected="props.selected" :selection-proxy="tree.order.collapsed.includes(group.id) && selectedProjectId === group.id" :focused-key="activeFocusKey" :editing-key="editingKey" :edit-error="editError" :menu-open="projectMenuId === group.id" :hidden="tree.order.hidden.projects.includes(group.id)" :pinned="true" :sections="tree.order.sections" :section-id="bucket.section.id" :collapsed="tree.order.collapsed.includes(group.id)" :home="home" :rename-project="renameProject" :rename-session="renameSession" @header-click="projectHeaderClick" @long-press-start="startProjectLongPress" @long-press-move="moveProjectLongPress" @long-press-end="endProjectLongPress" @menu-open="(open, id) => projectMenuId = open ? id : ''" @start-rename="startRename" @cancel-rename="cancelRename" @hide-project="hideProject" @remove-project="emit('removeProject', $event)" @kill-project-sessions="emit('killProjectSessions', $event)" @toggle-pin="toggleProjectPin" @assign-section="tree.assignProjectSection" @select="emit('select', $event)" @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)" @split="(name, dir) => emit('split', name, dir)" @hide-session="hideSession" @kill="emit('kill', $event)" @create-queue="emit('createQueue', $event)" @create-session-in-project="emit('createSessionInProject', $event)" @reorder-sessions="tree.reorderSessions" />
      </VueDraggable>
      <h4 v-if="bucket.pinned.length && bucket.unpinned.length" class="px-1 pt-1 text-[10px] font-semibold tracking-wide text-muted uppercase">Projects</h4>
      <VueDraggable v-if="bucket.unpinned.length" :model-value="bucket.unpinned" @update:model-value="reorderSectionBucket(bucket.section.id, $event, false)" tag="ul" role="group" :aria-label="bucket.section.name + ' projects'" data-project-section="unpinned" :data-project-section-id="bucket.section.id" item-key="id" handle=".project-drag-handle" class="flex flex-col gap-1" :animation="150" :force-fallback="true" :fallback-on-body="true" :fallback-tolerance="4" :delay="250" :delay-on-touch-only="true" :touch-start-threshold="4" :group="{ name: 'project-sections', pull: true, put: true }" :on-move="canMoveProject">
        <ProjectTreeRow v-for="group in bucket.unpinned" :key="group.id" :group="group" :selected="props.selected" :selection-proxy="tree.order.collapsed.includes(group.id) && selectedProjectId === group.id" :focused-key="activeFocusKey" :editing-key="editingKey" :edit-error="editError" :menu-open="projectMenuId === group.id" :hidden="tree.order.hidden.projects.includes(group.id)" :pinned="false" :sections="tree.order.sections" :section-id="bucket.section.id" :collapsed="tree.order.collapsed.includes(group.id)" :home="home" :rename-project="renameProject" :rename-session="renameSession" @header-click="projectHeaderClick" @long-press-start="startProjectLongPress" @long-press-move="moveProjectLongPress" @long-press-end="endProjectLongPress" @menu-open="(open, id) => projectMenuId = open ? id : ''" @start-rename="startRename" @cancel-rename="cancelRename" @hide-project="hideProject" @remove-project="emit('removeProject', $event)" @kill-project-sessions="emit('killProjectSessions', $event)" @toggle-pin="toggleProjectPin" @assign-section="tree.assignProjectSection" @select="emit('select', $event)" @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)" @split="(name, dir) => emit('split', name, dir)" @hide-session="hideSession" @kill="emit('kill', $event)" @create-queue="emit('createQueue', $event)" @create-session-in-project="emit('createSessionInProject', $event)" @reorder-sessions="tree.reorderSessions" />
      </VueDraggable>
      <p v-if="!bucket.pinned.length && !bucket.unpinned.length" class="px-1 py-1 text-[11px] text-muted">Empty section</p>
      </template>
    </section>
    </VueDraggable>
    <section v-if="unpinnedProjectRows.length" role="group" aria-label="Projects">
      <h3 v-if="pinnedProjectRows.length" class="px-1.5 pt-1 pb-0.5 text-[11px] font-semibold tracking-wider text-muted uppercase">Projects</h3>
      <VueDraggable v-model="unpinnedProjectRows" tag="ul" role="group" aria-label="Projects list" data-project-section="unpinned" data-project-section-id="__unsectioned-unpinned" item-key="id" handle=".project-drag-handle" class="flex flex-col gap-1" :animation="150" :force-fallback="true" :fallback-on-body="true" :fallback-tolerance="4" :delay="250" :delay-on-touch-only="true" :touch-start-threshold="4" :group="{ name: 'project-sections', pull: true, put: true }" :on-move="canMoveProject">
        <ProjectTreeRow v-for="group in unpinnedProjectRows" :key="group.id" :group="group" :selected="props.selected" :selection-proxy="tree.order.collapsed.includes(group.id) && selectedProjectId === group.id" :focused-key="activeFocusKey" :editing-key="editingKey" :edit-error="editError" :menu-open="projectMenuId === group.id" :hidden="tree.order.hidden.projects.includes(group.id)" :pinned="false" :sections="tree.order.sections" :collapsed="tree.order.collapsed.includes(group.id)" :home="home" :rename-project="renameProject" :rename-session="renameSession" @header-click="projectHeaderClick" @long-press-start="startProjectLongPress" @long-press-move="moveProjectLongPress" @long-press-end="endProjectLongPress" @menu-open="(open, id) => projectMenuId = open ? id : ''" @start-rename="startRename" @cancel-rename="cancelRename" @hide-project="hideProject" @remove-project="emit('removeProject', $event)" @kill-project-sessions="emit('killProjectSessions', $event)" @toggle-pin="toggleProjectPin" @assign-section="tree.assignProjectSection" @select="emit('select', $event)" @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)" @split="(name, dir) => emit('split', name, dir)" @hide-session="hideSession" @kill="emit('kill', $event)" @create-queue="emit('createQueue', $event)" @create-session-in-project="emit('createSessionInProject', $event)" @reorder-sessions="tree.reorderSessions" />
      </VueDraggable>
    </section>
    <li
      v-if="otherRows.length"
      role="treeitem"
      aria-level="1"
      :aria-expanded="!tree.order.collapsed.includes('__other__')"
      :tabindex="activeFocusKey === 'other' ? 0 : -1"
      aria-label="Other sessions"
      data-tree-key="other"
      data-tree-kind="other"
      class="mt-2 rounded"
    >
      <div class="tree-row flex min-h-8 items-center gap-1.5 px-0.5" @click="focusKey('other'); tree.toggleCollapsed('__other__')">
        <button
          type="button"
          class="row-action touch-target inline-flex min-h-6 min-w-4 items-center justify-center rounded text-muted"
          :aria-label="(tree.order.collapsed.includes('__other__') ? 'Expand ' : 'Collapse ') + 'Other sessions'"
          :aria-expanded="!tree.order.collapsed.includes('__other__')"
          :title="(tree.order.collapsed.includes('__other__') ? 'Expand ' : 'Collapse ') + 'Other sessions'"
          tabindex="-1"
          @click.stop="tree.toggleCollapsed('__other__')"
        >
          <ChevronRight :size="14" class="transition-transform" :class="!tree.order.collapsed.includes('__other__') ? 'rotate-90' : ''" aria-hidden="true" />
        </button>
        <Folder :size="14" class="shrink-0 text-muted" aria-hidden="true" />
        <span data-other-label class="min-w-0 flex-1 truncate text-[11px] font-semibold tracking-wider text-muted uppercase">Other sessions</span>
      </div>
    <div v-if="!tree.order.collapsed.includes('__other__')" role="group" class="py-0">
        <SessionList
          :sessions="otherRows"
          :selected="props.selected"
          :tree-view="true"
          :level="2"
          :focused-key="activeFocusKey"
          :editing-name="editingKey.startsWith('session:') ? editingKey.slice(8) : ''"
          :edit-error="editError"
          :commit-edit="renameSession"
          group-key="__other__"
          machine-chips
          sortable
          can-save-as-project
          list-label="Other sessions"
          @select="emit('select', $event)"
          @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)"
          @split="(name, dir) => emit('split', name, dir)"
          @rename="startRename('session:' + $event)"
          @edit-cancel="cancelRename('session:' + $event)"
          @hide="hideSession"
          @kill="emit('kill', $event)"
          @create-queue="emit('createQueue', $event)"
          @reorder="tree.reorderSessions('__other__', $event)"
          @save-as-project="saveAsProject"
        />
      </div>
    </li>
    <p v-if="projectRows.length === 0 && otherRows.length === 0" class="px-2 py-1 text-muted">
      {{ totalRows > 0 && !tree.order.showHidden ? 'Everything is hidden.' : 'No tmux sessions yet.' }}
    </p>
    </div>
    <div class="sticky bottom-0 mt-auto shrink-0 border-t border-border bg-surface/95 pt-1">
      <button type="button" class="touch-target flex min-h-8 w-full items-center gap-2 rounded px-1.5 text-left text-sm text-muted hover:bg-bg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent" aria-label="Create a new section" title="Create a new section" @click="editSection()"><Plus :size="16" aria-hidden="true" /><span>Create a new section</span></button>
    </div>
    <DialogRoot v-model:open="sectionDialogOpen">
      <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-[90] bg-overlay" />
      <DialogContent v-if="sectionEditor" class="fixed left-1/2 top-1/2 z-[90] w-[min(24rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-4 text-fg shadow-xl">
      <DialogTitle class="mb-1 text-base font-semibold">{{ sectionEditor.id ? 'Edit section' : 'Create a new section' }}</DialogTitle>
      <DialogDescription class="mb-3 text-sm text-muted">Choose a name and accent color for this project group.</DialogDescription>
      <form @submit.prevent="saveSection">
        <label class="mb-3 block text-sm">Section name<input v-model="sectionEditor.name" autocomplete="off" autofocus maxlength="80" required class="mt-1 min-h-10 w-full rounded border border-border bg-bg px-2 text-fg" /></label>
        <fieldset><legend class="mb-1 text-sm">Color</legend><div class="flex gap-2"> <button v-for="color in SECTION_COLORS" :key="color" type="button" class="touch-target flex h-10 w-10 items-center justify-center rounded-full border-2" :class="sectionEditor.color === color ? 'border-fg' : 'border-transparent'" :style="{ backgroundColor: sectionColorValues[color] }" :aria-label="color" :aria-pressed="sectionEditor.color === color" @click="sectionEditor.color = color"><span class="sr-only">{{ color }}</span></button></div></fieldset>
        <p v-if="sectionError" role="alert" class="mt-2 text-sm text-danger">{{ sectionError }}</p>
        <div class="mt-4 flex items-center justify-between gap-2">
          <button v-if="sectionEditor.id" type="button" class="touch-target rounded px-2 text-danger hover:bg-bg" @click="tree.deleteProjectSection(sectionEditor!.id!); sectionEditor = null">Delete section</button><span v-else></span>
          <div class="flex gap-2"><button type="button" class="touch-target rounded px-3 hover:bg-bg" @click="sectionEditor = null">Cancel</button><button type="submit" class="touch-target rounded bg-accent px-3 text-on-accent">Save</button></div>
        </div>
      </form>
      </DialogContent>
      </DialogPortal>
    </DialogRoot>
  </nav>
</template>
