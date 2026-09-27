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

export type ServerEvent =
  | { type: 'snapshot'; machines: Machine[]; sessions: Record<string, Session[]> }
  | { type: 'machine.status'; machine: string; payload: Machine }
  | { type: 'sessions.changed'; machine: string; payload: { sessions: Session[] } }
  | { type: 'projects.changed'; machine: string; payload: { action: string; project: Project } }
  | { type: 'heartbeat' }
