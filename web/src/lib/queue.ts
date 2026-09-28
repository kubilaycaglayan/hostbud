import type { QueueItem, QueueItemStatus, QueueStatus, RunStatus } from '@/api/types'

// Pure rules for the Queue panel (V2-M1, V2-M2) (mirrors internal/queue and
// internal/agents: the server enforces the same ones).

export const AGENTS = ['claude', 'codex'] as const
export type Agent = (typeof AGENTS)[number]

export const INSTRUCTION_PREFIX = '/goal '

/** The instruction's problem, or '' when it's a valid "/goal <condition>". */
export function instructionError(instruction: string): string {
  if (/[\r\n]/.test(instruction)) return 'The instruction must be one line.'
  if (!instruction.startsWith(INSTRUCTION_PREFIX) || !instruction.slice(INSTRUCTION_PREFIX.length).trim()) {
    return 'Start with /goal followed by the condition, e.g. /goal work on milestone 2 per docs/roadmap/M2-tasks.md.'
  }
  return ''
}

/** The flags' problem (unbalanced quotes, line breaks), or ''. */
export function flagsError(flags: string): string {
  if (/[\r\n]/.test(flags)) return 'Flags must be one line.'
  let quote: string | null = null
  for (let i = 0; i < flags.length; i++) {
    const c = flags[i]
    if (quote === "'") {
      if (c === "'") quote = null
    } else if (quote === '"') {
      if (c === '\\') i++
      else if (c === '"') quote = null
    } else if (c === '\\') {
      if (i === flags.length - 1) return 'Flags end with a lone backslash.'
      i++
    } else if (c === "'" || c === '"') quote = c
  }
  return quote ? `Flags have an unbalanced ${quote === "'" ? 'single' : 'double'} quote.` : ''
}

/** What the owner can do with an item in its current state. */
export interface ItemActions {
  edit: boolean
  remove: boolean
  move: boolean
  retry: boolean
  skip: boolean
  markDone: boolean
  openSession: boolean
}

export function itemActions(item: QueueItem): ItemActions {
  const queued = item.status === 'queued'
  const attention = item.status === 'needs_attention'
  return {
    edit: queued,
    remove: queued,
    move: queued,
    retry: attention,
    skip: attention,
    markDone: attention,
    openSession: Boolean(item.run?.sessionName),
  }
}

export function queueControls(status: QueueStatus, hasQueued: boolean): { start: boolean; pause: boolean; resume: boolean } {
  return {
    start: (status === 'idle' || status === 'finished') && hasQueued,
    pause: status === 'running',
    resume: status === 'paused',
  }
}

const ITEM_LABELS: Record<QueueItemStatus, string> = {
  queued: 'Queued',
  running: 'Running',
  done: 'Done',
  needs_attention: 'Needs attention',
  skipped: 'Skipped',
}

const RUN_LABELS: Record<RunStatus, string> = {
  starting: 'starting',
  running: 'running',
  achieved: 'goal achieved',
  failed: 'failed',
  exited: 'exited',
  stale: 'no signal (stale)',
  cancelled: 'cancelled',
}

/** The item's status badge text: its status and, if any, its run's; a
 * queued head item waiting for a run slot says so (V2-M2). */
export function statusLabel(item: QueueItem): string {
  const label = ITEM_LABELS[item.status] ?? item.status
  if (item.status === 'queued' && item.waitingForSlot) return `${label} · waiting for a free slot`
  return item.run ? `${label} · ${RUN_LABELS[item.run.status] ?? item.run.status}` : label
}

/** The cap input's problem, or '' for a whole number 1–32 or empty (no cap). */
export function capacityError(text: string): string {
  const value = text.trim()
  if (!value) return ''
  if (!/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 32) return 'Enter a whole number from 1 to 32, or leave it empty for no cap.'
  return ''
}

/** The cap input as the API wants it: null for empty (no cap). */
export function capacityValue(text: string): number | null {
  const value = text.trim()
  return value ? Number(value) : null
}

/** The positions after moving the queued item `id` by `delta` among the
 * queued items; null when it can't move. */
export function moveQueued(items: QueueItem[], id: string, delta: -1 | 1): string[] | null {
  const queued = items.filter((i) => i.status === 'queued').map((i) => i.id)
  const at = queued.indexOf(id)
  const to = at + delta
  if (at < 0 || to < 0 || to >= queued.length) return null
  ;[queued[at], queued[to]] = [queued[to], queued[at]]
  return queued
}
