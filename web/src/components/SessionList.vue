<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { VueDraggable } from 'vue-draggable-plus'
import { computed, onScopeDispose, ref } from 'vue'
import type { Session } from '@/api/types'
import type { SplitDir } from '@/lib/layout'

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
}>()
const emit = defineEmits<{
  select: [name: string]
  /** Open in a new pane beside the active tab's focused pane. */
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
  reorder: [names: string[]]
  saveAsProject: [session: Session]
}>()

const item = 'cursor-pointer rounded px-2 py-1 outline-none data-highlighted:bg-bg'
const openMenuName = ref('')
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
    class="flex flex-col gap-1"
  >
    <li
      v-for="s in sortableSessions"
      :key="s.name"
      :role="props.treeView ? 'treeitem' : 'listitem'"
      :aria-level="props.treeView ? (props.level ?? 1) : undefined"
      :aria-selected="props.treeView && s.name === props.selected ? 'true' : undefined"
      :aria-label="props.treeView ? s.name : undefined"
      :tabindex="props.treeView && props.focusedKey === ('session:' + s.name) ? 0 : props.treeView ? -1 : undefined"
      :data-tree-key="props.treeView ? 'session:' + s.name : undefined"
      :data-tree-kind="props.treeView ? 'session' : undefined"
      :data-tree-group="props.treeView ? props.groupKey : undefined"
      class="flex items-center gap-1 rounded px-1 py-0.5"
      :class="s.name === props.selected ? 'bg-bg' : ''"
    >
      <button
        v-if="props.sortable"
        type="button"
        class="session-drag-handle touch-target cursor-grab rounded text-muted"
        :aria-label="`Drag to reorder session ${s.name}`"
        title="Drag to reorder sessions"
        :tabindex="props.treeView ? -1 : undefined"
      >
        ⠿
      </button>
      <span
        role="img"
        :aria-label="s.attached > 0 ? 'attached' : 'detached'"
        :title="s.attached > 0 ? 'attached' : 'detached'"
        class="inline-block size-2 shrink-0 rounded-full"
        :class="s.attached > 0 ? 'bg-ok' : 'border border-muted'"
      />
      <button
        type="button"
        data-session-row
        :aria-label="s.name"
        :aria-current="s.name === props.selected ? 'true' : undefined"
        :tabindex="props.treeView ? -1 : undefined"
        class="touch-target min-w-[8ch] flex-1 truncate text-left"
        @pointerdown="startLongPress($event, s.name)"
        @pointermove="moveLongPress"
        @pointerup="finishLongPress"
        @pointercancel="finishLongPress"
        @pointerleave="finishLongPress"
        @click="selectSession(s.name)"
      >
        {{ s.name }}
      </button>
      <span class="flex shrink-0 items-center gap-0.5">
        <button
          type="button"
          :aria-label="`Kill ${s.name}`"
          title="Kill"
          :tabindex="props.treeView ? -1 : undefined"
          class="touch-target rounded px-1 text-muted hover:text-danger"
          @click="emit('kill', s.name)"
        >
          ✕
        </button>
        <DropdownMenuRoot :open="openMenuName === s.name" @update:open="(open) => openMenuName = open ? s.name : ''">
          <DropdownMenuTrigger
            :aria-label="`More actions for ${s.name}`"
            title="More"
            :tabindex="props.treeView ? -1 : undefined"
            class="touch-target rounded px-1 text-muted hover:text-fg"
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
          class="touch-target rounded px-1 text-muted hover:text-fg"
          @click="emit('rename', s.name)"
        >
          ✎
        </button>
      </span>
    </li>
  </VueDraggable>
</template>
