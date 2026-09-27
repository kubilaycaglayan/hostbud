<script setup lang="ts">
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { computed, nextTick, ref, watch } from 'vue'
import { outputRuns } from '@/lib/terminalOutput'
import { darkTerminalTheme, lightTerminalTheme } from '@/lib/theme'
import { useThemeStore } from '@/stores/theme'

const props = defineProps<{ mode: 'dictation' | 'snapshot'; snapshot?: string; loading?: boolean; error?: string }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ send: [text: string]; retry: [] }>()
const text = ref('')
const editor = ref<HTMLTextAreaElement>()
const output = ref<HTMLElement>()
const theme = useThemeStore()
const runs = computed(() => outputRuns(props.snapshot ?? '', theme.resolved === 'dark' ? darkTerminalTheme : lightTerminalTheme))

watch([open, () => props.loading], async ([isOpen, loading]) => {
  if (isOpen && !loading && props.mode === 'snapshot') {
    await nextTick()
    if (output.value) output.value.scrollTop = output.value.scrollHeight
  }
})

function selectOutput(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'a' && output.value) {
    event.preventDefault()
    const range = document.createRange()
    range.selectNodeContents(output.value)
    const selection = window.getSelection()
    selection?.removeAllRanges()
    selection?.addRange(range)
  }
}

watch(open, async (value) => {
  if (value) {
    text.value = props.mode === 'snapshot' ? props.snapshot ?? '' : ''
    if (props.mode === 'dictation') {
      await nextTick()
      editor.value?.focus()
    }
  }
})

function send() {
  if (!text.value) return
  emit('send', text.value)
  open.value = false
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent class="fixed inset-0 z-40 flex flex-col p-4 pb-[max(1rem,env(safe-area-inset-bottom))] pt-[max(1rem,env(safe-area-inset-top))] text-fg" :class="mode === 'snapshot' ? 'bg-bg' : 'bg-surface'">
        <div class="flex items-start justify-between gap-4">
          <div v-if="mode === 'dictation'">
            <DialogTitle class="text-base font-bold">Dictation</DialogTitle>
            <DialogDescription class="mt-1 text-muted">Review or dictate text, then send it to the terminal.</DialogDescription>
          </div>
          <button type="button" :aria-label="mode === 'dictation' ? 'Close dictation' : 'Close terminal view'" class="touch-target ml-auto rounded px-3 text-xl" @click="open = false">×</button>
        </div>
        <DialogTitle v-if="mode === 'snapshot'" class="sr-only">Terminal text view</DialogTitle>
        <DialogDescription v-if="mode === 'snapshot'" class="sr-only">Retained tmux history. Select text to copy it, or scroll to read earlier output.</DialogDescription>
        <textarea
          v-if="mode === 'dictation'"
          ref="editor"
          v-model="text"
          aria-label="Dictation text"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          class="mt-4 min-h-0 flex-1 resize-none rounded border border-border bg-bg p-3 font-mono text-base text-fg"
        />
        <template v-else>
          <p v-if="loading" role="status" class="text-muted">Loading terminal history…</p>
          <div v-else-if="error" role="alert">
            <p>{{ error }}</p>
            <button type="button" class="touch-target rounded border border-border px-3" @click="emit('retry')">Try again</button>
          </div>
          <div
            v-else
            ref="output"
            role="region"
            aria-label="Terminal text"
            tabindex="0"
            class="terminal-output min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain bg-bg text-fg"
            @keydown="selectOutput"
          ><pre class="m-0 max-w-full whitespace-pre-wrap font-mono text-sm [overflow-wrap:anywhere]"><span v-for="(run, index) in runs" :key="index" :style="run.style">{{ run.text }}</span></pre></div>
        </template>
        <div v-if="mode === 'dictation'" class="mt-3 flex justify-end gap-2">
          <button type="button" class="touch-target rounded border border-border px-4" @click="open = false">Cancel</button>
          <button type="button" class="touch-target rounded bg-accent px-4 font-bold text-bg" :disabled="!text" @click="send">Send</button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.terminal-output, .terminal-output * {
  -webkit-user-select: text;
  user-select: text;
}
.terminal-output {
  touch-action: pan-y pinch-zoom;
}
</style>
