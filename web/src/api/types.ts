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
export type QueueItemStatus = 'queued' | 'running' | 'done' | 'needs_attention' | 'skipped'
export type RunStatus = 'starting' | 'running' | 'achieved' | 'failed' | 'exited' | 'stale' | 'cancelled'

export interface RunSummary {
  id: string
  status: RunStatus
  sessionName: string
  detail?: string
  clientVersion?: string
  startedAt: string
  endedAt?: string
}

export interface QueueItem {
  id: string
  queueId: string
  position: number
  agent: 'claude' | 'codex'
  flags: string
  instruction: string
  status: QueueItemStatus
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
  projectName: string
  projectPath: string
  items: QueueItem[]
  warnings?: QueueWarning[]
}

/** GET /api/queues. */
export interface QueueList {
  queues: Queue[]
  /** The parallel-queues switch (V2-M2; Queue panel, default HOSTBUD_PARALLEL_QUEUES). */
  parallelQueues: boolean
}

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
}

export type ServerEvent =
  | { type: 'snapshot'; machines: Machine[]; sessions: Record<string, Session[]> }
  | { type: 'machine.status'; machine: string; payload: Machine }
  | { type: 'sessions.changed'; machine: string; payload: { sessions: Session[] } }
  | { type: 'projects.changed'; machine: string; payload: { action: string; project: Project } }
  | { type: 'queue.changed'; machine: string; payload: QueueChanged }
  | { type: 'run.changed'; machine: string; payload: RunChanged }
  | { type: 'heartbeat' }
