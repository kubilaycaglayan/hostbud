<script setup lang="ts">
import { computed, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import AuthView from '@/components/AuthView.vue'
import CreateSessionDialog from '@/components/CreateSessionDialog.vue'
import FileBrowser from '@/components/FileBrowser.vue'
import KillSessionDialog from '@/components/KillSessionDialog.vue'
import RenameSessionDialog from '@/components/RenameSessionDialog.vue'
import HostBanner from '@/components/HostBanner.vue'
import SessionList from '@/components/SessionList.vue'
import TabBar from '@/components/TabBar.vue'
import TabView from '@/components/TabView.vue'
import { NEW_SESSION_FOR_SPLIT } from '@/components/layoutKeys'
import type { SplitDir } from '@/lib/layout'
import { useMediaQuery, WIDE_QUERY } from '@/lib/media'
import ToastRegion from '@/components/ToastRegion.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLayoutStore } from '@/stores/layout'
import { useLiveStore } from '@/stores/live'
import { useMachinesStore } from '@/stores/machines'
import { useSessionsStore } from '@/stores/sessions'

const app = useAppStore()
const auth = useAuthStore()
const layout = useLayoutStore()
const live = useLiveStore()
const machines = useMachinesStore()
const sessions = useSessionsStore()

// v1 has one machine: the host.
const MACHINE = 'host'
const host = computed(() => machines.byId(MACHINE))
const hostSessions = computed(() => sessions.list(MACHINE))

const creating = ref(false)
const browsing = ref(false)
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
function onKilled(name: string) {
  layout.closeSession(MACHINE, name)
}

/** Shows a session: its open tab, or a new one. */
function openSession(name: string) {
  if (layout.open(MACHINE, name)) app.showTerminal()
}

/** A list row's "Open in split": beside the active tab's focused pane. */
function openInSplit(name: string, dir: SplitDir) {
  if (layout.splitFocused(dir, MACHINE, name)) app.showTerminal()
}

// "New session…" from a pane's split picker: the created session opens in a
// new pane beside it. The sidebar's New session opens a tab.
const splitTarget = ref<{ pane: string; dir: SplitDir } | null>(null)
provide(NEW_SESSION_FOR_SPLIT, (pane, dir) => {
  splitTarget.value = { pane, dir }
  creating.value = true
})
function newSession() {
  splitTarget.value = null
  creating.value = true
}
function onCreated(name: string) {
  const t = splitTarget.value
  splitTarget.value = null
  if (t && layout.split(t.pane, t.dir, MACHINE, name)) app.showTerminal()
  else openSession(name)
}

const wide = useMediaQuery(WIDE_QUERY, true)
const narrow = computed(() => !wide.value)

function closeTab(id: string) {
  layout.closeTab(id)
  if (layout.tabs.length === 0) app.showList()
}

const selectedSession = computed(() => layout.focused?.session)

// Signed in: the saved layout first (before any terminal mounts), then live
// state; the server pushes every change.
watch(
  () => auth.status,
  async (s) => {
    if (s !== 'authenticated') {
      live.stop()
      layout.reset()
      app.showList()
      return
    }
    await layout.load()
    if (auth.status === 'authenticated') live.start()
  },
)

// A reload right after a change still finds it saved.
const flushLayout = () => layout.flush()
onMounted(() => {
  auth.check().catch(() => auth.sessionEnded())
  window.addEventListener('pagehide', flushLayout)
})
onUnmounted(() => {
  window.removeEventListener('pagehide', flushLayout)
  live.stop()
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
    class="flex h-full flex-col"
  >
    <HostBanner :machine="host" />
    <!-- Narrow screens show the list or the open terminal, one at a time. -->
    <div class="flex min-h-0 flex-1">
      <aside
        v-if="app.sidebarOpen"
        aria-label="Sessions"
        class="w-full shrink-0 flex-col border-r border-border bg-surface p-3 md:flex md:w-64"
        :class="app.terminalShown ? 'hidden' : 'flex'"
      >
        <div class="flex items-center justify-between gap-2">
          <h1 class="font-bold text-accent">
            hostbud
          </h1>
          <button
            type="button"
            class="rounded border border-border px-2 py-1"
            @click="newSession"
          >
            New session
          </button>
        </div>
        <button
          type="button"
          class="mt-2 min-h-11 rounded border border-border px-3"
          @click="browsing = !browsing"
        >
          {{ browsing ? 'Sessions' : 'Browse files' }}
        </button>
        <p
          v-if="live.state === 'reconnecting' || live.state === 'connecting'"
          role="status"
          class="mt-1 text-muted"
        >
          {{ live.state === 'connecting' ? 'Connecting…' : 'Reconnecting…' }}
        </p>
        <FileBrowser
          v-if="browsing"
          :machine="MACHINE"
          class="min-h-0 flex-1 overflow-y-auto"
          @created="onCreated"
        />
        <div
          v-else
          class="mt-3 min-h-0 flex-1 overflow-y-auto"
        >
          <SessionList
            :sessions="hostSessions"
            :selected="selectedSession"
            @select="openSession"
            @split="openInSplit"
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
        class="min-h-0 min-w-0 flex-1 flex-col md:flex"
        :class="app.terminalShown ? 'flex' : 'hidden'"
      >
        <template v-if="layout.loaded && layout.tabs.length > 0">
          <TabBar
            :tabs="layout.tabs"
            :active="layout.layout.activeTab"
            @activate="layout.activate"
            @close="closeTab"
          />
          <!-- Inactive tabs stay mounted and attached (instant switching, and
               their scrollback keeps filling). -->
          <div
            v-for="t in layout.tabs"
            v-show="t.id === layout.layout.activeTab"
            :id="`tabpanel-${t.id}`"
            :key="t.id"
            role="tabpanel"
            :aria-labelledby="`tab-${t.id}`"
            class="min-h-0 flex-1"
          >
            <TabView
              :tab="t"
              :active="t.id === layout.layout.activeTab"
              :narrow="narrow"
              @back="app.showList()"
            />
          </div>
        </template>
        <p
          v-else
          class="flex h-full flex-1 items-center justify-center text-muted"
        >
          Select a session to open a terminal.
        </p>
      </main>
    </div>
    <CreateSessionDialog
      v-model:open="creating"
      :machine="MACHINE"
      @created="onCreated"
    />
    <RenameSessionDialog
      v-model:open="renaming"
      :machine="MACHINE"
      :session="target"
      @renaming="(from, to) => layout.expectRename(MACHINE, from, to)"
      @renamed="(from, to) => layout.renamed(MACHINE, from, to)"
      @update:open="(o) => o || layout.renameAbandoned(MACHINE, target)"
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
