<script setup lang="ts">
import type { Session } from '@/api/types'

const props = defineProps<{
  sessions: Session[]
  selected?: string
}>()
const emit = defineEmits<{ select: [name: string] }>()

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
    </li>
  </ul>
</template>
