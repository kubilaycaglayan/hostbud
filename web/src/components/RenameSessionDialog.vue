<script setup lang="ts">
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { computed, ref, watch } from 'vue'
import { sessionsApi } from '@/api/client'
import { sessionNameError } from '@/lib/names'
import FormError from './FormError.vue'
import { describeError } from '@/stores/toasts'

const props = defineProps<{ machine: string; session: string; compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
// renaming: the request is on its way (the list may change before it returns).
const emit = defineEmits<{ renaming: [from: string, to: string]; renamed: [from: string, to: string] }>()

const name = ref('')
const busy = ref(false)
const failure = ref<{ message: string; hint?: string } | null>(null)
const nameError = computed(() => sessionNameError(name.value.trim(), true))

watch(
  open,
  (o) => {
    if (o) {
      name.value = props.session
      failure.value = null
    }
  },
  { immediate: true },
)

async function submit() {
  if (nameError.value) return
  const to = name.value.trim()
  if (to === props.session) {
    open.value = false
    return
  }
  busy.value = true
  failure.value = null
  emit('renaming', props.session, to)
  try {
    await sessionsApi.rename(props.machine, props.session, to)
    open.value = false
    emit('renamed', props.session, to)
  } catch (e) {
    failure.value = describeError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-black/50" />
      <DialogContent
        class="fixed z-40 border border-border bg-surface p-5 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(24rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <DialogTitle class="text-base font-bold">
          Rename session
        </DialogTitle>
        <DialogDescription class="mt-1 text-muted">
          {{ props.session }}
        </DialogDescription>
        <form
          class="mt-4 flex flex-col gap-3"
          novalidate
          @submit.prevent="submit"
        >
          <label class="flex flex-col gap-1">
            <span>New name</span>
            <input
              v-model="name"
              name="name"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              :aria-invalid="!!nameError"
              aria-describedby="rename-name-error"
              class="rounded border border-border bg-bg px-2 py-2 text-base"
            >
            <span
              id="rename-name-error"
              class="text-danger"
            >{{ nameError }}</span>
          </label>
          <FormError
            v-if="failure"
            id="rename-error"
            title="Couldn't rename the session"
            :message="failure.message"
            :hint="failure.hint"
          />
          <div class="mt-2 flex justify-end gap-2">
            <DialogClose
              type="button"
              class="touch-target rounded border border-border px-3 py-2"
            >
              Cancel
            </DialogClose>
            <button
              type="submit"
              :disabled="busy || !!nameError"
              class="touch-target rounded bg-accent px-3 py-2 font-bold text-bg disabled:opacity-60"
            >
              Rename
            </button>
          </div>
        </form>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
