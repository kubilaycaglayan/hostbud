import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QueuePanel from './QueuePanel.vue'
import type { Queue } from '@/api/types'
import { useQueuesStore } from '@/stores/queues'
import { useProjectsStore } from '@/stores/projects'
import { stubFetch } from '@/test-utils'

const queue = (items: Queue['items'], status: Queue['status'] = 'paused'): Queue => ({ id: 'q1', projectId: 'p1', name: 'Milestones', status, projectName: 'app', projectPath: '/home/dev/app', items })
const attention: Queue['items'][number] = {
  id: 'i1', queueId: 'q1', position: 1, agent: 'claude', flags: '', instruction: '/goal m1', status: 'needs_attention',
  run: { id: 'r1', status: 'stale', sessionName: 'app-q1', startedAt: '', detail: 'no signal from the agent for 2h0m0s' },
}
const queued: Queue['items'][number] = { id: 'i2', queueId: 'q1', position: 2, agent: 'codex', flags: '--yolo', instruction: '/goal m2', status: 'queued' }

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const $$ = (sel: string) => [...document.body.querySelectorAll<HTMLElement>(sel)]
const button = (label: string) => $$('button').find((b) => b.getAttribute('aria-label') === label || b.textContent?.trim() === label)

async function mountPanel(q: Queue | null, compact = false) {
  const store = useQueuesStore()
  store.loaded = true
  store.queues = q ? [q] : []
  useProjectsStore().items = [{ id: 'p1', machineId: 'host', path: '/home/dev/app', name: 'app', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }]
  const w = mount(QueuePanel, { props: { open: true, compact }, attachTo: document.body })
  await flushPromises()
  return w
}

describe('QueuePanel', () => {
  it('shows each item with its status, reason and session, and only the allowed buttons', async () => {
    await mountPanel(queue([attention, queued]))
    const text = document.body.textContent ?? ''
    expect(text).toContain('Needs attention · no signal (stale)')
    expect(text).toContain('no signal from the agent for 2h0m0s')
    expect(text).toContain('app-q1')
    for (const label of ['Retry item 1', 'Skip item 1', 'Mark item 1 done', 'Open session of item 1', 'Edit item 2', 'Delete item 2', 'Move item 2 up']) {
      expect(button(label), label).toBeTruthy()
    }
    for (const label of ['Edit item 1', 'Delete item 1', 'Move item 1 up', 'Retry item 2', 'Skip item 2']) expect(button(label), label).toBeFalsy()
    expect(button('Resume')).toBeTruthy()
    expect(button('Pause')).toBeFalsy()
    for (const input of $$('input, select')) expect(input.getAttribute('autocomplete')).toBe('off')
  })

  it('asks before Mark done and Skip; Retry needs no confirmation', async () => {
    const calls = stubFetch((method, path) => ({ status: 200, body: path.endsWith('mark-done') || path.endsWith('retry') ? queue([{ ...attention, status: 'done' }, queued]) : undefined }))
    await mountPanel(queue([attention, queued]))
    button('Mark item 1 done')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain("This overrides the agent's own /goal verdict")
    const confirm = $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Mark done')
    expect(confirm).toBeTruthy()
    confirm!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/queue-items/i1/mark-done'])
    expect(useQueuesStore().queues[0].items[0].status).toBe('done')
  })

  it('asks before Skip', async () => {
    const calls = stubFetch(() => ({ status: 200, body: queue([{ ...attention, status: 'skipped' }, queued]) }))
    await mountPanel(queue([attention, queued]))
    button('Skip item 1')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Skip item 1?')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Skip')!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/queue-items/i1/skip'])
  })

  it('retries without asking', async () => {
    const calls = stubFetch(() => ({ status: 200, body: queue([{ ...attention, status: 'queued' }, queued]) }))
    await mountPanel(queue([attention, queued]))
    button('Retry item 1')!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/queue-items/i1/retry'])
    expect($$('[role="alertdialog"]')).toHaveLength(0)
  })

  it('validates the instruction before sending, prefilled with "/goal "', async () => {
    const calls = stubFetch(() => ({ status: 201, body: {} }))
    await mountPanel(queue([], 'idle'))
    const instruction = $$('form[aria-label="Add item"] input').find((i) => (i as HTMLInputElement).value === '/goal ') as HTMLInputElement
    expect(instruction).toBeTruthy()
    button('Add item')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Start with /goal followed by the condition')
    instruction.value = '/goal ship M2'
    instruction.dispatchEvent(new Event('input'))
    button('Add item')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'POST', path: '/api/queues/q1/items', body: { agent: 'claude', flags: '', instruction: '/goal ship M2' } }])
  })

  it('shows the server error with its hint (one-queue limit, 409s)', async () => {
    stubFetch((method, path) => path === '/api/queues' && method === 'GET'
      ? { status: 200, body: { queues: [] } }
      : { status: 409, body: { error: 'V2-M1 supports one queue; several queues arrive with V2-M2', hint: 'Add more items to the existing queue.' } })
    await mountPanel(null)
    button('Create queue')!.click()
    await flushPromises()
    const alert = $$('[role="alert"]').map((a) => a.textContent).join(' ')
    expect(alert).toContain('V2-M1 supports one queue')
    expect(alert).toContain('Add more items to the existing queue.')
  })

  it('reorders queued items with the move buttons and Alt+Arrow keys', async () => {
    const third = { ...queued, id: 'i3', position: 3, instruction: '/goal m3' }
    const calls = stubFetch(() => ({ status: 200, body: queue([attention, third, queued]) }))
    await mountPanel(queue([attention, queued, third]))
    button('Move item 3 up')!.click()
    await flushPromises()
    expect(calls[0]).toEqual({ method: 'PUT', path: '/api/queues/q1/order', body: { itemIds: ['i3', 'i2'] } })
    const row = $$('[data-queue-item="i3"]')[0]
    row.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', altKey: true, bubbles: true }))
    await flushPromises()
    expect(calls[1].body).toEqual({ itemIds: ['i2', 'i3'] })
  })

  it('opens a run session and closes the panel', async () => {
    const w = await mountPanel(queue([attention, queued]), true)
    button('Open session of item 1')!.click()
    await flushPromises()
    expect(w.emitted('openSession')).toEqual([['app-q1']])
    expect(w.emitted('update:open')).toEqual([[false]])
  })

  it('is a full-screen sheet on the phone, without drag handles', async () => {
    await mountPanel(queue([attention, queued]), true)
    const dialog = $$('[role="dialog"]')[0]
    expect(dialog.className).toContain('inset-0')
    expect($$('.queue-drag-handle')).toHaveLength(0)
    expect(button('Move item 2 up')!.className).toContain('min-h-11')
  })
})
