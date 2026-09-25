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
  attached: number
  windows: number
  created: string
  activity: string
}

export type ServerEvent =
  | { type: 'snapshot'; machines: Machine[]; sessions: Record<string, Session[]> }
  | { type: 'machine.status'; machine: string; payload: Machine }
  | { type: 'sessions.changed'; machine: string; payload: { sessions: Session[] } }
