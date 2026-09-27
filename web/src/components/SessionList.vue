<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { VueDraggable } from 'vue-draggable-plus'
import { computed, onScopeDispose, ref } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import type { Session } from '@/api/types'
import type { SplitDir } from '@/lib/layout'
import { sessionKey, windowKey } from '@/lib/tree'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'
import { describeError } from '@/stores/toasts'
import InlineRename from './InlineRename.vue'

const props = defineProps<{
  sessions: Session[]
  selected?: string
  sortable?: boolean
  listLabel?: string
  canSaveAsProject?: boolean
  treeView?: boolean
  level?: number
  focusedKey?: string
  groupKey?: string
  editingName?: string
  editError?: string
  commitEdit?: (from: string, to: string) => Promise<void>
  hiddenGroup?: boolean
}>()
const emit = defineEmits<{
  select: [name: string]
  /** Open in a new pane beside the active tab's focused pane. */
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
  reorder: [names: string[]]
  saveAsProject: [session: Session]
  selectWindow: [name: string, window: string, pane?: string]
  editCancel: [name: string]
  hide: [name: string, hidden: boolean]
}>()

const item = 'touch-target flex min-h-11 items-center cursor-pointer rounded px-2 py-1 outline-none data-highlighted:bg-bg'
const openMenuName = ref('')
const tree = props.treeView ? useTreeStore() : undefined
const windowsStore = props.treeView ? useWindowsStore() : undefined

function windowsFor(name: string) { return windowsStore?.bySession[sessionKey('host', name)] }
function isSessionHidden(name: string) { return tree?.order.hidden.sessions.includes(sessionKey('host', name)) ?? false }
function isHidden(name: string) { return Boolean(props.hiddenGroup || isSessionHidden(name)) }
function isExpanded(key: string) { return tree?.order.expanded.includes(key) ?? false }
function windowError(name: string) {
  const error = describeError(windowsFor(name)?.error)
  return [error.message, error.hint].filter(Boolean).join(' ')
}
/** Only rows that really expand get a chevron (M8 T2): several windows, or
 * one already-loaded window split into panes. The inventory reports window
 * counts only, so a single window's panes show once it has been loaded. */
function canExpand(s: Session) {
  if (!props.treeView) return false
  if (s.windows > 1) return true
  const entry = windowsFor(s.name)
  return entry?.status === 'ok' && (entry.windows.length > 1 || entry.windows.some((w) => w.panes.length > 1))
}
function toggleSession(name: string) { windowsStore?.toggleSession('host', name) }
function toggleWindow(name: string, id: string) { windowsStore?.toggleWindow('host', name, id) }
function retryWindows(name: string) { windowsStore?.refresh('host', name) }
const longPressTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const pointerStart = ref<{ x: number; y: number; name: string } | null>(null)
const longPressedName = ref('')

function clearLongPress() {
  if (longPressTimer.value) clearTimeout(longPressTimer.value)
  longPressTimer.value = null
  pointerStart.value = null
}

function startLongPress(event: PointerEvent, name: string) {
  if (event.pointerType !== 'touch') return
  longPressedName.value = ''
  clearLongPress()
  pointerStart.value = { x: event.clientX, y: event.clientY, name }
  longPressTimer.value = setTimeout(() => {
    longPressedName.value = name
    openMenuName.value = name
    longPressTimer.value = null
  }, 500)
}

function moveLongPress(event: PointerEvent) {
  if (!pointerStart.value) return
  if (Math.hypot(event.clientX - pointerStart.value.x, event.clientY - pointerStart.value.y) > 10) clearLongPress()
}

function finishLongPress() {
  clearLongPress()
}

function selectSession(name: string) {
  if (longPressedName.value === name) {
    longPressedName.value = ''
    return
  }
  emit('select', name)
}

function commitRename(name: string, value: string) {
  return props.commitEdit ? props.commitEdit(name, value) : Promise.resolve()
}

function renameOnDoubleClick(name: string) {
  if (props.treeView && window.matchMedia('(pointer: fine)').matches) emit('rename', name)
}

onScopeDispose(clearLongPress)
const sortableSessions = computed({
  get: () => props.sessions,
  set: (items: Session[]) => emit('reorder', items.map((s) => s.name)),
})
</script>

<template>
  <p
    v-if="props.sessions.length === 0"
    class="text-muted"
  >
    No tmux sessions yet.
  </p>
  <VueDraggable
    v-else
    v-model="sortableSessions"
    tag="ul"
    item-key="name"
    handle=".session-drag-handle"
    :disabled="!props.sortable"
    :animation="150"
    :delay-on-touch-only="true"
    :touch-start-threshold="3"
    :aria-label="props.listLabel ?? 'tmux sessions'"
    :role="props.treeView ? 'group' : 'list'"
    class="flex flex-col gap-0"
  >
    <li
      v-for="s in sortableSessions"
      :key="s.name"
      :role="props.treeView ? 'treeitem' : 'listitem'"
      :aria-level="props.treeView ? (props.level ?? 1) : undefined"
      :aria-selected="props.treeView && s.name === props.selected ? 'true' : undefined"
      :aria-expanded="canExpand(s) ? isExpanded(sessionKey('host', s.name)) : undefined"
      :aria-label="props.treeView ? s.name + (isHidden(s.name) ? ', hidden' : '') : undefined"
      :tabindex="props.treeView && props.focusedKey === ('session:' + s.name) ? 0 : props.treeView ? -1 : undefined"
      :data-tree-key="props.treeView ? 'session:' + s.name : undefined"
      :data-tree-kind="props.treeView ? 'session' : undefined"
      :data-tree-group="props.treeView ? props.groupKey : undefined"
      class="flex flex-wrap items-center gap-0.5 rounded px-1"
      :class="[s.name === props.selected ? 'bg-bg' : '', isHidden(s.name) ? 'opacity-50' : '']"
    >
      <!-- The name leads (M8 T2); status, expand and actions follow. -->
      <InlineRename
        v-if="props.treeView && props.editingName === s.name"
        :name="s.name"
        :error="props.editError"
        :commit="(value) => commitRename(s.name, value)"
        @cancel="emit('editCancel', s.name)"
      />
      <button
        v-else
        type="button"
        data-session-row
        :aria-label="s.name"
        :aria-current="s.name === props.selected ? 'true' : undefined"
        :tabindex="props.treeView ? -1 : undefined"
        class="touch-target min-h-7 min-w-[8ch] flex-1 truncate text-left font-medium text-fg"
        @pointerdown="startLongPress($event, s.name)"
        @pointermove="moveLongPress"
        @pointerup="finishLongPress"
        @pointercancel="finishLongPress"
        @pointerleave="finishLongPress"
        @click="selectSession(s.name)"
        @dblclick="renameOnDoubleClick(s.name)"
      >
        {{ s.name }}
      </button>
      <span
        role="img"
        :aria-label="s.attached > 0 ? 'attached' : 'detached'"
        :title="s.attached > 0 ? 'attached' : 'detached'"
        class="inline-block size-2 shrink-0 rounded-full"
        :class="s.attached > 0 ? 'bg-ok' : 'border border-muted'"
      />
      <button
        v-if="canExpand(s)"
        type="button"
        class="touch-target inline-flex min-h-7 min-w-6 shrink-0 items-center justify-center rounded text-muted"
        :aria-label="(isExpanded(sessionKey('host', s.name)) ? 'Collapse ' : 'Expand ') + s.name"
        :aria-expanded="isExpanded(sessionKey('host', s.name))"
        :title="(isExpanded(sessionKey('host', s.name)) ? 'Collapse ' : 'Expand ') + s.name"
        :tabindex="-1"
        @click.stop="toggleSession(s.name)"
      >
        <ChevronRight :size="16" class="transition-transform" :class="isExpanded(sessionKey('host', s.name)) ? 'rotate-90' : ''" aria-hidden="true" />
      </button>
      <span class="flex shrink-0 items-center">
        <button
          type="button"
          :aria-label="`Kill ${s.name}`"
          title="Kill"
          :tabindex="props.treeView ? -1 : undefined"
          class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted hover:text-danger"
          @click="emit('kill', s.name)"
        >
          ✕
        </button>
        <DropdownMenuRoot :open="openMenuName === s.name" @update:open="(open) => openMenuName = open ? s.name : ''">
          <DropdownMenuTrigger
            :aria-label="`More actions for ${s.name}`"
            title="More"
            :tabindex="props.treeView ? -1 : undefined"
            class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted hover:text-fg"
          >
            ⋯
          </DropdownMenuTrigger>
          <DropdownMenuPortal>
            <DropdownMenuContent
              align="end"
              :side-offset="4"
              class="z-30 min-w-48 rounded border border-border bg-surface p-1 text-fg shadow-lg"
            >
              <DropdownMenuItem
                v-if="props.treeView"
                :class="item"
                @select="emit('hide', s.name, isSessionHidden(s.name))"
              >
                {{ isSessionHidden(s.name) ? 'Unhide' : 'Hide' }}
              </DropdownMenuItem>
              <DropdownMenuItem
                v-if="props.treeView"
                :class="item"
                @select="emit('rename', s.name)"
              >
                Rename
              </DropdownMenuItem>
              <DropdownMenuItem
                :class="item"
                @select="emit('split', s.name, 'row')"
              >
                Open in split right
              </DropdownMenuItem>
              <DropdownMenuItem
                :class="item"
                @select="emit('split', s.name, 'column')"
              >
                Open in split down
              </DropdownMenuItem>
              <DropdownMenuItem
                v-if="props.canSaveAsProject"
                :class="item"
                @select="emit('saveAsProject', s)"
              >
                Save as project
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuPortal>
        </DropdownMenuRoot>
        <button
          type="button"
          :aria-label="`Rename ${s.name}`"
          title="Rename"
          :tabindex="props.treeView ? -1 : undefined"
          class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted hover:text-fg"
          @click="emit('rename', s.name)"
        >
          ✎
        </button>
      </span>
      <button
        v-if="props.sortable"
        type="button"
        class="session-drag-handle touch-target inline-flex min-h-7 min-w-5 shrink-0 items-center justify-center cursor-grab rounded text-muted"
        :aria-label="`Drag to reorder session ${s.name}`"
        title="Drag to reorder sessions"
        :tabindex="props.treeView ? -1 : undefined"
      >
        ⠿
      </button>
      <ul v-if="canExpand(s) && isExpanded(sessionKey('host', s.name))" role="group" class="ml-2.5 basis-[calc(100%-0.625rem)] border-l border-border py-0.5 pl-1.5">
        <li v-if="windowsFor(s.name)?.status === 'loading' || windowsFor(s.name)?.status === 'idle'" role="treeitem" :aria-level="(props.level ?? 1) + 1" aria-disabled="true" tabindex="-1" class="touch-target min-h-8 px-2 py-1 text-sm text-muted">
          <span class="animate-spin" aria-hidden="true">◌</span> Loading windows…
        </li>
        <li v-else-if="windowsFor(s.name)?.status === 'error'" role="treeitem" :aria-level="(props.level ?? 1) + 1" aria-disabled="true" tabindex="-1" class="touch-target flex min-h-8 items-center gap-2 px-2 text-sm text-danger">
          <span class="min-w-0 flex-1">{{ windowError(s.name) }}</span>
          <button type="button" class="touch-target min-h-7 rounded px-2 text-fg underline" @click="retryWindows(s.name)">Retry</button>
        </li>
        <template v-else>
          <li
            v-for="window in windowsFor(s.name)?.windows ?? []"
            :key="window.id"
            role="treeitem"
            :aria-level="(props.level ?? 1) + 1"
            :aria-expanded="window.panes.length > 1 ? isExpanded(windowKey('host', s.name, window.id)) : undefined"
            :tabindex="props.focusedKey === ('window:' + windowKey('host', s.name, window.id)) ? 0 : -1"
            :data-tree-key="'window:' + windowKey('host', s.name, window.id)"
            data-tree-kind="window"
            :data-tree-session="s.name"
            :data-tree-window="window.id"
            class="flex min-h-8 flex-wrap items-center gap-0.5 rounded px-1"
          >
            <button
              v-if="window.panes.length > 1"
              type="button"
              class="touch-target inline-flex min-h-7 min-w-6 items-center justify-center rounded text-muted"
              :aria-label="(isExpanded(windowKey('host', s.name, window.id)) ? 'Collapse ' : 'Expand ') + 'window ' + (window.index + 1)"
              :aria-expanded="isExpanded(windowKey('host', s.name, window.id))"
              tabindex="-1"
              @click.stop="toggleWindow(s.name, window.id)"
            >
              <ChevronRight :size="16" class="transition-transform" :class="isExpanded(windowKey('host', s.name, window.id)) ? 'rotate-90' : ''" aria-hidden="true" />
            </button>
            <button type="button" tabindex="-1" class="touch-target min-h-7 min-w-0 flex-1 truncate px-1.5 text-left" @click="emit('selectWindow', s.name, window.id)">
              {{ window.index + 1 }}: {{ window.name }} <span v-if="window.active" class="text-muted">(current)</span>
            </button>
            <ul v-if="window.panes.length > 1 && isExpanded(windowKey('host', s.name, window.id))" role="group" class="ml-2.5 basis-[calc(100%-0.625rem)] border-l border-border py-0.5 pl-1.5">
              <li
                v-for="pane in window.panes"
                :key="pane.id"
                role="treeitem"
                :aria-level="(props.level ?? 1) + 2"
                :tabindex="props.focusedKey === ('pane:' + windowKey('host', s.name, window.id) + '/' + pane.id) ? 0 : -1"
                :data-tree-key="'pane:' + windowKey('host', s.name, window.id) + '/' + pane.id"
                data-tree-kind="pane"
                :data-tree-session="s.name"
                :data-tree-window="window.id"
                :data-tree-pane="pane.id"
                class="flex min-h-8 items-center rounded px-1.5"
              >
                <button type="button" tabindex="-1" class="touch-target min-h-7 min-w-0 flex-1 truncate text-left" @click="emit('selectWindow', s.name, window.id, pane.id)">
                  Pane {{ pane.index + 1 }} — {{ pane.command }} <span v-if="pane.active" class="text-muted">(active)</span>
                </button>
              </li>
            </ul>
          </li>
          <li v-if="windowsFor(s.name)?.truncated" role="treeitem" :aria-level="(props.level ?? 1) + 1" aria-disabled="true" tabindex="-1" class="touch-target min-h-8 px-2 py-1 text-sm text-muted">More windows not shown</li>
        </template>
      </ul>
    </li>
  </VueDraggable>
</template>
