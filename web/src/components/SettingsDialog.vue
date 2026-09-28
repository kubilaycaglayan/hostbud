<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { queuesApi } from '@/api/client'
import FormError from './FormError.vue'
import { capacityError, capacityValue } from '@/lib/queue'
import { useQueuesStore } from '@/stores/queues'
import { describeError } from '@/stores/toasts'

// Settings (V2-M2): the per-machine cap on parallel queue runs. The server
// validates it too (1–32, or null for no cap) and publishes queue.changed,
// so the Queue panel's waiting state updates live.
const props = defineProps<{ compact?: boolean; machine: string }>()
const open = defineModel<boolean>('open', { default: false })

const queues = useQueuesStore()
const text = ref('')
const saved = ref('')
const loading = ref(false)
const busy = ref(false)
const touched = ref(false)
const error = ref<{ title: string; message: string; hint?: string } | null>(null)
const problem = computed(() => capacityError(text.value))

watch(open, async (isOpen) => {
  if (!isOpen) return
  touched.value = false
  error.value = null
  saved.value = ''
  loading.value = true
  if (!queues.loaded) void queues.load()
  try {
    const c = await queuesApi.capacity(props.machine)
    text.value = c.maxConcurrentRuns === null ? '' : String(c.maxConcurrentRuns)
  } catch (e) {
    error.value = { title: "Couldn't load the settings", ...describeError(e) }
  } finally {
    loading.value = false
  }
}, { immediate: true })

async function save() {
  touched.value = true
  saved.value = ''
  if (problem.value) return
  busy.value = true
  error.value = null
  try {
    const c = await queuesApi.setCapacity(props.machine, capacityValue(text.value))
    text.value = c.maxConcurrentRuns === null ? '' : String(c.maxConcurrentRuns)
    saved.value = c.maxConcurrentRuns === null ? 'Saved: no cap.' : `Saved: at most ${c.maxConcurrentRuns} at once.`
  } catch (e) {
    error.value = { title: "Couldn't save the cap", ...describeError(e) }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent
        class="fixed z-40 flex flex-col overflow-hidden border border-border bg-surface text-fg"
        :class="props.compact ? 'inset-0 h-dvh w-full pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)]' : 'left-1/2 top-1/2 max-h-[min(40rem,85vh)] w-[min(32rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <div class="flex items-start justify-between gap-2 border-b border-border px-3 py-2">
          <div class="min-w-0">
            <DialogTitle class="text-base font-bold">
              Settings
            </DialogTitle>
            <DialogDescription class="text-sm text-muted">
              Settings for this machine.
            </DialogDescription>
          </div>
          <DialogClose aria-label="Close settings" title="Close" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border">
            ×
          </DialogClose>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto p-3">
          <FormError v-if="error" id="settings-error" :title="error.title" :message="error.message" :hint="error.hint" />
          <form class="flex flex-col gap-2" aria-label="Queue runs" @submit.prevent="save">
            <h2 class="font-bold">
              Queue runs
            </h2>
            <p class="text-sm text-muted">
              How many queue runs may be active at once on this machine. A run that went stale still counts until you act on it. Leave it empty for no cap.
            </p>
            <p v-if="!queues.parallelQueues" data-testid="parallel-off" class="text-sm text-muted">
              Parallel queues are off (<span class="font-mono">HOSTBUD_PARALLEL_QUEUES</span>), so one queue runs at a time; the cap applies once they're on.
            </p>
            <label class="block">Maximum parallel runs
              <input
                v-model="text"
                type="text"
                inputmode="numeric"
                autocomplete="off"
                placeholder="No cap"
                :disabled="loading"
                :aria-invalid="touched && problem ? 'true' : undefined"
                class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base"
              >
            </label>
            <p v-if="touched && problem" class="text-sm text-danger">
              {{ problem }}
            </p>
            <p v-if="saved" role="status" class="text-sm text-ok">
              {{ saved }}
            </p>
            <div class="flex justify-end">
              <button type="submit" :disabled="busy || loading" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg">
                Save
              </button>
            </div>
          </form>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
