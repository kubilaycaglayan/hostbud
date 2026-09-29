import { describe, expect, it } from 'vitest'
import type { QueueItem } from '@/api/types'
import { capacityError, capacityValue, completedSessions, flagsError, instructionError, loopRuntimeError, loopRuntimeText, itemActions, moveQueued, queueControls, queueRunning, statusLabel, verifyCommandError, verifyLine } from './queue'

const item = (id: string, status: QueueItem['status'], run?: Partial<QueueItem['run']>): QueueItem => ({
  id, queueId: 'q', position: 1, agent: 'claude', flags: '', instruction: '/goal x', status,
  run: run ? { id: 'r', status: 'running', sessionName: 'app-q1', startedAt: '', ...run } as QueueItem['run'] : undefined,
})

describe('queue form rules', () => {
  it('requires a non-empty one-line instruction', () => {
    expect(instructionError('/goal work on M2')).toBe('')
    expect(instructionError('work on M2')).toBe('')
    for (const bad of ['', '   ', 'work on M2\nnext']) expect(instructionError(bad)).not.toBe('')
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
    expect(itemActions(item('a', 'queued'))).toEqual({ edit: true, remove: true, move: true, retry: false, skip: false, markDone: false, openSession: false, editGates: false, approve: false, reject: false, reverify: false, killSession: false })
    for (const s of ['running', 'done', 'skipped'] as const) {
      expect(itemActions(item('a', s, {}))).toMatchObject({ edit: false, remove: false, move: false, retry: false, skip: false, markDone: false, openSession: true })
    }
    expect(itemActions(item('a', 'needs_attention', { status: 'stale' }))).toMatchObject({ edit: false, retry: true, skip: true, markDone: true, openSession: true })
  })

  it('offers Kill session only for done items with a run session', () => {
    expect(itemActions(item('a', 'done', { sessionName: 'app-q1' })).killSession).toBe(true)
    for (const s of ['running', 'verifying', 'awaiting_approval', 'needs_attention', 'skipped'] as const) expect(itemActions(item('a', s, {})).killSession, s).toBe(false)
    expect(itemActions(item('a', 'done')).killSession).toBe(false)
  })

  it('lists the open sessions of done items once each for Kill completed sessions', () => {
    const items = [
      item('a', 'done', { sessionName: 'app-q1' }),
      item('b', 'done', { sessionName: 'app-q2' }),
      item('c', 'running', { sessionName: 'app-q3' }),
      item('d', 'done', { sessionName: 'app-q1' }),
      item('e', 'done', { sessionName: 'gone' }),
    ]
    expect(completedSessions(items, new Set(['app-q1', 'app-q2', 'app-q3']))).toEqual(['app-q1', 'app-q2'])
    expect(completedSessions(items, new Set())).toEqual([])
  })

  it('offers Start, Pause and Resume by queue state', () => {
    expect(queueRunning({ status: 'running', items: [] })).toBe(true)
    expect(queueRunning({ status: 'paused', items: [item('a', 'verifying')] })).toBe(true)
    expect(queueRunning({ status: 'paused', items: [item('a', 'done'), item('b', 'queued')] })).toBe(false)
    expect(queueRunning({ status: 'idle', items: [] })).toBe(false)
    expect(queueControls('idle', true)).toEqual({ start: true, pause: false, resume: false })
    expect(queueControls('idle', false)).toEqual({ start: false, pause: false, resume: false })
    expect(queueControls('finished', true)).toEqual({ start: true, pause: false, resume: false })
    expect(queueControls('running', true)).toEqual({ start: false, pause: true, resume: false })
    expect(queueControls('paused', true)).toEqual({ start: false, pause: false, resume: true })
    // A finished looping queue starts its items over; an idle one still needs a queued item.
    expect(queueControls('finished', false, true)).toEqual({ start: true, pause: false, resume: false })
    expect(queueControls('finished', false, false)).toEqual({ start: false, pause: false, resume: false })
    expect(queueControls('idle', false, true)).toEqual({ start: false, pause: false, resume: false })
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

describe('V2-M2 queue rules', () => {
  it('labels a queued item waiting for a run slot', () => {
    const item = { id: 'i', queueId: 'q', position: 1, agent: 'claude' as const, flags: '', instruction: '/goal x', status: 'queued' as const }
    expect(statusLabel(item)).toBe('Queued')
    expect(statusLabel({ ...item, waitingForSlot: true })).toBe('Queued · waiting for a free slot')
    expect(statusLabel({ ...item, status: 'running', waitingForSlot: true, run: { id: 'r', status: 'running', sessionName: 's', startedAt: '' } })).toBe('Running · running')
  })

  it('validates the cap: whole numbers 1–32, empty = no cap', () => {
    for (const ok of ['', '  ', '1', '32', ' 7 ']) expect(capacityError(ok), ok).toBe('')
    for (const bad of ['0', '33', '1.5', '-2', 'x', '1e1']) expect(capacityError(bad), bad).not.toBe('')
    expect(capacityValue('')).toBeNull()
    expect(capacityValue(' 4 ')).toBe(4)
  })
})

describe('completion gates (V2-M4)', () => {
  it('checks verify commands like flags, one line, at most 4096 bytes', () => {
    expect(verifyCommandError('')).toBe('')
    expect(verifyCommandError(`sh -c 'make test && make lint'`)).toBe('')
    expect(verifyCommandError(`make 'test`)).toBe('The verify command has an unbalanced single quote.')
    expect(verifyCommandError('make\ntest')).toBe('The verify command must be one line.')
    expect(verifyCommandError('x'.repeat(4097))).toBe('The verify command must be at most 4096 bytes.')
  })

  it('offers the gate actions per state', () => {
    const gated = (status: QueueItem['status'], runStatus?: string) => ({ ...item('a', status, runStatus ? { status: runStatus as never } : undefined), verifyCommand: 'make test' })
    expect(itemActions(gated('awaiting_approval', 'achieved'))).toMatchObject({ approve: true, reject: true, retry: false, markDone: false, edit: false, editGates: false })
    expect(itemActions(gated('needs_attention', 'achieved'))).toMatchObject({ reverify: true, editGates: true, retry: true, approve: false, edit: false })
    expect(itemActions(gated('needs_attention', 'failed')).reverify).toBe(false)
    expect(itemActions({ ...gated('needs_attention', 'achieved'), verifyCommand: '' }).reverify).toBe(false)
    for (const s of ['verifying', 'running'] as const) {
      expect(itemActions(gated(s, 'achieved'))).toMatchObject({ edit: false, editGates: false, approve: false, reverify: false, retry: false, skip: false, markDone: false })
    }
    expect(statusLabel(gated('verifying', 'achieved'))).toBe('Verifying · goal achieved')
    expect(statusLabel(gated('awaiting_approval', 'achieved'))).toBe('Awaiting approval · goal achieved')
  })

  it('summarizes a verify attempt', () => {
    expect(verifyLine({ attempt: 1, running: true })).toBe('Verify attempt 1: running…')
    expect(verifyLine({ attempt: 2, running: false, outcome: 'failed', exitCode: 1, durationMs: 3210 })).toBe('Verify attempt 2: failed · exit 1 · 3.2 s')
    expect(verifyLine({ attempt: 1, running: false, outcome: 'timeout', durationMs: 450 })).toBe('Verify attempt 1: timed out · 450 ms')
  })
})

describe('loop runtime limit', () => {
  it('formats seconds as a Go duration', () => {
    expect(loopRuntimeText(18000)).toBe('5h')
    expect(loopRuntimeText(5400)).toBe('1h30m')
    expect(loopRuntimeText(3661)).toBe('1h1m1s')
    expect(loopRuntimeText(45)).toBe('45s')
  })
  it('accepts 1s to 30 days and rejects anything else', () => {
    for (const ok of ['5h', '1h30m', '90m', '1s', '720h', ' 2h ']) expect(loopRuntimeError(ok), ok).toBeNull()
    for (const bad of ['', '5', 'forever', '0s', '721h', '-1h', '5d']) expect(loopRuntimeError(bad), bad).not.toBeNull()
  })
})
