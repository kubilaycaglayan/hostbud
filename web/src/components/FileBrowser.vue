<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { filesystemApi, projectsApi, type FileEntry } from '@/api/client'
import type { Project } from '@/api/types'
import FormError from './FormError.vue'
import { describeError } from '@/stores/toasts'
import { useProjectsStore } from '@/stores/projects'
import { useTreeStore } from '@/stores/tree'
import { FolderOpen, FolderPlus } from 'lucide-vue-next'

const props = defineProps<{ machine: string; startProjectId?: string }>()
const emit = defineEmits<{ created: [name: string] }>()
const path = ref('')
const pathInput = ref('')
const entries = ref<FileEntry[]>([])
const resolvingLinks = ref(new Set<string>())
const linkErrors = ref<Record<string, string>>({})
const projectStore = useProjectsStore()
const projects = computed(() => projectStore.items)
const hidden = ref(false)
const loading = ref(false)
const error = ref<{ message: string; hint?: string } | null>(null)
const folderName = ref('')
const folderError = ref('')
const busy = ref(false)
const sessionProject = ref<Project | null>(null)
const selectedProject = ref<Project | null>(null)
const sessionName = ref('')
const command = ref('')
const recentCommands = ref<string[]>([])
const recentCommandsError = ref('')
const suggestions = computed(() => {
  const query = pathInput.value.trim()
  if (!query || query === path.value) return []
  return entries.value.filter((entry) => entry.kind === 'directory' && entry.path.toLowerCase().includes(query.toLowerCase())).slice(0, 8)
})
const crumbs = computed(() => {
  const parts = path.value.split('/').filter(Boolean)
  return [{ name: '/', path: '/' }, ...parts.map((part, i) => ({ name: part, path: `/${parts.slice(0, i + 1).join('/')}` }))]
})

async function navigate(next: string) {
  loading.value = true
  error.value = null
  try {
    const result = await filesystemApi.list(props.machine, next, hidden.value)
    path.value = result.path
    pathInput.value = result.path
    entries.value = result.entries
  } catch (e) { error.value = describeError(e) }
  finally { loading.value = false }
}
async function initialize() {
  try {
    const home = await filesystemApi.home(props.machine)
    await navigate(home.path)
    await projectStore.load(props.machine)
    useTreeStore().sync()
    const requested = projectStore.items.find((item) => item.id === props.startProjectId)
    if (requested) void beginSession(requested)
  } catch (e) { error.value = describeError(e) }
}
watch(() => props.startProjectId, (id) => {
  const requested = projectStore.items.find((item) => item.id === id)
  if (requested) void beginSession(requested)
})
watch(hidden, () => { if (path.value) void navigate(path.value) })
onMounted(() => { void initialize() })

async function createFolder() {
  const name = folderName.value.trim()
  folderError.value = ''
  if (!name || name === '.' || name === '..' || /[\\/\0]/.test(name)) { folderError.value = 'Use a single folder name.'; return }
  busy.value = true
  try { await filesystemApi.mkdir(props.machine, path.value, name); folderName.value = ''; await navigate(path.value) }
  catch (e) { folderError.value = describeError(e).message }
  finally { busy.value = false }
}

async function resolveLink(entry: FileEntry) {
  if (resolvingLinks.value.has(entry.path)) return
  resolvingLinks.value = new Set(resolvingLinks.value).add(entry.path)
  delete linkErrors.value[entry.path]
  try {
    const result = await filesystemApi.stat(props.machine, entry.path)
    entries.value = entries.value.map((row) => row.path === entry.path ? { ...row, symlinkState: result.symlinkState } : row)
  } catch (e) {
    linkErrors.value[entry.path] = describeError(e).message
  } finally {
    const next = new Set(resolvingLinks.value)
    next.delete(entry.path)
    resolvingLinks.value = next
  }
}

async function beginSession(project: Project) {
  sessionProject.value = project
  sessionName.value = ''
  command.value = ''
  recentCommands.value = []
  recentCommandsError.value = ''
  try {
    const result = await projectsApi.recentCommands(project.id)
    if (sessionProject.value?.id === project.id) recentCommands.value = result.commands
  } catch (e) {
    if (sessionProject.value?.id === project.id) recentCommandsError.value = describeError(e).message
  }
}

async function openProject(entry: FileEntry) {
  const existing = projects.value.find((p) => p.path === entry.path)
  if (existing) { selectedProject.value = existing; return }
  busy.value = true
  try {
    const project = await projectsApi.create(props.machine, entry.path, entry.name)
    const prior = projects.value.find((p) => p.path === project.path)
    if (prior) { selectedProject.value = prior; return }
    projectStore.remember(project)
    selectedProject.value = project
  } catch (e) { error.value = describeError(e) }
  finally { busy.value = false }
}
async function createSession() {
  if (!sessionProject.value) return
  busy.value = true
  error.value = null
  try {
    const startCommand = command.value.trim() ? command.value : undefined
    const result = await projectsApi.createSession(sessionProject.value.id, { name: sessionName.value.trim() || undefined, startCommand })
    sessionProject.value = null
    recentCommands.value = []
    emit('created', result.name)
  } catch (e) { error.value = describeError(e) }
  finally { busy.value = false }
}
</script>

<template>
  <section
    aria-label="File browser"
    class="flex min-h-0 flex-col gap-3 p-3"
  >
    <div
      aria-label="Breadcrumbs"
      class="flex flex-wrap gap-1 text-sm"
    >
      <button
        v-for="crumb in crumbs"
        :key="crumb.path"
        type="button"
        class="min-h-11 rounded px-2 text-accent"
        @click="navigate(crumb.path)"
      >
        {{ crumb.name }}
      </button>
    </div>
    <form
      class="relative flex gap-2"
      @submit.prevent="navigate(pathInput)"
    >
      <label
        class="sr-only"
        for="browser-path"
      >Current path</label>
      <input
        id="browser-path"
        v-model="pathInput"
        role="combobox"
        aria-autocomplete="list"
        :aria-expanded="suggestions.length > 0"
        class="min-w-0 flex-1 rounded border border-border bg-bg px-3 py-2 text-base"
        @keydown.down.prevent="suggestions[0] && (pathInput = suggestions[0].path)"
        @keydown.enter.prevent="suggestions.length ? (pathInput = suggestions[0].path, navigate(pathInput)) : navigate(pathInput)"
      >
      <button
        class="min-h-11 rounded border border-border px-3"
        type="submit"
      >
        Go
      </button>
      <ul
        v-if="suggestions.length"
        role="listbox"
        class="absolute left-0 right-14 top-full z-10 border border-border bg-surface"
      >
        <li
          v-for="entry in suggestions"
          :key="entry.path"
        >
          <button
            type="button"
            role="option"
            class="min-h-11 w-full px-3 text-left"
            @click="pathInput = entry.path; navigate(entry.path)"
          >
            {{ entry.path }}
          </button>
        </li>
      </ul>
    </form>
    <label class="flex min-h-11 items-center gap-2"><input
      v-model="hidden"
      type="checkbox"
    > Show hidden files</label>
    <p
      v-if="loading"
      role="status"
    >
      Loading directory…
    </p>
    <FormError
      v-if="error"
      id="browser-error"
      title="Couldn't open this directory"
      :message="error.message"
      :hint="error.hint"
    />
    <ul
      v-if="!loading"
      class="min-h-0 flex-1 overflow-y-auto"
      aria-label="Directory entries"
    >
      <li
        v-for="entry in entries"
        :key="entry.path"
        class="flex flex-wrap items-center gap-2 border-b border-border py-2"
      >
        <button
          v-if="entry.kind === 'directory'"
          type="button"
          class="min-h-11 min-w-0 flex-1 truncate text-left"
          @click="navigate(entry.path)"
        >
          {{ entry.name }}/
        </button>
        <span
          v-else
          class="min-w-0 flex-1 truncate"
        >{{ entry.name }}<span
          v-if="entry.symlinkState"
          class="text-muted"
        > ({{ entry.symlinkState }})</span></span>
        <button
          v-if="entry.kind === 'symlink' && entry.symlinkState === 'unresolved'"
          type="button"
          :aria-label="`Check link ${entry.name}`"
          class="min-h-11 rounded border border-border px-3"
          :disabled="resolvingLinks.has(entry.path)"
          @click="resolveLink(entry)"
        >
          {{ resolvingLinks.has(entry.path) ? 'Checking link…' : 'Check link' }}
        </button>
        <span
          v-if="linkErrors[entry.path]"
          role="alert"
          class="w-full text-danger"
        >
          {{ linkErrors[entry.path] }}
        </span>
        <button
          v-if="entry.kind === 'directory'"
          type="button"
          :disabled="busy"
          class="inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border"
          :aria-label="projects.some((p) => p.path === entry.path) ? `Open project ${entry.name}` : `Add ${entry.name} as project`"
          :title="projects.some((p) => p.path === entry.path) ? `Open project ${entry.name}` : `Add ${entry.name} as project`"
          @click="openProject(entry)"
        >
          <FolderOpen
            v-if="projects.some((p) => p.path === entry.path)"
            :size="18"
            aria-hidden="true"
          />
          <FolderPlus
            v-else
            :size="18"
            aria-hidden="true"
          />
        </button>
      </li>
      <li
        v-if="!entries.length && !loading && !error"
        class="py-3 text-muted"
      >
        This directory is empty.
      </li>
    </ul>
    <form
      class="flex gap-2"
      @submit.prevent="createFolder"
    >
      <label
        class="sr-only"
        for="folder-name"
      >New folder name</label><input
        id="folder-name"
        v-model="folderName"
        class="min-w-0 flex-1 rounded border border-border bg-bg px-3 py-2 text-base"
        placeholder="New folder name"
      >
      <button
        type="submit"
        :disabled="busy"
        class="min-h-11 rounded border border-border px-3"
      >
        Create folder
      </button>
    </form>
    <p
      v-if="folderError"
      role="alert"
      class="text-danger"
    >
      {{ folderError }}
    </p>
    <div
      v-if="selectedProject"
      class="flex items-center justify-between gap-2 rounded border border-border p-2"
    >
      <span class="min-w-0 truncate">Project: {{ selectedProject.name }}</span>
      <button
        type="button"
        class="min-h-11 rounded bg-accent px-3 font-bold text-bg"
        @click="beginSession(selectedProject)"
      >
        New session here
      </button>
    </div>
    <div
      v-if="sessionProject"
      role="dialog"
      aria-modal="true"
      aria-label="New session here"
      class="fixed inset-0 z-30 flex items-center justify-center bg-black/50 p-4"
    >
      <form
        class="w-full max-w-md rounded border border-border bg-surface p-4"
        @submit.prevent="createSession"
      >
        <h2 class="font-bold">
          New session in {{ sessionProject.name }}
        </h2>
        <label class="mt-3 block">Name <input
          v-model="sessionName"
          class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3"
        ></label>
        <label class="mt-3 block">Start command <input
          v-model="command"
          class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3"
        ></label>
        <p
          v-if="recentCommandsError"
          role="status"
          class="mt-1 text-sm text-muted"
        >
          Recent commands couldn't be loaded: {{ recentCommandsError }}
        </p>
        <div
          v-if="recentCommands.length"
          class="mt-2"
        >
          <p class="text-sm text-muted">
            Recent commands for this project
          </p>
          <ul class="mt-1 max-h-32 overflow-y-auto rounded border border-border">
            <li
              v-for="recent in recentCommands"
              :key="recent"
            >
              <button
                type="button"
                class="min-h-11 w-full truncate px-2 text-left text-sm hover:bg-bg"
                :aria-label="`Use recent command ${recent}`"
                @click="command = recent"
              >
                {{ recent }}
              </button>
            </li>
          </ul>
        </div>
        <p class="mt-2 text-sm text-muted">
          Directory: {{ sessionProject.path }}
        </p>
        <div class="mt-4 flex justify-end gap-2">
          <button
            type="button"
            class="min-h-11 px-3"
            @click="sessionProject = null"
          >
            Cancel
          </button><button
            type="submit"
            :disabled="busy"
            class="min-h-11 rounded bg-accent px-3 font-bold text-bg"
          >
            Create session
          </button>
        </div>
      </form>
    </div>
  </section>
</template>
