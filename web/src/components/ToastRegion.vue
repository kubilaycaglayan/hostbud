<script setup lang="ts">
import { useToastsStore } from '@/stores/toasts'

const toasts = useToastsStore()
</script>

<template>
  <section
    aria-label="Notifications"
    class="pointer-events-none fixed inset-x-3 top-3 bottom-3 z-50 flex flex-col justify-between"
  >
    <div class="pointer-events-none flex flex-col items-end gap-2">
      <div
        v-for="t in toasts.toasts.filter((item) => item.placement === 'top-right')"
        :key="t.id"
        :role="t.tone === 'error' || !t.tone ? 'alert' : 'status'"
        :aria-labelledby="`toast-${t.id}`"
        class="pointer-events-auto w-full max-w-sm rounded border bg-surface p-3 shadow-lg"
        :class="t.tone === 'success' ? 'border-ok' : t.tone === 'info' ? 'border-accent' : 'border-danger'"
      >
        <div class="flex items-start justify-between gap-2">
          <p :id="`toast-${t.id}`" class="font-bold" :class="t.tone === 'success' ? 'text-ok' : t.tone === 'info' ? 'text-accent' : 'text-danger'">{{ t.title }}</p>
          <button type="button" aria-label="Dismiss" class="touch-target text-muted" @click="toasts.dismiss(t.id)">✕</button>
        </div>
        <p class="mt-1">{{ t.message }}</p>
        <p v-if="t.hint" class="mt-1 text-muted">{{ t.hint }}</p>
      </div>
    </div>
    <div class="pointer-events-none mt-auto flex flex-col items-end gap-2">
      <div
        v-for="t in toasts.toasts.filter((item) => item.placement !== 'top-right')"
        :key="t.id"
        :role="t.tone === 'error' || !t.tone ? 'alert' : 'status'"
        :aria-labelledby="`toast-${t.id}`"
        class="pointer-events-auto w-full max-w-sm rounded border bg-surface p-3 shadow-lg"
        :class="t.tone === 'success' ? 'border-ok' : t.tone === 'info' ? 'border-accent' : 'border-danger'"
      >
        <div class="flex items-start justify-between gap-2">
          <p :id="`toast-${t.id}`" class="font-bold" :class="t.tone === 'success' ? 'text-ok' : t.tone === 'info' ? 'text-accent' : 'text-danger'">{{ t.title }}</p>
          <button type="button" aria-label="Dismiss" class="touch-target text-muted" @click="toasts.dismiss(t.id)">✕</button>
        </div>
        <p class="mt-1">{{ t.message }}</p>
        <p v-if="t.hint" class="mt-1 text-muted">{{ t.hint }}</p>
      </div>
    </div>
  </section>
</template>
