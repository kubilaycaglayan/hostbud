<script setup lang="ts">
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import FileBrowser from './FileBrowser.vue'

const props = defineProps<{ machine: string; startProjectId?: string; compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ created: [name: string] }>()
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent class="fixed z-40 flex flex-col overflow-hidden border border-border bg-surface text-fg" :class="props.compact ? 'inset-x-0 bottom-0 top-auto max-h-[90dvh] h-[90dvh] w-full rounded-t-2xl pb-[env(safe-area-inset-bottom)]' : 'left-1/2 top-1/2 max-h-[90vh] w-[min(56rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'">
        <div class="flex items-start justify-between gap-3 border-b border-border p-4">
          <div>
            <DialogTitle class="text-base font-bold">
              Browse files
            </DialogTitle>
            <DialogDescription class="mt-1 text-sm text-muted">
              Choose a directory to open or add as a project.
            </DialogDescription>
          </div>
          <DialogClose
            aria-label="Close file browser"
            title="Close"
            class="touch-target min-h-11 min-w-11 rounded border border-border"
          >
            ×
          </DialogClose>
        </div>
        <FileBrowser
          v-if="open"
          :machine="props.machine"
          :start-project-id="props.startProjectId"
          class="min-h-0 flex-1 overflow-y-auto"
          @created="emit('created', $event)"
        />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
