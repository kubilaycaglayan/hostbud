<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { filesystemApi, projectsApi, type FileEntry } from '@/api/client'
import type { Project } from '@/api/types'
import FormError from './FormError.vue'
import ProjectSessionDialog from './ProjectSessionDialog.vue'
import { describeError } from '@/stores/toasts'
import { useProjectsStore } from '@/stores/projects'
import { useTreeStore } from '@/stores/tree'
import { FolderOpen, FolderPlus } from 'lucide-vue-next'

const props = defineProps<{ machine: string }>()
const emit = defineEmits<{ created: [name: string] }>()
const path = ref('')
const pathInput = ref('')
const entries = ref<FileEntry[]>([])
const resolvingLinks = ref(new Set<string>())
const linkErrors = ref<Record<string, string>>({})
const projectStore = useProjectsStore()
const projects = computed(() => projectStore.items)
const currentProject = computed(() => projects.value.find((project) => project.path === path.value))
const hidden = ref(false)
const loading = ref(false)
const error = ref<{ message: string; hint?: string } | null>(null)
const folderName = ref('')
const folderError = ref('')
const busy = ref(false)
let navigationController: AbortController | undefined
const sessionProject = ref<Project | null>(null)
const selectedProject = ref<Project | null>(null)
// Autocomplete: the directories of the typed path's parent whose names
// start with its last segment (`/a/b/ch` → `/a/b/child`).
const typedParent = computed(() => {
  const query = pathInput.value.trim()
  const slash = query.lastIndexOf('/')
  if (!query || query === path.value || slash < 0) return null
  return { parent: slash === 0 ? '/' : query.slice(0, slash), prefix: query.slice(slash + 1).toLowerCase() }
})
const parentEntries = ref<{ path: string; entries: FileEntry[] } | null>(null)
let suggestController: AbortController | undefined
let suggestTimer: ReturnType<typeof setTimeout> | undefined
watch(typedParent, (typed) => {
  clearTimeout(suggestTimer)
  suggestController?.abort()
  if (!typed || typed.parent === path.value || typed.parent === parentEntries.value?.path) return
  suggestTimer = setTimeout(async () => {
    const controller = new AbortController()
    suggestController = controller
    try {
      const result = await filesystemApi.list(props.machine, typed.parent, hidden.value, controller.signal)
      if (!controller.signal.aborted) parentEntries.value = { path: typed.parent, entries: result.entries }
    } catch {
      // No suggestions for a parent that can't be listed; Go explains why.
    }
  }, 150)
})
const suggestions = computed(() => {
  const query = pathInput.value.trim()
  if (!query || query === path.value) return []
  // Directories here whose path contains the text, then the typed parent's.
  const here = entries.value.filter((entry) => entry.kind === 'directory' && entry.path.toLowerCase().includes(query.toLowerCase()))
  const typed = typedParent.value
  const candidates = !typed ? [] : typed.parent === path.value ? entries.value
    : typed.parent === parentEntries.value?.path ? parentEntries.value.entries : []
  const below = candidates.filter((entry) => entry.kind === 'directory' && entry.name.toLowerCase().startsWith(typed!.prefix))
  return [...new Map([...here, ...below].map((entry) => [entry.path, entry])).values()].slice(0, 8)
})
const crumbs = computed(() => {
  const parts = path.value.split('/').filter(Boolean)
  return [{ name: '/', path: '/' }, ...parts.map((part, i) => ({ name: part, path: `/${parts.slice(0, i + 1).join('/')}` }))]
})

async function navigate(next: string) {
  navigationController?.abort()
  const controller = new AbortController()
  navigationController = controller
  loading.value = true
  error.value = null
  try {
    const result = await filesystemApi.list(props.machine, next, hidden.value, controller.signal)
    if (navigationController !== controller) return // a newer navigation won
    path.value = result.path
    pathInput.value = result.path
    entries.value = result.entries
  } catch (e) { if (!controller.signal.aborted) error.value = describeError(e) }
  finally { if (navigationController === controller) loading.value = false }
}
async function initialize() {
  try {
    const home = await filesystemApi.home(props.machine)
    // The user may already have gone somewhere while home was loading.
    if (!navigationController) await navigate(home.path)
    await projectStore.load(props.machine)
    useTreeStore().sync()
  } catch (e) { error.value = describeError(e) }
}
watch(hidden, () => { if (path.value) void navigate(path.value) })
onMounted(() => { void initialize() })
onBeforeUnmount(() => {
  navigationController?.abort()
  suggestController?.abort()
  clearTimeout(suggestTimer)
})

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

async function openProject(entry: FileEntry) {
  await openProjectPath(entry.path, entry.name)
}
async function openProjectPath(projectPath: string, name: string) {
  const existing = projects.value.find((p) => p.path === projectPath)
  if (existing) { selectedProject.value = existing; return }
  busy.value = true
  try {
    const project = await projectsApi.create(props.machine, projectPath, name)
    const prior = projects.value.find((p) => p.path === project.path)
    if (prior) { selectedProject.value = prior; return }
    projectStore.remember(project)
    selectedProject.value = project
  } catch (e) { error.value = describeError(e) }
  finally { busy.value = false }
}
function openCurrentProject() {
  if (!path.value) return
  const name = path.value.split('/').filter(Boolean).at(-1) || '/'
  void openProjectPath(path.value, name)
}
</script>

<template>
  <section
    aria-label="File browser"
    class="flex min-h-0 flex-col gap-2 p-2"
  >
    <div
      aria-label="Breadcrumbs"
      class="flex flex-wrap gap-0.5 text-sm"
    >
      <button
        v-for="crumb in crumbs"
        :key="crumb.path"
        type="button"
        class="touch-target min-h-7 rounded px-1.5 text-accent"
        @click="navigate(crumb.path)"
      >
        {{ crumb.name }}
      </button>
    </div>
    <form
      class="relative flex gap-1.5"
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
        autocomplete="off"
        class="min-w-0 flex-1 rounded border border-border bg-bg px-2 py-1 text-base"
        @keydown.down.prevent="suggestions[0] && (pathInput = suggestions[0].path)"
        @keydown.enter.prevent="suggestions.length ? (pathInput = suggestions[0].path, navigate(pathInput)) : navigate(pathInput)"
      >
      <button
        class="touch-target min-h-8 rounded border border-border px-2"
        type="submit"
      >
        Go
      </button>
      <button
        type="button"
        class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border"
        :aria-label="currentProject ? 'Open project' : 'Add this directory as project'"
        :title="currentProject ? 'Open project' : 'Add this directory as project'"
        :disabled="!projectStore.loaded || loading || Boolean(error) || busy || !path"
        @click="openCurrentProject"
      >
        <FolderOpen v-if="currentProject" :size="16" aria-hidden="true" />
        <FolderPlus v-else :size="16" aria-hidden="true" />
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
            class="touch-target min-h-8 w-full px-2 text-left"
            @click="pathInput = entry.path; navigate(entry.path)"
          >
            {{ entry.path }}
          </button>
        </li>
      </ul>
    </form>
    <label class="touch-target flex min-h-7 items-center gap-2 self-start text-sm"><input
      v-model="hidden"
      type="checkbox"
      autocomplete="off"
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
      :retry="() => navigate(path)"
    />
    <ul
      v-if="!loading && !error"
      class="min-h-0 flex-1 overflow-y-auto"
      aria-label="Directory entries"
    >
      <li
        v-for="entry in entries"
        :key="entry.path"
        class="flex flex-wrap items-center gap-1.5 border-b border-border py-0.5"
      >
        <button
          v-if="entry.kind === 'directory'"
          type="button"
          class="touch-target min-h-8 min-w-0 flex-1 truncate text-left"
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
          class="touch-target min-h-7 rounded border border-border px-2 text-sm"
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
          class="touch-target inline-flex min-h-7 min-w-7 items-center justify-center rounded border border-border"
          :aria-label="projects.some((p) => p.path === entry.path) ? `Open project ${entry.name}` : `Add ${entry.name} as project`"
          :title="projects.some((p) => p.path === entry.path) ? `Open project ${entry.name}` : `Add ${entry.name} as project`"
          @click="openProject(entry)"
        >
          <FolderOpen
            v-if="projects.some((p) => p.path === entry.path)"
            :size="16"
            aria-hidden="true"
          />
          <FolderPlus
            v-else
            :size="16"
            aria-hidden="true"
          />
        </button>
      </li>
      <li
        v-if="!entries.length && !loading && !error"
        class="py-2 text-muted"
      >
        This directory is empty.
      </li>
    </ul>
    <form
      class="flex gap-1.5"
      @submit.prevent="createFolder"
    >
      <label
        class="sr-only"
        for="folder-name"
      >New folder name</label><input
        id="folder-name"
        v-model="folderName"
        autocomplete="off"
        class="min-w-0 flex-1 rounded border border-border bg-bg px-2 py-1 text-base"
        placeholder="New folder name"
      >
      <button
        type="submit"
        :disabled="busy"
        class="touch-target min-h-8 rounded border border-border px-2"
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
      class="flex items-center justify-between gap-2 rounded border border-border px-2 py-1"
    >
      <span class="min-w-0 truncate">Project: {{ selectedProject.name }}</span>
      <button
        type="button"
        class="touch-target min-h-8 rounded bg-accent px-2 font-bold text-bg"
        @click="sessionProject = selectedProject"
      >
        New session here
      </button>
    </div>
    <ProjectSessionDialog
      v-model:project="sessionProject"
      @created="emit('created', $event)"
    />
  </section>
</template>
