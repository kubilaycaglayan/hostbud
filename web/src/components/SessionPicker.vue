<script setup lang="ts">
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { ref } from 'vue'

// Picks the session for a new split pane (M3 T8): a live session, or a new
// one (the create dialog).
const props = defineProps<{ label: string; icon: string; sessions: string[] }>()
const emit = defineEmits<{ pick: [name: string]; new: [] }>()

const open = ref(false)

function pick(name: string) {
  picked = true
  open.value = false
  emit('pick', name)
}

// After a pick the new pane takes focus: returning it to this trigger (in
// the old pane) would focus the old pane again.
let picked = false
function onCloseFocus(ev: Event) {
  if (picked) ev.preventDefault()
  picked = false
}

function create() {
  picked = true
  open.value = false
  emit('new')
}
</script>

<template>
  <PopoverRoot v-model:open="open">
    <PopoverTrigger
      :aria-label="props.label"
      :title="props.label"
      class="rounded border border-border px-2"
    >
      {{ props.icon }}
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent
        :aria-label="props.label"
        align="end"
        :side-offset="4"
        class="z-30 max-h-72 w-56 overflow-y-auto rounded border border-border bg-surface p-1 text-fg shadow-lg"
        @close-auto-focus="onCloseFocus"
      >
        <p class="px-2 py-1 text-muted">
          {{ props.label }}
        </p>
        <ul :aria-label="`Sessions to open (${props.label})`">
          <li
            v-for="s in props.sessions"
            :key="s"
          >
            <button
              type="button"
              class="touch-target w-full truncate rounded px-2 py-1 text-left hover:bg-bg"
              @click="pick(s)"
            >
              {{ s }}
            </button>
          </li>
        </ul>
        <button
          type="button"
          class="touch-target mt-1 w-full rounded border-t border-border px-2 py-1 text-left hover:bg-bg"
          @click="create"
        >
          New session…
        </button>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
