import type { APIRequestContext } from '@playwright/test'
import { expect } from '@playwright/test'
import { MACHINE, mutate } from './api.ts'
import { shq, type Target, uniqueName } from './target.ts'

// V2-M1 queue API helpers (through Caddy's loopback site).

export interface RunSummary {
  id: string
  status: string
  sessionName: string
  detail?: string
  clientVersion?: string
  startedAt: string
  endedAt?: string
  flag?: { label: string; reason: string; at: string } | null
}

/** V2-M4: the latest verify attempt of an item's latest run. */
export interface VerifySummary {
  attempt: number
  running: boolean
  outcome?: 'passed' | 'failed' | 'timeout' | 'missing_directory' | 'timeout_missing' | 'ssh_failed' | 'interrupted'
  exitCode?: number
  durationMs?: number
  truncated?: boolean
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
  status: 'queued' | 'running' | 'verifying' | 'awaiting_approval' | 'done' | 'needs_attention' | 'skipped'
  /** V2-M4 completion gates ("" = no verify command). */
  verifyCommand: string
  requiresApproval: boolean
  verify?: VerifySummary
  run?: RunSummary
  /** V2-M2: the head item of a running queue while the machine's cap is reached. */
  waitingForSlot?: boolean
}

export interface QueueWarning {
  code: 'shared_directory'
  message: string
  queues: { id: string; name: string; projectName: string }[]
}

export interface Queue {
  id: string
  projectId: string
  name: string
  status: 'idle' | 'running' | 'paused' | 'finished'
  projectName: string
  projectPath: string
  items: QueueItem[]
  /** V2-M2: non-blocking notices (another busy queue in the same directory). */
  warnings?: QueueWarning[]
}

/** GET /api/queues. */
export async function listQueues(request: APIRequestContext): Promise<Queue[]> {
  const res = await request.get('/api/queues')
  expect(res.status(), await res.text()).toBe(200)
  return (await res.json() as { queues: Queue[] }).queues
}

/** A saved project in a fresh folder on the target. */
export async function newProject(request: APIRequestContext, target: Target, prefix = 'e2e-queue'): Promise<{ id: string; name: string; path: string }> {
  const name = uniqueName(prefix)
  const path = `/home/dev/${name}`
  await target.run(`mkdir -p ${shq(path)}`)
  const res = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name })
  expect(res.status(), await res.text()).toBe(201)
  return { id: (await res.json() as { id: string }).id, name, path }
}

export async function createQueue(request: APIRequestContext, projectId: string, name = 'Milestones'): Promise<Queue> {
  const res = await mutate(request, 'POST', '/api/queues', { projectId, name })
  expect(res.status(), await res.text()).toBe(201)
  return await res.json() as Queue
}

export async function addItem(request: APIRequestContext, queueId: string, item: { agent?: 'claude' | 'codex'; flags?: string; instruction: string; verifyCommand?: string; requiresApproval?: boolean }): Promise<QueueItem> {
  const gates = item.verifyCommand !== undefined || item.requiresApproval !== undefined
    ? { verifyCommand: item.verifyCommand ?? '', requiresApproval: item.requiresApproval ?? false }
    : {}
  const res = await mutate(request, 'POST', `/api/queues/${queueId}/items`, { agent: item.agent ?? 'claude', flags: item.flags ?? '', instruction: item.instruction, ...gates })
  expect(res.status(), await res.text()).toBe(201)
  return await res.json() as QueueItem
}

export async function getQueue(request: APIRequestContext, queueId: string): Promise<Queue> {
  const res = await request.get(`/api/queues/${queueId}`)
  expect(res.status(), await res.text()).toBe(200)
  return await res.json() as Queue
}

export async function control(request: APIRequestContext, queueId: string, action: 'start' | 'pause' | 'resume') {
  return await mutate(request, 'POST', `/api/queues/${queueId}/${action}`, undefined)
}

/** V2-M4 gate actions: Approve, Reject and Re-run verify. */
export async function gateAction(request: APIRequestContext, itemId: string, action: 'approve' | 'reject' | 'reverify') {
  return await mutate(request, 'POST', `/api/queue-items/${itemId}/${action}`, undefined)
}

export async function override(request: APIRequestContext, itemId: string, action: 'retry' | 'skip' | 'mark-done') {
  return await mutate(request, 'POST', `/api/queue-items/${itemId}/${action}`, undefined)
}

/** The item as the queue shows it now. */
export async function itemOf(request: APIRequestContext, queueId: string, itemId: string): Promise<QueueItem> {
  const item = (await getQueue(request, queueId)).items.find((i) => i.id === itemId)
  if (!item) throw new Error(`item ${itemId} not in queue ${queueId}`)
  return item
}
