<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { ChevronRight, List } from 'lucide-vue-next'
import type { Project, Session } from '@/api/types'
import { projectsApi, sessionsApi } from '@/api/client'
import { useMachinesStore } from '@/stores/machines'
import { useProjectsStore } from '@/stores/projects'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'
import { describeError } from '@/stores/toasts'
import { projectNameError, sessionNameError } from '@/lib/names'
import { useLayoutStore } from '@/stores/layout'
import type { SplitDir } from '@/lib/layout'
import type { ProjectGroup } from '@/lib/tree'
import { canReorderProjectSections, sessionKey, windowKey } from '@/lib/tree'
import { matchingTreeShortcut } from '@/lib/shortcuts'
import SessionList from './SessionList.vue'
import ProjectTreeRow from './ProjectTreeRow.vue'

const props = defineProps<{ selected?: string }>()
const emit = defineEmits<{
  select: [name: string]
  selectWindow: [name: string, window: string, pane?: string]
  split: [name: string, dir: SplitDir]
  kill: [name: string]
  sessionInProject: [project: Project]
  create: []
}>()
const tree = useTreeStore()
const projects = useProjectsStore()
const machines = useMachinesStore()
const windows = useWindowsStore()
const layout = useLayoutStore()
const root = ref<HTMLElement>()
const showHiddenButton = ref<HTMLButtonElement>()
const focusedKey = ref('')
const deferredFocusKey = ref('')
const busySession = ref('')
const error = ref<{ message: string; hint?: string } | null>(null)
const editingKey = ref('')
const editError = ref('')
const projectMenuId = ref('')
const longPressedProjectId = ref('')
const projectPointerStart = ref<{ x: number; y: number; id: string } | null>(null)
let projectLongPressTimer: ReturnType<typeof setTimeout> | undefined
const projectRows = computed(() => tree.groups.groups
    .filter((group) => tree.order.showHidden || !tree.order.hidden.projects.includes(group.project.id))
    .map((group) => ({
      ...group,
      id: group.project.id,
      sessions: tree.order.showHidden ? group.sessions : group.sessions.filter((session) => !tree.order.hidden.sessions.includes(sessionKey('host', session.name))),
    })))
const pinnedProjectRows = computed({
  get: () => projectRows.value.filter((group) => tree.order.pinned.includes(group.project.id)),
  set: (groups: (ProjectGroup & { id: string })[]) => tree.reorderProjectSection(groups.map((group) => group.project.id), true),
})
const unpinnedProjectRows = computed({
  get: () => projectRows.value.filter((group) => !tree.order.pinned.includes(group.project.id)),
  set: (groups: (ProjectGroup & { id: string })[]) => tree.reorderProjectSection(groups.map((group) => group.project.id), false),
})
const otherRows = computed(() => tree.order.showHidden
  ? tree.groups.other
  : tree.groups.other.filter((session) => !tree.order.hidden.sessions.includes(sessionKey('host', session.name))))
const totalRows = computed(() => tree.groups.groups.length + tree.groups.groups.reduce((count, group) => count + group.sessions.length, 0) + tree.groups.other.length)
const home = computed(() => machines.byId('host')?.home ?? '')
const visibleKeys = computed(() => {
  const keys: string[] = []
  const appendSessions = (rows: Session[]) => {
    for (const session of rows) {
      const sessionID = sessionKey('host', session.name)
      keys.push('session:' + session.name)
      if (!tree.order.expanded.includes(sessionID)) continue
      for (const window of windows.bySession[sessionID]?.windows ?? []) {
        const winKey = windowKey('host', session.name, window.id)
        keys.push('window:' + winKey)
        if (window.panes.length > 1 && tree.order.expanded.includes(winKey)) {
          keys.push(...window.panes.map((pane) => 'pane:' + winKey + '/' + pane.id))
        }
      }
    }
  }
  for (const group of projectRows.value) {
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
  return canReorderProjectSections(event.from.dataset.projectSection ?? '', event.to.dataset.projectSection ?? '')
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
      const [, name] = focusedKey.value.slice('window:'.length).split('/')
      fallback = `session:${name}`
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
  const group = tree.groups.groups.find((candidate) => candidate.sessions.some((session) => session.name === name))
  tree.setCollapsed(group?.project.id ?? '__other__', false)
  await nextTick()
  focusKey('session:' + name)
  if (rename) startRename('session:' + name)
}

defineExpose({ revealProject, revealSession })

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

function hideSession(name: string, hidden: boolean) {
  const key = 'session:' + name
  const index = visibleKeys.value.indexOf(key)
  if (hidden) tree.unhideSession('host', name)
  else tree.hideSession('host', name)
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

async function renameSession(from: string, raw: string) {
  const to = raw.trim()
  const invalid = sessionNameError(to, true)
  if (invalid) { editError.value = invalid; throw new Error(invalid) }
  if (to === from) { cancelRename('session:' + from); return }
  layout.expectRename('host', from, to)
  try {
    await sessionsApi.rename('host', from, to)
    tree.renameSession('host', from, to)
    if (windows.bySession[`host/${from}`]) {
      windows.bySession[`host/${to}`] = windows.bySession[`host/${from}`]
      delete windows.bySession[`host/${from}`]
    }
    layout.renamed('host', from, to)
    editingKey.value = ''
    editError.value = ''
    focusKey('session:' + to)
  } catch (e) {
    layout.renameAbandoned('host', from)
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
      hideSession(name, tree.order.hidden.sessions.includes(sessionKey('host', name)))
    }
    return
  }
  if (matchingTreeShortcut(event, 'tree-pin') && kind === 'project') {
    event.preventDefault()
    const id = key.slice('project:'.length)
    if (tree.order.pinned.includes(id)) tree.unpinProject(id)
    else tree.pinProject(id)
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
      const rows = pinned ? pinnedProjectRows.value : unpinnedProjectRows.value
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
      const names = rows.map((session) => session.name)
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
      windows.toggleSession('host', name)
    } else if (kind === 'window' && item.getAttribute('aria-expanded') === 'false') {
      windows.toggleWindow('host', name, windowID)
    } else if (item.getAttribute('aria-expanded') === 'true') moveFocus(index + 1)
    return
  }
  if (matchingTreeShortcut(event, 'tree-collapse')) {
    event.preventDefault()
    if ((kind === 'project' || kind === 'other') && item.getAttribute('aria-expanded') === 'true') {
      tree.setCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length), true)
    } else if (kind === 'session' && item.getAttribute('aria-expanded') === 'true') {
      windows.toggleSession('host', name)
    } else if (kind === 'window' && item.getAttribute('aria-expanded') === 'true') {
      windows.toggleWindow('host', name, windowID)
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
  busySession.value = session.name
  error.value = null
  try {
    const project = projects.byPath(session.path) ?? await projectsApi.create('host', session.path, '')
    projects.remember(project)
    tree.sync()
  } catch (e) { error.value = describeError(e) }
  finally { busySession.value = '' }
}
</script>

<template>
  <nav aria-label="Project and session tree">
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
      class="flex flex-col gap-2 outline-none"
      @focusin="focusIn"
      @keydown="onTreeKeydown"
    >
    <p v-if="error" role="alert" class="text-danger">
      {{ error.message }}
    </p>
    <section v-if="pinnedProjectRows.length" role="group" aria-label="Pinned projects">
      <h3 class="px-2 py-1 text-xs font-semibold text-muted">Pinned</h3>
      <VueDraggable v-model="pinnedProjectRows" tag="ul" role="group" aria-label="Pinned projects list" data-project-section="pinned" item-key="id" handle=".project-drag-handle" class="flex flex-col gap-1" :animation="150" :delay-on-touch-only="true" :touch-start-threshold="3" :group="{ name: 'project-sections', pull: true, put: true }" :on-move="canMoveProject">
        <ProjectTreeRow v-for="group in pinnedProjectRows" :key="group.id" :group="group" :selected="props.selected" :focused-key="activeFocusKey" :editing-key="editingKey" :edit-error="editError" :menu-open="projectMenuId === group.id" :hidden="tree.order.hidden.projects.includes(group.id)" :pinned="true" :collapsed="tree.order.collapsed.includes(group.id)" :home="home" :rename-project="renameProject" :rename-session="renameSession" @header-click="projectHeaderClick" @long-press-start="startProjectLongPress" @long-press-move="moveProjectLongPress" @long-press-end="endProjectLongPress" @menu-open="(open, id) => projectMenuId = open ? id : ''" @start-rename="startRename" @cancel-rename="cancelRename" @hide-project="hideProject" @toggle-pin="tree.unpinProject" @select="emit('select', $event)" @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)" @split="(name, dir) => emit('split', name, dir)" @hide-session="hideSession" @kill="emit('kill', $event)" @session-in-project="emit('sessionInProject', $event)" @reorder-sessions="tree.reorderSessions" />
      </VueDraggable>
    </section>
    <section v-if="unpinnedProjectRows.length" role="group" aria-label="Projects">
      <h3 v-if="pinnedProjectRows.length" class="px-2 py-1 text-xs font-semibold text-muted">Projects</h3>
      <VueDraggable v-model="unpinnedProjectRows" tag="ul" role="group" aria-label="Projects list" data-project-section="unpinned" item-key="id" handle=".project-drag-handle" class="flex flex-col gap-1" :animation="150" :delay-on-touch-only="true" :touch-start-threshold="3" :group="{ name: 'project-sections', pull: true, put: true }" :on-move="canMoveProject">
        <ProjectTreeRow v-for="group in unpinnedProjectRows" :key="group.id" :group="group" :selected="props.selected" :focused-key="activeFocusKey" :editing-key="editingKey" :edit-error="editError" :menu-open="projectMenuId === group.id" :hidden="tree.order.hidden.projects.includes(group.id)" :pinned="false" :collapsed="tree.order.collapsed.includes(group.id)" :home="home" :rename-project="renameProject" :rename-session="renameSession" @header-click="projectHeaderClick" @long-press-start="startProjectLongPress" @long-press-move="moveProjectLongPress" @long-press-end="endProjectLongPress" @menu-open="(open, id) => projectMenuId = open ? id : ''" @start-rename="startRename" @cancel-rename="cancelRename" @hide-project="hideProject" @toggle-pin="tree.pinProject" @select="emit('select', $event)" @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)" @split="(name, dir) => emit('split', name, dir)" @hide-session="hideSession" @kill="emit('kill', $event)" @session-in-project="emit('sessionInProject', $event)" @reorder-sessions="tree.reorderSessions" />
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
      class="rounded bg-tree-header px-1"
    >
      <div class="flex min-h-12 items-center gap-1" @click="focusKey('other'); tree.toggleCollapsed('__other__')">
        <button
          type="button"
          class="touch-target inline-flex min-h-11 min-w-8 items-center justify-center rounded text-muted"
          :aria-label="(tree.order.collapsed.includes('__other__') ? 'Expand ' : 'Collapse ') + 'Other sessions'"
          :aria-expanded="!tree.order.collapsed.includes('__other__')"
          :title="(tree.order.collapsed.includes('__other__') ? 'Expand ' : 'Collapse ') + 'Other sessions'"
          tabindex="-1"
          @click.stop="tree.toggleCollapsed('__other__')"
        >
          <ChevronRight :size="16" class="transition-transform" :class="!tree.order.collapsed.includes('__other__') ? 'rotate-90' : ''" aria-hidden="true" />
        </button>
        <List :size="16" class="shrink-0 text-muted" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate font-semibold">Other sessions</span>
      </div>
      <div v-if="!tree.order.collapsed.includes('__other__')" role="group" class="ml-3 border-l border-border py-1 pl-3">
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
          @reorder="tree.reorderSessions('__other__', $event)"
          @save-as-project="saveAsProject"
        />
      </div>
    </li>
    <p v-if="projectRows.length === 0 && otherRows.length === 0" class="px-2 py-1 text-muted">
      {{ totalRows > 0 && !tree.order.showHidden ? 'Everything is hidden.' : 'No tmux sessions yet.' }}
    </p>
    </div>
  </nav>
</template>
