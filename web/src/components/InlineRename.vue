<script setup lang="ts">
import { onMounted, ref } from 'vue'

const props = defineProps<{
  name: string
  error?: string
  commit: (value: string) => Promise<void>
}>()
const emit = defineEmits<{ cancel: [] }>()
const input = ref<HTMLInputElement>()
const value = ref(props.name)
const pending = ref(false)
const finished = ref(false)

onMounted(() => {
  input.value?.focus()
  input.value?.select()
})

async function save() {
  if (pending.value || finished.value) return
  const next = value.value.trim()
  if (!next || next === props.name) {
    finished.value = true
    emit('cancel')
    return
  }
  pending.value = true
  try {
    await props.commit(next)
    finished.value = true
  } catch {
    // The parent supplies the actionable error while keeping this input mounted.
  } finally {
    pending.value = false
  }
}

function cancel() {
  if (pending.value || finished.value) return
  finished.value = true
  emit('cancel')
}
</script>

<template>
  <span class="flex min-w-0 flex-1 flex-col" @click.stop>
    <input
      ref="input"
      v-model="value"
      :aria-label="`Rename ${name}`"
      :aria-describedby="error ? 'inline-rename-error' : undefined"
      :aria-invalid="error ? 'true' : undefined"
      autocomplete="off"
      autocapitalize="off"
      autocorrect="off"
      spellcheck="false"
      :aria-busy="pending ? 'true' : undefined"
      class="min-h-11 min-w-0 w-full rounded border border-border bg-surface px-2 text-base text-fg outline-none focus:ring-2 focus:ring-accent"
      @keydown.enter.prevent="save"
      @keydown.esc.prevent="cancel"
      @blur="save"
    >
    <span v-if="error" id="inline-rename-error" class="text-xs text-danger" role="alert">{{ error }}</span>
  </span>
</template>
