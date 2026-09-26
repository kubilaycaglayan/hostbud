<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from 'vue'
import AuthView from '@/components/AuthView.vue'
import HostBanner from '@/components/HostBanner.vue'
import SessionList from '@/components/SessionList.vue'
import TerminalView from '@/components/TerminalView.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLiveStore } from '@/stores/live'
import { useMachinesStore } from '@/stores/machines'
import { useSessionsStore } from '@/stores/sessions'

const app = useAppStore()
const auth = useAuthStore()
const live = useLiveStore()
const machines = useMachinesStore()
const sessions = useSessionsStore()

// v1 has one machine: the host.
const MACHINE = 'host'
const host = computed(() => machines.byId(MACHINE))
const hostSessions = computed(() => sessions.list(MACHINE))

// Live state only while signed in; the server pushes every change.
watch(
  () => auth.status,
  (s) => (s === 'authenticated' ? live.start() : live.stop()),
)

onMounted(() => {
  auth.check().catch(() => auth.sessionEnded())
})
onUnmounted(() => live.stop())
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
      <p
        v-if="live.state === 'reconnecting' || live.state === 'connecting'"
        role="status"
        class="mt-1 text-muted"
      >
        {{ live.state === 'connecting' ? 'Connecting…' : 'Reconnecting…' }}
      </p>
      <div class="mt-3 min-h-0 flex-1 overflow-y-auto">
        <SessionList
          :sessions="hostSessions"
          :selected="app.selected?.name"
          @select="(name) => app.select(MACHINE, name)"
        />
      </div>
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
    <div class="flex min-w-0 flex-1 flex-col">
      <HostBanner :machine="host" />
      <main class="min-h-0 flex-1">
        <TerminalView
          v-if="app.selected"
          :key="`${app.selected.machine}/${app.selected.name}`"
          :machine="app.selected.machine"
          :session="app.selected.name"
        />
        <p
          v-else
          class="flex h-full items-center justify-center text-muted"
        >
          Select a session to open a terminal.
        </p>
      </main>
    </div>
  </div>
</template>
