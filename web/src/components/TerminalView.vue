<script setup lang="ts">
const props = defineProps<{ machine: string; session: string }>()
</script>

<template>
  <section
    :aria-label="`Terminal: ${props.session}`"
    class="flex h-full flex-col"
  >
    <h2 class="border-b border-border px-3 py-2 font-bold">
      {{ props.session }}
    </h2>
    <div
      data-testid="terminal"
      class="flex-1"
    />
  </section>
</template>
