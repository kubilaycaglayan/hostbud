<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import AuthView from '@/components/AuthView.vue'
import CreateSessionDialog from '@/components/CreateSessionDialog.vue'
import KillSessionDialog from '@/components/KillSessionDialog.vue'
import RenameSessionDialog from '@/components/RenameSessionDialog.vue'
import HostBanner from '@/components/HostBanner.vue'
import SessionList from '@/components/SessionList.vue'
import TerminalView from '@/components/TerminalView.vue'
import ToastRegion from '@/components/ToastRegion.vue'
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

const creating = ref(false)
const renaming = ref(false)
const killing = ref(false)
const target = ref('') // the session a rename/kill dialog is about

function askRename(name: string) {
  target.value = name
  renaming.value = true
}
function askKill(name: string) {
  target.value = name
  killing.value = true
}
function onRenamed(from: string, to: string) {
  if (app.selected?.name === from) app.select(MACHINE, to)
}
function onKilled(name: string) {
  if (app.selected?.name === name) app.clearSelection()
}

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
    class="flex h-full flex-col"
  >
    <HostBanner :machine="host" />
    <!-- Narrow screens show the list or the open terminal, one at a time. -->
    <div class="flex min-h-0 flex-1">
      <aside
        v-if="app.sidebarOpen"
        aria-label="Sessions"
        class="w-full shrink-0 flex-col border-r border-border bg-surface p-3 md:flex md:w-64"
        :class="app.selected ? 'hidden' : 'flex'"
      >
        <div class="flex items-center justify-between gap-2">
          <h1 class="font-bold text-accent">
            hostbud
          </h1>
          <button
            type="button"
            class="rounded border border-border px-2 py-1"
            @click="creating = true"
          >
            New session
          </button>
        </div>
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
            @rename="askRename"
            @kill="askKill"
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
      <main
        class="min-h-0 min-w-0 flex-1 md:block"
        :class="app.selected ? 'block' : 'hidden'"
      >
        <TerminalView
          v-if="app.selected"
          :key="`${app.selected.machine}/${app.selected.name}`"
          :machine="app.selected.machine"
          :session="app.selected.name"
          @back="app.clearSelection()"
        />
        <p
          v-else
          class="flex h-full items-center justify-center text-muted"
        >
          Select a session to open a terminal.
        </p>
      </main>
    </div>
    <CreateSessionDialog
      v-model:open="creating"
      :machine="MACHINE"
      @created="(name) => app.select(MACHINE, name)"
    />
    <RenameSessionDialog
      v-model:open="renaming"
      :machine="MACHINE"
      :session="target"
      @renamed="onRenamed"
    />
    <KillSessionDialog
      v-model:open="killing"
      :machine="MACHINE"
      :session="target"
      @killed="onKilled"
    />
  </div>
  <ToastRegion />
</template>
