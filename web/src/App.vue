<script setup lang="ts">
import { useAppStore } from '@/stores/app'

const app = useAppStore()
</script>

<template>
  <div class="flex h-full">
    <aside
      v-if="app.sidebarOpen"
      aria-label="Sessions"
      class="w-64 shrink-0 border-r border-border bg-surface p-3"
    >
      <h1 class="font-bold text-accent">
        hostbud
      </h1>
      <p class="mt-2 text-muted">
        No sessions yet.
      </p>
    </aside>
    <main class="flex flex-1 items-center justify-center text-muted">
      Select a session to open a terminal.
    </main>
  </div>
</template>
