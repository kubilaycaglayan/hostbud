<script setup lang="ts">
// A compact icon-only button (M8 T1): the visible box is the icon plus 4 px
// padding, while `touch-target` keeps a 44 px hit area on touch screens.
defineProps<{ label: string }>()
</script>

<template>
  <button
    type="button"
    :aria-label="label"
    :title="label"
    class="touch-target group inline-flex shrink-0 items-center justify-center rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
  >
    <span data-icon-box class="inline-flex items-center justify-center rounded border border-border p-1 group-hover:bg-tree-header">
      <slot />
    </span>
  </button>
</template>
