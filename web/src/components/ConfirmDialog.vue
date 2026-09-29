<script setup lang="ts">
import {
  AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogOverlay, AlertDialogPortal, AlertDialogRoot, AlertDialogTitle,
} from 'reka-ui'

// The app's confirmation dialog: every confirmation uses it, never the
// browser's window.confirm. Confirm closes the dialog, then emits
// `confirm`; Cancel, Escape or the overlay just close it.
const props = defineProps<{ title: string; body: string; action: string; danger?: boolean; compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ confirm: [] }>()

function onConfirm() {
  open.value = false
  emit('confirm')
}
</script>

<template>
  <AlertDialogRoot v-model:open="open">
    <AlertDialogPortal>
      <AlertDialogOverlay class="fixed inset-0 z-[60] bg-overlay" />
      <AlertDialogContent
        class="fixed z-[60] border border-border bg-surface p-5 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(24rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <AlertDialogTitle class="text-base font-bold">
          {{ props.title }}
        </AlertDialogTitle>
        <AlertDialogDescription class="mt-2 text-muted">
          {{ props.body }}
        </AlertDialogDescription>
        <div class="mt-4 flex justify-end gap-2">
          <AlertDialogCancel class="touch-target min-h-11 rounded border border-border px-3 py-2">
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction
            class="touch-target min-h-11 rounded px-3 py-2 font-bold text-bg"
            :class="props.danger ? 'bg-danger' : 'bg-accent'"
            @click.prevent="onConfirm"
          >
            {{ props.action }}
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>
