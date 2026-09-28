import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import type { Queue, QueueItem } from '@/api/types'
import { applyQueueChanged, applyRunChanged, useQueuesStore } from './queues'
import { stubFetch } from '@/test-utils'

const it1: QueueItem = { id: 'i1', queueId: 'q1', position: 1, agent: 'claude', flags: '', instruction: '/goal m1', status: 'running', run: { id: 'r1', status: 'starting', sessionName: 'app-q1', startedAt: 't0' } }
const it2: QueueItem = { id: 'i2', queueId: 'q1', position: 2, agent: 'codex', flags: '', instruction: '/goal m2', status: 'queued' }
const q1: Queue = { id: 'q1', projectId: 'p', name: 'Q', status: 'running', projectName: 'app', projectPath: '/home/dev/app', items: [it1, it2] }

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

describe('queue event reducers', () => {
  it('replaces, adds and removes queues on queue.changed', () => {
    expect(applyQueueChanged([], { action: 'created', queueId: 'q1', queue: q1 })).toEqual([q1])
    const paused = { ...q1, status: 'paused' as const }
    expect(applyQueueChanged([q1], { action: 'paused', queueId: 'q1', queue: paused })).toEqual([paused])
    expect(applyQueueChanged([q1], { action: 'deleted', queueId: 'q1' })).toEqual([])
  })

  it('updates the item run on run.changed, and a retry replaces the summary', () => {
    const [next] = applyRunChanged([q1], { runId: 'r1', itemId: 'i1', queueId: 'q1', status: 'running' })
    expect(next.items[0].run).toEqual({ id: 'r1', status: 'running', sessionName: 'app-q1', startedAt: 't0', detail: undefined })
    const [failed] = applyRunChanged([next], { runId: 'r1', itemId: 'i1', queueId: 'q1', status: 'failed', detail: 'no' })
    expect(failed.items[0].run?.detail).toBe('no')
    const [retried] = applyRunChanged([failed], { runId: 'r2', itemId: 'i1', queueId: 'q1', status: 'starting' })
    expect(retried.items[0].run).toMatchObject({ id: 'r2', status: 'starting', sessionName: '' })
    expect(applyRunChanged([q1], { runId: 'x', itemId: 'i9', queueId: 'q9', status: 'running' })).toEqual([q1])
  })

  it('applies a live supervisor flag and clears it on a later null flag', () => {
    const flag = { label: 'waiting_input' as const, reason: 'waiting for input', at: '2026-09-28T12:00:00Z' }
    const [flagged] = applyRunChanged([q1], { runId: 'r1', itemId: 'i1', queueId: 'q1', status: 'running', flag })
    expect(flagged.items[0].run?.flag).toEqual(flag)
    const [cleared] = applyRunChanged([flagged], { runId: 'r1', itemId: 'i1', queueId: 'q1', status: 'running', flag: null })
    expect(cleared.items[0].run?.flag).toBeNull()
  })
})

describe('queues store', () => {
  it('refetches once per snapshot (every WebSocket connect) and never polls', async () => {
    vi.useFakeTimers()
    const calls = stubFetch(() => ({ status: 200, body: { queues: [q1] } }))
    const store = useQueuesStore()
    store.apply({ type: 'snapshot', machines: [], sessions: {} })
    store.apply({ type: 'snapshot', machines: [], sessions: {} }) // coalesced while loading
    await flushPromises()
    expect(calls.filter((c) => c.path === '/api/queues')).toHaveLength(1)
    expect(store.queues).toEqual([q1])
    await vi.advanceTimersByTimeAsync(10 * 60_000)
    expect(calls).toHaveLength(1)
    // A reconnect sends a new snapshot: one more fetch.
    store.apply({ type: 'snapshot', machines: [], sessions: {} })
    await flushPromises()
    expect(calls).toHaveLength(2)
    vi.useRealTimers()
  })

  it('applies events without fetching', async () => {
    const calls = stubFetch(() => ({ status: 500 }))
    const store = useQueuesStore()
    store.apply({ type: 'queue.changed', machine: 'host', payload: { action: 'created', queueId: 'q1', queue: q1 } })
    store.apply({ type: 'run.changed', machine: 'host', payload: { runId: 'r1', itemId: 'i1', queueId: 'q1', status: 'achieved' } })
    store.apply({ type: 'queue.changed', machine: 'host', payload: { action: 'item_done', queueId: 'q1', queue: { ...q1, items: [{ ...it1, status: 'done' }, it2] } } })
    expect(store.queues[0].items[0].status).toBe('done')
    expect(calls).toHaveLength(0)
  })

  it('follows the parallel-queues switch from queue.changed', () => {
    const store = useQueuesStore()
    store.apply({ type: 'queue.changed', machine: 'host', payload: { action: 'parallel_changed', queueId: 'q1', queue: q1, parallelQueues: true } })
    expect(store.parallelQueues).toBe(true)
    store.apply({ type: 'queue.changed', machine: 'host', payload: { action: 'created', queueId: 'q1', queue: q1 } })
    expect(store.parallelQueues).toBe(true)
    store.apply({ type: 'queue.changed', machine: 'host', payload: { action: 'parallel_changed', queueId: 'q1', queue: q1, parallelQueues: false } })
    expect(store.parallelQueues).toBe(false)
  })

  it('keeps a load error for the panel', async () => {
    stubFetch(() => ({ status: 503, body: { error: 'database unavailable', hint: 'Try again.' } }))
    const store = useQueuesStore()
    await store.load()
    expect(store.loadError).toBe('Database unavailable.')
    expect(store.loaded).toBe(false)
  })
})
