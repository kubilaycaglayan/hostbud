<script setup lang="ts">
import { useToastsStore } from '@/stores/toasts'

const toasts = useToastsStore()
</script>

<template>
  <section
    aria-label="Notifications"
    class="pointer-events-none fixed right-3 bottom-3 left-3 z-50 flex flex-col items-end gap-2"
  >
    <div
      v-for="t in toasts.toasts"
      :key="t.id"
      role="alert"
      :aria-labelledby="`toast-${t.id}`"
      class="pointer-events-auto w-full max-w-sm rounded border border-danger bg-surface p-3 shadow-lg"
    >
      <div class="flex items-start justify-between gap-2">
        <p
          :id="`toast-${t.id}`"
          class="font-bold text-danger"
        >
          {{ t.title }}
        </p>
        <button
          type="button"
          aria-label="Dismiss"
          class="touch-target text-muted"
          @click="toasts.dismiss(t.id)"
        >
          ✕
        </button>
      </div>
      <p class="mt-1">
        {{ t.message }}
      </p>
      <p
        v-if="t.hint"
        class="mt-1 text-muted"
      >
        {{ t.hint }}
      </p>
    </div>
  </section>
</template>
