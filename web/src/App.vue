<script setup lang="ts">
import { computed, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import AuthView from '@/components/AuthView.vue'
import UnreachableView from '@/components/UnreachableView.vue'
import CreateSessionDialog from '@/components/CreateSessionDialog.vue'
import FileBrowserDialog from '@/components/FileBrowserDialog.vue'
import ProjectSessionDialog from '@/components/ProjectSessionDialog.vue'
import TreePanel from '@/components/TreePanel.vue'
import KillSessionDialog from '@/components/KillSessionDialog.vue'
import RenameSessionDialog from '@/components/RenameSessionDialog.vue'
import HostBanner from '@/components/HostBanner.vue'
import TabBar from '@/components/TabBar.vue'
import TabView from '@/components/TabView.vue'
import { NEW_SESSION_FOR_SPLIT } from '@/components/layoutKeys'
import type { Project } from '@/api/types'
import type { SplitDir } from '@/lib/layout'
import { useMediaQuery, COMPACT_QUERY } from '@/lib/media'
import ToastRegion from '@/components/ToastRegion.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLayoutStore } from '@/stores/layout'
import { useLiveStore } from '@/stores/live'
import { useMachinesStore } from '@/stores/machines'
import { useTreeStore } from '@/stores/tree'
import { useProjectsStore } from '@/stores/projects'
import { FolderPlus } from 'lucide-vue-next'

const app = useAppStore()
const auth = useAuthStore()
const layout = useLayoutStore()
const live = useLiveStore()
const machines = useMachinesStore()
const tree = useTreeStore()
const projects = useProjectsStore()

// v1 has one machine: the host.
const MACHINE = 'host'
const host = computed(() => machines.byId(MACHINE))

const creating = ref(false)
const browsing = ref(false)
const sessionProject = ref<Project | null>(null) // the project a New session here dialog is for
const renaming = ref(false)
const killing = ref(false)
const drawerOpen = ref(false)
const swipeStart = ref<{ x: number; y: number } | null>(null)
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
  drawerOpen.value = false
  if (layout.open(MACHINE, name)) app.showTerminal()
}

/** A list row's "Open in split": beside the active tab's focused pane. */
function openInSplit(name: string, dir: SplitDir) {
  drawerOpen.value = false
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
  drawerOpen.value = false
  splitTarget.value = null
  creating.value = true
}
function newProjectSession(project: Project) {
  drawerOpen.value = false
  sessionProject.value = project
}

function browseFiles() {
  drawerOpen.value = false
  browsing.value = true
}

function showTree() {
  if (compact.value && hasTabs.value) {
    drawerOpen.value = true
    return
  }
  if (!compact.value) app.toggleSidebar()
}

function onDrawerPointerDown(event: PointerEvent) {
  swipeStart.value = { x: event.clientX, y: event.clientY }
}
function onDrawerPointerUp(event: PointerEvent) {
  if (!swipeStart.value) return
  const dx = event.clientX - swipeStart.value.x
  const dy = event.clientY - swipeStart.value.y
  swipeStart.value = null
  if (dx <= -60 && Math.abs(dx) > Math.abs(dy)) drawerOpen.value = false
}
function onCreated(name: string) {
  browsing.value = false
  const t = splitTarget.value
  splitTarget.value = null
  if (t && layout.split(t.pane, t.dir, MACHINE, name)) app.showTerminal()
  else openSession(name)
}

const compact = useMediaQuery(COMPACT_QUERY)
const hasTabs = computed(() => layout.loaded && layout.tabs.length > 0)

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
    await Promise.all([
      layout.load(),
      tree.load(),
      projects.load(MACHINE).catch((error) => console.warn("hostbud: can't load projects", error)),
    ])
    tree.sync()
    if (auth.status === 'authenticated') live.start()
  },
)

// A reload right after a change still finds it saved.
const flushLayout = () => layout.flush()
onMounted(() => {
  void auth.check()
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
  <UnreachableView v-else-if="auth.status === 'unreachable'" />
  <main v-else-if="auth.status === 'server-error'" class="p-4" role="alert">
    {{ auth.serverError }} <button class="underline" type="button" @click="auth.check">Try again</button>
  </main>
  <AuthView v-else-if="auth.status === 'anonymous'" />
  <div
    v-else
    class="flex h-full flex-col"
  >
    <HostBanner :machine="host" />
    <header class="flex min-h-12 items-center gap-3 border-b border-border bg-surface px-3">
      <h1 class="font-bold text-accent">
        hostbud
      </h1>
      <button
        type="button"
        class="min-h-11 rounded px-2"
        aria-label="Show project tree"
        :aria-expanded="compact && hasTabs ? drawerOpen : app.sidebarOpen"
        @click="showTree"
      >
        ☰
      </button>
      <button
        v-if="!compact"
        type="button"
        aria-label="Browse files"
        title="Browse files"
        class="inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border"
        @click="browseFiles"
      >
        <FolderPlus
          :size="18"
          aria-hidden="true"
        />
      </button>
      <div class="ml-auto min-w-0 text-sm text-muted">
        <details v-if="compact" class="relative">
          <summary aria-label="Account" class="flex min-h-11 cursor-pointer list-none items-center rounded border border-border px-3">Account</summary>
          <div class="absolute right-0 top-full z-30 mt-1 w-56 rounded border border-border bg-surface p-2 shadow-lg">
            <p class="truncate px-2 py-2" data-testid="account-email">{{ auth.email }}</p>
            <button type="button" class="min-h-11 w-full rounded px-2 text-left" @click="auth.logout()">Sign out</button>
          </div>
        </details>
        <div v-else class="flex items-center gap-2">
          <span class="max-w-40 truncate">{{ auth.email }}</span>
          <button type="button" class="min-h-11 rounded border border-border px-3" @click="auth.logout()">Sign out</button>
        </div>
      </div>
    </header>
    <div class="flex min-h-0 flex-1">
      <aside
        v-if="!compact && app.sidebarOpen"
        aria-label="Sessions"
        class="flex w-64 shrink-0 flex-col border-r border-border bg-surface p-3"
      >
        <TreePanel :selected="selectedSession" :connection-state="live.state" @select="openSession" @split="openInSplit" @rename="askRename" @kill="askKill" @session-in-project="newProjectSession" @create="newSession" @browse="browseFiles" />
      </aside>
      <main v-if="compact && !hasTabs" class="min-h-0 min-w-0 flex-1 overflow-y-auto bg-surface p-3">
        <TreePanel :selected="selectedSession" :connection-state="live.state" @select="openSession" @split="openInSplit" @rename="askRename" @kill="askKill" @session-in-project="newProjectSession" @create="newSession" @browse="browseFiles" />
      </main>
      <main
        v-else
        class="flex min-h-0 min-w-0 flex-1 flex-col"
      >
        <template v-if="layout.loaded && layout.tabs.length > 0">
          <TabBar
            v-if="!compact"
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
              :narrow="compact"
              :tabs="layout.tabs"
              :active-tab="layout.layout.activeTab"
              @activate-tab="layout.activate"
              @close-tab="closeTab"
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
    <DialogRoot v-if="compact && hasTabs" v-model:open="drawerOpen">
      <DialogPortal>
        <DialogOverlay class="fixed inset-0 z-40 bg-black/50" />
        <DialogContent
          class="fixed inset-y-0 left-0 z-50 flex w-[min(85vw,20rem)] flex-col border-r border-border bg-surface p-3 pt-[max(0.75rem,env(safe-area-inset-top))] text-fg shadow-xl"
          @pointerdown="onDrawerPointerDown"
          @pointerup="onDrawerPointerUp"
        >
          <div class="mb-2 flex items-center justify-between">
            <DialogTitle class="text-base font-bold">Project tree</DialogTitle>
            <DialogClose aria-label="Close project tree" class="min-h-11 min-w-11 rounded border border-border">×</DialogClose>
          </div>
          <DialogDescription class="sr-only">Choose a project or session.</DialogDescription>
          <TreePanel :selected="selectedSession" :connection-state="live.state" @select="openSession" @split="openInSplit" @rename="askRename" @kill="askKill" @session-in-project="newProjectSession" @create="newSession" @browse="browseFiles" />
        </DialogContent>
      </DialogPortal>
    </DialogRoot>
    <CreateSessionDialog
      v-model:open="creating"
      :machine="MACHINE"
      :compact="compact"
      @created="onCreated"
    />
    <FileBrowserDialog
      v-model:open="browsing"
      :machine="MACHINE"
      :compact="compact"
      @created="onCreated"
    />
    <ProjectSessionDialog
      v-model:project="sessionProject"
      @created="onCreated"
    />
    <RenameSessionDialog
      v-model:open="renaming"
      :machine="MACHINE"
      :compact="compact"
      :session="target"
      @renaming="(from, to) => layout.expectRename(MACHINE, from, to)"
      @renamed="(from, to) => layout.renamed(MACHINE, from, to)"
      @update:open="(o) => o || layout.renameAbandoned(MACHINE, target)"
    />
    <KillSessionDialog
      v-model:open="killing"
      :machine="MACHINE"
      :compact="compact"
      :session="target"
      @killed="onKilled"
    />
  </div>
  <ToastRegion />
</template>
