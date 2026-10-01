<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  ComboboxContent, ComboboxEmpty, ComboboxGroup, ComboboxInput, ComboboxItem, ComboboxLabel, ComboboxRoot,
  DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle,
} from 'reka-ui'
import { fuzzyFilter } from '@/lib/fuzzy'
import type { PaletteGroup, PaletteItem } from '@/lib/palette'

const props = defineProps<{ open: boolean; items: PaletteItem[]; placeholder?: string }>()
const emit = defineEmits<{
  'update:open': [open: boolean]
  select: [id: string]
}>()
const query = ref('')
const selected = ref<string>()
const input = ref<InstanceType<typeof ComboboxInput>>()
let preventFocusRestore = false
const groups = ['Sessions', 'Windows', 'Projects', 'Create', 'Open', 'Organize', 'Terminal', 'Appearance', 'Account', 'Destructive'] as const
const groupColors: Record<PaletteGroup, string> = {
  Sessions: 'var(--hb-ok)',
  Windows: 'var(--hb-accent)',
  Projects: 'color-mix(in srgb, var(--hb-accent) 55%, var(--hb-danger))',
  Create: 'var(--hb-ok)',
  Open: 'var(--hb-accent)',
  Organize: 'var(--hb-warning)',
  Terminal: 'color-mix(in srgb, var(--hb-accent) 55%, var(--hb-ok))',
  Appearance: 'color-mix(in srgb, var(--hb-accent) 55%, var(--hb-danger))',
  Account: 'var(--hb-muted)',
  Destructive: 'var(--hb-danger)',
}
const filtered = computed(() => fuzzyFilter(query.value, props.items, 50))
const filteredByGroup = computed(() => Object.fromEntries(groups.map((group) => [group, filtered.value.filter((item) => item.group === group)])) as Record<PaletteGroup, PaletteItem[]>)
const visibleGroups = computed(() => groups.filter((group) => filteredByGroup.value[group].length > 0))

watch(() => props.open, (open) => {
  if (open) {
    query.value = ''
    selected.value = undefined
    void nextTick(() => (input.value?.$el as HTMLInputElement | undefined)?.focus())
  }
})
watch(() => props.placeholder, () => { query.value = '' })
// Reka re-highlights only when the list goes from empty to not empty. When
// the highlighted item is filtered out (a paste, say), Enter runs the first
// match instead of doing nothing.
function onEnter(event: KeyboardEvent) {
  const list = (event.target as HTMLElement).closest('[role="dialog"]')
  if (event.isComposing) return
  const highlighted = list?.querySelector<HTMLElement>('[role="option"][data-highlighted]')
  if (highlighted) {
    const id = highlighted.dataset.paletteId
    if (id) {
      event.preventDefault()
      select(id)
    }
    return
  }
  const first = groups.flatMap((group) => filteredByGroup.value[group])[0]
  if (!first) return
  event.preventDefault()
  select(first.id)
}

function onHorizontalNavigation(event: KeyboardEvent) {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
  const currentInput = event.currentTarget as HTMLInputElement
  const content = currentInput.closest('[role="dialog"]')?.querySelector<HTMLElement>('[data-palette-items]')
  const current = content?.querySelector<HTMLElement>('[role="option"][data-highlighted]')
  const currentGroup = current?.closest<HTMLElement>('[data-palette-group]')
  if (!content || !current || !currentGroup) return
  const groupElements = [...content.querySelectorAll<HTMLElement>('[data-palette-group]')]
  const currentGroupIndex = groupElements.indexOf(currentGroup)
  if (currentGroupIndex < 0 || groupElements.length < 2) return

  const direction = event.key === 'ArrowRight' ? 1 : -1
  const nextGroupIndex = (currentGroupIndex + direction + groupElements.length) % groupElements.length
  const currentGroupItems = [...currentGroup.querySelectorAll<HTMLElement>('[role="option"]')]
  const nextGroup = groupElements[nextGroupIndex]
  const nextGroupItems = [...nextGroup.querySelectorAll<HTMLElement>('[role="option"]')]
  const localIndex = currentGroupItems.indexOf(current)
  const next = nextGroupItems[Math.min(Math.max(localIndex, 0), nextGroupItems.length - 1)]
  if (!next) return
  const allItems = groupElements.flatMap((group) => [...group.querySelectorAll<HTMLElement>('[role="option"]')])
  const distance = allItems.indexOf(next) - allItems.indexOf(current)
  if (!distance) return

  event.preventDefault()
  event.stopPropagation()
  const key = distance > 0 ? 'ArrowDown' : 'ArrowUp'
  for (let i = 0; i < Math.abs(distance); i++) {
    currentInput.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: false, cancelable: true }))
  }
}

function select(id?: string) {
  if (!id) return
  if (id !== 'action:split-right' && id !== 'action:split-down') preventFocusRestore = true
  emit('select', id)
}

function onCloseAutoFocus(event: Event) {
  if (!preventFocusRestore) return
  preventFocusRestore = false
  event.preventDefault()
}
</script>

<template>
  <DialogRoot :open="props.open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-50 bg-overlay" />
      <DialogContent
        aria-label="Command palette"
        class="fixed left-1/2 top-[min(12vh,5rem)] z-50 max-h-[84vh] w-[min(96vw,68rem)] -translate-x-1/2 overflow-hidden rounded-lg border border-border bg-surface text-fg shadow-xl"
        @keydown.esc.stop="emit('update:open', false)"
        @close-auto-focus="onCloseAutoFocus"
      >
        <DialogTitle class="sr-only">Command palette</DialogTitle>
        <DialogDescription class="sr-only">Search sessions, windows, projects and commands grouped by purpose.</DialogDescription>
        <ComboboxRoot
          v-model="selected"
          :ignore-filter="true"
          :open-on-focus="true"
          @update:model-value="select"
        >
          <ComboboxInput
            ref="input"
            :placeholder="props.placeholder ?? 'Type a session, project or command…'"
            aria-label="Command palette"
            autocomplete="off"
            class="h-14 w-full border-b border-border bg-surface px-4 text-base text-fg outline-none placeholder:text-muted focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent"
            @update:model-value="query = String($event ?? '')"
            @keydown.enter="onEnter"
            @keydown="onHorizontalNavigation"
          />
          <ComboboxContent class="max-h-[calc(84vh-3.5rem)] overflow-y-auto p-2 outline-none">
            <ComboboxEmpty class="p-4 text-sm text-muted">No matching sessions, projects or commands.</ComboboxEmpty>
            <div data-palette-items class="grid grid-cols-1 gap-2 min-[700px]:grid-cols-2 min-[1050px]:grid-cols-3">
              <ComboboxGroup v-for="group in visibleGroups" :key="group" class="palette-group min-w-0 rounded-md border border-border/60 p-1" :data-palette-group="group" :style="{ '--palette-group-color': groupColors[group] }">
                <ComboboxLabel class="palette-group-label flex items-center justify-between px-2 py-1 text-xs font-semibold uppercase tracking-wide">
                  <span>{{ group }}</span><span data-palette-count :aria-label="`${filteredByGroup[group].length} ${filteredByGroup[group].length === 1 ? 'item' : 'items'}`" class="rounded-full bg-bg px-1.5 py-0.5 text-[10px] tabular-nums">{{ filteredByGroup[group].length }}</span>
                </ComboboxLabel>
                <ComboboxItem
                  v-for="item in filteredByGroup[group]"
                  :key="item.id"
                  :data-palette-id="item.id"
                  :value="item.id"
                  :text-value="[item.label, item.secondary].filter(Boolean).join(' ')"
                  class="palette-item flex min-h-10 cursor-pointer items-center justify-between gap-2 rounded px-2 text-sm outline-none data-[highlighted]:bg-bg data-[highlighted]:ring-2 data-[highlighted]:ring-accent"
                >
                  <span class="min-w-0 truncate">{{ item.label }}<span v-if="item.hidden" class="ml-2 rounded bg-bg px-1.5 py-0.5 text-xs text-muted">hidden</span><span v-if="item.detail" class="ml-2 text-xs text-muted">{{ item.detail }}</span></span>
                  <kbd v-if="item.shortcut" class="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-xs text-muted">{{ item.shortcut }}</kbd>
                </ComboboxItem>
              </ComboboxGroup>
            </div>
          </ComboboxContent>
        </ComboboxRoot>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.palette-group {
  --palette-group-color: var(--hb-accent);
}

.palette-group-label {
  color: var(--hb-fg);
}

.palette-group-label::before {
  display: inline-block;
  width: 0.45rem;
  height: 0.45rem;
  margin-right: 0.5rem;
  border-radius: 9999px;
  background-color: var(--palette-group-color);
  content: '';
  vertical-align: 0.08rem;
}

.palette-item {
  border-left: 2px solid color-mix(in srgb, var(--palette-group-color) 72%, transparent);
}
</style>
