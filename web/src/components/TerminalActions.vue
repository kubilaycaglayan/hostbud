<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
import { ref, watch } from 'vue'
const props = defineProps<{ hasSelection: boolean; canSplit: boolean; sessions: string[] }>()
const emit = defineEmits<{
  action: [name: 'search' | 'copy' | 'keyboard' | 'dictation' | 'snapshot' | 'photos' | 'diagnostics' | 'close']
  split: [direction: 'row' | 'column', session: string | null]
}>()
const item = 'touch-target flex min-h-11 cursor-pointer items-center rounded px-3 py-1 outline-none data-disabled:text-muted data-highlighted:bg-bg'
const open = ref(false)
const splitStep = ref<'start' | 'position' | 'session'>('start')
const splitDirection = ref<'row' | 'column'>('row')
let focusKeyboardOnClose = false

watch(open, (isOpen) => {
  if (!isOpen) splitStep.value = 'start'
})

function choosePosition(direction: 'row' | 'column', event: Event) {
  splitDirection.value = direction
  splitStep.value = 'session'
  event.preventDefault()
}

function chooseSession(session: string | null) {
  open.value = false
  emit('split', splitDirection.value, session)
}

function chooseAction(name: 'search' | 'copy' | 'keyboard' | 'dictation' | 'snapshot' | 'photos' | 'diagnostics' | 'close') {
  // Wait for close-autofocus so xterm gets focus after menu selection while
  // preserving the tap's user activation for the phone keyboard.
  if (name === 'keyboard') {
    focusKeyboardOnClose = true
    return
  }
  emit('action', name)
}

function onCloseAutoFocus(event: Event) {
  if (!focusKeyboardOnClose) return
  focusKeyboardOnClose = false
  event.preventDefault()
  emit('action', 'keyboard')
}
</script>

<template>
  <DropdownMenuRoot v-model:open="open">
    <DropdownMenuTrigger type="button" aria-label="Terminal actions" title="Terminal actions" class="touch-target rounded px-2 text-xl font-extrabold leading-none">⋮</DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent align="end" :side-offset="4" aria-label="Terminal actions" class="z-50 max-h-[min(80dvh,36rem)] min-w-48 overflow-y-auto rounded border border-border bg-surface p-1 text-fg shadow-lg" @close-auto-focus="onCloseAutoFocus">
        <DropdownMenuItem :class="item" @select="chooseAction('search')">Search</DropdownMenuItem>
        <DropdownMenuItem :disabled="!props.hasSelection" :class="item" @select="chooseAction('copy')">Copy selected text</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="chooseAction('keyboard')">Show keyboard</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="chooseAction('dictation')">Dictation</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="chooseAction('snapshot')">View terminal text</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="chooseAction('photos')">Send photos to this repo</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="chooseAction('diagnostics')">Connection diagnostics</DropdownMenuItem>
        <template v-if="props.canSplit">
          <DropdownMenuItem v-if="splitStep === 'start'" :class="item" @select.prevent="splitStep = 'position'">Split pane…</DropdownMenuItem>
          <template v-else-if="splitStep === 'position'">
            <DropdownMenuLabel class="px-3 py-2 text-xs text-muted">Choose split position</DropdownMenuLabel>
            <DropdownMenuItem :class="item" @select="choosePosition('row', $event)">Split right</DropdownMenuItem>
            <DropdownMenuItem :class="item" @select="choosePosition('column', $event)">Split down</DropdownMenuItem>
          </template>
          <template v-else>
            <DropdownMenuItem :class="item" @select.prevent="splitStep = 'position'">← Position: {{ splitDirection === 'row' ? 'right' : 'down' }}</DropdownMenuItem>
            <DropdownMenuItem v-for="session in props.sessions" :key="session" :class="item" @select="chooseSession(session)">{{ session }}</DropdownMenuItem>
            <DropdownMenuItem :class="item" @select="chooseSession(null)">New session…</DropdownMenuItem>
          </template>
        </template>
        <DropdownMenuItem :class="item" @select="chooseAction('close')">Close pane</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>
