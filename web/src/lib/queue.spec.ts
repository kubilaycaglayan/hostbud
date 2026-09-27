import { describe, expect, it } from 'vitest'
import type { QueueItem } from '@/api/types'
import { flagsError, instructionError, itemActions, moveQueued, queueControls, statusLabel } from './queue'

const item = (id: string, status: QueueItem['status'], run?: Partial<QueueItem['run']>): QueueItem => ({
  id, queueId: 'q', position: 1, agent: 'claude', flags: '', instruction: '/goal x', status,
  run: run ? { id: 'r', status: 'running', sessionName: 'app-q1', startedAt: '', ...run } as QueueItem['run'] : undefined,
})

describe('queue form rules', () => {
  it('requires "/goal " and a condition on one line', () => {
    expect(instructionError('/goal work on M2')).toBe('')
    for (const bad of ['', '/goal', '/goal ', '/goal   ', 'work on M2', '/goalx', '/goal a\nb']) expect(instructionError(bad)).not.toBe('')
  })

  it('checks flags split like the server', () => {
    for (const ok of ['', '--yolo', `--model 'opus 4'`, `-c "a \\"b\\""`, 'a\\ b']) expect(flagsError(ok)).toBe('')
    expect(flagsError(`--model 'x`)).toContain('single')
    expect(flagsError('-c "x')).toContain('double')
    expect(flagsError('trailing\\')).toContain('backslash')
    expect(flagsError('a\nb')).toContain('one line')
  })
})

describe('button availability per status', () => {
  it('lets only queued items be edited, deleted and moved, and only needs-attention items be overridden', () => {
    expect(itemActions(item('a', 'queued'))).toEqual({ edit: true, remove: true, move: true, retry: false, skip: false, markDone: false, openSession: false })
    for (const s of ['running', 'done', 'skipped'] as const) {
      expect(itemActions(item('a', s, {}))).toMatchObject({ edit: false, remove: false, move: false, retry: false, skip: false, markDone: false, openSession: true })
    }
    expect(itemActions(item('a', 'needs_attention', { status: 'stale' }))).toMatchObject({ edit: false, retry: true, skip: true, markDone: true, openSession: true })
  })

  it('offers Start, Pause and Resume by queue state', () => {
    expect(queueControls('idle', true)).toEqual({ start: true, pause: false, resume: false })
    expect(queueControls('idle', false)).toEqual({ start: false, pause: false, resume: false })
    expect(queueControls('finished', true)).toEqual({ start: true, pause: false, resume: false })
    expect(queueControls('running', true)).toEqual({ start: false, pause: true, resume: false })
    expect(queueControls('paused', true)).toEqual({ start: false, pause: false, resume: true })
  })

  it('labels an item with its run state', () => {
    expect(statusLabel(item('a', 'queued'))).toBe('Queued')
    expect(statusLabel(item('a', 'needs_attention', { status: 'stale' }))).toBe('Needs attention · no signal (stale)')
    expect(statusLabel(item('a', 'done', { status: 'achieved' }))).toBe('Done · goal achieved')
  })

  it('moves queued items among themselves only', () => {
    const items = [item('d', 'done'), item('a', 'queued'), item('b', 'queued'), item('c', 'queued')]
    expect(moveQueued(items, 'b', -1)).toEqual(['b', 'a', 'c'])
    expect(moveQueued(items, 'b', 1)).toEqual(['a', 'c', 'b'])
    expect(moveQueued(items, 'a', -1)).toBeNull()
    expect(moveQueued(items, 'c', 1)).toBeNull()
    expect(moveQueued(items, 'd', 1)).toBeNull()
  })
})
