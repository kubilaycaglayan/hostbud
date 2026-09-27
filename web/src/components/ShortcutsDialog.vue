<script setup lang="ts">
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { shortcuts, shortcutLabels, shortcutPlatform } from '@/lib/shortcuts'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [open: boolean] }>()
const platform = shortcutPlatform()
const groups = ['General', 'Tabs', 'Tree'] as const
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-50 bg-overlay" />
      <DialogContent class="fixed left-1/2 top-1/2 z-50 max-h-[85vh] w-[min(92vw,36rem)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
        <div class="flex items-center justify-between gap-3">
          <DialogTitle class="text-lg font-bold">Keyboard shortcuts</DialogTitle>
          <DialogClose aria-label="Close keyboard shortcuts" class="min-h-11 min-w-11 rounded border border-border">×</DialogClose>
        </div>
        <DialogDescription class="mt-1 text-sm text-muted">Shortcuts aren't customizable yet.</DialogDescription>
        <section v-for="group in groups" :key="group" class="mt-5">
          <h2 class="mb-2 text-sm font-semibold">{{ group }}</h2>
          <dl class="space-y-2">
            <div v-for="entry in shortcuts.filter((candidate) => candidate.group === group)" :key="entry.id" class="flex items-center justify-between gap-4 border-b border-border/60 pb-2">
              <dt>{{ entry.label }}<span v-if="entry.bindings.some((binding) => binding.scope === 'global')" class="ml-2 text-xs text-muted">Works in the terminal</span></dt>
              <dd class="flex flex-wrap justify-end gap-1">
                <kbd v-for="label in shortcutLabels(entry, platform)" :key="label" class="rounded border border-border bg-bg px-2 py-1 font-mono text-xs">{{ label }}</kbd>
              </dd>
            </div>
          </dl>
        </section>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
