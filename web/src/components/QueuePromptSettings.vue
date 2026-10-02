<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { queuesApi } from '@/api/client'
import FormError from './FormError.vue'
import { DEFAULT_PROMPT, defaultPromptError } from '@/lib/queue'
import { useQueuesStore } from '@/stores/queues'
import { describeError } from '@/stores/toasts'

// Settings → Queue default prompt: opt-in text that each new queue item's
// instruction starts with (the Queue panel prefills it; nothing is added
// to an instruction behind the owner's back). Stored per machine.
const props = defineProps<{ machine: string }>()
const queues = useQueuesStore()
const enabled = ref(queues.defaultPrompt.enabled)
const text = ref(queues.defaultPrompt.text)
const busy = ref(false)
const saved = ref('')
const error = ref<{ title: string; message: string; hint?: string } | null>(null)
const problem = computed(() => defaultPromptError(text.value))

// Another device's change (queue.changed) shows here too.
watch(() => queues.defaultPrompt, (p) => { enabled.value = p.enabled; text.value = p.text })

async function save() {
  saved.value = ''
  if (problem.value) return
  busy.value = true
  error.value = null
  try {
    queues.defaultPrompt = await queuesApi.setDefaultPrompt(props.machine, { enabled: enabled.value, text: text.value })
    saved.value = queues.defaultPrompt.enabled ? 'Saved: new queue items start with the default prompt.' : 'Saved: new queue items start empty.'
  } catch (e) {
    error.value = { title: "Couldn't save the default prompt", ...describeError(e) }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="flex flex-col gap-2" aria-label="Queue default prompt" data-testid="default-prompt-settings" @submit.prevent="save">
    <h2 class="font-bold">
      Queue default prompt
    </h2>
    <p class="text-sm text-muted">
      When on, each new queue item's instruction starts with this text, so you can type your instruction in front of it and edit it per item.
    </p>
    <FormError v-if="error" id="default-prompt-error" :title="error.title" :message="error.message" :hint="error.hint" />
    <label class="flex min-h-11 items-center gap-2 text-sm">
      <input v-model="enabled" type="checkbox" autocomplete="off" class="size-4" data-testid="default-prompt-enabled">
      Start new queue items with the default prompt
    </label>
    <label class="block">Default prompt
      <input
        v-model="text"
        data-testid="default-prompt-text"
        type="text"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :placeholder="DEFAULT_PROMPT"
        :aria-invalid="problem ? 'true' : undefined"
        class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 font-mono text-base"
      >
    </label>
    <p v-if="problem" class="text-sm text-danger">
      {{ problem }}
    </p>
    <p v-if="saved" role="status" class="text-sm text-ok">
      {{ saved }}
    </p>
    <div class="flex justify-end gap-2">
      <button type="button" class="touch-target min-h-11 rounded border border-border px-3" :disabled="busy || text === DEFAULT_PROMPT" @click="text = DEFAULT_PROMPT">
        Reset text
      </button>
      <button type="submit" :disabled="busy" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg">
        Save
      </button>
    </div>
  </form>
</template>
