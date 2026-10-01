<script setup lang="ts">
import '@xterm/xterm/css/xterm.css'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal } from '@xterm/xterm'
import { Folder } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from 'vue'
import { TermSession, termURL, type SessionState } from '@/api/term'
import { ApiError, copyModeApi, filesystemApi, terminalOutputApi } from '@/api/client'
import { blurActiveFieldOnHide } from '@/lib/pageFocus'
import TerminalMenu from '@/components/TerminalMenu.vue'
import TerminalActions from '@/components/TerminalActions.vue'
import TerminalTextDialog from '@/components/TerminalTextDialog.vue'
import PhotoUploadDialog from '@/components/PhotoUploadDialog.vue'
import TerminalSearch from '@/components/TerminalSearch.vue'
import KeyBar from '@/components/KeyBar.vue'
import ScrollBar from '@/components/ScrollBar.vue'
import { copySelection, installOsc52 } from '@/lib/clipboard'
import { clipboardImages } from '@/lib/clipboardImages'
import { registerPane, unregisterPane } from '@/lib/e2eHooks'
import { hyperlinkHandler, openLink, type LinkHover } from '@/lib/links'
import { keepScrollback, WHEEL_SMOOTH_SCROLL_MS } from '@/lib/scrollback'
import { cellAt, moveCaret, settleAfterWrites } from '@/lib/altClick'
import { darkTerminalTheme, dimmedTerminalTheme, lightTerminalTheme, solarizedTerminalTheme, TERMINAL_MIN_CONTRAST } from '@/lib/theme'
import type { SplitDir } from '@/lib/layout'
import { clipboardKey, editingKey, searchKey } from '@/lib/terminalKeys'
import { applyModifiers, createModifiers } from '@/lib/keyBar'
import { createCopyModeController } from '@/lib/copyMode'
import { useAuthStore } from '@/stores/auth'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useTreeStore } from '@/stores/tree'
import { useToastsStore } from '@/stores/toasts'
import { useThemeStore } from '@/stores/theme'
import { shouldInterceptGlobalShortcut, shortcutPlatform } from '@/lib/shortcuts'
import { attachTouchScroll } from '@/lib/touchScroll'

const props = withDefaults(
  defineProps<{
    machine: string
    session: string
    /** The layout pane this terminal shows (e2e hooks are keyed by it). */
    paneId?: string
    /** Its layout is the one shown; inactive views stay mounted but detached. */
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
  }>(),
  { paneId: 'pane', active: true, focused: true, paneIndex: 1, paneCount: 1, canSplit: false, narrow: false },
)
const emit = defineEmits<{
  focus: []
  /** Open a session (null: a new one) in a new pane beside this one. */
  split: [dir: SplitDir, session: string | null]
  close: []
  cyclePane: []
}>()
const takesInput = () => props.active && props.focused

const el = ref<HTMLDivElement>()
const state = ref<SessionState>('connecting')
const attempt = ref(0)
const linkHover = ref<LinkHover | null>(null)
const hasSelection = ref(false)
const term = shallowRef<Terminal>()
const search = shallowRef<SearchAddon>()
const searchOpen = ref(false)
const dictationOpen = ref(false)
const snapshotOpen = ref(false)
const terminalSnapshot = ref('')
const snapshotLoading = ref(false)
const snapshotError = ref('')
const photoUploadOpen = ref(false)
let snapshotRequest = 0
const searchInitial = ref('')
const searchBar = ref<InstanceType<typeof TerminalSearch>>()
const modifiers = reactive(createModifiers())
let fit: FitAddon | null = null
let conn: TermSession | null = null
let observer: ResizeObserver | null = null
let selectionChange: { dispose: () => void } | null = null
let disposeBackgroundBlur = () => {}
let disposeVisibilityListener = () => {}
let disposeTouchScroll = () => {}
let disposeClipboardImagePaste = () => {}
let attachmentSuspended = false
let disposed = false
let touchSelectTimer: ReturnType<typeof setTimeout> | null = null
let last = { cols: 0, rows: 0 }
const auth = useAuthStore()
const theme = useThemeStore()
const projects = useProjectsStore()
const sessions = useSessionsStore()
const tree = useTreeStore()
const splitTargets = computed(() => sessions.list(props.machine).map((x) => x.name))
const currentSession = computed(() => sessions.list(props.machine).find((x) => x.name === props.session))
const sessionDirectory = computed(() => currentSession.value?.path ?? '')
const sessionDirectoryName = computed(() => sessionDirectory.value.split('/').filter(Boolean).at(-1) ?? '/')
const sessionSectionColor = computed(() => {
  const projectId = tree.groups.groups.find((group) => group.sessions.some((session) => session.name === props.session))?.project.id
  if (!projectId) return ''
  const sectionId = tree.order.projectSections[projectId]
  const color = tree.order.sections.find((section) => section.id === sectionId)?.color
  return color ? `var(--hb-section-${color})` : ''
})
const uploadDirectory = computed(() => {
  const session = sessions.list(props.machine).find((x) => x.name === props.session)
  if (!session) return ''
  return projects.items.find((project) => project.id === session.projectId)?.path ?? session.path ?? ''
})
const copyMode = createCopyModeController(
  (action, lines) => copyModeApi.action(props.machine, props.session, action, lines),
  (error) => useToastsStore().error('Could not scroll terminal history', error),
)
const { inMode, scrollPosition, historySize, busy } = copyMode
let touchSelectStart: { x: number; y: number } | null = null

function enterScrollMode() {
  void copyMode.action('enter')
}

function scrollAction(action: Parameters<typeof copyMode.action>[0], lines?: number) {
  void copyMode.action(action, lines)
}

function startTouchSelection(event: PointerEvent) {
  clearTouchSelectTimer()
  touchSelectStart = null
  if (event.pointerType !== 'touch') return
  touchSelectStart = { x: event.clientX, y: event.clientY }
  const gesture = touchSelectStart
  touchSelectTimer = window.setTimeout(() => {
    touchSelectTimer = null
    if (touchSelectStart !== gesture) return
    if (selectTouchWord(gesture.x, gesture.y)) touchSelectStart = null
  }, 450)
}

function cancelTouchSelectionOnMove(event: PointerEvent) {
  const start = touchSelectStart
  if (!start || event.pointerType !== 'touch') return
  if (Math.hypot(event.clientX - start.x, event.clientY - start.y) >= 8) {
    clearTouchSelectTimer()
    touchSelectStart = null
  }
}

function finishTouchSelection() {
  clearTouchSelectTimer()
  touchSelectStart = null
}

function cellHeight() {
  const screen = term.value?.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
  return screen && term.value ? screen.height / term.value.rows : 16
}

function clearTouchSelectTimer() {
  if (touchSelectTimer !== null) window.clearTimeout(touchSelectTimer)
  touchSelectTimer = null
}

/** Long-press selects the word under a touch; xterm's canvas has no DOM text
 * for iOS to select. The context menu's Copy action can then copy it. */
function selectTouchWord(x: number, y: number): boolean {
  const t = term.value
  const screen = t?.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
  if (!t || !screen || !screen.width || !screen.height) return false
  const column = Math.max(0, Math.min(t.cols - 1, Math.floor((x - screen.left) / (screen.width / t.cols))))
  const row = Math.max(0, Math.min(t.rows - 1, Math.floor((y - screen.top) / (screen.height / t.rows))))
  const buffer = t.buffer.active
  const bufferRow = buffer.viewportY + row
  const line = buffer.getLine(bufferRow)
  if (!line) return false
  const cell = (col: number) => col >= 0 && col < t.cols ? line.getCell(col) : undefined
  const isWord = (value: string) => /[\p{L}\p{M}\p{N}_-]/u.test(value)
  let start = column
  while (start > 0 && (cell(start)?.getWidth() ?? 1) === 0) start--
  const selected = cell(start)
  if (!selected || !isWord(selected.getChars())) return false
  let end = start + Math.max(1, selected.getWidth())
  while (start > 0) {
    let previous = start - 1
    while (previous > 0 && (cell(previous)?.getWidth() ?? 1) === 0) previous--
    if (!isWord(cell(previous)?.getChars() ?? '')) break
    start = previous
  }
  while (end < t.cols) {
    const next = cell(end)
    if (!next) break
    if (next.getWidth() === 0) {
      end++
      continue
    }
    if (!isWord(next.getChars())) break
    end += Math.max(1, next.getWidth())
  }
  t.select(start, bufferRow, end - start)
  return true
}

// Option/Alt-click moves the program's caret to the clicked cell (M8 T5,
// lib/altClick.ts). A new click or key press cancels a move in progress.
let altClickStart: { x: number; y: number; time: number } | null = null
let caretMove = 0

function startAltClick(event: MouseEvent) {
  caretMove++
  altClickStart = event.button === 0 && event.altKey ? { x: event.clientX, y: event.clientY, time: event.timeStamp } : null
}

function finishAltClick(event: MouseEvent) {
  const start = altClickStart
  altClickStart = null
  const t = term.value
  if (!start || !t || !conn || event.button !== 0 || !event.altKey) return
  // A drag or a long press selects (Option+drag), it doesn't move the caret.
  if (event.timeStamp - start.time > 500 || Math.hypot(event.clientX - start.x, event.clientY - start.y) > 4) return
  if (copyMode.inMode.value || t.getSelection().length > 1) return
  const buffer = t.buffer.active
  if (buffer.viewportY !== buffer.baseY) return // scrolled back: not the prompt
  // With the mouse captured, Alt+click belongs to the program (Option+click
  // is forced to local selection on macOS).
  if (t.modes.mouseTrackingMode !== 'none' && shortcutPlatform() !== 'mac') return
  const screen = t.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
  if (!screen || !screen.width || !screen.height) return
  const target = cellAt(event, screen, t.cols, t.rows)
  const generation = ++caretMove
  void moveCaret({
    cursor: () => ({ x: t.buffer.active.cursorX, y: t.buffer.active.cursorY }),
    rowText: (y) => t.buffer.active.getLine(t.buffer.active.baseY + y)?.translateToString(true) ?? '',
    send: (data) => conn?.send(data),
    settle: () => settleAfterWrites(t),
    cancelled: () => generation !== caretMove || term.value !== t,
  }, target, t.modes.applicationCursorKeysMode)
}

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
      if (s === 'limited') {
        useToastsStore().push({
          title: 'Too many open terminals',
          message: 'Close some tabs or panes; each open terminal keeps an ssh process on the host.',
          tone: 'error',
        })
      }
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
  if (!t || !fit || !props.active || document.visibilityState !== 'visible') return
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

/** A connected tmux client participates in the session's shared size
 * calculation even when its page is hidden. Detach inactive views so their
 * stale PTY dimensions cannot constrain another visible terminal. */
function syncAttachment() {
  const shouldAttach = props.active && document.visibilityState === 'visible'
  if (!shouldAttach) {
    if (conn) {
      conn.close()
      conn = null
    }
    attachmentSuspended = true
    return
  }
  if (!attachmentSuspended && conn) return
  attachmentSuspended = false
  refit()
  connect()
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

function copySelectedText() {
  if (term.value) void copySelection(term.value)
}

async function openSnapshot() {
  const request = ++snapshotRequest
  terminalSnapshot.value = ''
  snapshotError.value = ''
  snapshotLoading.value = true
  snapshotOpen.value = true
  try {
    const result = await terminalOutputApi.read(props.machine, props.session)
    if (request === snapshotRequest) terminalSnapshot.value = result.output
  } catch (error) {
    if (request === snapshotRequest) snapshotError.value = error instanceof Error
      ? `Could not load terminal history: ${error.message}. Try again.`
      : 'Could not load terminal history. Check the host connection and try again.'
  } finally {
    if (request === snapshotRequest) snapshotLoading.value = false
  }
}

watch(snapshotOpen, (open) => {
  if (!open) { snapshotRequest++; terminalSnapshot.value = '' }
})

function onToolbarAction(action: 'search' | 'copy' | 'keyboard' | 'dictation' | 'snapshot' | 'photos' | 'close') {
  if (action === 'search') openSearch()
  else if (action === 'copy') copySelectedText()
  else if (action === 'keyboard') showKeyboard()
  else if (action === 'dictation') dictationOpen.value = true
  else if (action === 'snapshot') openSnapshot()
  else if (action === 'photos') photoUploadOpen.value = true
  else emit('close')
}

function sendDictation(text: string) {
  term.value?.paste(text)
}

function relativePhotoPath(absolutePath: string) {
  const sessionPath = sessions.list(props.machine).find((session) => session.name === props.session)?.path ?? uploadDirectory.value
  const from = sessionPath.split('/').filter(Boolean)
  const to = absolutePath.split('/').filter(Boolean)
  let common = 0
  while (common < from.length && common < to.length && from[common] === to[common]) common++
  const relative = [...Array(from.length - common).fill('..'), ...to.slice(common)].join('/')
  return relative.startsWith('..') ? relative : `./${relative}`
}

async function uploadPastedPhotos(files: File[]) {
  if (!uploadDirectory.value) {
    useToastsStore().push({ title: 'Could not send photo', message: 'The active session repository folder is unavailable.', tone: 'error' })
    return
  }
  for (const file of files) {
    try {
      const result = await filesystemApi.uploadPhotoUnique(props.machine, uploadDirectory.value, file)
      const relativePath = relativePhotoPath(result.path)
      useToastsStore().push({ title: 'Photo added to repo', message: `${relativePath} · ${result.size.toLocaleString()} bytes; path pasted into terminal`, tone: 'success', placement: 'top-right' }, 5_000)
      term.value?.paste(relativePath)
    } catch (cause) {
      if (cause instanceof ApiError) useToastsStore().error(`Could not send ${file.name}`, cause)
      else useToastsStore().push({ title: `Could not send ${file.name}`, message: cause instanceof Error ? cause.message : 'Try pasting the photo again.', tone: 'error' })
      return
    }
  }
}

function pasteUploadedPhotoPath(absolutePath: string) {
  term.value?.paste(relativePhotoPath(absolutePath))
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

// Tab and pane selection only changes which terminal receives keys. The
// browser keyboard opens from an explicit terminal tap or Show keyboard.

onMounted(async () => {
  disposeBackgroundBlur = blurActiveFieldOnHide(document)
  await document.fonts?.ready
  if (disposed) return
  const t = new Terminal({
    allowProposedApi: true, // unicode11
    cursorBlink: true,
    fontFamily: "'JetBrains Mono', ui-monospace, monospace",
    fontSize: 14,
    // Option+drag selects even when the program captures the mouse (Shift+drag
    // does on other platforms).
    macOptionClickForcesSelection: true,
    // Replaced by finishAltClick: xterm's version miscounts multiline prompts.
    altClickMovesCursor: false,
    // OSC 8 hyperlinks: http(s) only; hovering shows the real target.
    linkHandler: hyperlinkHandler((h) => (linkHover.value = h)),
    scrollback: 5000,
    smoothScrollDuration: WHEEL_SMOOTH_SCROLL_MS,
    // Legible text on backgrounds a program chose for another theme (M8 T7).
    minimumContrastRatio: TERMINAL_MIN_CONTRAST,
    theme: theme.resolved === 'dark' ? darkTerminalTheme : theme.resolved === 'solarized' ? solarizedTerminalTheme : theme.resolved === 'dimmed' ? dimmedTerminalTheme : lightTerminalTheme,
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
  // Image paste is a file transfer to the active repo. Plain text paste is
  // left to xterm so bracketed-paste behavior remains unchanged.
  const pasteImage = (event: ClipboardEvent) => {
    const files = clipboardImages(event)
    if (!files.length) return
    event.preventDefault()
    event.stopImmediatePropagation()
    void uploadPastedPhotos(files)
  }
  t.textarea?.addEventListener('paste', pasteImage, true)
  disposeClipboardImagePaste = () => t.textarea?.removeEventListener('paste', pasteImage, true)
  disposeTouchScroll = attachTouchScroll(el.value!, cellHeight, (direction, lines) => copyMode.swipe(direction, lines))
  prepareInput(t.textarea)
  try {
    const webgl = new WebglAddon()
    webgl.onContextLoss(() => webgl.dispose())
    t.loadAddon(webgl)
  } catch {
    // No WebGL: xterm's DOM renderer is used.
  }
  term.value = t
  selectionChange = t.onSelectionChange(() => { hasSelection.value = t.hasSelection() })
  watch(() => theme.resolved, (value) => { t.options.theme = value === 'dark' ? darkTerminalTheme : value === 'solarized' ? solarizedTerminalTheme : value === 'dimmed' ? dimmedTerminalTheme : lightTerminalTheme }, { immediate: true })
  t.onData((d) => {
    const bytes = applyModifiers(d, modifiers)
    if (copyMode.inMode.value) void copyMode.exitThen(() => conn?.send(bytes))
    else conn?.send(bytes)
  })
  // Mac editing shortcuts (Option/Cmd+Backspace, +←/→): send what a Mac
  // terminal sends, once per keydown, instead of xterm's or the browser's
  // default (Cmd+← would navigate back).
  t.attachCustomKeyEventHandler((ev) => {
    if (ev.type === 'keydown') caretMove++ // typing stops an Option-click move
    if (ev.type === 'keydown' && shouldInterceptGlobalShortcut(ev, shortcutPlatform())) return false
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
  observer = new ResizeObserver(() => refit())
  observer.observe(el.value!)
  // An attached tmux client can constrain the shared session size even when
  // no resize frames are sent, so hidden pages detach and reattach on return.
  const onVisibilityChange = () => {
    syncAttachment()
    // Returning to hostbud restores keyboard input to the active pane. Only
    // this pane has takesInput=true, so inactive split panes remain unfocused.
    if (document.visibilityState === 'visible' && takesInput()) term.value?.focus()
  }
  document.addEventListener('visibilitychange', onVisibilityChange)
  disposeVisibilityListener = () => document.removeEventListener('visibilitychange', onVisibilityChange)
  syncAttachment()
  window.addEventListener('mouseup', finishAltClick, true)
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
      termTheme: () => ({ background: String(t.options.theme?.background ?? ''), foreground: String(t.options.theme?.foreground ?? '') }),
    }, () => ({ session: props.session, active: props.active, focused: takesInput() }))
})

onBeforeUnmount(() => {
  disposed = true
  clearTouchSelectTimer()
  disposeBackgroundBlur()
  disposeVisibilityListener()
  disposeTouchScroll()
  disposeClipboardImagePaste()
  snapshotRequest++
  selectionChange?.dispose()
  window.removeEventListener('mouseup', finishAltClick, true)
  caretMove++
  copyMode.reset()
  observer?.disconnect()
  conn?.close()
  term.value?.dispose()
  if (import.meta.env.VITE_E2E === '1') unregisterPane(props.paneId)
})

watch(() => [props.active, props.focused, props.session, props.paneId] as const, ([active, focused]) => {
  if (!active || !focused) copyMode.reset()
})
watch(() => props.active, syncAttachment)
watch(state, (current) => {
  if (current !== 'open') copyMode.reset()
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
    <div data-terminal-header class="flex min-w-0 items-center gap-2 border-b border-border px-2 py-1.5" :class="takesInput() && sessionSectionColor ? 'text-section-fg' : 'text-fg'" :style="takesInput() && sessionSectionColor ? { backgroundColor: sessionSectionColor } : undefined">
      <Folder data-terminal-directory-icon :size="16" class="shrink-0" aria-hidden="true" />
      <h2 data-terminal-session-name class="min-w-0 flex-1 truncate text-base font-bold tracking-tight">
        {{ props.session }}
      </h2>
      <span
        v-if="sessionDirectory"
        data-terminal-directory
        :aria-label="`Directory: ${sessionDirectory}`"
        :title="sessionDirectory"
        class="min-w-0 max-w-[40%] shrink truncate text-xs font-medium"
      >{{ sessionDirectoryName }}</span>
      <!-- Narrow screens show one pane of a split at a time. -->
      <button
        v-if="props.narrow && props.paneCount > 1"
        type="button"
        class="touch-target shrink-0 rounded border border-border px-2"
        :aria-label="`Pane ${props.paneIndex} of ${props.paneCount}: show the next pane`"
        @click="emit('cyclePane')"
      >
        Pane {{ props.paneIndex }} of {{ props.paneCount }}
      </button>
      <span class="ml-auto" />
      <TerminalActions
        :has-selection="hasSelection"
        :can-split="props.canSplit && !props.narrow"
        :sessions="splitTargets"
        @action="onToolbarAction"
        @split="(direction, session) => emit('split', direction, session)"
      />
    </div>
    <TerminalMenu :term="term">
      <!-- The menu's trigger; the ref sits inside it (as-child clones it). -->
      <div class="relative flex min-h-0 flex-1 flex-col">
        <div
          ref="el"
          data-testid="terminal"
          class="terminal-touch min-h-0 flex-1 touch-none overflow-hidden bg-bg p-1"
          @mousedown.capture="startAltClick"
          @pointerdown="startTouchSelection"
          @pointermove="cancelTouchSelectionOnMove"
          @pointerup="finishTouchSelection"
          @pointercancel="finishTouchSelection"
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
            class="touch-target rounded border border-border px-2"
            @click="retryNow"
          >
            Retry now
          </button>
        </div>
      </div>
    </TerminalMenu>
    <TerminalTextDialog v-model:open="dictationOpen" mode="dictation" @send="sendDictation" />
    <TerminalTextDialog v-model:open="snapshotOpen" mode="snapshot" :snapshot="terminalSnapshot" :loading="snapshotLoading" :error="snapshotError" @retry="openSnapshot" />
    <PhotoUploadDialog v-model:open="photoUploadOpen" :machine="props.machine" :directory="uploadDirectory" @uploaded="pasteUploadedPhotoPath" />
    <KeyBar
      v-if="!inMode"
      :term="term"
      :modifiers="modifiers"
      :focused="takesInput()"
      :busy="busy"
      @scroll="enterScrollMode"
    />
    <ScrollBar
      v-else
      :scroll-position="scrollPosition"
      :history-size="historySize"
      :busy="busy"
      @action="scrollAction"
    />
    <div
      v-if="state === 'exited' || state === 'disconnected' || state === 'limited'"
      role="status"
      class="absolute inset-x-0 bottom-0 z-10 flex items-center justify-between gap-3 border-t border-border bg-surface px-3 py-2"
    >
        <span>{{ state === 'limited' ? 'Too many open terminals. Close a tab or pane, then retry.' : state === 'exited' ? 'Session detached or ended.' : 'Disconnected: the session is gone.' }}</span>
        <button
        type="button"
        class="touch-target rounded bg-accent px-3 py-1 font-bold text-bg"
          @click="reconnect"
        >
        {{ state === 'limited' ? 'Retry now' : 'Reconnect' }}
      </button>
    </div>
  </section>
</template>
