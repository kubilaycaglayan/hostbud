<script setup lang="ts">
import type { ISearchOptions, SearchAddon } from '@xterm/addon-search'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { searchDecorations } from '@/lib/theme'
import { useThemeStore } from '@/stores/theme'

// The terminal's search bar. It searches xterm's buffer: the screen plus the
// scrollback received since attaching (older tmux history is copy mode's
// job).
const props = defineProps<{
  search: Pick<SearchAddon, 'findNext' | 'findPrevious' | 'clearDecorations' | 'onDidChangeResults'>
  initial: string
}>()
const emit = defineEmits<{ close: [] }>()

const theme = useThemeStore()

const query = ref(props.initial)
const caseSensitive = ref(false)
const regex = ref(false)
const invalid = ref(false)
const results = ref<{ index: number; count: number } | null>(null)
const input = ref<HTMLInputElement>()

const sub = props.search.onDidChangeResults((e) => (results.value = { index: e.resultIndex, count: e.resultCount }))

const status = computed(() => {
  if (invalid.value) return 'Invalid pattern'
  if (!query.value || !results.value) return ''
  const { index, count } = results.value
  if (count === 0) return 'No results'
  return index < 0 ? `${count} found` : `${index + 1} of ${count}`
})

function isValid(): boolean {
  if (!regex.value) return true
  try {
    new RegExp(query.value)
    return true
  } catch {
    return false
  }
}

function run(dir: 'next' | 'previous', incremental = false) {
  invalid.value = !isValid()
  if (invalid.value || !query.value) {
    props.search.clearDecorations()
    results.value = null
    return
  }
  const opts: ISearchOptions = {
    caseSensitive: caseSensitive.value,
    regex: regex.value,
    incremental,
    decorations: searchDecorations[theme.resolved],
  }
  if (dir === 'next') props.search.findNext(query.value, opts)
  else props.search.findPrevious(query.value, opts)
}

// Typing searches as you go; option changes search again. The addon only
// re-highlights for a new term (it compares the options after storing
// them), so an option change clears its cache first.
watch(query, () => run('next', true))
watch(() => theme.resolved, () => run('next', true))
watch([caseSensitive, regex], () => {
  props.search.clearDecorations()
  run('next', true)
})

function onKey(ev: KeyboardEvent) {
  if (ev.key === 'Enter') {
    ev.preventDefault()
    run(ev.shiftKey ? 'previous' : 'next')
  } else if (ev.key === 'Escape') {
    ev.preventDefault()
    close()
  }
}

function close() {
  props.search.clearDecorations()
  emit('close')
}

/** Focuses the field again (search reopened), with new text if given. */
async function focus(text?: string) {
  if (text) query.value = text
  await nextTick()
  input.value?.focus()
  input.value?.select()
}

onMounted(() => {
  void focus()
  if (query.value) run('next')
})
onBeforeUnmount(() => sub.dispose())

defineExpose({ focus })
</script>

<template>
  <div
    role="search"
    aria-label="Search the terminal"
    class="absolute top-1 right-1 z-20 flex max-w-[calc(100%-0.5rem)] flex-col gap-1 rounded border border-border bg-surface p-2 shadow-lg"
  >
    <div class="flex items-center gap-1">
      <input
        ref="input"
        v-model="query"
        type="text"
        aria-label="Find"
        placeholder="Find"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        class="w-40 min-w-0 rounded border border-border bg-bg px-2 py-1 text-base text-fg sm:w-56 sm:text-sm"
        @keydown="onKey"
      >
      <button
        type="button"
        aria-label="Previous match"
        class="touch-target rounded border border-border px-2 py-1"
        @click="run('previous')"
      >
        ↑
      </button>
      <button
        type="button"
        aria-label="Next match"
        class="touch-target rounded border border-border px-2 py-1"
        @click="run('next')"
      >
        ↓
      </button>
      <button
        type="button"
        aria-label="Close search"
        class="touch-target rounded px-2 py-1 text-muted"
        @click="close"
      >
        ✕
      </button>
    </div>
    <div class="flex items-center gap-3 text-muted">
      <label class="flex items-center gap-1">
        <input
          v-model="caseSensitive"
          type="checkbox"
        >
        Match case
      </label>
      <label class="flex items-center gap-1">
        <input
          v-model="regex"
          type="checkbox"
        >
        Regex
      </label>
      <span
        aria-live="polite"
        data-testid="search-results"
        class="ml-auto"
        :class="invalid ? 'text-danger' : ''"
      >{{ status }}</span>
    </div>
    <p
      v-if="!query"
      class="max-w-64 text-muted"
    >
      Searches the output received since attaching. For older history use tmux copy mode (prefix [ then ?).
    </p>
  </div>
</template>
