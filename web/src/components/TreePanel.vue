<script setup lang="ts">
import { FolderPlus, SquareTerminal } from 'lucide-vue-next'
import SessionTree from './SessionTree.vue'
import { ref } from 'vue'
import type { SplitDir } from '@/lib/layout'
import type { Project } from '@/api/types'

defineProps<{ selected?: string; connectionState: string }>()
const emit = defineEmits<{
  select: [name: string]
  selectWindow: [name: string, window: string, pane?: string]
  split: [name: string, dir: SplitDir]
  kill: [name: string]
  removeProject: [id: string]
  sessionInProject: [project: Project]
  create: []
  browse: []
}>()
const tree = ref<InstanceType<typeof SessionTree>>()
defineExpose({
  revealProject: (id: string, rename = false) => tree.value?.revealProject(id, rename),
  revealSession: (name: string, rename = false) => tree.value?.revealSession(name, rename),
})
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="Sessions">
    <div class="flex items-center gap-2">
      <button type="button" aria-label="New session" title="New session" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border focus-visible:ring-2 focus-visible:ring-accent" @click="emit('create')">
        <SquareTerminal :size="18" aria-hidden="true" />
      </button>
      <button type="button" aria-label="Add project" title="Add project" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border focus-visible:ring-2 focus-visible:ring-accent" @click="emit('browse')">
        <FolderPlus :size="18" aria-hidden="true" />
      </button>
    </div>
    <p v-if="connectionState === 'reconnecting' || connectionState === 'connecting'" role="status" class="mt-1 text-muted">
      {{ connectionState === 'connecting' ? 'Connecting…' : 'Reconnecting…' }}
    </p>
    <div class="mt-3 min-h-0 flex-1 overflow-y-auto">
      <SessionTree
        ref="tree"
        :selected="selected"
        @select="emit('select', $event)"
        @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)"
        @split="(name, dir) => emit('split', name, dir)"
        @kill="emit('kill', $event)"
        @remove-project="emit('removeProject', $event)"
        @session-in-project="emit('sessionInProject', $event)"
        @create="emit('create')"
      />
    </div>
  </section>
</template>
