<script setup lang="ts">
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
  killProjectSessions: [id: string]
  sessionInProject: [project: Project]
  createSessionInProject: [project: Project]
  create: []
}>()
const tree = ref<InstanceType<typeof SessionTree>>()
defineExpose({
  revealProject: (id: string, rename = false) => tree.value?.revealProject(id, rename),
  revealSession: (name: string, rename = false) => tree.value?.revealSession(name, rename),
  showSession: (name: string) => tree.value?.showSession(name),
})
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="Sessions">
    <p v-if="connectionState === 'reconnecting' || connectionState === 'connecting'" role="status" class="mb-1 text-muted">
      {{ connectionState === 'connecting' ? 'Connecting…' : 'Reconnecting…' }}
    </p>
    <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <SessionTree
        ref="tree"
        :selected="selected"
        @select="emit('select', $event)"
        @select-window="(name, window, pane) => emit('selectWindow', name, window, pane)"
        @split="(name, dir) => emit('split', name, dir)"
        @kill="emit('kill', $event)"
        @remove-project="emit('removeProject', $event)"
        @kill-project-sessions="emit('killProjectSessions', $event)"
        @session-in-project="emit('sessionInProject', $event)"
        @create-session-in-project="emit('createSessionInProject', $event)"
        @create="emit('create')"
      />
    </div>
  </section>
</template>
