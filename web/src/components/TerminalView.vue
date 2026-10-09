<script setup lang="ts">
import '@xterm/xterm/css/xterm.css'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal } from '@xterm/xterm'
import { Folder, GitBranch } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from 'vue'
import { TermSession, termURL, type SessionState, type TerminalDiagnostics } from '@/api/term'
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
import { clipboardImages, relativePath } from '@/lib/clipboardImages'
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
import { useMachinesStore } from '@/stores/machines'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useTreeStore } from '@/stores/tree'
import { useToastsStore } from '@/stores/toasts'
import { useThemeStore } from '@/stores/theme'
import { matchingShortcut, shouldInterceptGlobalShortcut, shortcutPlatform } from '@/lib/shortcuts'
import { attachTouchScroll } from '@/lib/touchScroll'
import { MAX_TERMINAL_FONT_SIZE, MIN_TERMINAL_FONT_SIZE, setTerminalFontSize, terminalFontSize } from '@/lib/terminalFontSize'
import { isIOS, isStandalone } from '@/lib/notificationDevice'

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
    /** Focus this pane once when hostbud first loads its saved terminal layout. */
    focusInitialTerminal?: boolean
    /** Its place among its tab's panes (1-based) and their number. */
    paneIndex?: number
    paneCount?: number
    /** The tab can take another pane (Split right/down shown). */
    canSplit?: boolean
    /** Narrow layout: one pane at a time, with a pane switcher. */
    narrow?: boolean
  }>(),
  { paneId: 'pane', active: true, focused: true, focusInitialTerminal: false, paneIndex: 1, paneCount: 1, canSplit: false, narrow: false },
)
const emit = defineEmits<{
  focus: []
  initialFocus: []
  /** Open a session (null: a new one) in a new pane beside this one. */
  split: [dir: SplitDir, session: string | null]
  close: []
  cyclePane: []
}>()
const standalonePwa = isStandalone()
const repeatIOSBackspace = standalonePwa && isIOS({ userAgent: navigator.userAgent, platform: navigator.platform, maxTouchPoints: navigator.maxTouchPoints })
const takesInput = () => props.active && props.focused

const el = ref<HTMLDivElement>()
const headerEl = ref<HTMLDivElement>()
const headerOverflow = ref(false)
const headerExpanded = ref(false)
const state = ref<SessionState>('connecting')
const attempt = ref(0)
const lastDisconnect = ref<{ code: number | null; reason: string } | null>(null)
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
const diagnosticsOpen = ref(false)
const diagnostics = reactive<TerminalDiagnostics & { renderMs: number | null }>({ pingMs: null, pingP95Ms: null, inputAckMs: null, inputAckP95Ms: null, ptyWriteMs: null, ptyWriteP95Ms: null, remoteSSHMs: null, remoteSSHP95Ms: null, remoteSSHCommandMs: null, remoteSSHCommandP95Ms: null, remoteSSHOK: null, bufferedBytes: 0, inputCount: 0, pendingInputs: 0, outputBytes: 0, renderMs: null })
const photoUploadOpen = ref(false)
let snapshotRequest = 0
const searchInitial = ref('')
const searchBar = ref<InstanceType<typeof TerminalSearch>>()
const modifiers = reactive(createModifiers())
let fit: FitAddon | null = null
let conn: TermSession | null = null
let observer: ResizeObserver | null = null
let headerObserver: ResizeObserver | null = null
let selectionChange: { dispose: () => void } | null = null
let disposeBackgroundBlur = () => {}
let disposeVisibilityListener = () => {}
let disposeTouchScroll = () => {}
let disposeClipboardImagePaste = () => {}
let attachmentSuspended = false
let disposed = false
let touchSelectTimer: ReturnType<typeof setTimeout> | null = null
let backspaceRepeatTimer: ReturnType<typeof setTimeout> | null = null
let backspaceRepeatInterval: ReturnType<typeof setInterval> | null = null
let last = { cols: 0, rows: 0 }
const auth = useAuthStore()
const machines = useMachinesStore()
const theme = useThemeStore()
const projects = useProjectsStore()
const sessions = useSessionsStore()
const tree = useTreeStore()
const splitTargets = computed(() => sessions.list(props.machine).map((x) => x.name))
const currentSession = computed(() => sessions.list(props.machine).find((x) => x.name === props.session))
const projectName = computed(() => projects.items.find((project) => project.id === currentSession.value?.projectId)?.name ?? '')
const terminalTitle = computed(() => {
  if (props.narrow && !headerExpanded.value) {
    if (!standalonePwa) return props.session
    return `${projectName.value ? `${projectName.value.slice(0, 10)}/` : ''}${props.session.slice(0, 10)}`
  }
  return projectName.value ? `${projectName.value}/${props.session}` : props.session
})
const currentFontSize = computed(() => terminalFontSize(props.machine, props.session))
const machineChip = computed(() => props.machine !== 'host' ? machines.label(props.machine) : '')
const agentUsage = computed(() => currentSession.value?.agentUsage)
const tokenNumber = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
const tokenDetail = computed(() => {
  const usage = agentUsage.value
  if (!usage) return ''
  const number = (n: number) => n.toLocaleString('en-US')
  const limit = usage.contextWindow ? ` of ${number(usage.contextWindow)}` : ''
  const agent = usage.agent === 'claude' ? 'Claude Code' : 'Codex'
  return `${agent}: ${number(usage.contextTokens)}${limit} context tokens; ${number(usage.totalTokens)} total tokens consumed (includes cached input). Last reported by ${agent}.`
})
const sessionDirectory = computed(() => currentSession.value?.path ?? '')
const gitBranch = computed(() => currentSession.value?.gitBranch ?? '')
const gitChangedFiles = computed(() => currentSession.value?.gitChangedFiles ?? 0)
const hasHeaderDetails = computed(() => Boolean(sessionDirectory.value || machineChip.value || agentUsage.value || gitBranch.value || (props.narrow && props.paneCount > 1)))
const headerCanExpand = computed(() => standalonePwa ? hasHeaderDetails.value : headerOverflow.value)
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
let suppressNextTouchLinkOpen = false
let pointerStart: { id: number; type: string; x: number; y: number } | null = null
let pointerMoved = false
let capturedLinkClick: { x: number; y: number; expiresAt: number } | null = null

function enterScrollMode() {
  void copyMode.action('enter')
}

function scrollAction(action: Parameters<typeof copyMode.action>[0], lines?: number) {
  void copyMode.action(action, lines)
}

function startTouchSelection(event: PointerEvent) {
  pointerStart = { id: event.pointerId, type: event.pointerType, x: event.clientX, y: event.clientY }
  pointerMoved = false
  clearTouchSelectTimer()
  touchSelectStart = null
  if (event.pointerType !== 'touch') return
  touchSelectStart = { x: event.clientX, y: event.clientY }
  const gesture = touchSelectStart
  touchSelectTimer = window.setTimeout(() => {
    touchSelectTimer = null
    if (touchSelectStart !== gesture) return
    const screen = term.value?.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
    const point = screen && term.value ? cellAt({ clientX: gesture.x, clientY: gesture.y }, screen, term.value.cols, term.value.rows) : null
    const selectedURL = point ? Boolean(wrappedURLAt(point.x, point.y)) : false
    if (selectTouchWord(gesture.x, gesture.y)) {
      touchSelectStart = null
      suppressNextTouchLinkOpen = selectedURL
    }
  }, 450)
}

function cancelTouchSelectionOnMove(event: PointerEvent) {
  if (pointerStart?.id === event.pointerId && Math.hypot(event.clientX - pointerStart.x, event.clientY - pointerStart.y) >= 8)
    pointerMoved = true
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
  pointerStart = null
  pointerMoved = false
}

function captureFollowupLinkClick(event: PointerEvent) {
  capturedLinkClick = { x: event.clientX, y: event.clientY, expiresAt: Date.now() + 500 }
}

// WebLinksAddon activates its visible row fragment on the browser's `click`,
// which follows pointerup. When pointerup already opened the reconstructed
// full URL, stop that follow-up event so it cannot also open a truncated URL.
function suppressFollowupLinkClick(event: MouseEvent) {
  const captured = capturedLinkClick
  if (!captured) return
  capturedLinkClick = null
  if (Date.now() > captured.expiresAt || Math.hypot(event.clientX - captured.x, event.clientY - captured.y) >= 8) return
  event.preventDefault()
  event.stopImmediatePropagation()
}

function cellHeight() {
  const screen = term.value?.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
  return screen && term.value ? screen.height / term.value.rows : 16
}

// Some terminal apps print long URLs with literal line breaks rather than
// terminal soft wraps. Recover them from adjacent full-width rows as well as
// xterm's wrapped rows.
function logicalRowsAt(viewportRow: number): { firstRow: number; text: string } {
  const buffer = term.value?.buffer.active
  const t = term.value
  if (!buffer || !t) return { firstRow: viewportRow, text: '' }
  const lineText = (row: number) => buffer.getLine(buffer.viewportY + row)?.translateToString(true) ?? ''
  const lineWrapped = (row: number) => buffer.getLine(buffer.viewportY + row)?.isWrapped ?? false
  const continues = (row: number) => lineWrapped(row) || (lineText(row - 1).length >= t.cols && Boolean(lineText(row)) && !/^\s/.test(lineText(row)))
  let first = viewportRow
  while (first > 0 && continues(first)) first--
  let joined = ''
  for (let row = first; row < (term.value?.rows ?? 0); row++) {
    if (!buffer.getLine(buffer.viewportY + row) || (row > first && !continues(row))) break
    joined += lineText(row)
  }
  return { firstRow: first, text: joined }
}

function wrappedURLAt(column: number, viewportRow: number): string {
  const { text, firstRow } = logicalRowsAt(viewportRow)
  let clicked = column
  for (let row = firstRow; row < viewportRow; row++) clicked += term.value?.buffer.active.getLine(term.value.buffer.active.viewportY + row)?.translateToString(true).length ?? 0
  const urlPattern = /https?:\/\/[^\s"'!*(){}|\\^<>`]+[^\s"':,.!?{}|\\^~\[\]`()<>]/gi
  for (const match of text.matchAll(urlPattern)) {
    const start = match.index ?? 0
    if (clicked >= start && clicked < start + match[0].length) return match[0]
  }
  return ''
}

function openCapturedLink(event: PointerEvent) {
  const t = term.value
  if (suppressNextTouchLinkOpen) {
    suppressNextTouchLinkOpen = false
    captureFollowupLinkClick(event)
    finishTouchSelection()
    return
  }
  const start = pointerStart
  const click = start?.id === event.pointerId && start.type === event.pointerType && !pointerMoved && Math.hypot(event.clientX - start.x, event.clientY - start.y) < 8
  if (!t || !click) return
  const screen = t.element?.querySelector('.xterm-screen')?.getBoundingClientRect()
  if (!screen || !screen.width || !screen.height) return
  const point = cellAt(event, screen, t.cols, t.rows)
  const url = wrappedURLAt(point.x, point.y)
  if (!url || !openLink(url)) return
  captureFollowupLinkClick(event)
  finishTouchSelection()
  event.preventDefault()
  event.stopImmediatePropagation()
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
  const url = wrappedURLAt(column, row)
  if (url) {
    // Selecting all of a wrapped URL makes iOS's native Copy action useful;
    // the normal word selection remains the fallback for other terminal text.
    const { firstRow, text: logical } = logicalRowsAt(row)
    let urlStart = column
    for (let lineRow = firstRow; lineRow < row; lineRow++)
      urlStart += buffer.getLine(buffer.viewportY + lineRow)?.translateToString(true).length ?? 0
    const index = logical.indexOf(url)
    if (index >= 0 && urlStart >= index && urlStart < index + url.length) {
      const line = buffer.getLine(buffer.viewportY + firstRow)
      const startCol = index
      if (line) t.select(startCol, buffer.viewportY + firstRow, url.length)
      return true
    }
  }
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
    onData: (bytes) => {
      const measuring = diagnosticsOpen.value
      const started = measuring ? performance.now() : 0
      t.write(bytes, () => {
        if (measuring && diagnosticsOpen.value) {
          requestAnimationFrame(() => {
            if (diagnosticsOpen.value) diagnostics.renderMs = performance.now() - started
          })
        }
      })
    },
    onDiagnostics: (value) => Object.assign(diagnostics, value),
    onState: (s, info) => {
      state.value = s
      attempt.value = info.attempt
      lastDisconnect.value = info.lastDisconnect
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

function stopBackspaceRepeat() {
  if (backspaceRepeatTimer) clearTimeout(backspaceRepeatTimer)
  if (backspaceRepeatInterval) clearInterval(backspaceRepeatInterval)
  backspaceRepeatTimer = null
  backspaceRepeatInterval = null
}

/** Focuses the terminal, which brings up a phone's on-screen keyboard. */
function showKeyboard() {
  term.value?.focus()
}
function toggleStandaloneKeyboard(event: Event) {
  if (!props.active || !takesInput()) return
  if ((event as CustomEvent<{ open: boolean }>).detail?.open) showKeyboard()
  else term.value?.textarea?.blur()
}

/** Applies the one-time focus requested by App after restoring a saved layout. */
function applyInitialTerminalFocus() {
  if (!props.focusInitialTerminal || !takesInput() || document.visibilityState !== 'visible' || !term.value) return
  term.value.focus()
  emit('initialFocus')
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

function onToolbarAction(action: 'search' | 'copy' | 'keyboard' | 'dictation' | 'snapshot' | 'photos' | 'diagnostics' | 'close') {
  if (action === 'search') openSearch()
  else if (action === 'copy') copySelectedText()
  else if (action === 'keyboard') showKeyboard()
  else if (action === 'dictation') dictationOpen.value = true
  else if (action === 'snapshot') openSnapshot()
  else if (action === 'photos') photoUploadOpen.value = true
  else if (action === 'diagnostics') {
    diagnosticsOpen.value = true
    diagnostics.renderMs = null
    conn?.setDiagnostics(true)
  }
  else emit('close')
}

function closeDiagnostics() {
  diagnosticsOpen.value = false
  conn?.setDiagnostics(false)
}

function diagnosticsValue(last: number | null, p95: number | null, digits = 1) {
  if (last === null) return 'waiting'
  return `${last.toFixed(digits)} ms (p95 ${p95?.toFixed(digits) ?? 'waiting'} ms)`
}

async function copyDiagnostics() {
  const capturedAt = new Intl.DateTimeFormat('en-GB', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date())
  const report = [
    'Hostbud terminal diagnostics',
    `Captured: ${capturedAt}`,
    `Connection state: ${state.value}; reconnect attempt: ${attempt.value}`,
    `Previous disconnect: ${lastDisconnect.value ? `WebSocket ${lastDisconnect.value.code ?? 'unknown'} — ${lastDisconnect.value.reason}` : 'none recorded'}`,
    `WebSocket round trip: ${diagnosticsValue(diagnostics.pingMs, diagnostics.pingP95Ms)}`,
    `Input acknowledgment: ${diagnosticsValue(diagnostics.inputAckMs, diagnostics.inputAckP95Ms)}`,
    `Hostbud PTY write: ${diagnosticsValue(diagnostics.ptyWriteMs, diagnostics.ptyWriteP95Ms, 2)}`,
    `Added-server SSH command: ${diagnostics.remoteSSHOK === false ? 'failed; ' : ''}${diagnosticsValue(diagnostics.remoteSSHCommandMs, diagnostics.remoteSSHCommandP95Ms)}`,
    `SSH probe WebSocket round trip: ${diagnosticsValue(diagnostics.remoteSSHMs, diagnostics.remoteSSHP95Ms)}`,
    `Terminal output processing to next frame: ${diagnostics.renderMs === null ? 'waiting' : `${diagnostics.renderMs.toFixed(1)} ms`}`,
    `Browser WebSocket buffer: ${diagnostics.bufferedBytes} bytes`,
    `Pending input probes: ${diagnostics.pendingInputs}`,
    `Input probe count: ${diagnostics.inputCount}`,
    `Terminal output bytes: ${diagnostics.outputBytes}`,
  ].join('\n')
  try {
    await navigator.clipboard.writeText(report)
    useToastsStore().push({ title: 'Diagnostics copied', message: 'The report contains timings only, not typed text or terminal output.', tone: 'success' })
  } catch {
    useToastsStore().push({ title: "Couldn't copy diagnostics", message: 'The browser blocked clipboard access. Try again from the HTTPS app or copy the values manually.', tone: 'error' })
  }
}

function sendDictation(text: string) {
  term.value?.paste(text)
}

function relativePhotoPath(absolutePath: string) {
  const sessionPath = sessions.list(props.machine).find((session) => session.name === props.session)?.path ?? uploadDirectory.value
  return relativePath(sessionPath, absolutePath)
}

async function uploadPastedPhotos(files: File[]) {
  if (!uploadDirectory.value) {
    useToastsStore().push({ title: 'Could not send photo', message: 'The active session repository folder is unavailable.', tone: 'error' })
    return
  }
  for (const file of files) {
    try {
      const result = await filesystemApi.uploadPhotoUnique(props.machine, uploadDirectory.value, file)
      const pastedPath = relativePhotoPath(result.path)
      useToastsStore().push({ title: 'Photo added to repo', message: `${pastedPath} · ${result.size.toLocaleString()} bytes; path pasted into terminal`, tone: 'success', placement: 'top-right' }, 5_000)
      term.value?.paste(pastedPath)
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

function changeFontSize(delta: number) {
  setTerminalFontSize(props.machine, props.session, currentFontSize.value + delta)
}

function measureHeader() {
  const header = headerEl.value
  if (!header || headerExpanded.value) return
  const clippedContent = [...header.querySelectorAll<HTMLElement>('.truncate')].some((node) => node.scrollWidth > node.clientWidth + 1)
  const childTops = [...header.children]
    .filter((child) => (child as HTMLElement).getClientRects().length > 0)
    .map((child) => Math.round((child as HTMLElement).getBoundingClientRect().top))
  const wrapped = !standalonePwa && new Set(childTops).size > 1
  headerOverflow.value = wrapped || header.scrollWidth > header.clientWidth + 1 || header.scrollHeight > header.clientHeight + 1 || clippedContent
  if (!headerOverflow.value) headerExpanded.value = false
}
function setHeaderExpanded(expanded: boolean) {
  headerExpanded.value = expanded && headerCanExpand.value
  if (!headerExpanded.value) requestAnimationFrame(measureHeader)
}
function onOutsideHeader(event: PointerEvent) {
  if (headerExpanded.value && !headerEl.value?.contains(event.target as Node)) setHeaderExpanded(false)
}
function onHeaderFocus(event: FocusEvent) {
  if (headerExpanded.value && !headerEl.value?.contains(event.target as Node)) setHeaderExpanded(false)
}
function onHeaderClick(event: MouseEvent) {
  if (headerCanExpand.value && !(event.target as HTMLElement).closest('button, a, input')) setHeaderExpanded(!headerExpanded.value)
}
function onHeaderKey(event: KeyboardEvent) {
  if (event.key === 'Escape' && headerExpanded.value) setHeaderExpanded(false)
}

// Tab and pane selection only changes which terminal receives keys. The
// browser keyboard opens from an explicit terminal tap or Show keyboard.

onMounted(async () => {
  window.addEventListener('hostbud:toggle-keyboard', toggleStandaloneKeyboard)
  disposeBackgroundBlur = blurActiveFieldOnHide(document)
  await document.fonts?.ready
  if (disposed) return
  const t = new Terminal({
    allowProposedApi: true, // unicode11
    cursorBlink: true,
    fontFamily: "'JetBrains Mono', ui-monospace, monospace",
    fontSize: currentFontSize.value,
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
  t.element?.addEventListener('pointerup', openCapturedLink, true)
  t.element?.addEventListener('click', suppressFollowupLinkClick, true)
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
  watch(currentFontSize, (fontSize) => {
    t.options.fontSize = fontSize
    requestAnimationFrame(refit)
  })
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
    if (repeatIOSBackspace && ev.key === 'Backspace' && !ev.altKey && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey) {
      if (ev.type === 'keyup') stopBackspaceRepeat()
      if (ev.type === 'keydown') {
        ev.preventDefault()
        if (!backspaceRepeatTimer && !backspaceRepeatInterval) {
          t.input('\x7f')
          backspaceRepeatTimer = setTimeout(() => {
            backspaceRepeatTimer = null
            t.input('\x7f')
            backspaceRepeatInterval = setInterval(() => t.input('\x7f'), 80)
          }, 400)
        }
      }
      return false
    }
    if (ev.type === 'keydown') caretMove++ // typing stops an Option-click move
    if (ev.type === 'keydown' && matchingShortcut(ev, shortcutPlatform(), 'global')?.id === 'undo-terminal-edit') {
      ev.preventDefault()
      // Readline and compatible line editors interpret Ctrl+_ as undo. xterm
      // bracketed paste is recorded as one edit, so one undo removes the paste.
      t.input('\x1f')
      return false
    }
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
  t.textarea?.addEventListener('blur', stopBackspaceRepeat)
  observer = new ResizeObserver(() => refit())
  observer.observe(el.value!)
  headerObserver = new ResizeObserver(measureHeader)
  if (headerEl.value) {
    headerObserver.observe(headerEl.value)
    for (const child of headerEl.value.children) headerObserver.observe(child)
  }
  window.addEventListener('pointerdown', onOutsideHeader, true)
  window.addEventListener('focusin', onHeaderFocus, true)
  window.addEventListener('keydown', onHeaderKey, true)
  requestAnimationFrame(measureHeader)
  // An attached tmux client can constrain the shared session size even when
  // no resize frames are sent, so hidden pages detach and reattach on return.
  const onVisibilityChange = () => {
    syncAttachment()
    // Returning to hostbud restores keyboard input to the active pane. Only
    // this pane has takesInput=true, so inactive split panes remain unfocused.
    if (document.visibilityState === 'visible' && takesInput()) {
      if (props.focusInitialTerminal) applyInitialTerminalFocus()
      else term.value?.focus()
    }
  }
  document.addEventListener('visibilitychange', onVisibilityChange)
  disposeVisibilityListener = () => document.removeEventListener('visibilitychange', onVisibilityChange)
  syncAttachment()
  applyInitialTerminalFocus()
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
  stopBackspaceRepeat()
  window.removeEventListener('hostbud:toggle-keyboard', toggleStandaloneKeyboard)
  disposed = true
  finishTouchSelection()
  disposeBackgroundBlur()
  disposeVisibilityListener()
  disposeTouchScroll()
  disposeClipboardImagePaste()
  snapshotRequest++
  selectionChange?.dispose()
  window.removeEventListener('mouseup', finishAltClick, true)
  term.value?.element?.removeEventListener('pointerup', openCapturedLink, true)
  term.value?.element?.removeEventListener('click', suppressFollowupLinkClick, true)
  caretMove++
  copyMode.reset()
  observer?.disconnect()
  window.removeEventListener('pointerdown', onOutsideHeader, true)
  window.removeEventListener('focusin', onHeaderFocus, true)
  window.removeEventListener('keydown', onHeaderKey, true)
  headerObserver?.disconnect()
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
watch(() => [props.session, gitBranch.value, agentUsage.value?.agent, agentUsage.value?.contextTokens, agentUsage.value?.totalTokens] as const, () => {
  requestAnimationFrame(measureHeader)
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
    <div ref="headerEl" data-terminal-header :data-overflow="headerOverflow || undefined" :data-expandable="headerCanExpand || undefined" :data-expanded="headerExpanded || undefined" class="flex min-w-0 items-center gap-2 border-b border-border px-2" :class="[takesInput() && sessionSectionColor ? 'text-section-fg' : 'text-fg', standalonePwa ? 'py-0' : 'py-1.5', headerExpanded ? 'flex-wrap content-start max-h-24 overflow-y-auto' : standalonePwa ? 'flex-nowrap overflow-hidden' : 'flex-wrap max-h-24 overflow-hidden', headerCanExpand ? 'cursor-pointer' : '']" :style="takesInput() && sessionSectionColor ? { backgroundColor: sessionSectionColor } : undefined" @click="onHeaderClick">
      <Folder data-terminal-directory-icon :size="16" class="shrink-0" :title="sessionDirectory" :aria-label="sessionDirectory ? `Directory: ${sessionDirectory}` : undefined" :aria-hidden="sessionDirectory ? undefined : true" />
      <h2 data-terminal-session-name class="min-w-0 max-w-full text-base font-bold tracking-tight" :class="standalonePwa && !headerExpanded ? 'flex-1 truncate' : 'whitespace-normal break-words'">
        <button v-if="headerCanExpand" type="button" class="block max-w-full text-left text-inherit" :class="standalonePwa && !headerExpanded ? 'truncate' : 'whitespace-normal break-words'" :style="{ font: 'inherit', letterSpacing: 'inherit' }" :aria-label="headerExpanded ? 'Hide terminal details' : 'Show terminal details'" :aria-expanded="headerExpanded" @click.stop="setHeaderExpanded(!headerExpanded)">{{ terminalTitle }}</button>
        <template v-else>{{ terminalTitle }}</template>
      </h2>
      <span v-if="machineChip" data-terminal-machine-chip class="shrink-0 truncate rounded-full border-2 border-danger bg-danger px-2.5 font-sans text-xs font-black leading-4 text-bg" :title="'On ' + machineChip">{{ machineChip }}</span>
      <span v-if="(!standalonePwa || headerExpanded) && agentUsage" data-agent-usage :title="tokenDetail" :aria-label="tokenDetail" class="shrink-0 whitespace-nowrap text-center text-xs tabular-nums" :class="standalonePwa ? 'max-sm:order-last max-sm:basis-full max-sm:text-left' : ''">
        {{ tokenNumber.format(agentUsage.contextTokens) }} ctx · {{ tokenNumber.format(agentUsage.totalTokens) }} used
      </span>
      <span v-if="(!standalonePwa || headerExpanded) && gitBranch" data-git-branch class="inline-flex min-w-0 shrink items-center gap-1 truncate text-xs" :title="`Branch: ${gitBranch}${gitChangedFiles ? ` (${gitChangedFiles} changed files)` : ''}`" :aria-label="`Branch: ${gitBranch}${gitChangedFiles ? ` (${gitChangedFiles} changed files)` : ''}`"><GitBranch :size="14" class="shrink-0" />{{ gitBranch }}<span v-if="gitChangedFiles" data-git-changed-files>({{ gitChangedFiles }})</span></span>
      <div class="flex min-w-0 flex-1 items-center justify-end gap-2">
        <!-- Narrow screens show one pane of a split at a time. -->
        <button
          v-if="(!standalonePwa || headerExpanded) && props.narrow && props.paneCount > 1"
          type="button"
          class="touch-target shrink-0 rounded border border-border px-2"
          :aria-label="`Pane ${props.paneIndex} of ${props.paneCount}: show the next pane`"
          @click="emit('cyclePane')"
        >
          Pane {{ props.paneIndex }} of {{ props.paneCount }}
        </button>
        <span class="ml-auto" />
        <div v-if="takesInput()" data-terminal-font-controls class="flex shrink-0 items-center gap-0.5">
          <button type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded px-1 text-xs font-bold hover:bg-bg/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-current" aria-label="Decrease terminal font size" title="Decrease terminal font size" :disabled="currentFontSize <= MIN_TERMINAL_FONT_SIZE" @click="changeFontSize(-1)">A−</button>
          <button type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded px-1 text-xs font-bold hover:bg-bg/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-current" aria-label="Increase terminal font size" title="Increase terminal font size" :disabled="currentFontSize >= MAX_TERMINAL_FONT_SIZE" @click="changeFontSize(1)">A+</button>
        </div>
        <TerminalActions
          :has-selection="hasSelection"
          :can-split="props.canSplit && !props.narrow"
          :sessions="splitTargets"
          @action="onToolbarAction"
          @split="(direction, session) => emit('split', direction, session)"
        />
      </div>
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
    <div v-if="diagnosticsOpen" role="dialog" aria-modal="false" aria-labelledby="terminal-diagnostics-title" class="pointer-events-none fixed inset-0 z-[70]">
      <section class="pointer-events-auto absolute right-3 top-16 max-h-[55dvh] w-[min(30rem,calc(100vw-1.5rem))] overflow-y-auto rounded-lg border border-border bg-surface p-4 text-fg shadow-xl md:right-4 md:top-20 md:max-h-[80dvh]">
        <div class="mb-3 flex items-center justify-between gap-4">
          <h2 id="terminal-diagnostics-title" class="text-base font-bold">Connection diagnostics</h2>
          <button type="button" aria-label="Close diagnostics" class="touch-target rounded px-3" @click="closeDiagnostics">Close</button>
        </div>
        <p class="mb-3 text-sm text-muted">Measurements run only while this panel is open. Typed text and terminal output are never recorded.</p>
        <dl class="grid grid-cols-1 gap-x-4 gap-y-2 text-sm tabular-nums sm:grid-cols-2">
          <dt>WebSocket round trip (last / p95)</dt><dd>{{ diagnostics.pingMs === null ? 'Waiting…' : `${diagnostics.pingMs.toFixed(1)} / ${diagnostics.pingP95Ms?.toFixed(1)} ms` }}</dd>
          <dt>Input acknowledgment (last / p95)</dt><dd>{{ diagnostics.inputAckMs === null ? 'Type to measure' : `${diagnostics.inputAckMs.toFixed(1)} / ${diagnostics.inputAckP95Ms?.toFixed(1)} ms` }}</dd>
          <dt>Server PTY write (last / p95)</dt><dd>{{ diagnostics.ptyWriteMs === null ? 'Type to measure' : `${diagnostics.ptyWriteMs.toFixed(2)} / ${diagnostics.ptyWriteP95Ms?.toFixed(2)} ms` }}</dd>
          <dt>Added-server SSH command (last / p95)</dt><dd>{{ diagnostics.remoteSSHCommandMs === null ? 'Waiting…' : `${diagnostics.remoteSSHOK ? '' : 'Failed · '}${diagnostics.remoteSSHCommandMs.toFixed(1)} / ${diagnostics.remoteSSHCommandP95Ms?.toFixed(1)} ms` }}</dd>
          <dt>SSH probe WebSocket round trip</dt><dd>{{ diagnostics.remoteSSHMs === null ? 'Waiting…' : `${diagnostics.remoteSSHMs.toFixed(1)} / ${diagnostics.remoteSSHP95Ms?.toFixed(1)} ms` }}</dd>
          <dt>Terminal output processing to next frame</dt><dd>{{ diagnostics.renderMs === null ? 'Waiting…' : `${diagnostics.renderMs.toFixed(1)} ms` }}</dd>
          <dt>Browser WebSocket buffer</dt><dd>{{ diagnostics.bufferedBytes.toLocaleString() }} bytes</dd>
          <dt>Pending probes</dt><dd>{{ diagnostics.pendingInputs }}</dd>
          <dt>Previous disconnect</dt><dd>{{ lastDisconnect ? `WebSocket ${lastDisconnect.code ?? 'unknown'} · ${lastDisconnect.reason}` : 'None recorded' }}</dd>
          <dt>Input probes / output bytes</dt><dd>{{ diagnostics.inputCount }} / {{ diagnostics.outputBytes.toLocaleString() }}</dd>
        </dl>
        <button type="button" class="touch-target mt-4 w-full rounded border border-border px-3 py-2 text-sm font-semibold" @click="copyDiagnostics">Copy diagnostics report</button>
        <p class="mt-3 text-xs text-muted">Recent p95 uses up to the last 50 samples. While open, the added-server SSH probe runs a harmless `true` command every 10 seconds. The command time is measured inside hostbud; the WebSocket round trip includes the browser path. High command time points to hostbud-to-server SSH latency or server scheduling. High WebSocket time with a low command time points to browser-to-hostbud latency. PTY write is local to hostbud's SSH process; it does not confirm remote receipt. Output processing to next frame includes xterm write processing and browser frame scheduling, not confirmed display presentation. A high value points to browser main-thread/render pressure or heavy terminal output.</p>
      </section>
    </div>
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
