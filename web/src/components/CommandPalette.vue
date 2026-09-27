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
const groups = ['Sessions', 'Windows', 'Projects', 'Actions'] as const
const filtered = computed(() => fuzzyFilter(query.value, props.items, 50))
const filteredByGroup = computed(() => Object.fromEntries(groups.map((group) => [group, filtered.value.filter((item) => item.group === group)])) as Record<PaletteGroup, PaletteItem[]>)

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
  if (event.isComposing || list?.querySelector('[role="option"][data-highlighted]')) return
  const first = groups.flatMap((group) => filteredByGroup.value[group])[0]
  if (!first) return
  event.preventDefault()
  select(first.id)
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
        class="fixed left-1/2 top-[min(20vh,8rem)] z-50 max-h-[75vh] w-[min(92vw,42rem)] -translate-x-1/2 overflow-hidden rounded-lg border border-border bg-surface text-fg shadow-xl"
        @keydown.esc.stop="emit('update:open', false)"
        @close-auto-focus="onCloseAutoFocus"
      >
        <DialogTitle class="sr-only">Command palette</DialogTitle>
        <DialogDescription class="sr-only">Search sessions, windows, projects and actions.</DialogDescription>
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
          />
          <ComboboxContent class="max-h-[calc(75vh-3.5rem)] overflow-y-auto p-2 outline-none">
            <ComboboxEmpty class="p-4 text-sm text-muted">No matching sessions, projects or commands.</ComboboxEmpty>
            <ComboboxGroup v-for="group in groups" :key="group" class="mb-2">
              <template v-if="filteredByGroup[group].length">
                <ComboboxLabel class="px-2 py-1 text-xs font-semibold uppercase tracking-wide text-muted">{{ group }}</ComboboxLabel>
                <ComboboxItem
                  v-for="item in filteredByGroup[group]"
                  :key="item.id"
                  :value="item.id"
                  :text-value="[item.label, item.secondary].filter(Boolean).join(' ')"
                  class="flex min-h-11 cursor-pointer items-center justify-between gap-3 rounded px-2 text-sm outline-none data-[highlighted]:bg-bg data-[highlighted]:ring-2 data-[highlighted]:ring-accent"
                >
                  <span class="min-w-0 truncate">{{ item.label }}<span v-if="item.hidden" class="ml-2 rounded bg-bg px-1.5 py-0.5 text-xs text-muted">hidden</span><span v-if="item.detail" class="ml-2 text-xs text-muted">{{ item.detail }}</span></span>
                  <kbd v-if="item.shortcut" class="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-xs text-muted">{{ item.shortcut }}</kbd>
                </ComboboxItem>
              </template>
            </ComboboxGroup>
          </ComboboxContent>
        </ComboboxRoot>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
