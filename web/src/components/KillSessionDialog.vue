<script setup lang="ts">
import {
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogOverlay,
  AlertDialogPortal,
  AlertDialogRoot,
  AlertDialogTitle,
} from 'reka-ui'
import { ref } from 'vue'
import { sessionsApi } from '@/api/client'
import { useToastsStore } from '@/stores/toasts'

// Killing is destructive: it only happens after an explicit confirmation.
const props = defineProps<{ machine: string; session: string; compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ killed: [name: string] }>()
const busy = ref(false)
const toasts = useToastsStore()

async function confirm() {
  busy.value = true
  try {
    await sessionsApi.kill(props.machine, props.session)
    emit('killed', props.session)
  } catch (e) {
    toasts.error("Couldn't kill the session", e)
  } finally {
    busy.value = false
    open.value = false
  }
}
</script>

<template>
  <AlertDialogRoot v-model:open="open">
    <AlertDialogPortal>
      <AlertDialogOverlay class="fixed inset-0 z-40 bg-black/50" />
      <AlertDialogContent
        class="fixed z-40 border border-border bg-surface p-5 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(24rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <AlertDialogTitle class="text-base font-bold">
          Kill session {{ props.session }}?
        </AlertDialogTitle>
        <AlertDialogDescription class="mt-2 text-muted">
          This ends every program running in it. It can't be undone.
        </AlertDialogDescription>
        <div class="mt-4 flex justify-end gap-2">
          <AlertDialogCancel class="touch-target rounded border border-border px-3 py-2">
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction
            :disabled="busy"
            class="touch-target rounded bg-danger px-3 py-2 font-bold text-bg"
            @click.prevent="confirm"
          >
            Kill session
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>
