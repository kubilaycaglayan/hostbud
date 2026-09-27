<script setup lang="ts">
import { ref, watch } from 'vue'
import { projectsApi } from '@/api/client'
import type { Project } from '@/api/types'
import { describeError, useToastsStore } from '@/stores/toasts'

// "New session here": a session in a saved project's directory. Open while
// `project` is set; Cancel or a created session clears it.
const project = defineModel<Project | null>('project', { default: null })
const emit = defineEmits<{ created: [name: string] }>()
const toasts = useToastsStore()

const sessionName = ref('')
const command = ref('')
const recentCommands = ref<string[]>([])
const recentCommandsError = ref('')
const busy = ref(false)
const error = ref('')

watch(project, async (p) => {
  sessionName.value = ''
  command.value = ''
  recentCommands.value = []
  recentCommandsError.value = ''
  error.value = ''
  if (!p) return
  try {
    const result = await projectsApi.recentCommands(p.id)
    if (project.value?.id === p.id) recentCommands.value = result.commands
  } catch (e) {
    if (project.value?.id === p.id) recentCommandsError.value = describeError(e).message
  }
}, { immediate: true })

async function createSession() {
  if (!project.value) return
  busy.value = true
  error.value = ''
  try {
    const requestedName = sessionName.value.trim()
    const startCommand = command.value.trim() ? command.value : undefined
    const result = await projectsApi.createSession(project.value.id, { name: requestedName || undefined, startCommand })
    project.value = null
    if (requestedName && result.name !== requestedName) {
      toasts.push({ title: 'Session name changed', message: `Named "${result.name}": "${requestedName}" was already taken.`, tone: 'info' })
    }
    emit('created', result.name)
  } catch (e) { error.value = describeError(e).message }
  finally { busy.value = false }
}
</script>

<template>
  <div
    v-if="project"
    role="dialog"
    aria-modal="true"
    aria-label="New session here"
    class="fixed inset-0 z-50 flex items-center justify-center bg-overlay p-4"
  >
    <form
      class="w-full max-w-md rounded border border-border bg-surface p-4 text-fg"
      @submit.prevent="createSession"
    >
      <h2 class="font-bold">
        New session in {{ project.name }}
      </h2>
      <label class="mt-3 block">Name <input
        v-model="sessionName"
        autocomplete="off"
        class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base"
      ></label>
      <label class="mt-3 block">Start command <input
        v-model="command"
        autocomplete="off"
        class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base"
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
              class="touch-target min-h-11 w-full truncate px-2 text-left text-sm hover:bg-bg"
              :aria-label="`Use recent command ${recent}`"
              @click="command = recent"
            >
              {{ recent }}
            </button>
          </li>
        </ul>
      </div>
      <p class="mt-2 text-sm text-muted">
        Directory: {{ project.path }}
      </p>
      <p
        v-if="error"
        role="alert"
        class="mt-2 text-danger"
      >
        {{ error }}
      </p>
      <div class="mt-4 flex justify-end gap-2">
        <button
          type="button"
          class="touch-target min-h-11 px-3"
          @click="project = null"
        >
          Cancel
        </button><button
          type="submit"
          :disabled="busy"
          class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg"
        >
          Create session
        </button>
      </div>
    </form>
  </div>
</template>
