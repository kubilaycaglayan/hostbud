<script setup lang="ts">
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { ref, watch } from 'vue'
import { ApiError, filesystemApi } from '@/api/client'
import { useToastsStore } from '@/stores/toasts'

const props = defineProps<{ machine: string; directory: string }>()
const emit = defineEmits<{ uploaded: [absolutePath: string] }>()
const open = defineModel<boolean>('open', { default: false })
const files = ref<File[]>([])
const busy = ref(false)
const progress = ref('')
const error = ref('')
const results = ref<string[]>([])

watch(open, (value) => {
  if (!value) {
    files.value = []
    busy.value = false
    progress.value = ''
    error.value = ''
    results.value = []
  }
})

function selectFiles(event: Event) {
  const input = event.target as HTMLInputElement
  files.value = Array.from(input.files ?? [])
  error.value = ''
  results.value = []
}

function preventDismissDuringUpload(event: Event) {
  if (busy.value) event.preventDefault()
}

async function send() {
  if (!files.value.length || !props.directory || busy.value) return
  busy.value = true
  error.value = ''
  results.value = []
  try {
    const batch = [...files.value]
    for (let index = 0; index < batch.length; index++) {
      const file = batch[index]
      progress.value = `Sending ${index + 1} of ${batch.length}: ${file.name}`
      const result = await filesystemApi.uploadPhotoUnique(props.machine, props.directory, file)
      const uploadedName = result.path.split('/').pop() || file.name
      results.value.push(`${uploadedName} · ${result.size.toLocaleString()} bytes`)
      emit('uploaded', result.path)
      useToastsStore().push({
        title: 'Photo added to repo',
        message: `${uploadedName} · ${result.size.toLocaleString()} bytes; path pasted into terminal`,
        tone: 'success',
        placement: 'top-right',
      }, 5_000)
      const pendingIndex = files.value.indexOf(file)
      if (pendingIndex >= 0) files.value.splice(pendingIndex, 1)
    }
    files.value = []
  } catch (cause) {
    error.value = cause instanceof ApiError
      ? `${cause.message}${cause.hint ? ` ${cause.hint}` : ''}`
      : cause instanceof Error ? cause.message : 'The photo could not be sent. Try again.'
  } finally {
    busy.value = false
    progress.value = ''
  }
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent class="fixed left-1/2 top-1/2 z-40 flex max-h-[min(88dvh,42rem)] w-[min(92vw,32rem)] -translate-x-1/2 -translate-y-1/2 flex-col gap-3 overflow-y-auto rounded border border-border bg-surface p-4 text-fg shadow-xl" @interact-outside="preventDismissDuringUpload" @escape-key-down="preventDismissDuringUpload">
        <div class="flex items-start justify-between gap-3">
          <div>
            <DialogTitle class="font-bold">Send photos to this repo</DialogTitle>
            <DialogDescription class="mt-1 text-sm text-muted">Choose original photo files. hostbud sends the selected file bytes unchanged to this session’s folder.</DialogDescription>
          </div>
          <button type="button" aria-label="Close photo upload" class="touch-target rounded px-2 text-xl" :disabled="busy" @click="open = false">×</button>
        </div>
        <p class="break-all rounded bg-bg p-2 font-mono text-xs" aria-label="Photo destination">{{ props.directory || 'Session folder unavailable' }}</p>
        <label class="flex flex-col gap-2 text-sm">
          <span>Photos</span>
          <input
            type="file"
            accept="image/*,.heic,.heif,.dng"
            multiple
            autocomplete="off"
            :disabled="busy || !props.directory"
            class="touch-target min-w-0 max-w-full rounded border border-border bg-bg p-2 text-base"
            @change="selectFiles"
          >
        </label>
        <ul v-if="files.length" aria-label="Selected photos" class="max-h-32 overflow-y-auto rounded bg-bg p-2 text-sm">
          <li v-for="file in files" :key="`${file.name}-${file.lastModified}-${file.size}`" class="truncate">{{ file.name }} · {{ file.size.toLocaleString() }} bytes</li>
        </ul>
        <p class="text-xs text-muted">Each photo is limited to 100 MiB. Existing files are kept; rename a photo if its name is already in use.</p>
        <p v-if="busy" role="status" class="text-sm">{{ progress }}</p>
        <p v-if="error" role="alert" class="text-sm text-danger">{{ error }}</p>
        <ul v-if="results.length" aria-label="Sent photos" class="text-sm text-ok">
          <li v-for="result in results" :key="result">Sent {{ result }}</li>
        </ul>
        <div class="flex justify-end gap-2">
          <button type="button" class="touch-target rounded border border-border px-3" :disabled="busy" @click="open = false">Close</button>
          <button type="button" class="touch-target rounded bg-accent px-3 font-bold text-bg" :disabled="busy || !files.length || !props.directory" @click="send">{{ busy ? 'Sending…' : 'Send photos' }}</button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
