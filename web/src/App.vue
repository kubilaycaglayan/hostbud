<script setup lang="ts">
import { onMounted } from 'vue'
import AuthView from '@/components/AuthView.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const app = useAppStore()
const auth = useAuthStore()

onMounted(() => {
  auth.check().catch(() => auth.sessionEnded())
})
</script>

<template>
  <p
    v-if="auth.status === 'loading'"
    class="p-4 text-muted"
  >
    Loading…
  </p>
  <AuthView v-else-if="auth.status === 'anonymous'" />
  <div
    v-else
    class="flex h-full"
  >
    <aside
      v-if="app.sidebarOpen"
      aria-label="Sessions"
      class="flex w-64 shrink-0 flex-col border-r border-border bg-surface p-3"
    >
      <h1 class="font-bold text-accent">
        hostbud
      </h1>
      <p class="mt-2 flex-1 text-muted">
        No sessions yet.
      </p>
      <div class="mt-2 flex items-center justify-between gap-2 text-muted">
        <span class="truncate">{{ auth.email }}</span>
        <button
          type="button"
          class="rounded border border-border px-2 py-1"
          @click="auth.logout()"
        >
          Sign out
        </button>
      </div>
    </aside>
    <main class="flex flex-1 items-center justify-center text-muted">
      Select a session to open a terminal.
    </main>
  </div>
</template>
