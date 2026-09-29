<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle,
} from 'reka-ui'
import { VueDraggable } from 'vue-draggable-plus'
import { ArrowDown, ArrowUp, Check, ChevronsDown, CircleCheck, GripVertical, History, ListOrdered, ListX, Pause, Pencil, Play, Plus, PowerOff, RotateCcw, RotateCw, Save, ShieldCheck, SkipForward, SquareTerminal, ThumbsDown, ThumbsUp, Trash2, TriangleAlert, X } from 'lucide-vue-next'
import { queuesApi, sessionsApi } from '@/api/client'
import type { QueueItem, QueueItemHistory } from '@/api/types'
import ConfirmDialog from './ConfirmDialog.vue'
import DurationPicker from './DurationPicker.vue'
import FormError from './FormError.vue'
import { AGENTS, type Agent, completedSessions, DEFAULT_LOOP_RUNTIME_SECONDS, flagsError, loopRuntimeError, loopRuntimeText, INSTRUCTION_PREFIX, instructionError, itemActions, moveQueued, queueControls, queueRunning, statusLabel, verifyCommandError, verifyLine } from '@/lib/queue'
import { useQueuesStore } from '@/stores/queues'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { describeError } from '@/stores/toasts'

// Queue panel: a queue belongs to a project; its items run one after
// another in their own tmux session, advancing only when the agent's own
// tracked native goal is achieved. V2-M2 (HOSTBUD_PARALLEL_QUEUES): several queues, a
// switcher (a list on desktop, a select on the phone), rename, the
// shared-directory warning and "waiting for a free slot". V2-M4: per-item
// completion gates (verify command, approval), their state and output, and
// Approve / Reject / Re-run verify. It updates from /ws/events
// (queue.changed, run.changed) only. Done items' sessions can be killed one
// by one or all at once ("Kill completed sessions"), after a confirmation.
const props = withDefaults(defineProps<{ compact?: boolean; machine?: string }>(), { machine: 'host' })
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ openSession: [name: string]; killed: [name: string] }>()

const store = useQueuesStore()
const projects = useProjectsStore()
const sessions = useSessionsStore()
// The shown queue: the one picked in the switcher, else the first.
const selectedId = ref<string | null>(null)
const queue = computed(() => store.queues.find((q) => q.id === selectedId.value) ?? store.queues[0] ?? null)
const selectModel = computed({
  get: () => queue.value?.id ?? '',
  set: (id: string) => { selectedId.value = id },
})
function select(id: string) {
  selectedId.value = id
  renaming.value = false
  editing.value = null
}
// V2-M3: a notification opened an item: select its queue, highlight it.
const highlighted = ref<string | null>(null)
watch(() => store.focus, async (f) => {
  if (!f) return
  select(f.queueId)
  highlighted.value = f.itemId
  if (!store.loaded) await store.load()
  await nextTick()
  if (f.itemId) document.querySelector(`[data-queue-item="${CSS.escape(f.itemId)}"]`)?.scrollIntoView?.({ block: 'nearest' })
})
const items = ref<QueueItem[]>([])
watch(() => queue.value?.items, (next) => { items.value = [...(next ?? [])] }, { immediate: true })
const hasQueued = computed(() => items.value.some((i) => i.status === 'queued'))
const controls = computed(() => queue.value ? queueControls(queue.value.status, hasQueued.value, !!queue.value.loop?.enabled && items.value.length > 0) : null)
// Parallel queues off: queues can still be created to organize work, but
// only one runs at a time. The other active queue blocks Start/Resume (the
// server's 409 stays authoritative).
const runBlockedBy = computed(() => {
  if (store.parallelQueues || !queue.value) return null
  const id = queue.value.id
  return store.queues.find((q) => q.id !== id && queueRunning(q)) ?? null
})
const runBlockedTitle = computed(() => runBlockedBy.value
  ? `Queue ${runBlockedBy.value.name} is running. Pause it and wait for its run to end, or turn on Run queues in parallel.`
  : undefined)
const progressSegments = computed(() => items.value.map((item) => ({
  id: item.id,
  label: statusLabel(item),
  color: item.status === 'done' ? 'bg-ok'
    : item.status === 'needs_attention' ? 'bg-danger'
      : ['running', 'verifying', 'awaiting_approval'].includes(item.status) ? 'bg-warning'
        : 'bg-border',
})))
const remainingCount = computed(() => items.value.filter((item) => item.status === 'queued').length)
const progressDescription = computed(() => {
  const activeIndex = items.value.findIndex((item) => ['running', 'verifying', 'awaiting_approval'].includes(item.status))
  const errorIndex = items.value.findIndex((item) => item.status === 'needs_attention')
  const state = errorIndex >= 0 ? `item ${errorIndex + 1} needs attention`
    : activeIndex >= 0 ? `item ${activeIndex + 1} active`
      : remainingCount.value ? `${remainingCount.value} queued`
        : items.value.length ? 'complete' : 'empty'
  return `${state}; ${remainingCount.value} left`
})

const error = ref<{ title: string; message: string; hint?: string } | null>(null)
const busy = ref(false)
const historyOpen = ref(false)
const history = ref<QueueItemHistory[]>([])
const historyGroups = computed(() => {
  const groups = new Map<string, { id: string; name: string; project: string; entries: QueueItemHistory[] }>()
  for (const entry of history.value) {
    const group = groups.get(entry.queueId) ?? { id: entry.queueId, name: entry.queueName, project: entry.projectName, entries: [] }
    group.entries.push(entry)
    groups.set(entry.queueId, group)
  }
  return [...groups.values()]
})
const expandedHistory = ref(new Set<string>())
function toggleHistoryGroup(id: string) {
  const next = new Set(expandedHistory.value)
  if (!next.delete(id)) next.add(id)
  expandedHistory.value = next
}
const historyOffset = ref(0)
const historyHasMore = ref(false)
const historyLoading = ref(false)
const historyError = ref('')
async function loadHistory(reset = false) {
  if (historyLoading.value) return
  historyLoading.value = true
  historyError.value = ''
  try {
    const offset = reset ? 0 : historyOffset.value
    const page = await queuesApi.history(100, offset)
    history.value = reset ? page.items : [...history.value, ...page.items]
    historyOffset.value = offset + page.items.length
    historyHasMore.value = page.items.length === 100
  } catch (e) {
    historyError.value = describeError(e).message
  } finally {
    historyLoading.value = false
  }
}
function showHistory() {
  historyOpen.value = true
  expandedHistory.value = new Set()
  void loadHistory(true)
}
function actionLabel(action: QueueItemHistory['action']) {
  return ({ created: 'Added', edited: 'Edited', status: 'Status', deleted: 'Deleted' })[action]
}

async function act(title: string, fn: () => Promise<unknown>) {
  busy.value = true
  error.value = null
  try {
    const result = await fn()
    if (result && typeof result === 'object' && 'items' in result && 'status' in result) store.put(result as never)
  } catch (e) {
    error.value = { title, ...describeError(e) }
    void store.load()
  } finally {
    busy.value = false
  }
}

/** The parallel-queues switch: stored on the server, no redeploy.
 * Switching off stops no run. The box shows the server's state until the
 * change is confirmed and saved. */
function toggleParallel(e: Event) {
  const box = e.target as HTMLInputElement
  const on = box.checked
  box.checked = store.parallelQueues
  ask(on ? 'parallel-on' : 'parallel-off')
}
function setParallel(on: boolean) {
  void act("Couldn't change parallel queues", async () => {
    store.parallelQueues = (await queuesApi.setParallel(props.machine, on)).parallelQueues
  })
}

watch(open, (isOpen) => { if (isOpen && !store.loaded) void store.load() })

// ---- creating, renaming and deleting queues ----
const newProjectId = ref('')
const newName = ref('Milestones')
// '' starts normally; 'run:<id>' waits for a tracked goal, 'session:<name>'
// for any existing session (a queue run or not) to be idle.
const afterLink = ref('')
const activeGoalRuns = computed(() => store.queues.flatMap((q) => q.items
  .filter((it) => it.run && ['starting', 'running', 'stale'].includes(it.run.status)
    && (it.agent === 'codex' || it.instruction.startsWith('/goal ')))
  .map((it) => ({ id: it.run!.id, session: it.run!.sessionName, queue: q.name, goal: it.instruction.replace(/^\/goal\s+/, '') }))))
const linkableSessions = computed(() => {
  const goalSessions = new Set(activeGoalRuns.value.map((run) => run.session))
  return sessions.list(props.machine).filter((s) => !goalSessions.has(s.name))
})
watch(() => projects.items, (list) => { if (!newProjectId.value && list.length) newProjectId.value = list[0].id }, { immediate: true })
// With queues present, "New queue" opens the form (with or without the switch).
const addingQueue = ref(false)
const showCreate = computed(() => store.loaded && (!store.queues.length || addingQueue.value))
function newQueue() {
  addingQueue.value = true
  newName.value = ''
  afterLink.value = ''
}
function createQueue() {
  // An unnamed queue: "Milestones" for the first, else the server's "Queue n".
  const name = newName.value.trim() || (store.queues.length ? '' : 'Milestones')
  void act("Couldn't create the queue", async () => {
    const [kind, target] = afterLink.value.split(/:(.*)/s)
    const created = await queuesApi.create(newProjectId.value, name, kind === 'run' ? target : '', kind === 'session' ? target : '')
    selectedId.value = created.id
    addingQueue.value = false
    return created
  })
}

const renaming = ref(false)
const renameText = ref('')
function startRename() {
  renaming.value = true
  renameText.value = queue.value?.name ?? ''
}
function saveRename() {
  const q = queue.value
  const name = renameText.value.trim()
  if (!q || !name) return
  void act("Couldn't rename the queue", async () => {
    const renamed = await queuesApi.rename(q.id, name)
    renaming.value = false
    return renamed
  })
}

// ---- looping: run the items again until the runtime limit since Start ----
const loop = computed(() => queue.value?.loop)
// The limit in seconds, picked as hours and minutes.
const loopLimit = ref(DEFAULT_LOOP_RUNTIME_SECONDS)
const loopError = ref<string | null>(null)
watch(() => [queue.value?.id, loop.value?.maxRuntimeSeconds] as const, ([, seconds]) => {
  loopLimit.value = seconds || DEFAULT_LOOP_RUNTIME_SECONDS
  loopError.value = null
}, { immediate: true })
watch(loopLimit, () => { loopError.value = null })
// When the loop stops starting passes: the limit after the loop's start.
const loopEndsAt = computed(() => loop.value?.enabled && loop.value.startedAt
  ? new Date(Date.parse(loop.value.startedAt) + loop.value.maxRuntimeSeconds * 1000).toISOString()
  : null)
// The box shows the server's state until the change is saved.
function toggleLoop(e: Event) {
  const box = e.target as HTMLInputElement
  const on = box.checked
  box.checked = !!loop.value?.enabled
  saveLoop(on)
}
function saveLoop(enabled: boolean) {
  const q = queue.value
  if (!q) return
  loopError.value = loopLimit.value < 60 ? 'Pick a limit of at least 1 minute.' : loopRuntimeError(loopRuntimeText(loopLimit.value))
  if (loopError.value) return
  void act("Couldn't change looping", () => queuesApi.setLoop(q.id, enabled, loopRuntimeText(loopLimit.value)))
}

// ---- adding and editing items ----
interface Draft { agent: Agent; flags: string; instruction: string; executionMode: 'agent' | 'session'; targetSession: string; command: string; verifyCommand: string; requiresApproval: boolean }
const VERIFY_HINT = "Runs as argv in the project directory on the host, not in the agent's session; quote words like flags. For pipes or &&: sh -c '…'."
/** The gate fields to send: none when unset (the server's default). */
function gates(d: Pick<Draft, 'verifyCommand' | 'requiresApproval'>) {
  const verifyCommand = d.verifyCommand.trim()
  return {
    ...(verifyCommand ? { verifyCommand } : {}),
    ...(d.requiresApproval ? { requiresApproval: true } : {}),
  }
}
const permissionFlag: Record<Agent, string> = {
  claude: '--dangerously-skip-permissions',
  codex: '--yolo',
}
function hasFlag(flags: string, flag: string): boolean {
  return new RegExp(`(?:^|\\s)${flag}(?=\\s|$)`).test(flags)
}
function setFlag(flags: string, flag: string, enabled: boolean): string {
  const without = flags.replace(new RegExp(`(^|\\s)${flag}(?=\\s|$)`, 'g'), '$1').trim()
  return enabled ? [without, flag].filter(Boolean).join(' ') : without
}
function switchAgentFlags(flags: string, agent: Agent): string {
  const custom = setFlag(setFlag(flags, permissionFlag.claude, false), permissionFlag.codex, false)
  return setFlag(custom, permissionFlag[agent], true)
}
const draft = ref<Draft>({ agent: 'claude', flags: permissionFlag.claude, instruction: INSTRUCTION_PREFIX, executionMode: 'agent', targetSession: '', command: '', verifyCommand: '', requiresApproval: false })
const draftPermissionFlag = computed({
  get: () => draft.value.agent,
  set: (agent: Agent) => { draft.value = { ...draft.value, agent, flags: switchAgentFlags(draft.value.flags, agent) } },
})
const edit = ref<Draft>({ agent: 'claude', flags: '', instruction: '', executionMode: 'agent', targetSession: '', command: '', verifyCommand: '', requiresApproval: false })
const editPermissionFlag = computed({
  get: () => edit.value.agent,
  set: (agent: Agent) => { edit.value = { ...edit.value, agent, flags: switchAgentFlags(edit.value.flags, agent) } },
})
const draftTouched = ref(false)
const draftErrors = computed(() => ({ flags: flagsError(draft.value.flags), instruction: instructionError(draft.value.instruction), verify: verifyCommandError(draft.value.verifyCommand) }))
function addItem() {
  draftTouched.value = true
  if (!queue.value || (draft.value.executionMode === 'agent' && (draftErrors.value.flags || draftErrors.value.instruction || draftErrors.value.verify)) || (draft.value.executionMode === 'session' && (!draft.value.targetSession || !draft.value.command.trim()))) return
  const q = queue.value
  const d = { ...draft.value }
  void act("Couldn't add the item", async () => {
    await queuesApi.addItem(q.id, { agent: d.agent, flags: d.flags, instruction: d.instruction, executionMode: d.executionMode, ...(d.executionMode === 'session' ? { targetSession: d.targetSession, command: d.command } : { ...gates(d) }) })
    draft.value = { agent: d.agent, flags: permissionFlag[d.agent], instruction: INSTRUCTION_PREFIX, executionMode: 'agent', targetSession: '', command: '', verifyCommand: '', requiresApproval: false }
    draftTouched.value = false
  })
}

const editing = ref<string | null>(null)
// V2-M4: a needs-attention item edits only its gates (the server refuses
// other fields); a queued one edits everything.
const gatesOnly = ref(false)
const editErrors = computed(() => ({
  flags: gatesOnly.value || edit.value.executionMode === 'session' ? '' : flagsError(edit.value.flags),
  instruction: gatesOnly.value || edit.value.executionMode === 'session' ? '' : instructionError(edit.value.instruction),
  verify: edit.value.executionMode === 'session' ? '' : verifyCommandError(edit.value.verifyCommand),
}))
function startEdit(item: QueueItem, onlyGates = false) {
  editing.value = item.id
  gatesOnly.value = onlyGates
  edit.value = { agent: item.agent, flags: item.flags, instruction: item.instruction, executionMode: item.executionMode ?? 'agent', targetSession: item.targetSession ?? '', command: item.command ?? '', verifyCommand: item.verifyCommand ?? '', requiresApproval: Boolean(item.requiresApproval) }
}
function saveEdit(item: QueueItem) {
  if (editErrors.value.flags || editErrors.value.instruction || editErrors.value.verify) return
  if (!gatesOnly.value && edit.value.executionMode === 'session' && (!edit.value.targetSession || !edit.value.command.trim())) return
  const e = { ...edit.value }
  const gateFields = { verifyCommand: e.verifyCommand.trim(), requiresApproval: e.requiresApproval }
  const body = gatesOnly.value ? gateFields : { agent: e.agent, flags: e.flags, instruction: e.instruction, executionMode: e.executionMode, ...(e.executionMode === 'session' ? { targetSession: e.targetSession, command: e.command } : { targetSession: '', command: '', ...gateFields }) }
  void act(gatesOnly.value ? "Couldn't save the gates" : "Couldn't save the item", async () => {
    await queuesApi.updateItem(item.id, body)
    editing.value = null
  })
}

// ---- order: drag (desktop), move buttons and Alt+Arrow keys ----
function reorder(ids: string[]) {
  if (!queue.value) return
  const q = queue.value
  void act("Couldn't reorder the queue", () => queuesApi.reorder(q.id, ids))
}
function move(item: QueueItem, delta: -1 | 1) {
  const ids = moveQueued(items.value, item.id, delta)
  if (!ids) return
  reorder(ids)
  void nextTick(() => document.querySelector<HTMLElement>(`[data-queue-item="${item.id}"]`)?.focus())
}
function onRowKey(event: KeyboardEvent, item: QueueItem) {
  if (!event.altKey || !itemActions(item).move) return
  if (event.key === 'ArrowUp') { event.preventDefault(); move(item, -1) }
  if (event.key === 'ArrowDown') { event.preventDefault(); move(item, 1) }
}
function onDragMove(e: { related?: HTMLElement }) {
  return e.related?.dataset.queueStatus === 'queued'
}
function onDragEnd() {
  reorder(items.value.filter((i) => i.status === 'queued').map((i) => i.id))
}

// ---- queue controls and owner overrides ----
function control(action: 'start' | 'pause' | 'resume') {
  if (!queue.value) return
  const q = queue.value
  void act(`Couldn't ${action} the queue`, () => action === 'start' ? queuesApi.start(q.id, startDelay.value ? loopRuntimeText(startDelay.value) : '') : queuesApi[action](q.id))
}
// The start delay in seconds; 0 starts now.
const startDelay = ref(0)

// The pending confirmation. Its dialog's open state is separate: closing
// the dialog (the action button closes it first) must not lose the action.
type ConfirmKind = 'skip' | 'mark-done' | 'delete-queue' | 'delete-item' | 'reject' | 'parallel-on' | 'parallel-off' | 'kill-session' | 'kill-completed'
const confirming = ref<{ kind: ConfirmKind; item?: QueueItem } | null>(null)
const confirmOpen = ref(false)
function ask(kind: ConfirmKind, item?: QueueItem) {
  confirming.value = { kind, item }
  confirmOpen.value = true
}
const confirmText = computed(() => {
  const c = confirming.value
  if (!c) return { title: '', body: '', action: '' }
  if (c.kind === 'delete-queue') return { title: `Delete queue ${queue.value?.name ?? ''}?`, body: 'Its items and their history are removed. Run sessions stay open; close them yourself.', action: 'Delete queue', danger: true }
  if (c.kind === 'delete-item') return { title: `Delete item ${c.item?.position}?`, body: "It is removed from the queue. This can't be undone.", action: 'Delete item', danger: true }
  if (c.kind === 'kill-session') return { title: `Kill session ${c.item?.run?.sessionName ?? ''}?`, body: `Item ${c.item?.position} is done. This ends every program running in its session. It can't be undone.`, action: 'Kill session', danger: true }
  if (c.kind === 'kill-completed') {
    const n = completedOpen.value.length
    return { title: `Kill ${n} completed ${n === 1 ? 'session' : 'sessions'}?`, body: `This ends every program running in ${completedOpen.value.join(', ')}. It can't be undone.`, action: 'Kill sessions', danger: true }
  }
  if (c.kind === 'parallel-on') return { title: 'Run queues in parallel?', body: 'Up to 2 runs will be active at once by default. Change this limit in Settings.', action: 'Turn on' }
  if (c.kind === 'parallel-off') return { title: 'Turn off parallel queues?', body: 'Active runs will continue, and new queues will wait.', action: 'Turn off' }
  if (c.kind === 'skip') return { title: `Skip item ${c.item?.position}?`, body: 'The queue moves on without it when you resume.', action: 'Skip' }
  if (c.kind === 'reject') return { title: `Reject item ${c.item?.position}?`, body: 'It will need your attention and the queue pauses. Retry reruns the agent; Mark done overrides.', action: 'Reject' }
  return { title: `Mark item ${c.item?.position} done?`, body: "Confirm that the agent's work is complete. The queue moves on when you resume.", action: 'Mark done' }
})
function confirmAction() {
  const c = confirming.value
  confirming.value = null
  confirmOpen.value = false
  if (!c) return
  if (c.kind === 'kill-completed') {
    void killSessions([...completedOpen.value])
  } else if (c.kind === 'kill-session' && c.item?.run?.sessionName) {
    void killSessions([c.item.run.sessionName])
  } else if (c.kind === 'parallel-on' || c.kind === 'parallel-off') {
    setParallel(c.kind === 'parallel-on')
  } else if (c.item && c.kind === 'delete-item') {
    const it = c.item
    void act("Couldn't delete the item", () => queuesApi.removeItem(it.id))
  } else if (c.kind === 'delete-queue' && queue.value) {
    const q = queue.value
    void act("Couldn't delete the queue", async () => {
      await queuesApi.remove(q.id)
      store.queues = store.queues.filter((x) => x.id !== q.id)
      selectedId.value = null
    })
  } else if (c.item && c.kind === 'reject') {
    const it = c.item
    void act("Couldn't reject the item", () => queuesApi.reject(it.id))
  } else if (c.item) {
    const it = c.item
    void act(c.kind === 'skip' ? "Couldn't skip the item" : "Couldn't mark the item done", () => (c.kind === 'skip' ? queuesApi.skip(it.id) : queuesApi.markDone(it.id)))
  }
}
function approve(item: QueueItem) {
  void act("Couldn't approve the item", () => queuesApi.approve(item.id))
}
function reverify(item: QueueItem) {
  void act("Couldn't re-run verify", () => queuesApi.reverify(item.id))
}
function retry(item: QueueItem) {
  void act("Couldn't retry the item", () => queuesApi.retry(item.id))
}
// The open run sessions of this queue's done items.
const openSessions = computed(() => new Set(sessions.list(props.machine).map((s) => s.name)))
const completedOpen = computed(() => completedSessions(items.value, openSessions.value))
/** Kills the sessions one after another; a failure doesn't stop the rest. */
async function killSessions(names: string[]) {
  busy.value = true
  error.value = null
  const failed: string[] = []
  let first: ReturnType<typeof describeError> | null = null
  for (const name of names) {
    try {
      await sessionsApi.kill(props.machine, name)
      emit('killed', name)
    } catch (e) {
      failed.push(name)
      first ??= describeError(e)
    }
  }
  if (first) error.value = { title: `Couldn't kill ${failed.length === 1 ? 'session' : 'sessions'} ${failed.join(', ')}`, ...first }
  busy.value = false
}
function openSession(item: QueueItem) {
  if (!item.run?.sessionName) return
  open.value = false
  emit('openSession', item.run.sessionName)
}

const badge: Record<QueueItem['status'], string> = {
  queued: 'border-border text-muted',
  running: 'border-accent text-accent',
  verifying: 'border-accent text-accent',
  awaiting_approval: 'border-accent text-fg',
  done: 'border-ok text-ok',
  needs_attention: 'border-danger text-danger',
  skipped: 'border-border text-muted',
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent
        class="fixed z-40 flex flex-col overflow-hidden border border-border bg-surface text-fg"
        :class="props.compact ? 'inset-0 h-dvh w-full pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)]' : 'left-1/2 top-1/2 max-h-[min(48rem,90vh)] w-[min(52rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <div class="flex items-start justify-between gap-2 border-b border-border px-3 py-2">
          <div class="min-w-0">
            <DialogTitle aria-label="Queue" class="flex items-center gap-2 text-base font-bold">
              <span>Queue</span>
              <span v-if="items.length" class="inline-flex items-center gap-1.5" :aria-label="progressDescription" :title="progressDescription" data-testid="queue-progress">
                <span class="flex items-center gap-0.5" aria-hidden="true">
                  <span v-for="segment in progressSegments" :key="segment.id" class="h-1.5 w-2 rounded-sm" :class="segment.color" />
                </span>
                <span class="text-[11px] font-normal text-muted">{{ remainingCount }} left</span>
              </span>
            </DialogTitle>
            <DialogDescription class="text-sm text-muted">
              Items run one after another in their own session. Codex completion is tracked from its thread goal; Claude pauses after a turn for you to review and mark done.
            </DialogDescription>
            <p v-if="queue?.startedAt" class="mt-1 text-xs text-muted">
              Started <time :datetime="queue.startedAt">{{ new Date(queue.startedAt).toLocaleString() }}</time>
              <template v-if="queue.endedAt"> · Finished <time :datetime="queue.endedAt">{{ new Date(queue.endedAt).toLocaleString() }}</time></template>
            </p>
          </div>
          <div class="flex shrink-0 gap-2">
            <button v-if="!historyOpen" type="button" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border" aria-label="History" title="Queue history" @click="showHistory"><History :size="18" aria-hidden="true" /></button>
            <button v-else type="button" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border" aria-label="Queue" title="Back to the queue" @click="historyOpen = false"><ListOrdered :size="18" aria-hidden="true" /></button>
            <DialogClose aria-label="Close queue panel" title="Close" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border">
              <X :size="18" aria-hidden="true" />
            </DialogClose>
          </div>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto overflow-x-hidden p-3">
          <section v-if="historyOpen" aria-label="Queue history" data-testid="queue-history">
            <h2 class="text-lg font-bold">Queue history</h2>
            <p class="mt-1 text-sm text-muted">Status changes and item metadata. Terminal output and session transcripts are not stored here.</p>
            <p v-if="historyError" role="alert" class="mt-2 text-danger">Couldn't load queue history: {{ historyError }}</p>
            <p v-if="!history.length && !historyLoading && !historyError" class="mt-4 text-muted">No queue history yet.</p>
            <ol class="mt-3 space-y-4">
              <li v-for="group in historyGroups" :key="group.id" class="min-w-0">
                <h3>
                  <button
                    type="button"
                    class="touch-target flex min-h-11 w-full min-w-0 items-center gap-2 rounded border border-border px-3 text-left"
                    :aria-expanded="expandedHistory.has(group.id)"
                    :aria-controls="`queue-history-${group.id}`"
                    @click="toggleHistoryGroup(group.id)"
                  >
                    <span aria-hidden="true" class="text-muted">{{ expandedHistory.has(group.id) ? '▾' : '▸' }}</span>
                    <span class="min-w-0 flex-1 break-words font-bold">{{ group.name }} <span class="font-normal text-muted">· {{ group.project }}</span></span>
                    <span class="shrink-0 text-xs text-muted">{{ group.entries.length }} {{ group.entries.length === 1 ? 'entry' : 'entries' }}</span>
                  </button>
                </h3>
                <ol v-if="expandedHistory.has(group.id)" :id="`queue-history-${group.id}`" class="mt-2 space-y-2">
                  <li v-for="entry in group.entries" :key="entry.id" class="min-w-0 rounded border border-border p-3" :data-history-id="entry.id">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <p class="font-semibold">Item {{ entry.position }} — {{ actionLabel(entry.action) }}: {{ entry.status.replace('_', ' ') }}</p>
                      <time class="text-xs text-muted" :datetime="entry.occurredAt">{{ new Date(entry.occurredAt).toLocaleString() }}</time>
                    </div>
                    <p class="mt-1 break-words font-mono text-sm">{{ entry.executionMode === 'session' ? entry.command : entry.instruction }}</p>
                    <p class="mt-1 text-sm text-muted">{{ entry.executionMode === 'session' ? `Command in ${entry.targetSession || 'existing session'}` : `${entry.agent}${entry.flags ? ` · ${entry.flags}` : ''}` }}</p>
                    <p v-if="entry.verifyCommand || entry.requiresApproval" class="mt-1 break-words text-sm text-muted">
                      <template v-if="entry.verifyCommand">Verify: <span class="font-mono">{{ entry.verifyCommand }}</span></template>
                      <template v-if="entry.verifyCommand && entry.requiresApproval"> · </template>
                      <template v-if="entry.requiresApproval">Requires approval</template>
                    </p>
                    <p v-if="entry.detail" class="mt-1 break-words text-sm text-danger">{{ entry.detail }}</p>
                  </li>
                </ol>
              </li>
            </ol>
            <button v-if="historyHasMore" type="button" class="touch-target mt-3 inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border" :aria-label="historyLoading ? 'Loading…' : 'Load older'" title="Load older" :disabled="historyLoading" @click="loadHistory()"><ChevronsDown :size="18" aria-hidden="true" /></button>
            <p v-else-if="historyLoading" class="mt-3 text-sm text-muted">Loading…</p>
          </section>
          <template v-else>
          <FormError v-if="error" id="queue-error" :title="error.title" :message="error.message" :hint="error.hint" />
          <p v-if="store.loadError" role="alert" class="mt-2 text-danger">
            Couldn't load the queue: {{ store.loadError }}
          </p>

          <!-- V2-M2 queue switcher: buttons on desktop (not a list: the items are), a select on the phone. -->
          <div v-if="store.queues.length" class="mt-1 flex flex-wrap items-center gap-2">
            <label v-if="props.compact" class="block min-w-0 flex-1">Queue
              <select v-model="selectModel" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                <option v-for="q in store.queues" :key="q.id" :value="q.id">{{ q.name }} · {{ q.projectName }} ({{ q.status }})</option>
              </select>
            </label>
            <nav v-else aria-label="Queues" class="flex min-w-0 flex-wrap gap-1">
              <button
                v-for="q in store.queues"
                :key="q.id"
                type="button"
                :aria-current="q.id === queue?.id ? 'true' : undefined"
                :aria-label="`Show queue ${q.name}`"
                :title="`${q.name} · ${q.projectName} (${q.status})`"
                :data-running="queueRunning(q) ? 'true' : undefined"
                class="touch-target inline-flex min-h-8 max-w-56 items-center gap-1 rounded border px-2 text-sm"
                :class="[q.id === queue?.id ? 'text-accent' : '', queueRunning(q) ? 'border-ok ring-1 ring-ok' : q.id === queue?.id ? 'border-accent' : 'border-border']"
                @click="select(q.id)"
              >
                <TriangleAlert v-if="q.warnings?.length" :size="14" aria-hidden="true" class="shrink-0 text-danger" />
                <span class="truncate">{{ q.name }}</span>
                <span class="truncate text-muted">· {{ q.projectName }} · {{ q.status }}</span>
              </button>
            </nav>
            <button
              type="button"
              :disabled="busy || addingQueue"
              class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border"
              aria-label="New queue"
              title="Create another queue"
              @click="newQueue"
            >
              <Plus :size="16" aria-hidden="true" />
            </button>
          </div>
          <label v-if="store.queues.length" class="mt-1 flex min-h-8 items-center gap-2 text-sm">
            <input
              type="checkbox"
              data-testid="parallel-toggle"
              autocomplete="off"
              class="size-4"
              :checked="store.parallelQueues"
              :disabled="busy"
              @change="toggleParallel"
            >
            Run queues in parallel
          </label>
          <p v-if="store.queues.length && !store.parallelQueues" data-testid="parallel-off" class="text-sm text-muted">
            One queue runs at a time. You can still create queues to organize work; turn this on to run several at once (each stays sequential).
          </p>

          <form v-if="showCreate" class="mt-2 flex flex-col gap-3" aria-label="Create queue" @submit.prevent="createQueue">
            <p v-if="!projects.items.length" class="text-muted">
              Save a folder as a project first (Browse files → Open as project).
            </p>
            <label class="block">Project
              <select v-model="newProjectId" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                <option v-for="p in projects.items" :key="p.id" :value="p.id">{{ p.name }} — {{ p.path }}</option>
              </select>
            </label>
            <label class="block">Queue name
              <input v-model="newName" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
            </label>
            <label v-if="activeGoalRuns.length || linkableSessions.length" class="block">Start after (optional)
              <select v-model="afterLink" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                <option value="">Start normally</option>
                <optgroup v-if="activeGoalRuns.length" label="Active goal is achieved">
                  <option v-for="run in activeGoalRuns" :key="run.id" :value="`run:${run.id}`">{{ run.session }} · {{ run.goal }} ({{ run.queue }})</option>
                </optgroup>
                <optgroup v-if="linkableSessions.length" label="Existing session is idle">
                  <option v-for="s in linkableSessions" :key="s.name" :value="`session:${s.name}`">{{ s.name }} · {{ s.path }}</option>
                </optgroup>
              </select>
            </label>
            <p v-if="afterLink.startsWith('session:')" class="text-sm text-muted">
              The first item waits until that session's agent finishes its turn or exits, or the session closes. Without agent status hooks, an open agent counts as busy until it exits.
            </p>
            <div class="flex gap-2">
              <button type="submit" :disabled="busy || !newProjectId" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded bg-accent text-bg" aria-label="Create queue" title="Create queue">
                <Check :size="18" aria-hidden="true" />
              </button>
              <button v-if="store.queues.length" type="button" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border" aria-label="Cancel" title="Cancel" @click="addingQueue = false">
                <X :size="18" aria-hidden="true" />
              </button>
            </div>
          </form>

          <template v-if="queue && !addingQueue">
            <p v-if="queue.afterSession" class="mt-2 text-sm text-muted" data-testid="queue-dependency">
              <template v-if="queue.items.some((it) => it.status !== 'queued')">Started after session <span class="font-mono">{{ queue.afterSession }}</span> was idle.</template>
              <template v-else>Waits for session <span class="font-mono">{{ queue.afterSession }}</span> to be idle before its first item.</template>
            </p>
            <p v-if="queue.afterRunId" class="mt-2 text-sm text-muted" data-testid="queue-dependency">
              <template v-if="queue.afterRunStatus === 'achieved'">Started after its linked goal was achieved.</template>
              <template v-else-if="['starting', 'running', 'stale'].includes(queue.afterRunStatus || '')">Waiting for the linked session's goal to be achieved.</template>
              <template v-else>The linked goal is unavailable or ended without achievement. This queue is paused; create a new queue linked to an active goal.</template>
            </p>
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <form v-if="renaming" class="flex min-w-0 flex-wrap items-end gap-2" aria-label="Rename queue" @submit.prevent="saveRename">
                <label class="block min-w-0">Queue name
                  <input v-model="renameText" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                </label>
                <button type="submit" :disabled="busy || !renameText.trim()" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded bg-accent text-bg" aria-label="Save" title="Save name"><Check :size="18" aria-hidden="true" /></button>
                <button type="button" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border" aria-label="Cancel" title="Cancel" @click="renaming = false"><X :size="18" aria-hidden="true" /></button>
              </form>
              <h3 v-else class="min-w-0 font-bold">
                {{ queue.name }} <span class="font-normal text-muted">· {{ queue.projectName }}</span>
              </h3>
              <button v-if="!renaming" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" aria-label="Rename queue" title="Rename" @click="startRename">
                <Pencil :size="15" aria-hidden="true" />
              </button>
              <span data-testid="queue-status" class="rounded border border-border px-2 text-sm" :class="queue.status === 'running' ? 'border-accent font-bold text-accent' : ''">{{ queue.status }}</span>
              <div class="ml-auto flex flex-wrap gap-2">
                <div v-if="controls?.start" class="text-sm">
                  <span aria-hidden="true">Start after</span>
                  <DurationPicker v-model="startDelay" label="Start delay" />
                </div>
                <button v-if="controls?.start || queue.status === 'idle' || queue.status === 'finished'" type="button" :disabled="busy || !controls?.start || !!runBlockedBy" aria-label="Start" :title="runBlockedTitle ?? 'Start the queue'" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded bg-accent text-bg" @click="control('start')"><Play :size="18" aria-hidden="true" /></button>
                <button v-if="controls?.pause" type="button" :disabled="busy" aria-label="Pause" title="Pause the queue" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border" @click="control('pause')"><Pause :size="18" aria-hidden="true" /></button>
                <button v-if="controls?.resume" type="button" :disabled="busy || !!runBlockedBy" aria-label="Resume" :title="runBlockedTitle ?? 'Resume the queue'" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded bg-accent text-bg" @click="control('resume')"><Play :size="18" aria-hidden="true" /></button>
                <button v-if="completedOpen.length" type="button" :disabled="busy" aria-label="Kill completed sessions" :title="`Kill completed sessions (${completedOpen.length})`" data-testid="kill-completed" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded gap-1 border border-border px-2 text-danger" @click="ask('kill-completed')"><ListX :size="18" aria-hidden="true" /><span class="text-xs" aria-hidden="true">{{ completedOpen.length }}</span></button>
                <button type="button" :disabled="busy" aria-label="Delete queue" title="Delete queue" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border text-danger" @click="ask('delete-queue')"><Trash2 :size="18" aria-hidden="true" /></button>
              </div>
            </div>
            <p v-if="runBlockedBy && (controls?.start || controls?.resume)" data-testid="queue-run-blocked" class="mt-1 text-sm text-muted">
              Can't run yet: queue {{ runBlockedBy.name }} is running and Run queues in parallel is off.
            </p>
            <form aria-label="Loop queue" data-testid="queue-loop" class="mt-2 flex flex-wrap items-end gap-2 text-sm" @submit.prevent="saveLoop(!!loop?.enabled)">
              <label class="flex min-h-11 items-center gap-2">
                <input type="checkbox" autocomplete="off" :checked="!!loop?.enabled" :disabled="busy" aria-label="Loop the queue" class="h-5 w-5" @change="toggleLoop">
                Loop the queue
              </label>
              <div>
                <span aria-hidden="true">Stop starting passes after</span>
                <DurationPicker v-model="loopLimit" label="Loop runtime limit" />
              </div>
              <button v-if="loop?.enabled && loopLimit !== loop.maxRuntimeSeconds" type="submit" :disabled="busy" aria-label="Save limit" title="Save limit" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border"><Save :size="18" aria-hidden="true" /></button>
              <p id="queue-loop-help" class="basis-full text-muted">
                <span v-if="loopError" role="alert" class="text-danger">{{ loopError }}</span>
                <template v-else-if="loop?.enabled">
                  <span data-testid="queue-loop-status">Pass {{ loop.pass }}<template v-if="loopEndsAt">; no new pass starts after <time :datetime="loopEndsAt">{{ new Date(loopEndsAt).toLocaleString() }}</time></template>.</span>
                  After the last item the queue runs all its items again, at most once a minute. The limit counts from Start and is checked between passes; a pass that began always finishes.
                </template>
              </p>
            </form>
            <p v-if="queue.scheduledAt" data-testid="queue-scheduled" class="mt-1 text-sm text-accent">
              Scheduled for <time :datetime="queue.scheduledAt">{{ new Date(queue.scheduledAt).toLocaleString() }}</time>.
            </p>
            <p class="mt-1 break-all text-sm text-muted">
              {{ queue.projectPath }}
            </p>
            <p v-for="w in queue.warnings ?? []" :key="w.code" role="status" data-testid="queue-warning" class="mt-2 flex items-start gap-2 rounded border border-danger px-2 py-1 text-sm">
              <TriangleAlert :size="16" aria-hidden="true" class="mt-0.5 shrink-0 text-danger" />
              <span class="break-words">{{ w.message }}</span>
            </p>

            <p v-if="!items.length" class="mt-3 text-muted">
              No items yet. Add one below.
            </p>
            <VueDraggable
              v-else
              v-model="items"
              tag="ol"
              handle=".queue-drag-handle"
              :disabled="props.compact || busy"
              :animation="150"
              :force-fallback="true"
              :fallback-on-body="true"
              :fallback-tolerance="4"
              :on-move="onDragMove"
              aria-label="Queue items"
              class="mt-3 flex flex-col gap-2"
              @end="onDragEnd"
            >
              <li
                v-for="item in items"
                :key="item.id"
                :data-queue-item="item.id"
                :data-queue-status="item.status"
                :data-highlighted="highlighted === item.id ? 'true' : undefined"
                :aria-label="`Item ${item.position}: ${item.instruction}`"
                tabindex="0"
                class="rounded border p-2"
                :class="highlighted === item.id ? 'border-accent ring-2 ring-accent' : 'border-border'"
                @keydown="onRowKey($event, item)"
              >
                <form v-if="editing === item.id" class="flex flex-col gap-2" :aria-label="gatesOnly ? `Edit gates of item ${item.position}` : `Edit item ${item.position}`" @submit.prevent="saveEdit(item)">
                  <label v-if="!gatesOnly" class="block">Execution
                    <select v-model="edit.executionMode" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                      <option value="agent">New session with agent</option><option value="session">Command in existing session</option>
                    </select>
                  </label>
                  <template v-if="!gatesOnly && edit.executionMode === 'session'">
                    <label class="block">Existing session
                      <select v-model="edit.targetSession" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                        <option value="">Choose a session</option><option v-for="s in sessions.list(props.machine)" :key="s.name" :value="s.name">{{ s.name }} · {{ s.path }}</option>
                      </select>
                    </label>
                    <label class="block">Command
                      <textarea v-model="edit.command" autocomplete="off" spellcheck="false" rows="2" class="mt-1 min-h-11 w-full resize-y rounded border border-border bg-bg px-2 py-1 font-mono text-base"></textarea>
                    </label>
                  </template>
                  <template v-if="!gatesOnly && edit.executionMode === 'agent'">
                  <label class="block">Agent
                    <select v-model="editPermissionFlag" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                      <option v-for="a in AGENTS" :key="a" :value="a">{{ a }}</option>
                    </select>
                  </label>
                  <label class="block">Flags
                    <input v-model="edit.flags" autocomplete="off" autocapitalize="off" dir="ltr" spellcheck="false" class="mt-1 min-h-11 w-full min-w-0 rounded border border-border bg-bg px-3 text-left font-mono text-base">
                  </label>
                  <label class="flex min-h-11 items-center gap-2 text-sm">
                    <input type="checkbox" autocomplete="off" class="size-4" :checked="hasFlag(edit.flags, permissionFlag[edit.agent])" @change="edit.flags = setFlag(edit.flags, permissionFlag[edit.agent], ($event.target as HTMLInputElement).checked)">
                    {{ edit.agent === 'claude' ? 'Skip permission prompts' : 'YOLO mode' }}
                  </label>
                  <p v-if="editErrors.flags" class="text-sm text-danger">{{ editErrors.flags }}</p>
                  <label class="block">Instruction
                    <textarea v-model="edit.instruction" autocomplete="off" spellcheck="false" rows="2" class="mt-1 min-h-11 w-full resize-y rounded border border-border bg-bg px-2 py-1 font-mono text-base"></textarea>
                  </label>
                  <p v-if="editErrors.instruction" class="text-sm text-danger">{{ editErrors.instruction }}</p>
                  </template>
                  <template v-if="edit.executionMode === 'agent' || gatesOnly">
                  <label class="block">Verify command
                    <input v-model="edit.verifyCommand" data-testid="verify-command" autocomplete="off" autocapitalize="off" dir="ltr" spellcheck="false" placeholder="e.g. make test" class="mt-1 min-h-11 w-full min-w-0 rounded border border-border bg-bg px-3 text-left font-mono text-base">
                  </label>
                  <p class="text-sm text-muted">{{ VERIFY_HINT }}</p>
                  <p v-if="editErrors.verify" class="text-sm text-danger">{{ editErrors.verify }}</p>
                  <label class="flex min-h-11 items-center gap-2 text-sm">
                    <input v-model="edit.requiresApproval" data-testid="requires-approval" type="checkbox" autocomplete="off" class="size-4">
                    Require approval
                  </label>
                  </template>
                  <div class="flex justify-end gap-1.5">
                    <button type="submit" :disabled="busy" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded bg-accent text-bg" aria-label="Save" title="Save"><Check :size="16" aria-hidden="true" /></button>
                    <button type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" aria-label="Cancel" title="Cancel" @click="editing = null"><X :size="16" aria-hidden="true" /></button>
                  </div>
                </form>
                <template v-else>
                  <div class="flex items-start gap-2">
                    <span v-if="itemActions(item).move && !props.compact" class="queue-drag-handle touch-target inline-flex min-h-11 min-w-8 cursor-grab items-center justify-center text-muted" :aria-label="`Drag to reorder item ${item.position}`" role="button">
                      <GripVertical :size="16" aria-hidden="true" />
                    </span>
                    <span class="min-w-6 pt-1 text-muted">{{ item.position }}.</span>
                    <div class="min-w-0 flex-1">
                      <p class="break-words font-mono text-sm">{{ item.executionMode === 'session' ? item.command : item.instruction }}</p>
                      <p class="mt-1 break-words text-sm text-muted">
                        <template v-if="item.executionMode === 'session'">Command in <span class="font-mono">{{ item.targetSession }}</span></template>
                        <template v-else>{{ item.agent }}<template v-if="item.flags"> · <span class="font-mono">{{ item.flags }}</span></template>
                          <template v-if="item.run?.sessionName"> · session <span class="font-mono">{{ item.run.sessionName }}</span></template>
                        </template>
                      </p>
                      <p class="mt-1 flex flex-wrap items-center gap-2">
                        <span data-testid="item-status" :class="badge[item.status]" class="rounded border px-2 text-sm">{{ statusLabel(item) }}</span>
                        <time v-if="item.startedAt" class="text-xs text-muted" :datetime="item.startedAt">
                          Started {{ new Date(item.startedAt).toLocaleString() }}
                        </time>
                        <time v-if="item.endedAt" class="text-xs text-muted" :datetime="item.endedAt">
                          Ended {{ new Date(item.endedAt).toLocaleString() }}
                        </time>
                      </p>
                      <p v-if="item.run?.detail && (item.status === 'needs_attention' || item.run.status === 'failed')" role="status" class="mt-1 break-words text-sm text-danger">
                        {{ item.run.detail }}
                      </p>
                      <p v-if="item.executionMode === 'session' && item.status === 'done'" class="mt-1 text-sm text-muted">Sent to the existing session; the command may still be running.</p>
                      <p v-if="item.executionMode === 'session' && item.status === 'needs_attention'" role="status" class="mt-1 text-sm text-danger">The command could not be sent. Check that the session is still open, then retry.</p>
                      <div v-if="item.run?.flag && !['running', 'unknown'].includes(item.run.flag.label)" data-testid="llm-flag" class="mt-2 rounded border border-warning p-2 text-sm" role="status">
                        <p class="font-semibold">{{ item.run.flag.label === 'completed' ? 'Looks finished — check and Mark done' : `Supervisor: ${item.run.flag.label.replace('_', ' ')}` }}</p>
                        <p class="mt-1 break-words text-muted">{{ item.run.flag.reason }}</p>
                        <time class="mt-1 block text-xs text-muted" :datetime="item.run.flag.at">{{ new Date(item.run.flag.at).toLocaleString() }}</time>
                      </div>
                      <p v-if="item.verifyCommand || item.requiresApproval" data-testid="item-gates" class="mt-1 break-words text-sm text-muted">
                        <template v-if="item.verifyCommand">verify <span class="font-mono">{{ item.verifyCommand }}</span></template>
                        <template v-if="item.verifyCommand && item.requiresApproval"> · </template>
                        <template v-if="item.requiresApproval">requires approval</template>
                      </p>
                      <div v-if="item.verify" data-testid="item-verify" class="mt-1 text-sm">
                        <p>{{ verifyLine(item.verify) }}</p>
                        <details v-if="item.verify.output" class="mt-1">
                          <summary class="cursor-pointer text-muted">Output<template v-if="item.verify.truncated"> (truncated: the last 16 KiB)</template></summary>
                          <pre data-testid="verify-output" class="mt-1 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded border border-border bg-bg p-2 font-mono text-xs">{{ item.verify.output }}</pre>
                        </details>
                      </div>
                    </div>
                  </div>
                  <div class="mt-1 flex flex-wrap justify-end gap-1">
                    <template v-if="itemActions(item).move">
                      <button type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Move item ${item.position} up`" :disabled="busy" @click="move(item, -1)">
                        <ArrowUp :size="16" aria-hidden="true" />
                      </button>
                      <button type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Move item ${item.position} down`" :disabled="busy" @click="move(item, 1)">
                        <ArrowDown :size="16" aria-hidden="true" />
                      </button>
                    </template>
                    <button v-if="itemActions(item).edit" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Edit item ${item.position}`" title="Edit" @click="startEdit(item)"><Pencil :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).remove" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border text-danger" :aria-label="`Delete item ${item.position}`" title="Delete" :disabled="busy" @click="ask('delete-item', item)"><Trash2 :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).approve" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded bg-accent text-bg" :aria-label="`Approve item ${item.position}`" title="Approve" :disabled="busy" @click="approve(item)"><ThumbsUp :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).reject" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Reject item ${item.position}`" title="Reject" :disabled="busy" @click="ask('reject', item)"><ThumbsDown :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).reverify" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Re-run verify of item ${item.position}`" title="Re-run verify" :disabled="busy" @click="reverify(item)"><RotateCw :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).editGates" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Edit gates of item ${item.position}`" title="Edit gates" @click="startEdit(item, true)"><ShieldCheck :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).retry" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded bg-accent text-bg" :aria-label="`Retry item ${item.position}`" title="Retry" :disabled="busy" @click="retry(item)"><RotateCcw :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).skip" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Skip item ${item.position}`" title="Skip" :disabled="busy" @click="ask('skip', item)"><SkipForward :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).markDone" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Mark item ${item.position} done`" title="Mark done" :disabled="busy" @click="ask('mark-done', item)"><CircleCheck :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).openSession" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" :aria-label="`Open session of item ${item.position}`" title="Open session" @click="openSession(item)"><SquareTerminal :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).killSession && openSessions.has(item.run!.sessionName)" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border text-danger" :aria-label="`Kill session of item ${item.position}`" title="Kill session" :disabled="busy" @click="ask('kill-session', item)"><PowerOff :size="15" aria-hidden="true" /></button>
                  </div>
                </template>
              </li>
            </VueDraggable>

            <form class="mt-3 flex flex-col gap-2 border-t border-border pt-2" aria-label="Add item" @submit.prevent="addItem">
              <h4 class="font-bold">Add item</h4>
              <label class="block">Execution
                <select v-model="draft.executionMode" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                  <option value="agent">New session with agent</option><option value="session">Command in existing session</option>
                </select>
              </label>
              <template v-if="draft.executionMode === 'session'">
                <label class="block">Existing session
                  <select v-model="draft.targetSession" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                    <option value="">Choose a session</option><option v-for="s in sessions.list(props.machine)" :key="s.name" :value="s.name">{{ s.name }} · {{ s.path }}</option>
                  </select>
                </label>
                <label class="block">Command
                  <textarea v-model="draft.command" autocomplete="off" spellcheck="false" rows="2" placeholder="e.g. make test" class="mt-1 min-h-11 w-full resize-y rounded border border-border bg-bg px-2 py-1 font-mono text-base"></textarea>
                </label>
              </template>
              <template v-else>
              <div class="flex flex-wrap gap-2">
                <label class="block min-w-32">Agent
                  <select v-model="draftPermissionFlag" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                    <option v-for="a in AGENTS" :key="a" :value="a">{{ a }}</option>
                  </select>
                </label>
                <label class="block min-w-0 flex-1">Flags
                  <input v-model="draft.flags" autocomplete="off" autocapitalize="off" dir="ltr" spellcheck="false" :placeholder="permissionFlag[draft.agent]" class="mt-1 min-h-11 w-full min-w-0 rounded border border-border bg-bg px-3 text-left font-mono text-base">
                </label>
              </div>
              <label class="flex min-h-11 items-center gap-2 text-sm">
                <input type="checkbox" autocomplete="off" class="size-4" :checked="hasFlag(draft.flags, permissionFlag[draft.agent])" @change="draft.flags = setFlag(draft.flags, permissionFlag[draft.agent], ($event.target as HTMLInputElement).checked)">
                {{ draft.agent === 'claude' ? 'Skip permission prompts' : 'YOLO mode' }}
              </label>
              <p v-if="draftTouched && draftErrors.flags" class="text-sm text-danger">{{ draftErrors.flags }}</p>
              <label class="block">Instruction
                <textarea v-model="draft.instruction" autocomplete="off" spellcheck="false" rows="2" class="mt-1 min-h-11 w-full resize-y rounded border border-border bg-bg px-2 py-1 font-mono text-base"></textarea>
              </label>
              <p v-if="draftTouched && draftErrors.instruction" class="text-sm text-danger">{{ draftErrors.instruction }}</p>
              </template>
              <template v-if="draft.executionMode === 'agent'">
              <label class="block">Verify command <span class="text-muted">(optional)</span>
                <input v-model="draft.verifyCommand" data-testid="verify-command" autocomplete="off" autocapitalize="off" dir="ltr" spellcheck="false" placeholder="e.g. make test" class="mt-1 min-h-11 w-full min-w-0 rounded border border-border bg-bg px-3 text-left font-mono text-base">
              </label>
              <p class="text-sm text-muted">{{ VERIFY_HINT }}</p>
              <p v-if="draftTouched && draftErrors.verify" class="text-sm text-danger">{{ draftErrors.verify }}</p>
              <label class="flex min-h-11 items-center gap-2 text-sm">
                <input v-model="draft.requiresApproval" data-testid="requires-approval" type="checkbox" autocomplete="off" class="size-4">
                Require approval before the item is done
              </label>
              </template>
              <div class="flex justify-end">
                <button type="submit" :disabled="busy" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded bg-accent text-bg" aria-label="Add item" title="Add item"><Plus :size="18" aria-hidden="true" /></button>
              </div>
            </form>
          </template>
          </template>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
  <ConfirmDialog
    v-model:open="confirmOpen"
    :title="confirmText.title"
    :body="confirmText.body"
    :action="confirmText.action"
    :danger="confirmText.danger"
    :compact="props.compact"
    @confirm="confirmAction"
  />
</template>
