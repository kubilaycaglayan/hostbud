<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import type { Terminal } from '@xterm/xterm'
import { useMediaQuery } from '@/lib/media'
import { sendKey, toggleModifier, type KeyBarKey, type KeyModifiers } from '@/lib/keyBar'

const props = withDefaults(defineProps<{ term?: Terminal; modifiers: KeyModifiers; focused: boolean; busy?: boolean; scrollEnabled?: boolean }>(), { scrollEnabled: true })
const emit = defineEmits<{ scroll: [] }>()
const coarse = useMediaQuery('(pointer: coarse)')
const collapsed = ref(false)
const visible = computed(() => coarse.value && props.focused && !!props.term)
const leadingKeys: { key: KeyBarKey; label: string; text: string }[] = [
  { key: 'Escape', label: 'Escape', text: 'Esc' },
  { key: 'Tab', label: 'Tab', text: 'Tab' },
]
const keys: { key: KeyBarKey; label: string; text: string }[] = [
  { key: 'ArrowLeft', label: 'Left arrow', text: '←' },
  { key: 'ArrowUp', label: 'Up arrow', text: '↑' },
  { key: 'ArrowDown', label: 'Down arrow', text: '↓' },
  { key: 'ArrowRight', label: 'Right arrow', text: '→' },
  { key: '|', label: 'Pipe', text: '|' },
  { key: '~', label: 'Tilde', text: '~' },
  { key: '/', label: 'Slash', text: '/' },
  { key: '-', label: 'Hyphen', text: '-' },
]
let repeatTimer: ReturnType<typeof setTimeout> | null = null
let repeatInterval: ReturnType<typeof setInterval> | null = null
let pendingKey: { key?: KeyBarKey; action?: () => void; pointerId: number; x: number; y: number; sent: boolean } | null = null

function input(key: KeyBarKey) {
  const terminal = props.term
  if (!terminal) return
  terminal.input(sendKey(key, terminal.modes.applicationCursorKeysMode, props.modifiers), true)
}

function modifier(name: 'ctrl' | 'alt') {
  toggleModifier(props.modifiers, name)
}

function stopRepeat() {
  if (repeatTimer) clearTimeout(repeatTimer)
  if (repeatInterval) clearInterval(repeatInterval)
  repeatTimer = null
  repeatInterval = null
}

function press(event: PointerEvent, key: KeyBarKey) {
  event.preventDefault()
  stopRepeat()
  pendingKey = { key, pointerId: event.pointerId, x: event.clientX, y: event.clientY, sent: false }
  if (key.startsWith('Arrow')) {
    repeatTimer = setTimeout(() => {
      if (!pendingKey || pendingKey.pointerId !== event.pointerId || !pendingKey.key) return
      pendingKey.sent = true
      input(key)
      repeatInterval = setInterval(() => input(key), 80)
    }, 400)
  }
}

function move(event: PointerEvent) {
  if (!pendingKey || pendingKey.pointerId !== event.pointerId) return
  if (Math.abs(event.clientX - pendingKey.x) > 8 || Math.abs(event.clientY - pendingKey.y) > 8) {
    pendingKey = null
    stopRepeat()
  }
}

function release(event: PointerEvent) {
  if (pendingKey?.pointerId === event.pointerId) {
    if (!pendingKey.sent) {
      if (pendingKey.key) input(pendingKey.key)
      else pendingKey.action?.()
    }
    pendingKey = null
  }
  stopRepeat()
}

function cancel(event: PointerEvent) {
  if (pendingKey?.pointerId === event.pointerId) pendingKey = null
  stopRepeat()
}

function arm(event: PointerEvent, name: 'ctrl' | 'alt') {
  event.preventDefault()
  pendingKey = { action: () => modifier(name), pointerId: event.pointerId, x: event.clientX, y: event.clientY, sent: false }
}

function scroll(event: PointerEvent) {
  event.preventDefault()
  pendingKey = { action: () => emit('scroll'), pointerId: event.pointerId, x: event.clientX, y: event.clientY, sent: false }
}

onBeforeUnmount(stopRepeat)
</script>

<template>
  <section
    v-if="visible"
    aria-label="On-screen key bar"
    class="shrink-0 border-t border-border bg-surface"
    data-testid="key-bar"
  >
    <button
      v-if="collapsed"
      type="button"
      class="touch-target w-full rounded text-xs text-muted"
      aria-label="Show key bar"
      tabindex="-1"
      @pointerdown.prevent="collapsed = false"
      @mousedown.prevent
      @click="collapsed = false"
    >
      ⌃ Show key bar
    </button>
    <div v-else class="flex min-h-12 items-center gap-1 pr-1">
      <div class="flex min-w-0 flex-1 touch-pan-x items-center gap-1 overflow-x-auto px-1">
        <button
          v-for="item in leadingKeys"
          :key="item.key"
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          :aria-label="item.label"
          tabindex="-1"
          @pointerdown="press($event, item.key)"
          @pointermove="move"
          @pointerup="release"
          @pointercancel="cancel"
          @pointerleave="cancel"
          @mousedown.prevent
        >
          {{ item.text }}
        </button>
        <button
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          aria-label="Control"
          :aria-pressed="props.modifiers.ctrl.armed"
          :class="props.modifiers.ctrl.armed ? 'bg-accent text-bg' : ''"
          tabindex="-1"
          @pointerdown="arm($event, 'ctrl')"
          @pointermove="move"
          @pointerup="release"
          @pointercancel="cancel"
          @pointerleave="cancel"
          @mousedown.prevent
        >
          Ctrl
        </button>
        <button
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          aria-label="Alt"
          :aria-pressed="props.modifiers.alt.armed"
          :class="props.modifiers.alt.armed ? 'bg-accent text-bg' : ''"
          tabindex="-1"
          @pointerdown="arm($event, 'alt')"
          @pointermove="move"
          @pointerup="release"
          @pointercancel="cancel"
          @pointerleave="cancel"
          @mousedown.prevent
        >
          Alt
        </button>
        <button
          v-for="item in keys"
          :key="item.key"
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          :aria-label="item.label"
          tabindex="-1"
          @pointerdown="press($event, item.key)"
          @pointermove="move"
          @pointerup="release"
          @pointercancel="cancel"
          @pointerleave="cancel"
          @mousedown.prevent
        >
          {{ item.text }}
        </button>
        <button
          v-if="props.scrollEnabled"
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          aria-label="Scroll history"
          :disabled="props.busy"
          tabindex="-1"
          @pointerdown="scroll"
          @pointermove="move"
          @pointerup="release"
          @pointercancel="cancel"
          @pointerleave="cancel"
          @mousedown.prevent
        >
          Scroll
        </button>
        <button
          type="button"
          class="touch-target ml-auto shrink-0 rounded px-2 text-muted"
          aria-label="Hide key bar"
          tabindex="-1"
          @pointerdown.prevent="collapsed = true"
          @mousedown.prevent
          @click="collapsed = true"
        >
          ⌄
        </button>
      </div>
    </div>
  </section>
</template>
