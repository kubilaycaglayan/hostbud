<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { VueDraggable } from 'vue-draggable-plus'
import { computed } from 'vue'
import type { Session } from '@/api/types'
import type { SplitDir } from '@/lib/layout'

const props = defineProps<{
  sessions: Session[]
  selected?: string
  sortable?: boolean
  listLabel?: string
}>()
const emit = defineEmits<{
  select: [name: string]
  /** Open in a new pane beside the active tab's focused pane. */
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
  reorder: [names: string[]]
}>()

const item = 'cursor-pointer rounded px-2 py-1 outline-none data-highlighted:bg-bg'
const sortableSessions = computed({
  get: () => props.sessions,
  set: (items: Session[]) => emit('reorder', items.map((s) => s.name)),
})
function moveSession(name: string, direction: -1 | 1) {
  const names = props.sessions.map((s) => s.name)
  const index = names.indexOf(name)
  const target = index + direction
  if (index < 0 || target < 0 || target >= names.length) return
  names.splice(target, 0, ...names.splice(index, 1))
  emit('reorder', names)
}
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
    class="flex flex-col gap-1"
  >
    <li
      v-for="s in sortableSessions"
      :key="s.name"
      class="flex items-center gap-2 rounded px-2 py-1"
      :class="s.name === props.selected ? 'bg-bg' : ''"
    >
      <button
        v-if="props.sortable"
        type="button"
        class="session-drag-handle min-h-11 min-w-8 cursor-grab rounded text-muted"
        :aria-label="`Reorder ${s.name}`"
        title="Reorder"
      >
        ⠿
      </button>
      <span
        v-if="props.sortable"
        class="flex"
      >
        <button
          type="button"
          class="min-h-11 min-w-8 text-xs text-muted"
          :aria-label="`Move session ${s.name} up`"
          :disabled="props.sessions[0]?.name === s.name"
          @click="moveSession(s.name, -1)"
        >▲</button>
        <button
          type="button"
          class="min-h-11 min-w-8 text-xs text-muted"
          :aria-label="`Move session ${s.name} down`"
          :disabled="props.sessions.at(-1)?.name === s.name"
          @click="moveSession(s.name, 1)"
        >▼</button>
      </span>
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
        class="min-w-0 flex-1 truncate py-1 text-left"
        @click="emit('select', s.name)"
      >
        {{ s.name }}
      </button>
      <span class="flex shrink-0 items-center gap-1">
        <button
          type="button"
          :aria-label="`Kill ${s.name}`"
          title="Kill"
          class="min-h-11 min-w-10 rounded px-1 text-muted hover:text-danger"
          @click="emit('kill', s.name)"
        >
          ✕
        </button>
        <DropdownMenuRoot>
          <DropdownMenuTrigger
            :aria-label="`More actions for ${s.name}`"
            title="More"
            class="min-h-11 min-w-10 rounded px-1 text-muted hover:text-fg"
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
            </DropdownMenuContent>
          </DropdownMenuPortal>
        </DropdownMenuRoot>
        <button
          type="button"
          :aria-label="`Rename ${s.name}`"
          title="Rename"
          class="min-h-11 min-w-10 rounded px-1 text-muted hover:text-fg"
          @click="emit('rename', s.name)"
        >
          ✎
        </button>
      </span>
    </li>
  </VueDraggable>
</template>
