<script setup lang="ts">
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { computed, ref, watch } from 'vue'
import { sessionsApi } from '@/api/client'
import { sessionNameError } from '@/lib/names'
import FormError from './FormError.vue'
import { describeError, useToastsStore } from '@/stores/toasts'

const props = defineProps<{ machine: string; compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ created: [name: string] }>()
const toasts = useToastsStore()

const name = ref('')
const path = ref('~')
const startCommand = ref('')
const busy = ref(false)
const touched = ref(false)
const failure = ref<{ message: string; hint?: string } | null>(null)

const nameError = computed(() => sessionNameError(name.value.trim()))

watch(
  open,
  (o) => {
    if (o) {
      failure.value = null
      name.value = ''
      path.value = '~'
      startCommand.value = ''
      touched.value = false
    }
  },
  { immediate: true },
)

async function submit() {
  touched.value = true
  if (nameError.value) return
  busy.value = true
  failure.value = null
  try {
    const requestedName = name.value.trim()
    const res = await sessionsApi.create(props.machine, {
      name: requestedName || undefined,
      path: path.value.trim() || '~',
      startCommand: startCommand.value.trim() || undefined,
    })
    open.value = false
    if (requestedName && res.name !== requestedName) {
      toasts.push({ title: 'Session name changed', message: `Named "${res.name}": "${requestedName}" was already taken.`, tone: 'info' })
    }
    emit('created', res.name)
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
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent
        class="fixed z-40 border border-border bg-surface p-5 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <DialogTitle class="text-base font-bold">
          New session
        </DialogTitle>
        <DialogDescription class="mt-1 text-muted">
          Starts a detached tmux session on the host.
        </DialogDescription>
        <form
          class="mt-4 flex flex-col gap-3"
          novalidate
          @submit.prevent="submit"
        >
          <label class="flex flex-col gap-1">
            <span>Directory</span>
            <input
              v-model="path"
              name="path"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              class="rounded border border-border bg-bg px-2 py-2 text-base"
            >
          </label>
          <label class="flex flex-col gap-1">
            <span>Name <span class="text-muted">(optional)</span></span>
            <input
              v-model="name"
              name="name"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              placeholder="the directory's name"
              :aria-invalid="touched && !!nameError"
              aria-describedby="create-name-error"
              class="rounded border border-border bg-bg px-2 py-2 text-base"
              @blur="touched = true"
            >
            <span
              id="create-name-error"
              class="text-danger"
            >{{ touched ? nameError : '' }}</span>
          </label>
          <label class="flex flex-col gap-1">
            <span>Start command <span class="text-muted">(optional)</span></span>
            <input
              v-model="startCommand"
              name="startCommand"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              placeholder="e.g. htop"
              class="rounded border border-border bg-bg px-2 py-2 text-base"
            >
          </label>
          <FormError
            v-if="failure"
            id="create-error"
            title="Couldn't create the session"
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
              :disabled="busy"
              class="touch-target rounded bg-accent px-3 py-2 font-bold text-bg disabled:opacity-60"
            >
              Create
            </button>
          </div>
        </form>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
