<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { panesOf, type Tab } from '@/lib/layout'

const props = defineProps<{ tabs: Tab[]; active: string | null; compact?: boolean }>()
const emit = defineEmits<{ activate: [id: string]; close: [id: string] }>()

const list = ref<HTMLElement>()

/** A tab's label: its focused pane's session (then "+n" for more panes). */
function label(t: Tab): string {
  const panes = panesOf(t.root)
  const main = panes.find((p) => p.id === t.focusedPane) ?? panes[0]
  return panes.length > 1 ? `${main.session} +${panes.length - 1}` : main.session
}

/** Arrow keys, Home and End move between tabs and activate them; Delete
 * closes the focused one. */
async function onKey(ev: KeyboardEvent, i: number) {
  const n = props.tabs.length
  if (ev.key === 'Delete') {
    ev.preventDefault()
    emit('close', props.tabs[i].id)
    return
  }
  const to = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: n - 1 }[ev.key]
  if (to === undefined) return
  ev.preventDefault()
  const tab = props.tabs[(to + n) % n]
  emit('activate', tab.id)
  await nextTick()
  list.value?.querySelector<HTMLElement>(`[data-tab="${tab.id}"]`)?.focus()
}

function onAuxClick(ev: MouseEvent, id: string) {
  if (ev.button !== 1) return // middle click closes
  ev.preventDefault()
  emit('close', id)
}
</script>

<template>
  <div
    ref="list"
    role="tablist"
    aria-label="Open terminals"
    class="flex shrink-0 overflow-x-auto bg-surface"
    :class="props.compact ? 'min-w-0 flex-1 border-0' : 'border-b border-border'"
  >
    <div
      v-for="(t, i) in props.tabs"
      :key="t.id"
      class="flex min-w-0 shrink-0 items-center border-r border-border"
      :class="t.id === props.active ? 'bg-bg' : ''"
      @auxclick="onAuxClick($event, t.id)"
      @mousedown.middle.prevent
    >
      <button
        :id="`tab-${t.id}`"
        type="button"
        role="tab"
        :data-tab="t.id"
        :aria-selected="t.id === props.active"
        :aria-controls="`tabpanel-${t.id}`"
        :tabindex="t.id === props.active ? 0 : -1"
        class="touch-target max-w-48 truncate py-1 pr-1 pl-3 md:py-1.5"
        :class="[props.compact ? 'max-w-20 px-1 text-xs' : '', t.id === props.active ? 'font-bold text-fg' : 'text-muted']"
        @click="emit('activate', t.id)"
        @keydown="onKey($event, i)"
      >
        {{ label(t) }}
      </button>
      <button
        type="button"
        :aria-label="`Close ${label(t)}`"
        title="Close (the session keeps running)"
        tabindex="-1"
        class="touch-target px-2 py-1 text-muted hover:text-fg"
        :class="props.compact ? 'px-1 text-xs' : ''"
        @click="emit('close', t.id)"
      >
        ×
      </button>
    </div>
  </div>
</template>
