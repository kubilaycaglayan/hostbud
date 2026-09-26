<script setup lang="ts">
import { FolderPlus } from 'lucide-vue-next'
import SessionTree from './SessionTree.vue'
import type { SplitDir } from '@/lib/layout'
import type { Project } from '@/api/types'

defineProps<{ selected?: string; connectionState: string }>()
const emit = defineEmits<{
  select: [name: string]
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
  sessionInProject: [project: Project]
  create: []
  browse: []
}>()
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="Sessions">
    <h2 class="mb-2 px-1 text-sm font-semibold">Projects &amp; sessions</h2>
    <div class="flex items-center gap-2">
      <button type="button" class="min-h-10 min-w-0 whitespace-nowrap rounded border border-border px-2 text-xs" @click="emit('create')">
        New session
      </button>
      <button type="button" aria-label="Browse files" title="Browse files" class="inline-flex min-h-10 min-w-10 items-center justify-center rounded border border-border" @click="emit('browse')">
        <FolderPlus :size="18" aria-hidden="true" />
      </button>
    </div>
    <p v-if="connectionState === 'reconnecting' || connectionState === 'connecting'" role="status" class="mt-1 text-muted">
      {{ connectionState === 'connecting' ? 'Connecting…' : 'Reconnecting…' }}
    </p>
    <div class="mt-3 min-h-0 flex-1 overflow-y-auto">
      <SessionTree
        :selected="selected"
        @select="emit('select', $event)"
        @split="(name, dir) => emit('split', name, dir)"
        @rename="emit('rename', $event)"
        @kill="emit('kill', $event)"
        @session-in-project="emit('sessionInProject', $event)"
      />
    </div>
  </section>
</template>
