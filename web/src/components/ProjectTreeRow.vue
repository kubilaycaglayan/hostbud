<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { ChevronRight, Folder, MoreHorizontal, Pin, Plus } from 'lucide-vue-next'
import type { Project } from '@/api/types'
import type { SplitDir } from '@/lib/layout'
import type { ProjectGroup } from '@/lib/tree'
import SessionList from './SessionList.vue'
import InlineRename from './InlineRename.vue'

const props = defineProps<{
  group: ProjectGroup & { id: string }
  selected?: string
  focusedKey: string
  editingKey: string
  editError: string
  menuOpen: boolean
  hidden: boolean
  pinned: boolean
  collapsed: boolean
  home: string
  renameProject: (id: string, name: string) => Promise<void>
  renameSession: (from: string, to: string) => Promise<void>
}>()
const emit = defineEmits<{
  headerClick: [id: string]
  longPressStart: [event: PointerEvent, id: string]
  longPressMove: [event: PointerEvent]
  longPressEnd: []
  menuOpen: [open: boolean, id: string]
  startRename: [key: string]
  cancelRename: [key: string]
  hideProject: [id: string]
  removeProject: [id: string]
  togglePin: [id: string]
  select: [name: string]
  selectWindow: [name: string, window: string, pane?: string]
  split: [name: string, dir: SplitDir]
  hideSession: [name: string, hidden: boolean]
  kill: [name: string]
  sessionInProject: [project: Project]
  reorderSessions: [group: string, names: string[]]
}>()

function renameSessionCommit(from: string, to: string) {
  return props.renameSession(from, to)
}

function shortPath(path: string): string {
  if (!props.home) return path
  if (path === props.home) return '~'
  return path.startsWith(props.home + '/') ? '~' + path.slice(props.home.length) : path
}

function renameOnFinePointer() {
  if (window.matchMedia('(pointer: fine)').matches) emit('startRename', 'project:' + props.group.project.id)
}

let restoringMenuFocus = true
function startProjectRename() {
  // Closing this menu normally focuses its trigger. That would immediately
  // blur (and cancel) the inline editor that replaces the project label.
  restoringMenuFocus = false
  emit('startRename', 'project:' + props.group.project.id)
}
function onMenuCloseAutoFocus(event: Event) {
  if (restoringMenuFocus) return
  restoringMenuFocus = true
  event.preventDefault()
}
</script>

<template>
  <li
    :role="'treeitem'"
    :aria-level="1"
    :aria-expanded="!props.collapsed"
    :tabindex="props.focusedKey === ('project:' + props.group.project.id) ? 0 : -1"
    :aria-label="props.group.project.name + (props.hidden ? ', hidden' : '')"
    :data-tree-key="'project:' + props.group.project.id"
    data-tree-kind="project"
    class="rounded bg-tree-header px-0.5"
    :class="props.hidden ? 'opacity-50' : ''"
  >
    <div
      class="flex min-h-8 items-center gap-0"
      @click="emit('headerClick', props.group.project.id)"
      @pointerdown="emit('longPressStart', $event, props.group.project.id)"
      @pointermove="emit('longPressMove', $event)"
      @pointerup="emit('longPressEnd')"
      @pointercancel="emit('longPressEnd')"
      @pointerleave="emit('longPressEnd')"
    >
      <button type="button" class="touch-target project-drag-handle min-h-7 min-w-5 cursor-grab rounded text-muted" :aria-label="'Drag to reorder project ' + props.group.project.name" title="Drag to reorder projects" tabindex="-1" @click.stop>⠿</button>
      <button type="button" class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted" :aria-label="(props.collapsed ? 'Expand ' : 'Collapse ') + props.group.project.name" :aria-expanded="!props.collapsed" :title="(props.collapsed ? 'Expand ' : 'Collapse ') + props.group.project.name" tabindex="-1" @click.stop="emit('headerClick', props.group.project.id)">
        <ChevronRight :size="16" class="transition-transform" :class="!props.collapsed ? 'rotate-90' : ''" aria-hidden="true" />
      </button>
      <Folder :size="16" class="shrink-0 text-muted" aria-hidden="true" />
      <InlineRename v-if="props.editingKey === 'project:' + props.group.project.id" :name="props.group.project.name" :error="props.editError" :commit="(value) => props.renameProject(props.group.project.id, value)" @cancel="emit('cancelRename', 'project:' + props.group.project.id)" />
      <span v-else class="min-w-0 flex-1" @dblclick.stop="renameOnFinePointer">
        <span class="block truncate font-semibold">{{ props.group.project.name }}</span>
        <span class="block truncate text-xs text-muted" :title="props.group.project.path">{{ shortPath(props.group.project.path) }}</span>
      </span>
      <button v-if="props.pinned" type="button" class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted" :aria-label="'Unpin ' + props.group.project.name" title="Pinned" tabindex="-1" @click.stop="emit('togglePin', props.group.project.id)">
        <Pin :size="16" aria-hidden="true" />
      </button>
      <DropdownMenuRoot :open="props.menuOpen" @update:open="(open) => emit('menuOpen', open, props.group.project.id)">
        <DropdownMenuTrigger type="button" class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted hover:bg-bg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent" :aria-label="'More actions for ' + props.group.project.name" title="More" tabindex="-1" @click.stop><MoreHorizontal :size="16" aria-hidden="true" /></DropdownMenuTrigger>
        <DropdownMenuPortal>
          <DropdownMenuContent align="end" :side-offset="4" class="z-[60] min-w-48 rounded border border-border bg-surface p-1 text-fg shadow-lg" @close-auto-focus="onMenuCloseAutoFocus">
            <DropdownMenuItem class="touch-target flex min-h-11 cursor-pointer items-center rounded px-2 py-1 outline-none data-highlighted:bg-bg" @select="startProjectRename">Rename</DropdownMenuItem>
            <DropdownMenuItem class="touch-target flex min-h-11 cursor-pointer items-center rounded px-2 py-1 outline-none data-highlighted:bg-bg" @select="emit('hideProject', props.group.project.id)">{{ props.hidden ? 'Unhide' : 'Hide' }}</DropdownMenuItem>
            <DropdownMenuItem class="touch-target flex min-h-11 cursor-pointer items-center rounded px-2 py-1 outline-none data-highlighted:bg-bg" @select="emit('togglePin', props.group.project.id)">{{ props.pinned ? 'Unpin' : 'Pin' }}</DropdownMenuItem>
            <DropdownMenuItem class="touch-target flex min-h-11 cursor-pointer items-center rounded px-2 py-1 text-danger outline-none data-highlighted:bg-bg" @select="emit('removeProject', props.group.project.id)">Remove project…</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenuPortal>
      </DropdownMenuRoot>
      <button type="button" class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted hover:bg-bg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent" :aria-label="'New session in ' + props.group.project.name" :title="'New session in ' + props.group.project.name" tabindex="-1" @click.stop="emit('sessionInProject', props.group.project)"><Plus :size="16" aria-hidden="true" /></button>
    </div>
    <div v-if="!props.collapsed" role="group" class="ml-2 border-l border-border py-0 pl-1.5">
      <SessionList
        :sessions="props.group.sessions"
        :selected="props.selected"
        :tree-view="true"
        :level="2"
        :focused-key="props.focusedKey"
        :group-key="props.group.project.id"
        :editing-name="props.editingKey.startsWith('session:') ? props.editingKey.slice(8) : ''"
        :edit-error="props.editError"
        :commit-edit="renameSessionCommit"
        :hidden-group="props.hidden"
        sortable
        :list-label="'Sessions in ' + props.group.project.name"
        @select="emit('select', $event)"
        @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)"
        @split="(name, dir) => emit('split', name, dir)"
        @rename="emit('startRename', 'session:' + $event)"
        @edit-cancel="emit('cancelRename', 'session:' + $event)"
        @hide="(name, hidden) => emit('hideSession', name, hidden)"
        @kill="emit('kill', $event)"
        @reorder="emit('reorderSessions', props.group.project.id, $event)"
      />
    </div>
  </li>
</template>
