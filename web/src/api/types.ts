// Shapes of the server's JSON (internal/inventory, internal/tmux, internal/api).

export type MachineStatus = 'unknown' | 'ok' | 'unreachable' | 'tmux_missing'

export interface Machine {
  id: string
  label: string
  status: MachineStatus
  error?: string
  hint?: string
  os: string
  home: string
  tmuxVersion: string
  tmuxMissing: boolean
  lastSeen?: string
}

export interface Session {
  id: string
  name: string
  path: string
  agents?: ('codex' | 'claude')[]
  status?: 'working' | 'blocked' | 'ended'
  /** Active pane's title (the agent's current task); absent for tmux's default. */
  title?: string
  agentUsage?: { agent: 'codex' | 'claude'; contextTokens: number; totalTokens: number; contextWindow: number }
  projectId?: string
  attached: number
  windows: number
  created: string
  activity: string
}

export interface TmuxPane {
  id: string
  index: number
  active: boolean
  command: string
  width: number
  height: number
}

export interface TmuxWindow {
  id: string
  index: number
  name: string
  active: boolean
  panes: TmuxPane[]
}

export interface TmuxWindows {
  windows: TmuxWindow[]
  truncated: boolean
}

export interface Project {
  id: string
  machineId: string
  path: string
  name: string
  sortOrder: number
  pinned: boolean
  createdAt: string
  updatedAt: string
}

// v2 queue (internal/queue, internal/store; V2-M1 and V2-M2).
export type QueueStatus = 'idle' | 'running' | 'paused' | 'finished'
export type QueueItemStatus = 'queued' | 'running' | 'verifying' | 'awaiting_approval' | 'done' | 'needs_attention' | 'skipped'
export type RunStatus = 'starting' | 'running' | 'achieved' | 'failed' | 'exited' | 'stale' | 'cancelled'

export interface RunSummary {
  id: string
  status: RunStatus
  sessionName: string
  detail?: string
  clientVersion?: string
  startedAt: string
  endedAt?: string
  flag?: RunFlag | null
  /** Token totals read from the transcript (Claude Code); absent until known. Input counts cache reads. */
  inputTokens?: number
  outputTokens?: number
}

/** V2-M4: the latest verify attempt of an item's latest run. */
export interface VerifySummary {
  attempt: number
  running: boolean
  outcome?: 'passed' | 'failed' | 'timeout' | 'missing_directory' | 'timeout_missing' | 'ssh_failed' | 'interrupted'
  exitCode?: number
  durationMs?: number
  truncated?: boolean
  /** Sanitized text (the last 16 KiB); render as text only. */
  output?: string
  detail?: string
}

export interface QueueItem {
  id: string
  queueId: string
  position: number
  agent: 'claude' | 'codex'
  flags: string
  instruction: string
  status: QueueItemStatus
  /** V2-M7: first run start and latest terminal completion time. */
  startedAt?: string
  endedAt?: string
  /** V2-M4 completion gates ("" = no verify command). */
  verifyCommand?: string
  requiresApproval?: boolean
  /** V2-M8: new tracked agent session or command sent to an existing session. */
  executionMode?: 'agent' | 'session'
  targetSession?: string
  command?: string
  verify?: VerifySummary
  run?: RunSummary
  /** V2-M2: the head item of a running queue while the machine's cap is reached. */
  waitingForSlot?: boolean
}

/** V2-M2: a non-blocking notice on a queue. */
export interface QueueWarning {
  code: 'shared_directory'
  message: string
  queues: { id: string; name: string; projectName: string }[]
}

export interface Queue {
  id: string
  projectId: string
  name: string
  status: QueueStatus
  /** V2-M7: first start and finish time. */
  startedAt?: string
  endedAt?: string
  /** V2-M8: scheduled start, cleared when the delay expires or queue pauses. */
  scheduledAt?: string
  projectName: string
  projectPath: string
  items: QueueItem[]
  warnings?: QueueWarning[]
  /** V2-M6: selected active run whose achieved goal releases this queue. */
  afterRunId?: string
  afterRunStatus?: string
  /** Any existing session (tracked or not) the first item waits for to be idle. */
  afterSession?: string
  /** When the link was set on an existing queue (absent: at creation). */
  afterLinkedAt?: string
  /** The session link was released: an item started after the session went idle. */
  afterReleased?: boolean
  /** Loop settings; absent while they are the default (off, 5 h). */
  loop?: QueueLoop
  /** Set once it differs from the default (off, ", commit regularly."). */
  defaultPrompt?: DefaultPrompt
}

/** A looping queue runs its items again after the last one ends, until maxRuntimeSeconds since startedAt have passed (checked between passes). */
export interface QueueLoop {
  enabled: boolean
  maxRuntimeSeconds: number
  startedAt?: string
  passStartedAt?: string
  /** The current pass, from 1. */
  pass: number
}

/** GET /api/queues. */
export interface QueueList {
  queues: Queue[]
  /** The parallel-queues switch (V2-M2; Queue panel, default HOSTBUD_PARALLEL_QUEUES). */
  parallelQueues: boolean
}

/** A queue's default prompt (PUT /api/queues/:id/default-prompt): when
 * enabled, each new item's instruction starts with text (opt-in). */
export interface DefaultPrompt {
  enabled: boolean
  text: string
}

/** Metadata-only historical snapshot for a queue item lifecycle event (V2-M9). */
export interface QueueItemHistory {
  id: number
  machineId: string
  queueId: string
  queueName: string
  projectName: string
  itemId: string
  position: number
  executionMode: 'agent' | 'session'
  targetSession?: string
  agent: 'claude' | 'codex'
  flags: string
  instruction: string
  command: string
  verifyCommand?: string
  requiresApproval?: boolean
  status: QueueItemStatus
  action: 'created' | 'edited' | 'status' | 'deleted'
  detail?: string
  /** The coding agent's own session id (Claude session_id, Codex thread id). */
  agentSessionId?: string
  occurredAt: string
}

export interface QueueHistoryPage { items: QueueItemHistory[] }

/** PUT /api/machines/:id/parallel-queues. */
export interface ParallelQueues {
  parallelQueues: boolean
}

/** GET|PUT /api/machines/:id/capacity (V2-M2): null = no cap. */
export interface Capacity {
  maxConcurrentRuns: number | null
}

export interface QueueChanged {
  action: string
  queueId: string
  queue?: Queue
  /** The parallel-queues switch at the time of the change. */
  parallelQueues?: boolean
  /** V2-M3: set on item done, needs attention and queue finished while an
   * account has notifications on. The only text a notification shows. */
  notification?: NotificationPayload
}

/** V2-M3: the allowlisted notification payload (internal/notify), the same
 * in-app and through push. */
export interface NotificationPayload {
  v: number
  kind: 'done' | 'attention' | 'finished'
  key: string
  project: string
  position: number
  outcome: string
  /** An app path: /queues/<id>?item=<id>. */
  url: string
  title: string
  body: string
}

/** GET|PUT /api/notifications/settings (the caller's account). */
export interface NotificationSettings {
  enabled: boolean
  onDone: boolean
  onAttention: boolean
  onFinished: boolean
  push: { available: boolean; reason?: string }
  vapidPublicKey?: string
}

export interface RunChanged {
  runId: string
  itemId: string
  queueId: string
  status: RunStatus
  detail?: string
  flag?: RunFlag | null
  inputTokens?: number
  outputTokens?: number
  notification?: NotificationPayload
}

export interface RunFlag {
  label: 'running' | 'waiting_input' | 'blocked' | 'completed' | 'failed' | 'unknown'
  reason: string
  at: string
}

export interface SupervisorStatus {
  enabled: boolean
  reason?: string
  provider?: string
  model?: string
  scrub?: boolean
  quietAfter?: string
  maxPerRunHour?: number
}

export type ServerEvent =
  | { type: 'snapshot'; machines: Machine[]; sessions: Record<string, Session[]> }
  | { type: 'machine.status'; machine: string; payload: Machine }
  | { type: 'sessions.changed'; machine: string; payload: { sessions: Session[] } }
  | { type: 'projects.changed'; machine: string; payload: { action: string; project: Project } }
  | { type: 'queue.changed'; machine: string; payload: QueueChanged }
  | { type: 'run.changed'; machine: string; payload: RunChanged }
  | { type: 'heartbeat' }
