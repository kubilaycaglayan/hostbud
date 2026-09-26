<script setup lang="ts">
import '@xterm/xterm/css/xterm.css'
import { FitAddon } from '@xterm/addon-fit'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal } from '@xterm/xterm'
import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { TermConnection, termURL, type TermState } from '@/api/term'
import { installE2EHooks, removeE2EHooks } from '@/lib/e2eHooks'

const props = defineProps<{ machine: string; session: string }>()
const emit = defineEmits<{ back: [] }>()

const el = ref<HTMLDivElement>()
const state = ref<TermState>('connecting')
const exitCode = ref<number>()
const term = shallowRef<Terminal>()
let fit: FitAddon | null = null
let conn: TermConnection | null = null
let observer: ResizeObserver | null = null
let last = { cols: 0, rows: 0 }

function connect() {
  const t = term.value
  if (!t) return
  conn?.close()
  state.value = 'connecting'
  exitCode.value = undefined
  last = { cols: t.cols, rows: t.rows }
  conn = new TermConnection(termURL(props.machine, props.session, t.cols, t.rows), {
    onData: (bytes) => t.write(bytes),
    onState: (s, code) => {
      state.value = s
      exitCode.value = code
      if (s === 'open') t.focus()
    },
  })
}

/** Fits the terminal to its box and tells the server about new sizes. */
function refit() {
  const t = term.value
  if (!t || !fit) return
  try {
    fit.fit()
  } catch {
    return // not laid out yet
  }
  if (t.cols !== last.cols || t.rows !== last.rows) {
    last = { cols: t.cols, rows: t.rows }
    conn?.resize(t.cols, t.rows)
  }
}

function reconnect() {
  term.value?.reset()
  connect()
}

onMounted(async () => {
  await document.fonts?.ready
  const t = new Terminal({
    allowProposedApi: true, // unicode11
    cursorBlink: true,
    fontFamily: "'JetBrains Mono', ui-monospace, monospace",
    fontSize: 14,
    scrollback: 5000,
    theme: { background: '#0f1115', foreground: '#d7dae0', cursor: '#5fb3f9' },
  })
  fit = new FitAddon()
  t.loadAddon(fit)
  t.loadAddon(new WebLinksAddon())
  const unicode = new Unicode11Addon()
  t.loadAddon(unicode)
  t.unicode.activeVersion = '11'
  t.open(el.value!)
  try {
    const webgl = new WebglAddon()
    webgl.onContextLoss(() => webgl.dispose())
    t.loadAddon(webgl)
  } catch {
    // No WebGL: xterm's DOM renderer is used.
  }
  term.value = t
  t.onData((d) => conn?.send(d))
  refit()
  connect()
  observer = new ResizeObserver(() => refit())
  observer.observe(el.value!)
  // Test hook, e2e builds only (a constant condition: dropped otherwise).
  if (import.meta.env.VITE_E2E === '1')
    installE2EHooks({
      termText: () => {
        const b = t.buffer.active
        const lines: string[] = []
        for (let i = 0; i < b.length; i++) {
          const line = b.getLine(i)
          const text = line?.translateToString(true) ?? ''
          // A wrapped row continues the previous line.
          if (line?.isWrapped && lines.length) lines[lines.length - 1] += text
          else lines.push(text)
        }
        return lines.join('\n').trimEnd()
      },
    })
})

onBeforeUnmount(() => {
  observer?.disconnect()
  conn?.close()
  term.value?.dispose()
  if (import.meta.env.VITE_E2E === '1') removeE2EHooks()
})

defineExpose({ refit, reconnect })
</script>

<template>
  <section
    :aria-label="`Terminal: ${props.session}`"
    class="relative flex h-full min-h-0 flex-col"
  >
    <div class="flex items-center gap-2 border-b border-border px-3 py-2">
      <button
        type="button"
        aria-label="Back to sessions"
        class="rounded border border-border px-2 md:hidden"
        @click="emit('back')"
      >
        ←
      </button>
      <h2 class="truncate font-bold">
        {{ props.session }}
      </h2>
    </div>
    <div
      ref="el"
      data-testid="terminal"
      class="min-h-0 flex-1 overflow-hidden bg-bg p-1"
    />
    <div
      v-if="state === 'exited' || state === 'disconnected'"
      role="status"
      class="absolute inset-x-0 bottom-0 z-10 flex items-center justify-between gap-3 border-t border-border bg-surface px-3 py-2"
    >
      <span>{{ state === 'exited' ? 'Session detached or ended.' : 'Disconnected from the terminal.' }}</span>
      <button
        type="button"
        class="rounded bg-accent px-3 py-1 font-bold text-bg"
        @click="reconnect"
      >
        Reconnect
      </button>
    </div>
  </section>
</template>
