<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import type { Session } from '@/api/types'
import type { SplitDir } from '@/lib/layout'

const props = defineProps<{
  sessions: Session[]
  selected?: string
}>()
const emit = defineEmits<{
  select: [name: string]
  /** Open in a new pane beside the active tab's focused pane. */
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
}>()

const item = 'cursor-pointer rounded px-2 py-1 outline-none data-highlighted:bg-bg'

const windowsLabel = (n: number) => (n === 1 ? '1 window' : `${n} windows`)
</script>

<template>
  <p
    v-if="props.sessions.length === 0"
    class="text-muted"
  >
    No tmux sessions yet.
  </p>
  <ul
    v-else
    aria-label="tmux sessions"
    class="flex flex-col gap-1"
  >
    <li
      v-for="s in props.sessions"
      :key="s.name"
      class="flex items-center gap-2 rounded px-2 py-1"
      :class="s.name === props.selected ? 'bg-bg' : ''"
    >
      <span
        role="img"
        :aria-label="s.attached > 0 ? 'attached' : 'detached'"
        :title="s.attached > 0 ? 'attached' : 'detached'"
        class="inline-block size-2 shrink-0 rounded-full"
        :class="s.attached > 0 ? 'bg-ok' : 'border border-muted'"
      />
      <button
        type="button"
        :aria-label="s.name"
        :aria-current="s.name === props.selected ? 'true' : undefined"
        class="min-w-0 flex-1 truncate py-1 text-left"
        @click="emit('select', s.name)"
      >
        {{ s.name }}
      </button>
      <span class="shrink-0 text-muted">{{ windowsLabel(s.windows) }}</span>
      <DropdownMenuRoot>
        <DropdownMenuTrigger
          :aria-label="`More actions for ${s.name}`"
          title="More"
          class="shrink-0 rounded px-1 text-muted hover:text-fg"
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
        class="shrink-0 rounded px-1 text-muted hover:text-fg"
        @click="emit('rename', s.name)"
      >
        ✎
      </button>
      <button
        type="button"
        :aria-label="`Kill ${s.name}`"
        title="Kill"
        class="shrink-0 rounded px-1 text-muted hover:text-danger"
        @click="emit('kill', s.name)"
      >
        ✕
      </button>
    </li>
  </ul>
</template>
