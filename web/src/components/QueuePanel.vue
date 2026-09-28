<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogOverlay, AlertDialogPortal, AlertDialogRoot, AlertDialogTitle,
  DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle,
} from 'reka-ui'
import { VueDraggable } from 'vue-draggable-plus'
import { ArrowDown, ArrowUp, GripVertical, Pencil, Plus, Trash2, TriangleAlert } from 'lucide-vue-next'
import { queuesApi } from '@/api/client'
import type { QueueItem } from '@/api/types'
import FormError from './FormError.vue'
import { AGENTS, type Agent, flagsError, INSTRUCTION_PREFIX, instructionError, itemActions, moveQueued, queueControls, statusLabel, verifyCommandError, verifyLine } from '@/lib/queue'
import { useQueuesStore } from '@/stores/queues'
import { useProjectsStore } from '@/stores/projects'
import { describeError } from '@/stores/toasts'

// Queue panel: a queue belongs to a project; its items run one after
// another in their own tmux session, advancing only when the agent's own
// /goal is achieved. V2-M2 (HOSTBUD_PARALLEL_QUEUES): several queues, a
// switcher (a list on desktop, a select on the phone), rename, the
// shared-directory warning and "waiting for a free slot". V2-M4: per-item
// completion gates (verify command, approval), their state and output, and
// Approve / Reject / Re-run verify. It updates from /ws/events
// (queue.changed, run.changed) only.
const props = withDefaults(defineProps<{ compact?: boolean; machine?: string }>(), { machine: 'host' })
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ openSession: [name: string] }>()

const store = useQueuesStore()
const projects = useProjectsStore()
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
const controls = computed(() => queue.value ? queueControls(queue.value.status, hasQueued.value) : null)

const error = ref<{ title: string; message: string; hint?: string } | null>(null)
const busy = ref(false)

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
 * Switching off stops no run. */
function toggleParallel(on: boolean) {
  void act("Couldn't change parallel queues", async () => {
    store.parallelQueues = (await queuesApi.setParallel(props.machine, on)).parallelQueues
  })
}

watch(open, (isOpen) => { if (isOpen && !store.loaded) void store.load() })

// ---- creating, renaming and deleting queues ----
const newProjectId = ref('')
const newName = ref('Milestones')
watch(() => projects.items, (list) => { if (!newProjectId.value && list.length) newProjectId.value = list[0].id }, { immediate: true })
// With queues present, "New queue" opens the form; it needs the switch.
const addingQueue = ref(false)
const showCreate = computed(() => store.loaded && (!store.queues.length || addingQueue.value))
function newQueue() {
  addingQueue.value = true
  newName.value = ''
}
function createQueue() {
  // An unnamed queue: "Milestones" for the first, else the server's "Queue n".
  const name = newName.value.trim() || (store.queues.length ? '' : 'Milestones')
  void act("Couldn't create the queue", async () => {
    const created = await queuesApi.create(newProjectId.value, name)
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

// ---- adding and editing items ----
interface Draft { agent: Agent; flags: string; instruction: string; verifyCommand: string; requiresApproval: boolean }
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
const draft = ref<Draft>({ agent: 'claude', flags: permissionFlag.claude, instruction: INSTRUCTION_PREFIX, verifyCommand: '', requiresApproval: false })
const draftPermissionFlag = computed({
  get: () => draft.value.agent,
  set: (agent: Agent) => { draft.value = { ...draft.value, agent, flags: switchAgentFlags(draft.value.flags, agent) } },
})
const edit = ref<Draft>({ agent: 'claude', flags: '', instruction: '', verifyCommand: '', requiresApproval: false })
const editPermissionFlag = computed({
  get: () => edit.value.agent,
  set: (agent: Agent) => { edit.value = { ...edit.value, agent, flags: switchAgentFlags(edit.value.flags, agent) } },
})
const draftTouched = ref(false)
const draftErrors = computed(() => ({ flags: flagsError(draft.value.flags), instruction: instructionError(draft.value.instruction), verify: verifyCommandError(draft.value.verifyCommand) }))
function addItem() {
  draftTouched.value = true
  if (!queue.value || draftErrors.value.flags || draftErrors.value.instruction || draftErrors.value.verify) return
  const q = queue.value
  const d = { ...draft.value }
  void act("Couldn't add the item", async () => {
    await queuesApi.addItem(q.id, { agent: d.agent, flags: d.flags, instruction: d.instruction, ...gates(d) })
    draft.value = { agent: d.agent, flags: permissionFlag[d.agent], instruction: INSTRUCTION_PREFIX, verifyCommand: '', requiresApproval: false }
    draftTouched.value = false
  })
}

const editing = ref<string | null>(null)
// V2-M4: a needs-attention item edits only its gates (the server refuses
// other fields); a queued one edits everything.
const gatesOnly = ref(false)
const editErrors = computed(() => ({
  flags: gatesOnly.value ? '' : flagsError(edit.value.flags),
  instruction: gatesOnly.value ? '' : instructionError(edit.value.instruction),
  verify: verifyCommandError(edit.value.verifyCommand),
}))
function startEdit(item: QueueItem, onlyGates = false) {
  editing.value = item.id
  gatesOnly.value = onlyGates
  edit.value = { agent: item.agent, flags: item.flags, instruction: item.instruction, verifyCommand: item.verifyCommand ?? '', requiresApproval: Boolean(item.requiresApproval) }
}
function saveEdit(item: QueueItem) {
  if (editErrors.value.flags || editErrors.value.instruction || editErrors.value.verify) return
  const e = { ...edit.value }
  const gateFields = { verifyCommand: e.verifyCommand.trim(), requiresApproval: e.requiresApproval }
  const body = gatesOnly.value ? gateFields : { agent: e.agent, flags: e.flags, instruction: e.instruction, ...gateFields }
  void act(gatesOnly.value ? "Couldn't save the gates" : "Couldn't save the item", async () => {
    await queuesApi.updateItem(item.id, body)
    editing.value = null
  })
}
function removeItem(item: QueueItem) {
  void act("Couldn't delete the item", () => queuesApi.removeItem(item.id))
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
  void act(`Couldn't ${action} the queue`, () => queuesApi[action](q.id))
}

// The pending confirmation. Its dialog's open state is separate: closing
// the dialog (the action button closes it first) must not lose the action.
type ConfirmKind = 'skip' | 'mark-done' | 'delete-queue' | 'reject'
const confirming = ref<{ kind: ConfirmKind; item?: QueueItem } | null>(null)
const confirmOpen = ref(false)
function ask(kind: ConfirmKind, item?: QueueItem) {
  confirming.value = { kind, item }
  confirmOpen.value = true
}
const confirmText = computed(() => {
  const c = confirming.value
  if (!c) return { title: '', body: '', action: '' }
  if (c.kind === 'delete-queue') return { title: `Delete queue ${queue.value?.name ?? ''}?`, body: 'Its items and their history are removed. Run sessions stay open; close them yourself.', action: 'Delete queue' }
  if (c.kind === 'skip') return { title: `Skip item ${c.item?.position}?`, body: 'The queue moves on without it when you resume.', action: 'Skip' }
  if (c.kind === 'reject') return { title: `Reject item ${c.item?.position}?`, body: 'It will need your attention and the queue pauses. Retry reruns the agent; Mark done overrides.', action: 'Reject' }
  return { title: `Mark item ${c.item?.position} done?`, body: "This overrides the agent's own /goal verdict. The queue moves on when you resume.", action: 'Mark done' }
})
function confirmAction() {
  const c = confirming.value
  confirming.value = null
  confirmOpen.value = false
  if (!c) return
  if (c.kind === 'delete-queue' && queue.value) {
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
            <DialogTitle class="text-base font-bold">
              Queue
            </DialogTitle>
            <DialogDescription class="text-sm text-muted">
              Items run one after another in their own session. The next starts only when the agent's /goal is achieved.
            </DialogDescription>
          </div>
          <DialogClose aria-label="Close queue panel" title="Close" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border">
            ×
          </DialogClose>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto overflow-x-hidden p-3">
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
                class="touch-target inline-flex min-h-8 max-w-56 items-center gap-1 rounded border px-2 text-sm"
                :class="q.id === queue?.id ? 'border-accent text-accent' : 'border-border'"
                @click="select(q.id)"
              >
                <TriangleAlert v-if="q.warnings?.length" :size="14" aria-hidden="true" class="shrink-0 text-danger" />
                <span class="truncate">{{ q.name }}</span>
                <span class="truncate text-muted">· {{ q.projectName }} · {{ q.status }}</span>
              </button>
            </nav>
            <button
              type="button"
              :disabled="busy || !store.parallelQueues || addingQueue"
              class="touch-target inline-flex min-h-8 items-center gap-1 rounded border border-border px-2 text-sm"
              :title="store.parallelQueues ? 'Create another queue' : 'Turn on Run queues in parallel to create another queue'"
              @click="newQueue"
            >
              <Plus :size="14" aria-hidden="true" />New queue
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
              @change="toggleParallel(($event.target as HTMLInputElement).checked)"
            >
            Run queues in parallel
          </label>
          <p v-if="store.queues.length && !store.parallelQueues" data-testid="parallel-off" class="text-sm text-muted">
            One queue at a time. Turn this on to create and run several queues at once (each stays sequential).
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
            <div class="flex gap-2">
              <button type="submit" :disabled="busy || !newProjectId" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg">
                Create queue
              </button>
              <button v-if="store.queues.length" type="button" class="touch-target min-h-11 rounded border border-border px-3" @click="addingQueue = false">
                Cancel
              </button>
            </div>
          </form>

          <template v-if="queue && !addingQueue">
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <form v-if="renaming" class="flex min-w-0 flex-wrap items-end gap-2" aria-label="Rename queue" @submit.prevent="saveRename">
                <label class="block min-w-0">Queue name
                  <input v-model="renameText" autocomplete="off" class="mt-1 min-h-11 w-full rounded border border-border bg-bg px-3 text-base">
                </label>
                <button type="submit" :disabled="busy || !renameText.trim()" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg">Save</button>
                <button type="button" class="touch-target min-h-11 rounded border border-border px-3" @click="renaming = false">Cancel</button>
              </form>
              <h3 v-else class="min-w-0 font-bold">
                {{ queue.name }} <span class="font-normal text-muted">· {{ queue.projectName }}</span>
              </h3>
              <button v-if="!renaming" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border" aria-label="Rename queue" title="Rename" @click="startRename">
                <Pencil :size="15" aria-hidden="true" />
              </button>
              <span data-testid="queue-status" class="rounded border border-border px-2 text-sm">{{ queue.status }}</span>
              <div class="ml-auto flex flex-wrap gap-2">
                <button v-if="controls?.start || queue.status === 'idle' || queue.status === 'finished'" type="button" :disabled="busy || !controls?.start" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg" @click="control('start')">Start</button>
                <button v-if="controls?.pause" type="button" :disabled="busy" class="touch-target min-h-11 rounded border border-border px-3" @click="control('pause')">Pause</button>
                <button v-if="controls?.resume" type="button" :disabled="busy" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg" @click="control('resume')">Resume</button>
                <button type="button" :disabled="busy" class="touch-target min-h-11 rounded border border-border px-3" @click="ask('delete-queue')">Delete queue</button>
              </div>
            </div>
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
              :move="onDragMove"
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
                  <template v-if="!gatesOnly">
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
                  <label class="block">Verify command
                    <input v-model="edit.verifyCommand" data-testid="verify-command" autocomplete="off" autocapitalize="off" dir="ltr" spellcheck="false" placeholder="e.g. make test" class="mt-1 min-h-11 w-full min-w-0 rounded border border-border bg-bg px-3 text-left font-mono text-base">
                  </label>
                  <p class="text-sm text-muted">{{ VERIFY_HINT }}</p>
                  <p v-if="editErrors.verify" class="text-sm text-danger">{{ editErrors.verify }}</p>
                  <label class="flex min-h-11 items-center gap-2 text-sm">
                    <input v-model="edit.requiresApproval" data-testid="requires-approval" type="checkbox" autocomplete="off" class="size-4">
                    Require approval
                  </label>
                  <div class="flex justify-end gap-1.5">
                    <button type="submit" :disabled="busy" class="touch-target min-h-8 rounded bg-accent px-2 font-bold text-bg">Save</button>
                    <button type="button" class="touch-target min-h-8 rounded border border-border px-2" @click="editing = null">Cancel</button>
                  </div>
                </form>
                <template v-else>
                  <div class="flex items-start gap-2">
                    <span v-if="itemActions(item).move && !props.compact" class="queue-drag-handle touch-target inline-flex min-h-11 min-w-8 cursor-grab items-center justify-center text-muted" :aria-label="`Drag to reorder item ${item.position}`" role="button">
                      <GripVertical :size="16" aria-hidden="true" />
                    </span>
                    <span class="min-w-6 pt-1 text-muted">{{ item.position }}.</span>
                    <div class="min-w-0 flex-1">
                      <p class="break-words font-mono text-sm">{{ item.instruction }}</p>
                      <p class="mt-1 break-words text-sm text-muted">
                        {{ item.agent }}<template v-if="item.flags"> · <span class="font-mono">{{ item.flags }}</span></template>
                        <template v-if="item.run?.sessionName"> · session <span class="font-mono">{{ item.run.sessionName }}</span></template>
                      </p>
                      <p class="mt-1 flex flex-wrap items-center gap-2">
                        <span data-testid="item-status" :class="badge[item.status]" class="rounded border px-2 text-sm">{{ statusLabel(item) }}</span>
                      </p>
                      <p v-if="item.run?.detail && (item.status === 'needs_attention' || item.run.status === 'failed')" role="status" class="mt-1 break-words text-sm text-danger">
                        {{ item.run.detail }}
                      </p>
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
                    <button v-if="itemActions(item).remove" type="button" class="touch-target inline-flex min-h-8 min-w-8 items-center justify-center rounded border border-border text-danger" :aria-label="`Delete item ${item.position}`" title="Delete" :disabled="busy" @click="removeItem(item)"><Trash2 :size="15" aria-hidden="true" /></button>
                    <button v-if="itemActions(item).approve" type="button" class="touch-target min-h-8 rounded bg-accent px-2 font-bold text-bg" :aria-label="`Approve item ${item.position}`" :disabled="busy" @click="approve(item)">Approve</button>
                    <button v-if="itemActions(item).reject" type="button" class="touch-target min-h-8 rounded border border-border px-2" :aria-label="`Reject item ${item.position}`" :disabled="busy" @click="ask('reject', item)">Reject</button>
                    <button v-if="itemActions(item).reverify" type="button" class="touch-target min-h-8 rounded border border-border px-2" :aria-label="`Re-run verify of item ${item.position}`" :disabled="busy" @click="reverify(item)">Re-run verify</button>
                    <button v-if="itemActions(item).editGates" type="button" class="touch-target min-h-8 rounded border border-border px-2" :aria-label="`Edit gates of item ${item.position}`" @click="startEdit(item, true)">Edit gates</button>
                    <button v-if="itemActions(item).retry" type="button" class="touch-target min-h-8 rounded bg-accent px-2 font-bold text-bg" :aria-label="`Retry item ${item.position}`" :disabled="busy" @click="retry(item)">Retry</button>
                    <button v-if="itemActions(item).skip" type="button" class="touch-target min-h-8 rounded border border-border px-2" :aria-label="`Skip item ${item.position}`" :disabled="busy" @click="ask('skip', item)">Skip</button>
                    <button v-if="itemActions(item).markDone" type="button" class="touch-target min-h-8 rounded border border-border px-2" :aria-label="`Mark item ${item.position} done`" :disabled="busy" @click="ask('mark-done', item)">Mark done</button>
                    <button v-if="itemActions(item).openSession" type="button" class="touch-target min-h-8 rounded border border-border px-2" :aria-label="`Open session of item ${item.position}`" @click="openSession(item)">Open session</button>
                  </div>
                </template>
              </li>
            </VueDraggable>

            <form class="mt-3 flex flex-col gap-2 border-t border-border pt-2" aria-label="Add item" @submit.prevent="addItem">
              <h4 class="font-bold">Add item</h4>
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
              <label class="block">Verify command <span class="text-muted">(optional)</span>
                <input v-model="draft.verifyCommand" data-testid="verify-command" autocomplete="off" autocapitalize="off" dir="ltr" spellcheck="false" placeholder="e.g. make test" class="mt-1 min-h-11 w-full min-w-0 rounded border border-border bg-bg px-3 text-left font-mono text-base">
              </label>
              <p class="text-sm text-muted">{{ VERIFY_HINT }}</p>
              <p v-if="draftTouched && draftErrors.verify" class="text-sm text-danger">{{ draftErrors.verify }}</p>
              <label class="flex min-h-11 items-center gap-2 text-sm">
                <input v-model="draft.requiresApproval" data-testid="requires-approval" type="checkbox" autocomplete="off" class="size-4">
                Require approval before the item is done
              </label>
              <div class="flex justify-end">
                <button type="submit" :disabled="busy" class="touch-target min-h-8 rounded bg-accent px-2 font-bold text-bg">Add item</button>
              </div>
            </form>
          </template>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
  <AlertDialogRoot v-model:open="confirmOpen">
    <AlertDialogPortal>
      <AlertDialogOverlay class="fixed inset-0 z-[60] bg-overlay" />
      <AlertDialogContent
        class="fixed z-[60] border border-border bg-surface p-5 text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl pb-[max(1.25rem,env(safe-area-inset-bottom))]' : 'top-1/2 left-1/2 w-[min(24rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <AlertDialogTitle class="text-base font-bold">
          {{ confirmText.title }}
        </AlertDialogTitle>
        <AlertDialogDescription class="mt-2 text-muted">
          {{ confirmText.body }}
        </AlertDialogDescription>
        <div class="mt-4 flex justify-end gap-2">
          <AlertDialogCancel class="touch-target min-h-11 rounded border border-border px-3 py-2">
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction class="touch-target min-h-11 rounded bg-accent px-3 py-2 font-bold text-bg" @click.prevent="confirmAction">
            {{ confirmText.action }}
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>
