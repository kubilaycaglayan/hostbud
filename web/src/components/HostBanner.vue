<script setup lang="ts">
import { computed } from 'vue'
import type { Machine } from '@/api/types'

const props = defineProps<{ machine?: Machine }>()

// Shown for problems the owner has to fix on the host.
const title = computed(() => {
  switch (props.machine?.status) {
    case 'unreachable':
      return 'Host unreachable'
    case 'tmux_missing':
      return 'tmux not found on the host'
    default:
      return ''
  }
})
</script>

<template>
  <div
    v-if="title"
    role="alert"
    class="border-b border-danger bg-surface px-3 py-2"
  >
    <p class="font-bold text-danger">
      {{ title }}
    </p>
    <p
      v-if="props.machine?.error && props.machine.status !== 'tmux_missing'"
      class="mt-1"
    >
      {{ props.machine.error }}
    </p>
    <p
      v-if="props.machine?.hint"
      class="mt-1 text-muted"
    >
      {{ props.machine.hint }}
    </p>
  </div>
</template>
