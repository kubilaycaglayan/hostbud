<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogOverlay, AlertDialogPortal, AlertDialogRoot, AlertDialogTitle,
} from 'reka-ui'
import type { Project } from '@/api/types'
import { killSessions } from '@/lib/killSessions'

// Killing every session of a project is destructive twice over, so it takes
// two confirmations: the first names the count, the second lists the sessions.
// The actions are plain buttons: AlertDialogAction would close the dialog.
const props = defineProps<{ machine: string; project: Project | null; sessions: string[]; compact?: boolean }>()
const emit = defineEmits<{ killed: [name: string]; done: []; cancel: [] }>()
const step = ref<1 | 2>(1)
const busy = ref(false)
const error = ref('')
watch(() => props.project?.id, () => {
  step.value = 1
  error.value = ''
})

function count(n: number) {
  return `${n} ${n === 1 ? 'session' : 'sessions'}`
}

/** Kills the sessions in one request; a failure doesn't stop the rest. */
async function kill() {
  if (!props.project || busy.value) return
  busy.value = true
  error.value = ''
  const outcome = await killSessions(props.machine, props.sessions)
  for (const name of outcome.killed) emit('killed', name)
  busy.value = false
  if (outcome.error) {
    error.value = [`Couldn't kill ${outcome.failed.join(', ')}.`, outcome.error.message, outcome.error.hint].filter(Boolean).join(' ')
    return
  }
  emit('done')
}
</script>

<template>
  <AlertDialogRoot :open="Boolean(props.project)" @update:open="(open) => { if (!open && !busy) emit('cancel') }">
    <AlertDialogPortal>
      <AlertDialogOverlay class="fixed inset-0 z-50 bg-overlay" />
      <AlertDialogContent
        v-if="props.project"
        class="fixed z-50 border border-border bg-surface p-4 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <template v-if="step === 1">
          <AlertDialogTitle class="font-bold">Kill all in {{ props.project.name }}?</AlertDialogTitle>
          <AlertDialogDescription class="mt-2 text-fg">
            This kills its {{ count(props.sessions.length) }}, including hidden ones, and ends every program running in them. The project stays.
          </AlertDialogDescription>
          <div class="mt-4 flex justify-end gap-2">
            <AlertDialogCancel class="touch-target min-h-11 rounded border border-border px-3">Cancel</AlertDialogCancel>
            <button type="button" class="touch-target min-h-11 rounded bg-danger px-3 font-bold text-bg" @click="step = 2">Continue…</button>
          </div>
        </template>
        <template v-else>
          <AlertDialogTitle class="font-bold">Really kill {{ count(props.sessions.length) }}?</AlertDialogTitle>
          <AlertDialogDescription class="mt-2 text-fg">
            <span class="break-words font-mono text-sm">{{ props.sessions.join(', ') }}</span>
            <br>It can't be undone.
          </AlertDialogDescription>
          <p v-if="error" role="alert" class="mt-2 text-danger">{{ error }}</p>
          <div class="mt-4 flex justify-end gap-2">
            <AlertDialogCancel class="touch-target min-h-11 rounded border border-border px-3" :disabled="busy">Cancel</AlertDialogCancel>
            <button type="button" class="touch-target min-h-11 rounded bg-danger px-3 font-bold text-bg" :disabled="busy" @click="kill">Kill {{ count(props.sessions.length) }}</button>
          </div>
        </template>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>
