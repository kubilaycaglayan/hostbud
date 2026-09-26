<script setup lang="ts">
import { computed, ref } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import type { Project, Session } from '@/api/types'
import { projectsApi } from '@/api/client'
import { useProjectsStore } from '@/stores/projects'
import { useTreeStore } from '@/stores/tree'
import { describeError } from '@/stores/toasts'
import type { SplitDir } from '@/lib/layout'
import type { ProjectGroup } from '@/lib/tree'
import SessionList from './SessionList.vue'

const props = defineProps<{ selected?: string }>()
const emit = defineEmits<{
  select: [name: string]
  split: [name: string, dir: SplitDir]
  rename: [name: string]
  kill: [name: string]
  sessionInProject: [project: Project]
}>()
const tree = useTreeStore()
const projects = useProjectsStore()
const busySession = ref('')
const error = ref<{ message: string; hint?: string } | null>(null)
const projectRows = computed({
  get: () => tree.groups.groups.map((group) => ({ ...group, id: group.project.id })),
  set: (groups: (ProjectGroup & { id: string })[]) => tree.reorderProjects(groups.map((group) => group.project.id)),
})

function moveProject(id: string, direction: -1 | 1) {
  const ids = tree.groups.groups.map((group) => group.project.id)
  const index = ids.indexOf(id)
  const target = index + direction
  if (index < 0 || target < 0 || target >= ids.length) return
  ids.splice(target, 0, ...ids.splice(index, 1))
  tree.reorderProjects(ids)
}
async function saveAsProject(session: Session) {
  busySession.value = session.name
  error.value = null
  try {
    const project = projects.byPath(session.path) ?? await projectsApi.create('host', session.path, '')
    projects.remember(project)
    tree.sync()
  } catch (e) { error.value = describeError(e) }
  finally { busySession.value = '' }
}
</script>

<template>
  <nav
    aria-label="Project and session tree"
    class="flex flex-col gap-2"
  >
    <p
      v-if="error"
      role="alert"
      class="text-danger"
    >
      {{ error.message }}
    </p>
    <VueDraggable
      v-model="projectRows"
      tag="ul"
      item-key="id"
      handle=".project-drag-handle"
      aria-label="Projects"
      class="flex flex-col gap-1"
      :animation="150"
      :delay-on-touch-only="true"
      :touch-start-threshold="3"
    >
      <li
        v-for="group in projectRows"
        :key="group.id"
        class="rounded border border-border/60"
      >
        <div class="flex items-center gap-1 px-1">
          <button
            type="button"
            class="project-drag-handle min-h-11 min-w-8 cursor-grab rounded text-muted"
            :aria-label="`Reorder project ${group.project.name}`"
            title="Reorder"
          >
            ⠿
          </button>
          <span class="flex">
            <button
              type="button"
              class="min-h-11 min-w-8 text-xs text-muted"
              :aria-label="`Move project ${group.project.name} up`"
              :disabled="tree.groups.groups[0]?.project.id === group.project.id"
              @click="moveProject(group.project.id, -1)"
            >▲</button>
            <button
              type="button"
              class="min-h-11 min-w-8 text-xs text-muted"
              :aria-label="`Move project ${group.project.name} down`"
              :disabled="tree.groups.groups.at(-1)?.project.id === group.project.id"
              @click="moveProject(group.project.id, 1)"
            >▼</button>
          </span>
          <span
            role="heading"
            aria-level="3"
            class="min-w-0 flex-1 truncate font-semibold"
          >
            {{ group.project.name }}
          </span>
          <button
            type="button"
            class="min-h-11 rounded px-2"
            :aria-label="`New session in ${group.project.name}`"
            @click="emit('sessionInProject', group.project)"
          >
            ＋
          </button>
        </div>
        <SessionList
          :sessions="group.sessions"
          :selected="props.selected"
          sortable
          :list-label="`Sessions in ${group.project.name}`"
          @select="emit('select', $event)"
          @split="(name, dir) => emit('split', name, dir)"
          @rename="emit('rename', $event)"
          @kill="emit('kill', $event)"
          @reorder="tree.reorderSessions(group.project.id, $event)"
        />
      </li>
    </VueDraggable>
    <section aria-label="Other sessions">
      <h2 class="px-2 py-1 text-xs font-semibold uppercase tracking-wide text-muted">
        Other sessions
      </h2>
      <SessionList
        :sessions="tree.groups.other"
        :selected="props.selected"
        sortable
        list-label="Other sessions"
        @select="emit('select', $event)"
        @split="(name, dir) => emit('split', name, dir)"
        @rename="emit('rename', $event)"
        @kill="emit('kill', $event)"
        @reorder="tree.reorderSessions('__other__', $event)"
      />
      <ul
        aria-label="Unmatched session actions"
        class="mt-1 flex flex-col gap-1"
      >
        <li
          v-for="session in tree.groups.other"
          :key="session.name"
          class="flex items-center justify-between gap-2 px-2"
        >
          <span class="min-w-0 truncate text-sm text-muted">{{ session.name }}</span>
          <button
            type="button"
            :aria-label="`Save ${session.name} as project`"
            class="min-h-11 rounded px-2 text-sm text-accent"
            :disabled="busySession === session.name"
            @click="saveAsProject(session)"
          >
            Save as project
          </button>
        </li>
      </ul>
    </section>
  </nav>
</template>
