<script setup lang="ts">
import '@xterm/xterm/css/xterm.css'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal } from '@xterm/xterm'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { TermSession, termURL, type SessionState } from '@/api/term'
import SessionPicker from '@/components/SessionPicker.vue'
import TerminalMenu from '@/components/TerminalMenu.vue'
import TerminalSearch from '@/components/TerminalSearch.vue'
import TabBar from '@/components/TabBar.vue'
import { copySelection, installOsc52 } from '@/lib/clipboard'
import { registerPane, unregisterPane } from '@/lib/e2eHooks'
import { hyperlinkHandler, openLink, type LinkHover } from '@/lib/links'
import { keepScrollback } from '@/lib/scrollback'
import type { SplitDir, Tab } from '@/lib/layout'
import { clipboardKey, editingKey, searchKey } from '@/lib/terminalKeys'
import { useAuthStore } from '@/stores/auth'
import { useSessionsStore } from '@/stores/sessions'

const props = withDefaults(
  defineProps<{
    machine: string
    session: string
    /** The layout pane this terminal shows (e2e hooks are keyed by it). */
    paneId?: string
    /** Its tab is the one shown (inactive tabs stay mounted and attached). */
    active?: boolean
    /** It's the focused pane of its tab: keyboard input goes here. */
    focused?: boolean
    /** Its place among its tab's panes (1-based) and their number. */
    paneIndex?: number
    paneCount?: number
    /** The tab can take another pane (Split right/down shown). */
    canSplit?: boolean
    /** Narrow layout: one pane at a time, with a pane switcher. */
    narrow?: boolean
    tabs?: Tab[]
    activeTab?: string | null
  }>(),
  { paneId: 'pane', active: true, focused: true, paneIndex: 1, paneCount: 1, canSplit: false, narrow: false },
)
const emit = defineEmits<{
  focus: []
  /** Open a session (null: a new one) in a new pane beside this one. */
  split: [dir: SplitDir, session: string | null]
  close: []
  cyclePane: []
  activateTab: [id: string]
  closeTab: [id: string]
}>()
const takesInput = () => props.active && props.focused

const el = ref<HTMLDivElement>()
const state = ref<SessionState>('connecting')
const attempt = ref(0)
const linkHover = ref<LinkHover | null>(null)
const term = shallowRef<Terminal>()
const search = shallowRef<SearchAddon>()
const searchOpen = ref(false)
const searchInitial = ref('')
const searchBar = ref<InstanceType<typeof TerminalSearch>>()
let fit: FitAddon | null = null
let conn: TermSession | null = null
let observer: ResizeObserver | null = null
let last = { cols: 0, rows: 0 }
const auth = useAuthStore()
const sessions = useSessionsStore()
const splitTargets = computed(() => sessions.list(props.machine).map((x) => x.name))

/** Attaches, and keeps re-attaching after drops (api/term.ts TermSession). */
function connect() {
  const t = term.value
  if (!t) return
  conn?.close()
  conn = new TermSession({
    url: () => {
      last = { cols: t.cols, rows: t.rows }
      return termURL(props.machine, props.session, t.cols, t.rows)
    },
    onData: (bytes) => t.write(bytes),
    onState: (s, info) => {
      state.value = s
      attempt.value = info.attempt
      if (s === 'open' && takesInput()) t.focus()
    },
    isListed: () => sessions.list(props.machine).some((x) => x.name === props.session),
    stillAuthorized: () => auth.stillAuthorized(),
    onSignedOut: () => auth.sessionEnded(),
  })
}

/** Fits the terminal to its box and tells the server about new sizes. */
function refit() {
  const t = term.value
  // A hidden tab has no size: keep tmux at its last one until it's shown.
  if (!t || !fit || !props.active) return
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

/** Sets up the terminal's hidden input for on-screen keyboards: no
 * autocorrect, capitalization or suggestions rewriting what's typed. */
function prepareInput(input: HTMLTextAreaElement | undefined) {
  if (!input) return
  input.setAttribute('autocorrect', 'off')
  input.setAttribute('autocapitalize', 'off')
  input.setAttribute('autocomplete', 'off')
  input.setAttribute('spellcheck', 'false')
}

/** Focuses the terminal, which brings up a phone's on-screen keyboard. */
function showKeyboard() {
  term.value?.focus()
}

/** Opens the search bar (or refocuses it), pre-filled with the selection's
 * first line. */
function openSearch() {
  const selected = term.value?.getSelection().split('\n')[0].trim() ?? ''
  if (searchOpen.value) {
    void searchBar.value?.focus(selected)
    return
  }
  searchInitial.value = selected
  searchOpen.value = true
}

function closeSearch() {
  searchOpen.value = false
  term.value?.focus()
}

function retryNow() {
  conn?.retryNow()
}

function reconnect() {
  term.value?.reset()
  connect()
}

// Showing a tab or focusing a pane moves the keyboard there.
watch(takesInput, (v) => {
  if (v) void nextTick(() => term.value?.focus())
})

onMounted(async () => {
  await document.fonts?.ready
  const t = new Terminal({
    allowProposedApi: true, // unicode11
    cursorBlink: true,
    fontFamily: "'JetBrains Mono', ui-monospace, monospace",
    fontSize: 14,
    // Option+drag selects even when the program captures the mouse (Shift+drag
    // does on other platforms).
    macOptionClickForcesSelection: true,
    // OSC 8 hyperlinks: http(s) only; hovering shows the real target.
    linkHandler: hyperlinkHandler((h) => (linkHover.value = h)),
    scrollback: 5000,
    theme: { background: '#0f1115', foreground: '#d7dae0', cursor: '#5fb3f9' },
  })
  fit = new FitAddon()
  t.loadAddon(fit)
  t.loadAddon(new WebLinksAddon((_ev, uri) => openLink(uri)))
  const unicode = new Unicode11Addon()
  t.loadAddon(unicode)
  t.unicode.activeVersion = '11'
  installOsc52(t)
  keepScrollback(t)
  search.value = new SearchAddon()
  t.loadAddon(search.value)
  t.open(el.value!)
  prepareInput(t.textarea)
  try {
    const webgl = new WebglAddon()
    webgl.onContextLoss(() => webgl.dispose())
    t.loadAddon(webgl)
  } catch {
    // No WebGL: xterm's DOM renderer is used.
  }
  term.value = t
  t.onData((d) => conn?.send(d))
  // Mac editing shortcuts (Option/Cmd+Backspace, +←/→): send what a Mac
  // terminal sends, once per keydown, instead of xterm's or the browser's
  // default (Cmd+← would navigate back).
  t.attachCustomKeyEventHandler((ev) => {
    const bytes = editingKey(ev)
    if (bytes !== undefined) {
      if (ev.type === 'keydown') {
        ev.preventDefault()
        t.input(bytes)
      }
      return false
    }
    if (searchKey(ev)) {
      if (ev.type === 'keydown') {
        ev.preventDefault() // the browser's own find
        openSearch()
      }
      return false
    }
    // Copy/paste shortcuts. Paste keeps the browser default: its paste event
    // reaches xterm, which sends the text as bracketed paste (and needs no
    // clipboard-read permission).
    const action = clipboardKey(ev, t.hasSelection())
    if (action === undefined) return true
    if (action === 'copy' && ev.type === 'keydown') {
      ev.preventDefault() // Ctrl+Shift+C would open the browser's inspector
      void copySelection(t)
    }
    return false
  })
  refit()
  connect()
  observer = new ResizeObserver(() => refit())
  observer.observe(el.value!)
  // Test hook, e2e builds only (a constant condition: dropped otherwise).
  if (import.meta.env.VITE_E2E === '1')
    registerPane(props.paneId, {
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
      termSize: () => ({ cols: t.cols, rows: t.rows }),
      termSelection: () => t.getSelection(),
      termViewport: () => {
        const b = t.buffer.active
        const rows: string[] = []
        for (let i = 0; i < t.rows; i++) rows.push(b.getLine(b.viewportY + i)?.translateToString(true) ?? '')
        return rows.join('\n')
      },
      termTextRect: (needle) => {
        // The last on-screen occurrence, in page pixels (cells are laid out
        // evenly over the screen element).
        const b = t.buffer.active
        const screen = t.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
        if (!screen) return null
        const cw = screen.width / t.cols
        const ch = screen.height / t.rows
        for (let row = t.rows - 1; row >= 0; row--) {
          const col = b.getLine(b.viewportY + row)?.translateToString(true).indexOf(needle) ?? -1
          if (col >= 0)
            return { x: screen.left + col * cw, y: screen.top + row * ch, width: needle.length * cw, height: ch }
        }
        return null
      },
    }, () => ({ session: props.session, active: props.active, focused: takesInput() }))
})

onBeforeUnmount(() => {
  observer?.disconnect()
  conn?.close()
  term.value?.dispose()
  if (import.meta.env.VITE_E2E === '1') unregisterPane(props.paneId)
})

defineExpose({ refit, reconnect, showKeyboard })
</script>

<template>
  <section
    :aria-label="`Terminal: ${props.session}`"
    class="relative flex h-full min-h-0 flex-col"
    :class="props.paneCount > 1 && takesInput() && !props.narrow ? 'outline-1 -outline-offset-1 outline-accent outline' : ''"
    :data-focused="takesInput() ? 'true' : undefined"
    @focusin="emit('focus')"
  >
    <div class="flex min-w-0 items-center gap-2 border-b border-border px-2 py-1.5">
      <h2 class="max-w-[22vw] shrink truncate font-bold">
        {{ props.session }}
      </h2>
      <TabBar
        v-if="props.narrow && props.tabs && props.activeTab !== undefined"
        :tabs="props.tabs"
        :active="props.activeTab"
        compact
        @activate="emit('activateTab', $event)"
        @close="emit('closeTab', $event)"
      />
      <!-- Narrow screens show one pane of a split at a time. -->
      <button
        v-if="props.narrow && props.paneCount > 1"
        type="button"
        class="shrink-0 rounded border border-border px-2"
        :aria-label="`Pane ${props.paneIndex} of ${props.paneCount}: show the next pane`"
        @click="emit('cyclePane')"
      >
        Pane {{ props.paneIndex }} of {{ props.paneCount }}
      </button>
      <span class="ml-auto" />
      <template v-if="props.canSplit && !props.narrow">
        <SessionPicker
          label="Split right"
          icon="◫"
          :sessions="splitTargets"
          @pick="(n) => emit('split', 'row', n)"
          @new="emit('split', 'row', null)"
        />
        <SessionPicker
          label="Split down"
          icon="⊟"
          :sessions="splitTargets"
          @pick="(n) => emit('split', 'column', n)"
          @new="emit('split', 'column', null)"
        />
      </template>
      <button
        type="button"
        aria-label="Search"
        class="rounded border border-border px-2"
        @click="openSearch"
      >
        🔍
      </button>
      <!-- Touch screens: bring the on-screen keyboard back once dismissed. -->
      <button
        type="button"
        aria-label="Show keyboard"
        class="hidden rounded border border-border px-2 pointer-coarse:inline-block"
        @click="showKeyboard"
      >
        ⌨
      </button>
      <button
        type="button"
        aria-label="Close pane"
        title="Close pane (the session keeps running)"
        class="rounded px-2 text-muted hover:text-fg"
        @click="emit('close')"
      >
        ×
      </button>
    </div>
    <TerminalMenu :term="term">
      <!-- The menu's trigger; the ref sits inside it (as-child clones it). -->
      <div class="relative flex min-h-0 flex-1 flex-col">
        <div
          ref="el"
          data-testid="terminal"
          class="min-h-0 flex-1 touch-manipulation overflow-hidden bg-bg p-1"
        />
        <div
          v-if="linkHover && el"
          role="tooltip"
          class="pointer-events-none absolute z-20 max-w-[80%] truncate rounded border border-border bg-surface px-2 py-1 text-fg shadow"
          :style="{
            left: `${linkHover.x - el.getBoundingClientRect().left}px`,
            top: `${linkHover.y - el.getBoundingClientRect().top + 16}px`,
          }"
        >
          {{ linkHover.url }}
        </div>
        <TerminalSearch
          v-if="searchOpen && search"
          ref="searchBar"
          :search="search"
          :initial="searchInitial"
          @close="closeSearch"
        />
        <!-- Over the terminal, so its size (and tmux's) doesn't change. -->
        <div
          v-if="state === 'reconnecting'"
          role="status"
          class="absolute inset-x-0 top-0 z-10 flex items-center justify-between gap-3 border-b border-border bg-surface/90 px-3 py-1"
        >
          <span>Reconnecting… (attempt {{ attempt }})</span>
          <button
            type="button"
            class="rounded border border-border px-2"
            @click="retryNow"
          >
            Retry now
          </button>
        </div>
      </div>
    </TerminalMenu>
    <div
      v-if="state === 'exited' || state === 'disconnected'"
      role="status"
      class="absolute inset-x-0 bottom-0 z-10 flex items-center justify-between gap-3 border-t border-border bg-surface px-3 py-2"
    >
      <span>{{ state === 'exited' ? 'Session detached or ended.' : 'Disconnected: the session is gone.' }}</span>
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
