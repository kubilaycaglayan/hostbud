<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { panesOf, type Tab } from '@/lib/layout'

const props = defineProps<{ tabs: Tab[]; active: string | null; compact?: boolean }>()
const emit = defineEmits<{ activate: [id: string]; close: [id: string]; reorder: [ids: string[]] }>()

const list = ref<InstanceType<typeof VueDraggable>>()

/** The tabs themselves drag to a new position (M8 T4): no handle, no
 * restyling. Only the order is emitted; the active tab stays as it is. */
const sortableTabs = computed({
  get: () => props.tabs,
  set: (items: Tab[]) => emit('reorder', items.map((t) => t.id)),
})

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
  const el = list.value?.$el as HTMLElement | undefined
  el?.querySelector<HTMLElement>(`[data-tab="${tab.id}"]`)?.focus()
}

function onAuxClick(ev: MouseEvent, id: string) {
  if (ev.button !== 1) return // middle click closes
  ev.preventDefault()
  emit('close', id)
}
</script>

<template>
  <!-- Fallback dragging (Sortable's own pointer handling) behaves the same
       in every browser, lets a click through below the tolerance and
       swallows the click that ends a drag. On touch, a short hold starts a
       drag so a swipe still scrolls the bar. The drag ghost lives on <body>
       (fallback-on-body), outside the tablist, so the list never holds two
       tabs with one name. -->
  <VueDraggable
    ref="list"
    v-model="sortableTabs"
    tag="div"
    role="tablist"
    aria-label="Open terminals"
    class="flex shrink-0 overflow-x-auto bg-surface"
    :class="props.compact ? 'min-w-0 flex-1 border-0' : 'border-b border-border'"
    :animation="150"
    :force-fallback="true"
    :fallback-on-body="true"
    :fallback-tolerance="4"
    :delay="250"
    :delay-on-touch-only="true"
    :touch-start-threshold="4"
    direction="horizontal"
  >
    <div
      v-for="(t, i) in sortableTabs"
      :key="t.id"
      :data-tab-item="t.id"
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
  </VueDraggable>
</template>
