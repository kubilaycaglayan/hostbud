import type { Queue, QueueItem, QueueItemStatus, QueueStatus, RunStatus, VerifySummary } from '@/api/types'

// Pure rules for the Queue panel (V2-M1, V2-M2) (mirrors internal/queue and
// internal/agents: the server enforces the same ones).

export const AGENTS = ['claude', 'codex'] as const
export type Agent = (typeof AGENTS)[number]

/** The queue default prompt's text until the owner changes it (Settings). */
export const DEFAULT_PROMPT = ', commit regularly.'

/** A new item's instruction: the default prompt when the owner opted in. */
export function initialInstruction(prompt: { enabled: boolean; text: string } | null | undefined): string {
  return prompt?.enabled ? prompt.text : ''
}

/** Where the caret goes in a prefilled instruction: before a prompt that
 * continues a sentence (", commit regularly."), after one that opens it. */
export function promptCaret(text: string): number {
  return /^[\s,.;:!?)]/.test(text) ? 0 : text.length
}

/** The default prompt's problem, or '' (the server checks the same). */
export function defaultPromptError(text: string): string {
  if (/[\r\n]/.test(text)) return 'The default prompt must be one line.'
  if (new TextEncoder().encode(text).length > 1000) return 'Keep the default prompt under 1000 characters.'
  return ''
}

/** The instruction's problem, or '' when it is a valid one-line prompt. */
export function instructionError(instruction: string): string {
  if (/[\r\n]/.test(instruction)) return 'The instruction must be one line.'
  if (!instruction.trim()) return 'Enter an instruction for the agent.'
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

/** The verify command's problem, or '' (V2-M4: argv split like flags,
 * one line, at most 4096 bytes; empty = no verify gate). */
export function verifyCommandError(command: string): string {
  if (new TextEncoder().encode(command.trim()).length > 4096) return 'The verify command must be at most 4096 bytes.'
  if (/[\r\n]/.test(command)) return 'The verify command must be one line.'
  return flagsError(command).replace(/^Flags have/, 'The verify command has').replace(/^Flags end/, 'The verify command ends')
}

/** What the owner can do with an item in its current state. */
export interface ItemActions {
  edit: boolean
  /** V2-M4: a needs-attention item may change only its gates. */
  editGates: boolean
  remove: boolean
  move: boolean
  retry: boolean
  skip: boolean
  markDone: boolean
  approve: boolean
  reject: boolean
  reverify: boolean
  openSession: boolean
  /** A done item's run session can be killed (when it is still open). */
  killSession: boolean
}

export function itemActions(item: QueueItem): ItemActions {
  const queued = item.status === 'queued'
  const attention = item.status === 'needs_attention'
  const awaiting = item.status === 'awaiting_approval'
  return {
    edit: queued,
    editGates: attention,
    remove: queued,
    move: queued,
    retry: attention,
    skip: attention,
    markDone: attention,
    approve: awaiting,
    reject: awaiting,
    reverify: attention && Boolean(item.verifyCommand) && item.run?.status === 'achieved',
    openSession: Boolean(item.run?.sessionName),
    killSession: item.status === 'done' && Boolean(item.run?.sessionName),
  }
}

/** The still-open run sessions of a queue's done items, once each, in item
 * order: what "Kill completed sessions" kills. */
export function completedSessions(items: QueueItem[], open: ReadonlySet<string>): string[] {
  const names = items.filter((it) => itemActions(it).killSession && open.has(it.run!.sessionName)).map((it) => it.run!.sessionName)
  return [...new Set(names)]
}

const VERIFY_OUTCOMES: Record<NonNullable<VerifySummary['outcome']>, string> = {
  passed: 'passed',
  failed: 'failed',
  timeout: 'timed out',
  missing_directory: 'project directory missing',
  timeout_missing: '`timeout` missing on the host',
  ssh_failed: "didn't run (SSH)",
  interrupted: 'interrupted by a restart',
}

/** One line about the latest verify attempt, e.g. "Verify attempt 2: failed · exit 1 · 3.2 s". */
export function verifyLine(v: VerifySummary): string {
  if (v.running) return `Verify attempt ${v.attempt}: running…`
  const parts = [`Verify attempt ${v.attempt}: ${v.outcome ? VERIFY_OUTCOMES[v.outcome] ?? v.outcome : 'finished'}`]
  if (v.exitCode !== undefined) parts.push(`exit ${v.exitCode}`)
  if (v.durationMs !== undefined) parts.push(v.durationMs < 1000 ? `${v.durationMs} ms` : `${(v.durationMs / 1000).toFixed(1)} s`)
  return parts.join(' · ')
}

/** Whether the queue is running: its status says so, or an item still runs
 * or verifies (a paused queue finishing its current item). */
export function queueRunning(q: Pick<Queue, 'status' | 'items'>): boolean {
  return q.status === 'running' || q.items.some((it) => it.status === 'running' || it.status === 'verifying')
}

/** Start needs a queued item, except a finished looping queue: Start runs its items again. */
export function queueControls(status: QueueStatus, hasQueued: boolean, loopRestart = false): { start: boolean; pause: boolean; resume: boolean } {
  return {
    start: (status === 'idle' || status === 'finished') && (hasQueued || (status === 'finished' && loopRestart)),
    pause: status === 'running',
    resume: status === 'paused',
  }
}

const ITEM_LABELS: Record<QueueItemStatus, string> = {
  queued: 'Queued',
  running: 'Running',
  verifying: 'Verifying',
  awaiting_approval: 'Awaiting approval',
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

/** The queue header's count: "1 in progress | 2 left", "Last in progress" when
 * nothing is queued behind the active item, else "2 left". */
export function progressCount(items: QueueItem[]): string {
  const active = items.filter((item) => ['running', 'verifying', 'awaiting_approval'].includes(item.status)).length
  const left = items.filter((item) => item.status === 'queued').length
  if (!active) return `${left} left`
  return left ? `${active} in progress | ${left} left` : 'Last in progress'
}

/** The cap input's problem, or '' for a whole number 1–32 or empty (default 2). */
export function capacityError(text: string): string {
  const value = text.trim()
  if (!value) return ''
  if (!/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 32) return 'Enter a whole number from 1 to 32, or leave it empty for the default limit of 2.'
  return ''
}

/** The cap input as the API wants it: null for empty (default limit of 2). */
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

/** DEFAULT_LOOP_RUNTIME_SECONDS is a queue's loop runtime limit until it's changed. */
export const DEFAULT_LOOP_RUNTIME_SECONDS = 5 * 3600

/** A runtime limit in seconds as a Go duration: 18000 → "5h", 5400 → "1h30m". */
export function loopRuntimeText(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  return [h ? `${h}h` : '', m ? `${m}m` : '', s ? `${s}s` : ''].join('') || '0s'
}

/** A loop runtime limit's client-side check (the server re-checks): a Go duration from 1s to 30d. */
export function loopRuntimeError(text: string): string | null {
  const t = text.trim()
  if (!/^(\d+(\.\d+)?(h|m|s))+$/.test(t)) return 'Enter a duration such as 5h or 1h30m.'
  let seconds = 0
  for (const [, n, , unit] of t.matchAll(/(\d+(\.\d+)?)(h|m|s)/g)) seconds += Number(n) * (unit === 'h' ? 3600 : unit === 'm' ? 60 : 1)
  if (seconds < 1 || seconds > 30 * 24 * 3600) return 'The limit must be from 1s to 30 days (720h).'
  return null
}

const LIVE_ITEM_STATUSES = new Set<QueueItem['status']>(['running', 'verifying', 'awaiting_approval'])

/** Whether an item's elapsed time still counts up. */
export function itemLive(item: QueueItem): boolean {
  return Boolean(item.startedAt) && !item.endedAt && LIVE_ITEM_STATUSES.has(item.status)
}

/** An item's total elapsed time, from its first start to its end (or now
 * while it runs): "45s", "12m 05s", "2h 03m". Empty when it hasn't started,
 * or was requeued and waits to run again. */
export function elapsedText(item: QueueItem, now: number): string {
  if (!item.startedAt) return ''
  const end = item.endedAt ? Date.parse(item.endedAt) : itemLive(item) ? now : NaN
  const start = Date.parse(item.startedAt)
  if (Number.isNaN(start) || Number.isNaN(end)) return ''
  const s = Math.max(0, Math.floor((end - start) / 1000))
  const pad = (n: number) => String(n).padStart(2, '0')
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${pad(s % 60)}s`
  return `${Math.floor(s / 3600)}h ${pad(Math.floor((s % 3600) / 60))}m`
}

function compactCount(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1_000_000) return `${+(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`
  return `${+(n / 1_000_000).toFixed(n < 10_000_000 ? 2 : 1)}M`
}

/** A run's token usage: "1.24M in · 45k out"; empty when not known (Codex,
 * command items, or no turn has ended yet). Input counts cache reads. */
export function tokensText(run: QueueItem['run']): string {
  const input = run?.inputTokens ?? 0
  const output = run?.outputTokens ?? 0
  if (!input && !output) return ''
  return `${compactCount(input)} in · ${compactCount(output)} out`
}
