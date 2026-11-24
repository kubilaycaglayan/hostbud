<script setup lang="ts">
import { computed } from 'vue'
import { Server } from 'lucide-vue-next'
import { useMachinesStore } from '@/stores/machines'
import { useLiveStore } from '@/stores/live'

const machines = useMachinesStore()
const live = useLiveStore()
const health = computed(() => {
  if (live.state !== 'open') return 'unknown'
  if (machines.machines.some((m) => m.status === 'unreachable' || m.status === 'tmux_missing')) return 'error'
  return machines.machines.length && machines.machines.every((m) => m.status === 'ok') ? 'ok' : 'unknown'
})
const description = computed(() => health.value === 'ok' ? 'All servers connected' : health.value === 'error' ? 'A server needs attention' : 'Server status unknown')
</script>

<template>
  <span class="inline-flex" :title="description" :data-server-health="health">
    <Server :size="18" aria-hidden="true" class="server-status-icon" />
    <span class="sr-only">{{ description }}</span>
  </span>
</template>

<style scoped>
[data-server-health='ok'] :deep(rect) { fill: var(--color-ok); }
[data-server-health='error'] :deep(rect) { fill: var(--color-danger); }
</style>
