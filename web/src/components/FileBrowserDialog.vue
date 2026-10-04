<script setup lang="ts">
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { ref, watch } from 'vue'
import FileBrowser from './FileBrowser.vue'
import ServerPicker from './ServerPicker.vue'
import { useMachinesStore } from '@/stores/machines'

// machine is the default server; the picker browses another (V2-M13).
const props = defineProps<{ machine: string; startProjectId?: string; compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ created: [name: string] }>()
const machine = ref(props.machine)
const machines = useMachinesStore()
watch(open, (o) => { if (o) machine.value = props.machine })
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent class="fixed z-40 flex flex-col overflow-hidden border border-border bg-surface text-fg" :class="props.compact ? 'inset-x-0 bottom-0 top-auto max-h-[85dvh] w-full rounded-t-2xl pb-[env(safe-area-inset-bottom)]' : 'left-1/2 top-1/2 max-h-[min(40rem,85vh)] w-[min(38rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'">
        <div class="flex items-start justify-between gap-2 border-b border-border px-3 py-2">
          <div>
            <DialogTitle class="text-base font-bold">
              Browse files
            </DialogTitle>
            <DialogDescription class="text-sm text-muted">
              Choose a directory to open or add as a project.
            </DialogDescription>
          </div>
          <DialogClose
            aria-label="Close file browser"
            title="Close"
            class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border"
          >
            ×
          </DialogClose>
        </div>
        <div v-if="machines.machines.length > 1" class="border-b border-border px-3 py-2">
          <ServerPicker v-model="machine" />
        </div>
        <FileBrowser
          v-if="open"
          :key="machine"
          :machine="machine"
          :start-project-id="props.startProjectId"
          class="min-h-0 flex-1 overflow-y-auto"
          @created="emit('created', $event)"
        />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
