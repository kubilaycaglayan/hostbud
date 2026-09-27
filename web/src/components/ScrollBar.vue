<script setup lang="ts">
import type { CopyModeAction } from '@/api/client'

const props = defineProps<{ scrollPosition: number; historySize: number; busy?: boolean }>()
const emit = defineEmits<{ action: [action: CopyModeAction, lines?: number] }>()
const buttons: { label: string; text: string; action: CopyModeAction; lines?: number }[] = [
  { label: 'Top', text: '⤒', action: 'top' },
  { label: 'Page up', text: '⇞', action: 'page-up' },
  { label: 'Line up', text: '↑', action: 'scroll-up', lines: 1 },
  { label: 'Line down', text: '↓', action: 'scroll-down', lines: 1 },
  { label: 'Page down', text: '⇟', action: 'page-down' },
  { label: 'Bottom', text: '⤓', action: 'bottom' },
]
</script>

<template>
  <!-- Buttons don't take focus (mousedown.prevent): the terminal keeps the
       keyboard, so typing after Done reaches the shell, as with the key bar. -->
  <section
    aria-label="Scroll history controls"
    class="flex shrink-0 items-center gap-1 overflow-x-auto border-t border-border bg-surface px-1"
    data-testid="scroll-bar"
  >
    <button
      v-for="button in buttons"
      :key="button.action"
      type="button"
      class="touch-target shrink-0 rounded border border-border px-2"
      :aria-label="button.label"
      :disabled="props.busy"
      @mousedown.prevent
      @click="emit('action', button.action, button.lines)"
    >
      {{ button.text }}
    </button>
    <span class="min-w-max px-1 text-xs text-muted" role="status">
      Line {{ Math.max(0, props.historySize - props.scrollPosition) }} of {{ props.historySize }}
    </span>
    <button
      type="button"
      class="touch-target ml-auto shrink-0 rounded bg-accent px-3 font-bold text-bg"
      aria-label="Done"
      :disabled="props.busy"
      @mousedown.prevent
      @click="emit('action', 'exit')"
    >
      Done
    </button>
  </section>
</template>
