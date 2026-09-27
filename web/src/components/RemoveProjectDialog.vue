<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogOverlay, AlertDialogPortal, AlertDialogRoot, AlertDialogTitle,
} from 'reka-ui'
import type { Project } from '@/api/types'
import { projectsApi } from '@/api/client'
import { describeError } from '@/stores/toasts'

const props = defineProps<{
  project: Project | null
  sessionCount: number
  destinations: string
  displayPath: string
  compact?: boolean
}>()
const emit = defineEmits<{ removed: [id: string]; cancel: [] }>()
const busy = ref(false)
const error = ref('')
watch(() => props.project?.id, () => { error.value = '' })

async function remove() {
  if (!props.project || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await projectsApi.remove(props.project.id)
    emit('removed', props.project.id)
  } catch (e) {
    const detail = describeError(e)
    error.value = [detail.message, detail.hint].filter(Boolean).join(' ')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AlertDialogRoot :open="Boolean(props.project)" @update:open="(open) => { if (!open) emit('cancel') }">
    <AlertDialogPortal>
      <AlertDialogOverlay class="fixed inset-0 z-50 bg-overlay" />
      <AlertDialogContent
        v-if="props.project"
        class="fixed z-50 border border-border bg-surface p-4 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <AlertDialogTitle class="font-bold">Remove project {{ props.project.name }}?</AlertDialogTitle>
        <AlertDialogDescription class="mt-2 text-fg">
        <template v-if="props.sessionCount">
          Its {{ props.sessionCount }} {{ props.sessionCount === 1 ? 'session keeps' : 'sessions keep' }} running and move to {{ props.destinations }}.
        </template>
        <template v-else>It has no sessions to move.</template>
        <br>Files in {{ props.displayPath }} aren't touched. This removes it for every account.
        </AlertDialogDescription>
        <p v-if="error" role="alert" class="mt-2 text-danger">{{ error }}</p>
        <div class="mt-4 flex justify-end gap-2">
          <AlertDialogCancel class="touch-target min-h-11 rounded px-3" :disabled="busy">Cancel</AlertDialogCancel>
          <AlertDialogAction class="touch-target min-h-11 rounded bg-danger px-3 font-bold text-bg" :disabled="busy" @click.prevent="remove">Remove project</AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>
