<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { ChevronRight, Folder, List } from 'lucide-vue-next'
import type { Project, Session } from '@/api/types'
import { projectsApi } from '@/api/client'
import { useMachinesStore } from '@/stores/machines'
import { useProjectsStore } from '@/stores/projects'
import { useTreeStore } from '@/stores/tree'
import { describeError } from '@/stores/toasts'
import type { SplitDir } from '@/lib/layout'
import type { ProjectGroup } from '@/lib/tree'
import SessionList from './SessionList.vue'

const props = defineProps<{ selected?: string }>()
const emit = defineEmits<{
  select: [name: string]
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
  sessionInProject: [project: Project]
}>()
const tree = useTreeStore()
const projects = useProjectsStore()
const machines = useMachinesStore()
const root = ref<HTMLElement>()
const focusedKey = ref('')
const busySession = ref('')
const error = ref<{ message: string; hint?: string } | null>(null)
const projectRows = computed({
  get: () => tree.groups.groups.map((group) => ({ ...group, id: group.project.id })),
  set: (groups: (ProjectGroup & { id: string })[]) => tree.reorderProjects(groups.map((group) => group.project.id)),
})
const home = computed(() => machines.byId('host')?.home ?? '')
const visibleKeys = computed(() => {
  const keys: string[] = []
  for (const group of projectRows.value) {
    keys.push('project:' + group.project.id)
    if (!tree.order.collapsed.includes(group.project.id)) {
      keys.push(...group.sessions.map((session) => 'session:' + session.name))
    }
  }
  if (tree.groups.other.length) {
    keys.push('other')
    if (!tree.order.collapsed.includes('__other__')) keys.push(...tree.groups.other.map((session) => 'session:' + session.name))
  }
  return keys
})
const activeFocusKey = computed(() => visibleKeys.value.includes(focusedKey.value) ? focusedKey.value : (visibleKeys.value[0] ?? ''))

watch(visibleKeys, (keys) => {
  if (focusedKey.value && !keys.includes(focusedKey.value)) {
    const fallback = keys[0]
    focusedKey.value = fallback ?? ''
    if (fallback) focusKey(fallback)
  }
})

function shortPath(path: string): string {
  if (!home.value) return path
  if (path === home.value) return '~'
  return path.startsWith(home.value + '/') ? '~' + path.slice(home.value.length) : path
}

function focusKey(key: string) {
  focusedKey.value = key
  void nextTick(() => {
    const target = [...(root.value?.querySelectorAll<HTMLElement>('[data-tree-key]') ?? [])].find((item) => item.dataset.treeKey === key)
    target?.focus()
  })
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
  const items = visibleItems()
  const index = items.indexOf(item)
  const moveFocus = (to: number) => {
    if (to >= 0 && to < items.length) focusKey(items[to].dataset.treeKey ?? '')
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
  if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
    event.preventDefault()
    const offset = event.key === 'ArrowUp' ? -1 : 1
    if (kind === 'project') {
      const ids = projectRows.value.map((group) => group.project.id)
      const from = ids.indexOf(key.slice('project:'.length))
      const to = from + offset
      if (to >= 0 && to < ids.length) {
        ids.splice(to, 0, ids.splice(from, 1)[0])
        tree.reorderProjects(ids)
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
  if (event.key === 'ArrowDown') { event.preventDefault(); moveFocus(index + 1); return }
  if (event.key === 'ArrowUp') { event.preventDefault(); moveFocus(index - 1); return }
  if (event.key === 'Home') { event.preventDefault(); moveFocus(0); return }
  if (event.key === 'End') { event.preventDefault(); moveFocus(items.length - 1); return }
  if (event.key === 'ArrowRight') {
    event.preventDefault()
    if ((kind === 'project' || kind === 'other') && item.getAttribute('aria-expanded') === 'false') {
      tree.setCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length), false)
    } else if (item.getAttribute('aria-expanded') === 'true') moveFocus(index + 1)
    return
  }
  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    if ((kind === 'project' || kind === 'other') && item.getAttribute('aria-expanded') === 'true') {
      tree.setCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length), true)
    } else {
      const parent = item.closest('[role="group"]')?.parentElement?.closest<HTMLElement>('[role="treeitem"][data-tree-key]')
      if (parent?.dataset.treeKey) focusKey(parent.dataset.treeKey)
    }
    return
  }
  if (event.key === 'Enter') {
    event.preventDefault()
    if (kind === 'session') item.querySelector<HTMLButtonElement>('[data-session-row]')?.click()
    else tree.toggleCollapsed(kind === 'other' ? '__other__' : key.slice('project:'.length))
    return
  }
  if (event.key === 'Delete' && kind === 'session') {
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
    <VueDraggable
      v-model="projectRows"
      tag="ul"
      role="group"
      aria-label="Projects"
      item-key="id"
      handle=".project-drag-handle"
      class="flex flex-col gap-1"
      :animation="150"
      :delay-on-touch-only="true"
      :touch-start-threshold="3"
    >
      <li
        v-for="group in projectRows"
        :key="group.id"
        :role="'treeitem'"
        :aria-level="1"
        :aria-expanded="!tree.order.collapsed.includes(group.project.id)"
        :tabindex="activeFocusKey === ('project:' + group.project.id) ? 0 : -1"
        :aria-label="group.project.name"
        :data-tree-key="'project:' + group.project.id"
        data-tree-kind="project"
        class="rounded bg-tree-header px-1"
      >
        <div class="flex min-h-12 items-center gap-1" @click="focusKey('project:' + group.project.id); tree.toggleCollapsed(group.project.id)">
          <button
            type="button"
            class="touch-target project-drag-handle min-h-11 min-w-8 cursor-grab rounded text-muted"
            :aria-label="'Drag to reorder project ' + group.project.name"
            title="Drag to reorder projects"
            tabindex="-1"
            @click.stop
          >
            ⠿
          </button>
          <button
            type="button"
            class="touch-target inline-flex min-h-11 min-w-8 items-center justify-center rounded text-muted"
            :aria-label="(tree.order.collapsed.includes(group.project.id) ? 'Expand ' : 'Collapse ') + group.project.name"
            :aria-expanded="!tree.order.collapsed.includes(group.project.id)"
            :title="(tree.order.collapsed.includes(group.project.id) ? 'Expand ' : 'Collapse ') + group.project.name"
            tabindex="-1"
            @click.stop="tree.toggleCollapsed(group.project.id)"
          >
            <ChevronRight :size="16" class="transition-transform" :class="!tree.order.collapsed.includes(group.project.id) ? 'rotate-90' : ''" aria-hidden="true" />
          </button>
          <Folder :size="16" class="shrink-0 text-muted" aria-hidden="true" />
          <span class="min-w-0 flex-1">
            <span class="block truncate font-semibold">{{ group.project.name }}</span>
            <span class="block truncate text-xs text-muted" :title="group.project.path">{{ shortPath(group.project.path) }}</span>
          </span>
          <button
            type="button"
            class="touch-target min-h-11 rounded px-2"
            :aria-label="'New session in ' + group.project.name"
            :title="'New session in ' + group.project.name"
            tabindex="-1"
            @click.stop="emit('sessionInProject', group.project)"
          >
            ＋
          </button>
        </div>
        <div v-if="!tree.order.collapsed.includes(group.project.id)" role="group" class="ml-3 border-l border-border py-1 pl-3">
          <SessionList
            :sessions="group.sessions"
            :selected="props.selected"
            :tree-view="true"
            :level="2"
            :focused-key="activeFocusKey"
            :group-key="group.project.id"
            sortable
            :list-label="'Sessions in ' + group.project.name"
            @select="emit('select', $event)"
            @split="(name, dir) => emit('split', name, dir)"
            @rename="emit('rename', $event)"
            @kill="emit('kill', $event)"
            @reorder="tree.reorderSessions(group.project.id, $event)"
          />
        </div>
      </li>
    </VueDraggable>
    <li
      v-if="tree.groups.other.length"
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
          :sessions="tree.groups.other"
          :selected="props.selected"
          :tree-view="true"
          :level="2"
          :focused-key="activeFocusKey"
          group-key="__other__"
          sortable
          can-save-as-project
          list-label="Other sessions"
          @select="emit('select', $event)"
          @split="(name, dir) => emit('split', name, dir)"
          @rename="emit('rename', $event)"
          @kill="emit('kill', $event)"
          @reorder="tree.reorderSessions('__other__', $event)"
          @save-as-project="saveAsProject"
        />
      </div>
    </li>
    <p v-if="projectRows.length === 0 && tree.groups.other.length === 0" class="px-2 py-1 text-muted">
      No tmux sessions yet.
    </p>
    </div>
  </nav>
</template>
