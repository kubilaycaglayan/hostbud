<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { VueDraggable } from 'vue-draggable-plus'
import { computed, onScopeDispose, ref, watch } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import type { Session } from '@/api/types'
import type { SplitDir } from '@/lib/layout'
import { sessionKey, windowKey } from '@/lib/tree'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'
import { describeError } from '@/stores/toasts'
import InlineRename from './InlineRename.vue'
import AgentMark from './AgentMark.vue'

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
  /** A new queue on the session's directory, shown in the Queue panel. */
  createQueue: [session: Session]
  selectWindow: [name: string, window: string, pane?: string]
  editCancel: [name: string]
  hide: [name: string, hidden: boolean]
}>()

const item = 'touch-target flex min-h-11 items-center cursor-pointer rounded px-2 py-1 outline-none data-highlighted:bg-bg'
const openMenuName = ref('')
let skipMenuCloseFocus = false
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
function agentLabel(agent: 'codex' | 'claude') { return agent === 'codex' ? 'Codex running' : 'Claude Code running' }
function statusLabel(status: Session['status']) {
  if (status === 'working') return 'Working'
  if (status === 'blocked') return 'Blocked or waiting'
  if (status === 'ended') return 'Ended'
  return ''
}
function statusEmoji(status: Session['status']) {
  if (status === 'working') return '🟢'
  if (status === 'blocked') return '🚧'
  if (status === 'ended') return '🎯'
  return ''
}
function sessionLabel(s: Session) {
  return [s.name, statusLabel(s.status), ...(s.agents ?? []).map(agentLabel), ...(isHidden(s.name) ? ['hidden'] : [])].filter(Boolean).join(', ')
}
function visibleAgents(s: Session): ('codex' | 'claude')[] {
  if (!props.treeView) return []
  return (['codex', 'claude'] as const).filter((agent) => s.agents?.includes(agent))
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

// The click that ends a long press is swallowed (selectSession). If it
// never comes, closing the menu ends the gesture so the next tap works.
watch(openMenuName, (name) => {
  if (!name) longPressedName.value = ''
})

// Hiding unmounts the row with its menu still open: clear the menu first
// so Show hidden doesn't remount the row with the menu open again.
function hideFromMenu(name: string) {
  openMenuName.value = ''
  emit('hide', name, isSessionHidden(name))
}

function selectSession(name: string) {
  if (longPressedName.value === name) {
    longPressedName.value = ''
    return
  }
  emit('select', name)
}

function selectRowClick(event: MouseEvent, name: string) {
  const target = event.target
  if (!(target instanceof Element)) return
  if (target.closest('button, a, input, select, textarea, [role="menuitem"], [data-tree-key^="window:"], [data-tree-key^="pane:"]')) return
  selectSession(name)
}

function commitRename(name: string, value: string) {
  return props.commitEdit ? props.commitEdit(name, value) : Promise.resolve()
}

function startRenameFromMenu(name: string) {
  skipMenuCloseFocus = true
  emit('rename', name)
}

function onMenuCloseAutoFocus(event: Event) {
  if (!skipMenuCloseFocus) return
  skipMenuCloseFocus = false
  event.preventDefault()
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
    class="px-1.5 py-0.5 text-xs text-muted"
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
    :force-fallback="true"
    :fallback-on-body="true"
    :fallback-tolerance="4"
    :delay="250"
    :delay-on-touch-only="true"
    :touch-start-threshold="4"
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
      :aria-label="props.treeView ? sessionLabel(s) : undefined"
      :tabindex="props.treeView && props.focusedKey === ('session:' + s.name) ? 0 : props.treeView ? -1 : undefined"
      :data-tree-key="props.treeView ? 'session:' + s.name : undefined"
      :data-tree-kind="props.treeView ? 'session' : undefined"
      :data-tree-group="props.treeView ? props.groupKey : undefined"
      class="tree-row relative flex cursor-pointer flex-wrap items-center gap-x-1.5 rounded-r border-l-[3px] py-0.5 pr-1 pl-1 hover:bg-tree-header"
      :class="[s.name === props.selected ? 'border-accent bg-selected' : 'border-transparent', isHidden(s.name) ? 'opacity-50' : '']"
      @click="selectRowClick($event, s.name)"
    >
      <!-- Agent logos and status are display-only prefixes; the session's actual name stays unchanged. -->
      <!-- Two fixed slots (logo, status) stay reserved even when empty, so names line up. -->
      <span
        v-if="props.treeView"
        data-session-prefix
        class="flex shrink-0 items-center gap-x-1"
      >
        <span
          data-agent-slot
          class="flex min-w-3.5 shrink-0 items-center gap-x-0.5"
        >
          <AgentMark
            v-for="agent in visibleAgents(s)"
            :key="agent"
            :agent="agent"
          />
        </span>
        <span
          data-status-slot
          class="inline-flex w-4 shrink-0 items-center justify-center"
        >
          <span
            v-if="s.status"
            role="img"
            data-session-status
            :data-status="s.status"
            :aria-label="statusLabel(s.status)"
            :title="statusLabel(s.status)"
            class="text-xs leading-none"
          >{{ statusEmoji(s.status) }}</span>
        </span>
      </span>
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
        class="touch-target min-h-6 min-w-[8ch] flex-1 cursor-pointer truncate text-left font-semibold"
        :class="s.name === props.selected ? 'text-selected-fg' : 'text-fg'"
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
      <div data-session-actions class="flex shrink-0 items-center">
        <button
          v-if="canExpand(s)"
          type="button"
          class="row-action touch-target inline-flex min-h-7 min-w-6 shrink-0 items-center justify-center rounded text-muted"
          :aria-label="(isExpanded(sessionKey('host', s.name)) ? 'Collapse ' : 'Expand ') + s.name"
          :aria-expanded="isExpanded(sessionKey('host', s.name))"
          :title="(isExpanded(sessionKey('host', s.name)) ? 'Collapse ' : 'Expand ') + s.name"
          :tabindex="-1"
          @click.stop="toggleSession(s.name)"
        >
          <ChevronRight :size="16" class="transition-transform" :class="isExpanded(sessionKey('host', s.name)) ? 'rotate-90' : ''" aria-hidden="true" />
        </button>
        <span class="flex shrink-0 items-center">
        <DropdownMenuRoot :open="openMenuName === s.name" @update:open="(open) => openMenuName = open ? s.name : ''">
          <DropdownMenuTrigger
            :aria-label="`More actions for ${s.name}`"
            title="More"
            :tabindex="props.treeView ? -1 : undefined"
            class="row-action touch-target inline-flex min-h-6 min-w-5 items-center justify-center rounded text-muted hover:text-fg"
          >
            ⋯
          </DropdownMenuTrigger>
          <DropdownMenuPortal>
            <DropdownMenuContent
              align="end"
              :side-offset="4"
              class="z-[60] min-w-48 rounded border border-border bg-surface p-1 text-fg shadow-lg"
              @close-auto-focus="onMenuCloseAutoFocus"
            >
              <DropdownMenuItem
                v-if="props.treeView"
                :class="item"
                @select="hideFromMenu(s.name)"
              >
                {{ isSessionHidden(s.name) ? 'Unhide' : 'Hide' }}
              </DropdownMenuItem>
              <DropdownMenuItem
                :class="item"
                @select="startRenameFromMenu(s.name)"
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
              <DropdownMenuItem
                :class="item"
                @select="emit('createQueue', s)"
              >
                Create queue
              </DropdownMenuItem>
              <DropdownMenuItem
                :class="[item, 'text-danger']"
                @select="emit('kill', s.name)"
              >
                Kill…
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuPortal>
        </DropdownMenuRoot>
        </span>
        <button
          v-if="props.sortable"
          type="button"
          class="row-action session-drag-handle touch-target inline-flex min-h-6 min-w-4 shrink-0 items-center justify-center cursor-grab rounded text-muted"
          :aria-label="`Drag to reorder session ${s.name}`"
          title="Drag to reorder sessions"
          :tabindex="props.treeView ? -1 : undefined"
        >
          ⠿
        </button>
      </div>
      <ul v-if="canExpand(s) && isExpanded(sessionKey('host', s.name))" role="group" class="ml-2 basis-[calc(100%-0.5rem)] border-l border-border py-0 pl-1.5">
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
            <ul v-if="window.panes.length > 1 && isExpanded(windowKey('host', s.name, window.id))" role="group" class="ml-2 basis-[calc(100%-0.5rem)] border-l border-border py-0 pl-1.5">
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
