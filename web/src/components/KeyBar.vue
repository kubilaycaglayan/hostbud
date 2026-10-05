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
  { key: '-', label: 'Hyphen', text: '-' },
]
// Pinned outside the scrolling keys so it's always in reach.
const pinnedKey: { key: KeyBarKey; label: string; text: string } = { key: '/', label: 'Slash', text: '/' }
let repeatTimer: ReturnType<typeof setTimeout> | null = null
let repeatInterval: ReturnType<typeof setInterval> | null = null

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
  input(key)
  if (!key.startsWith('Arrow')) return
  stopRepeat()
  repeatTimer = setTimeout(() => {
    input(key)
    repeatInterval = setInterval(() => input(key), 80)
  }, 400)
}

function arm(event: PointerEvent, name: 'ctrl' | 'alt') {
  event.preventDefault()
  modifier(name)
}

function scroll(event: PointerEvent) {
  event.preventDefault()
  emit('scroll')
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
      <div class="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto px-1">
        <button
          v-for="item in leadingKeys"
          :key="item.key"
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          :aria-label="item.label"
          tabindex="-1"
          @pointerdown="press($event, item.key)"
          @pointerup="stopRepeat"
          @pointercancel="stopRepeat"
          @pointerleave="stopRepeat"
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
          @pointerup="stopRepeat"
          @pointercancel="stopRepeat"
          @pointerleave="stopRepeat"
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
      <button
        type="button"
        class="touch-target shrink-0 rounded border border-border px-2"
        :aria-label="pinnedKey.label"
        tabindex="-1"
        @pointerdown="press($event, pinnedKey.key)"
        @mousedown.prevent
      >
        {{ pinnedKey.text }}
      </button>
    </div>
  </section>
</template>
