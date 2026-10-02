import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { SelectRoot } from 'reka-ui'
import { nextTick } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import QueuePanel from './QueuePanel.vue'
import DurationPicker from './DurationPicker.vue'
import type { Queue } from '@/api/types'
import { useQueuesStore } from '@/stores/queues'
import { useSessionsStore } from '@/stores/sessions'
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
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const $$ = (sel: string) => [...document.body.querySelectorAll<HTMLElement>(sel)]
let panel: VueWrapper
const pickerSelects = (label: string) => panel.findAllComponents(DurationPicker).find((d) => d.props('label') === label)!.findAllComponents(SelectRoot)
/** Picks hours and minutes in the duration picker labelled `label`. */
async function pick(label: string, hours: number, minutes: number) {
  const [h, m] = pickerSelects(label)
  h.vm.$emit('update:modelValue', hours)
  await nextTick()
  m.vm.$emit('update:modelValue', minutes)
  await nextTick()
}
const picked = (label: string) => pickerSelects(label).map((s) => String((s.props() as { modelValue: unknown }).modelValue))
const button = (label: string) => $$('button').find((b) => b.getAttribute('aria-label') === label || b.textContent?.trim() === label)

async function mountPanel(q: Queue | Queue[] | null, compact = false, parallel = false) {
  const store = useQueuesStore()
  store.loaded = true
  store.queues = q === null ? [] : Array.isArray(q) ? q : [q]
  store.parallelQueues = parallel
  useProjectsStore().items = [{ id: 'p1', machineId: 'host', path: '/home/dev/app', name: 'app', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }]
  const w = mount(QueuePanel, { props: { open: true, compact }, attachTo: document.body })
  panel = w
  await flushPromises()
  return w
}

describe('QueuePanel', () => {
  it('uses fallback dragging for queue items', async () => {
    const wrapper = await mountPanel(queue([queued]))
    const list = wrapper.findComponent(VueDraggable)
    expect(list.props('forceFallback')).toBe(true)
    expect(list.props('fallbackOnBody')).toBe(true)
    expect(list.props('fallbackTolerance')).toBe(4)
  })

  it('shows each item with its status, reason and session, and only the allowed buttons', async () => {
    await mountPanel(queue([attention, queued]))
    const text = document.body.textContent ?? ''
    expect(text).toContain('Needs attention · no signal (stale)')
    expect(text).toContain('no signal from the agent for 2h0m0s')
    expect(text).toContain('app-q1')
    for (const label of ['Retry item 1', 'Skip item 1', 'Mark item 1 done', 'Open session of item 1', 'Edit item 2', 'Delete item 2']) {
      expect(button(label), label).toBeTruthy()
    }
    for (const label of ['Edit item 1', 'Delete item 1', 'Move item 1 up', 'Retry item 2', 'Skip item 2', 'Move item 2 up', 'Move item 2 down']) expect(button(label), label).toBeFalsy()
    expect(button('Resume')).toBeTruthy()
    expect(button('Pause')).toBeFalsy()
    for (const input of $$('input, select')) expect(input.getAttribute('autocomplete')).toBe('off')
  })

  it('shows queue and item lifecycle timestamps', async () => {
    const startedAt = '2026-09-29T10:00:00Z'
    const endedAt = '2026-09-29T10:05:00Z'
    await mountPanel({ ...queue([{ ...queued, startedAt, endedAt }], 'finished'), startedAt, endedAt })
    const times = $$('time').map((el) => el.getAttribute('datetime'))
    expect(times).toEqual([startedAt, endedAt, startedAt, endedAt])
    expect(document.body.textContent).toContain('Finished')
    expect(document.body.textContent).toContain('Ended')
  })

  it('loads and renders durable queue history as text', async () => {
    const historyEntry = {
      id: 42, machineId: 'host', queueId: 'deleted-q', queueName: 'Old queue', projectName: 'app', itemId: 'deleted-i',
      position: 1, executionMode: 'agent' as const, agent: 'claude' as const, flags: '--safe', instruction: '/goal recover release', command: '',
      status: 'needs_attention' as const, action: 'status' as const, detail: '<script>failed</script>', agentSessionId: 'c0ffee00-1111-2222-3333-444455556666', occurredAt: '2026-09-29T10:00:00Z',
    }
    const calls = stubFetch((_method, path) => path === '/api/queue-history?limit=100&offset=0' ? { status: 200, body: { items: [historyEntry] } } : { status: 200, body: { queues: [], parallelQueues: false } })
    await mountPanel(null)
    button('History')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('Old queue · app')
    expect(document.body.textContent).not.toContain('/goal recover release')
    const toggle = document.body.querySelector<HTMLButtonElement>('[data-testid="queue-history"] button[aria-expanded]')
    expect(toggle?.getAttribute('aria-expanded')).toBe('false')
    toggle?.click()
    await flushPromises()
    expect(toggle?.getAttribute('aria-expanded')).toBe('true')
    expect(document.body.textContent).toContain('Item 1 — Status: needs attention')
    expect(document.body.textContent).toContain('/goal recover release')
    expect(document.body.textContent).toContain('<script>failed</script>')
    expect(document.body.textContent).toContain('Claude session: c0ffee00-1111-2222-3333-444455556666')
    expect(document.body.querySelector('[data-testid="queue-history"] script')).toBeNull()
    expect(calls.map((call) => call.path)).toContain('/api/queue-history?limit=100&offset=0')
  })

  it('shows supervisor flags as text and explains completed as advisory', async () => {
    const flagged = {
      ...attention,
      run: { ...attention.run!, flag: { label: 'completed' as const, reason: '<script>mark done</script>', at: '2026-09-28T12:00:00Z' } },
    }
    await mountPanel(queue([flagged]))
    const banner = document.querySelector('[data-testid="llm-flag"]')
    expect(banner?.textContent).toContain('Looks finished — check and Mark done')
    expect(banner?.textContent).toContain('<script>mark done</script>')
    expect(banner?.querySelector('script')).toBeNull()
  })

  it('does not show running or unknown supervisor labels as attention badges', async () => {
    const running = { ...attention, run: { ...attention.run!, flag: { label: 'running' as const, reason: 'working', at: '2026-09-28T12:00:00Z' } } }
    const unknown = { ...queued, run: { id: 'r2', status: 'stale' as const, sessionName: 'app-q1', startedAt: '', flag: { label: 'unknown' as const, reason: 'unavailable', at: '2026-09-28T12:00:00Z' } } }
    await mountPanel(queue([running, unknown]))
    expect(document.querySelector('[data-testid="llm-flag"]')).toBeNull()
  })

  it('asks before Mark done and Skip; Retry needs no confirmation', async () => {
    const calls = stubFetch((method, path) => ({ status: 200, body: path.endsWith('mark-done') || path.endsWith('retry') ? queue([{ ...attention, status: 'done' }, queued]) : undefined }))
    await mountPanel(queue([attention, queued]))
    button('Mark item 1 done')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain("Confirm that the agent's work is complete")
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

  it('validates the instruction before sending, with an empty prompt field', async () => {
    const calls = stubFetch(() => ({ status: 201, body: {} }))
    await mountPanel(queue([], 'idle'))
    const instruction = $$('form[aria-label="Add item"] textarea')[0] as HTMLTextAreaElement
    expect(instruction.value).toBe('')
    expect(instruction.className).toContain('resize-y')
    expect(instruction).toBeTruthy()
    button('Add item')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Enter an instruction for the agent')
    instruction.value = '/goal ship M2'
    instruction.dispatchEvent(new Event('input'))
    button('Add item')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'POST', path: '/api/queues/q1/items', body: { agent: 'claude', flags: '--dangerously-skip-permissions', instruction: '/goal ship M2', executionMode: 'agent' } }])
  })

  it('starts the new item with the opted-in default prompt, caret before it, and follows a changed setting', async () => {
    const calls = stubFetch(() => ({ status: 201, body: {} }))
    useQueuesStore().defaultPrompt = { enabled: true, text: ', commit regularly.' }
    await mountPanel(queue([], 'idle'))
    const instruction = () => $$('[data-testid="new-item-instruction"]')[0] as HTMLTextAreaElement
    expect(instruction().value).toBe(', commit regularly.')
    vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => { fn(0); return 0 })
    instruction().setSelectionRange(19, 19)
    instruction().dispatchEvent(new FocusEvent('focus'))
    expect(instruction().selectionStart).toBe(0)
    // The prefill alone is not an instruction.
    button('Add item')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Add your instruction to the default prompt.')
    instruction().value = 'fix the login bug, commit regularly.'
    instruction().dispatchEvent(new Event('input'))
    button('Add item')!.click()
    await flushPromises()
    expect(calls[0].body).toMatchObject({ instruction: 'fix the login bug, commit regularly.' })
    // The next item starts with the prompt again; untouched, it follows the setting.
    expect(instruction().value).toBe(', commit regularly.')
    useQueuesStore().defaultPrompt = { enabled: false, text: ', commit regularly.' }
    await nextTick()
    expect(instruction().value).toBe('')
  })

  it('keeps a typed instruction when the default prompt setting changes', async () => {
    useQueuesStore().defaultPrompt = { enabled: true, text: ', commit regularly.' }
    await mountPanel(queue([], 'idle'))
    const instruction = $$('[data-testid="new-item-instruction"]')[0] as HTMLTextAreaElement
    instruction.value = 'ship it, commit regularly.'
    instruction.dispatchEvent(new Event('input'))
    useQueuesStore().defaultPrompt = { enabled: true, text: ' and push.' }
    await nextTick()
    expect(instruction.value).toBe('ship it, commit regularly.')
  })

  it('defaults permission modes by agent, exposes a quick toggle, and preserves custom flags', async () => {
    const calls = stubFetch(() => ({ status: 201, body: {} }))
    await mountPanel(queue([], 'idle'))
    const form = $$('form[aria-label="Add item"]')[0]
    const agent = form.querySelectorAll('select')[1] as HTMLSelectElement
    const flags = form.querySelector('input[spellcheck="false"]') as HTMLInputElement
    const mode = form.querySelector('input[type="checkbox"]') as HTMLInputElement
    expect(flags.value).toBe('--dangerously-skip-permissions')
    expect(mode.checked).toBe(true)

    agent.value = 'codex'
    agent.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(flags.value).toBe('--yolo')
    expect(form.textContent).toContain('YOLO mode')

    flags.value = "--model 'opus 4' --yolo"
    flags.dispatchEvent(new Event('input'))
    await flushPromises()
    mode.checked = false
    mode.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(flags.value).toBe("--model 'opus 4'")
    const instruction = form.querySelector('textarea') as HTMLTextAreaElement
    instruction.value = '/goal verify flags'
    instruction.dispatchEvent(new Event('input'))
    button('Add item')!.click()
    await flushPromises()
    expect(calls[0].body).toEqual({ agent: 'codex', flags: "--model 'opus 4'", instruction: '/goal verify flags', executionMode: 'agent' })
  })

  it('offers the matching permission-mode toggle while editing an item', async () => {
    await mountPanel(queue([{ ...queued, flags: "--model 'opus 4'" }]))
    button('Edit item 2')!.click()
    await flushPromises()
    const form = $$('form[aria-label="Edit item 2"]')[0]
    const flags = form.querySelector('input[spellcheck="false"]') as HTMLInputElement
    const mode = form.querySelector('input[type="checkbox"]') as HTMLInputElement
    expect(form.textContent).toContain('YOLO mode')
    expect(mode.checked).toBe(false)
    mode.checked = true
    mode.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(flags.value).toBe("--model 'opus 4' --yolo")
  })

  it('shows the server error with its hint (409s)', async () => {
    stubFetch((method, path) => path === '/api/queues' && method === 'GET'
      ? { status: 200, body: { queues: [] } }
      : { status: 409, body: { error: 'a queue named "Milestones" already exists in this project', hint: 'Pick another name; queue names are unique per project (case doesn\'t matter).' } })
    await mountPanel(null)
    button('Create queue')!.click()
    await flushPromises()
    const alert = $$('[role="alert"]').map((a) => a.textContent).join(' ')
    expect(alert).toContain('already exists in this project')
    expect(alert).toContain('Pick another name')
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

  it('shows a move button only where the queued item can move that way', async () => {
    const third = { ...queued, id: 'i3', position: 3, instruction: '/goal m3' }
    await mountPanel(queue([attention, queued, third]))
    for (const label of ['Move item 2 down', 'Move item 3 up']) expect(button(label), label).toBeTruthy()
    for (const label of ['Move item 2 up', 'Move item 3 down']) expect(button(label), label).toBeFalsy()
    expect((button('Move item 2 down') as HTMLButtonElement).disabled).toBe(false)
  })

  it('opens a run session and closes the panel', async () => {
    const w = await mountPanel(queue([attention, queued]), true)
    button('Open session of item 1')!.click()
    await flushPromises()
    expect(w.emitted('openSession')).toEqual([['app-q1']])
    expect(w.emitted('update:open')).toEqual([[false]])
  })

  it('kills a done item\'s open session after a confirmation', async () => {
    const done = { ...attention, status: 'done' as const, run: { ...attention.run!, status: 'achieved' as const } }
    const calls = stubFetch(() => ({ status: 200, body: { killed: ['app-q1'], failed: [] } }))
    useSessionsStore().$patch({ byMachine: { host: [{ id: '$1', name: 'app-q1', path: '/home/dev/app', attached: 0, windows: 1, created: '', activity: '' }] } })
    const w = await mountPanel(queue([done, queued]))
    expect(button('Kill session of item 1')!.querySelector('svg')).toBeTruthy()
    expect(button('Kill session of item 2')).toBeFalsy()
    button('Kill session of item 1')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Kill session app-q1?')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Kill session')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'POST', path: '/api/machines/host/sessions/kill', body: { names: ['app-q1'] } }])
    expect(w.emitted('killed')).toEqual([['app-q1']])
  })

  it('hides Kill session once the done item\'s session is gone', async () => {
    await mountPanel(queue([{ ...attention, status: 'done' }]))
    expect(button('Kill session of item 1')).toBeFalsy()
    expect(button('Kill completed sessions')).toBeFalsy()
  })

  it('kills every open completed session of the shown queue, continuing past a failure', async () => {
    const done = (id: string, position: number, sessionName: string) => ({ ...attention, id, position, status: 'done' as const, run: { ...attention.run!, id: `r${id}`, status: 'achieved' as const, sessionName } })
    const calls = stubFetch(() => ({ status: 200, body: { killed: ['app-q2'], failed: [{ name: 'app-q1', error: 'tmux failed' }] } }))
    const live = (name: string) => ({ id: name, name, path: '/home/dev/app', attached: 0, windows: 1, created: '', activity: '' })
    useSessionsStore().$patch({ byMachine: { host: [live('app-q1'), live('app-q2'), live('app-q3')] } })
    const w = await mountPanel(queue([done('a', 1, 'app-q1'), done('b', 2, 'app-q2'), { ...attention, id: 'c', position: 3, status: 'running', run: { ...attention.run!, sessionName: 'app-q3' } }]))
    button('Kill completed sessions')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain('Kill 2 completed sessions?')
    expect(document.body.textContent).toContain('app-q1, app-q2')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Kill sessions')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'POST', path: '/api/machines/host/sessions/kill', body: { names: ['app-q1', 'app-q2'] } }])
    expect(w.emitted('killed')).toEqual([['app-q2']])
    expect(document.body.textContent).toContain("Couldn't kill session app-q1")
  })

  it('uses icon buttons for the queue controls and item actions', async () => {
    await mountPanel(queue([attention, queued]))
    for (const label of ['History', 'Close queue panel', 'New queue', 'Resume', 'Delete queue', 'Add item', 'Retry item 1', 'Skip item 1', 'Mark item 1 done', 'Open session of item 1']) {
      const b = button(label)!
      expect(b, label).toBeTruthy()
      expect(b.querySelector('svg'), label).toBeTruthy()
      expect(b.textContent?.trim(), label).toBe('')
    }
  })

  it('uses compact actions and resizable instruction areas on desktop', async () => {
    await mountPanel(queue([{ ...queued, id: 'i0', position: 1 }, queued]))
    const up = button('Move item 2 up')!
    expect(up.className).toContain('min-h-8')
    expect(up.className).toContain('min-w-8')
    expect(button('Edit item 2')!.querySelector('svg')).toBeTruthy()
    expect(button('Delete item 2')!.querySelector('svg')).toBeTruthy()
    const form = $$('form[aria-label="Add item"]')[0]
    expect(form.querySelector('div.flex.justify-end')).toBeTruthy()
    expect(form.querySelector('textarea')?.className).toContain('resize-y')
    button('Edit item 2')!.click()
    await flushPromises()
    const edit = $$('form[aria-label="Edit item 2"]')[0]
    expect(edit.querySelector('textarea')?.className).toContain('resize-y')
  })

  it('is a full-screen sheet on the phone, without drag handles and with phone-sized actions', async () => {
    await mountPanel(queue([attention, { ...queued, id: 'i1b', position: 1 }, queued]), true)
    const dialog = $$('[role="dialog"]')[0]
    expect(dialog.className).toContain('inset-0')
    expect($$('.queue-drag-handle')).toHaveLength(0)
    expect(button('Move item 2 up')!.className).toContain('touch-target')
    expect(button('Move item 2 up')!.className).toContain('min-h-8')
  })

  // ---- V2-M2 ----

  const second = (over: Partial<Queue> = {}): Queue => ({
    ...queue([{ ...queued, id: 'j1', queueId: 'q2', position: 1, instruction: '/goal docs' }], 'running'),
    id: 'q2', name: 'Docs', ...over,
  })

  it('gives the running queue\'s tab a green border', async () => {
    await mountPanel([queue([attention, queued]), second()], false, true)
    expect(button('Show queue Docs')!.dataset.running).toBe('true')
    expect(button('Show queue Docs')!.className).toContain('queue-tab-running')
    expect(button('Show queue Milestones')!.dataset.running).toBeUndefined()
    expect(button('Show queue Milestones')!.className).not.toContain('queue-tab-running')
  })

  it('switches between queues: buttons on desktop, a select on the phone', async () => {
    await mountPanel([queue([attention, queued]), second()], false, true)
    expect($$('nav[aria-label="Queues"] button').map((b) => b.getAttribute('aria-label'))).toEqual(['Show queue Milestones', 'Show queue Docs'])
    // The switcher isn't a list: the items are (e2e selects rows by listitem).
    expect($$('nav[aria-label="Queues"] li')).toHaveLength(0)
    expect(button('Show queue Milestones')!.getAttribute('aria-current')).toBe('true')
    // The selected tab is filled with the accent color so it stands out; the others aren't.
    expect(button('Show queue Milestones')!.className).toContain('bg-accent')
    expect(button('Show queue Docs')!.className).not.toContain('bg-accent')
    expect(document.body.textContent).toContain('/goal m1')
    button('Show queue Docs')!.click()
    await flushPromises()
    expect(button('Show queue Docs')!.getAttribute('aria-current')).toBe('true')
    expect(button('Show queue Docs')!.className).toContain('queue-tab-selected')
    expect(button('Show queue Milestones')!.className).not.toContain('queue-tab-selected')
    expect(document.body.textContent).toContain('/goal docs')
    expect(document.body.textContent).not.toContain('/goal m1')
    document.body.innerHTML = ''
    setActivePinia(createPinia())
    await mountPanel([queue([attention, queued]), second()], true, true)
    expect($$('nav[aria-label="Queues"]')).toHaveLength(0)
    const select = $$('select').find((el) => el.closest('label')?.textContent?.trim().startsWith('Queue')) as HTMLSelectElement
    expect([...select.options].map((o) => o.value)).toEqual(['q1', 'q2'])
    select.value = 'q2'
    select.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(document.body.textContent).toContain('/goal docs')
  })

  it('creates another queue with the switch on and selects it', async () => {
    const created = second({ items: [] })
    const calls = stubFetch(() => ({ status: 201, body: created }))
    await mountPanel(queue([queued], 'idle'), false, true)
    const newButton = $$('button').find((b) => b.getAttribute('aria-label') === 'New queue') as HTMLButtonElement
    expect(newButton.disabled).toBe(false)
    newButton.click()
    await flushPromises()
    const name = $$('form[aria-label="Create queue"] input')[0] as HTMLInputElement
    name.value = 'Docs'
    name.dispatchEvent(new Event('input'))
    button('Create queue')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'POST', path: '/api/queues', body: { projectId: 'p1', name: 'Docs', afterRunId: '', afterSession: '' } }])
    expect(button('Show queue Docs')!.getAttribute('aria-current')).toBe('true')
    expect($$('form[aria-label="Create queue"]')).toHaveLength(0)
  })

  it('can opt into waiting for a tracked active goal', async () => {
    const active = { ...queued, status: 'running' as const, run: { id: 'run-active', status: 'running' as const, sessionName: 'app-q2', startedAt: '' } }
    const plainClaude = { ...active, id: 'plain', agent: 'claude' as const, instruction: 'ordinary Claude prompt', run: { ...active.run, id: 'run-plain', sessionName: 'app-q3' } }
    const calls = stubFetch(() => ({ status: 201, body: second({ items: [] }) }))
    await mountPanel(queue([active, plainClaude], 'running'), false, true)
    button('New queue')!.click()
    await flushPromises()
    const selector = $$('form[aria-label="Create queue"] select').at(-1) as HTMLSelectElement
    expect(selector.options[0].textContent).toContain('Start normally')
    expect(selector.options[1].textContent).toContain('app-q2')
    expect([...selector.options].some((option) => option.textContent?.includes('app-q3'))).toBe(false)
    selector.value = 'run:run-active'
    selector.dispatchEvent(new Event('change'))
    const name = $$('form[aria-label="Create queue"] input')[0] as HTMLInputElement
    name.value = 'Following'; name.dispatchEvent(new Event('input'))
    button('Create queue')!.click()
    await flushPromises()
    expect(calls[0].body).toMatchObject({ afterRunId: 'run-active', afterSession: '' })
  })

  it('can attach a new queue to any existing session, not only queue runs', async () => {
    const sessions = useSessionsStore()
    sessions.byMachine = { host: [{ id: '$1', name: 'manual-work', path: '/home/dev/app', agents: ['claude'], status: 'working', attached: 0, windows: 1, created: '', activity: '' }] }
    const calls = stubFetch(() => ({ status: 201, body: second({ items: [], afterSession: 'manual-work' }) }))
    await mountPanel(queue([queued], 'idle'), false, true)
    button('New queue')!.click()
    await flushPromises()
    const selector = $$('form[aria-label="Create queue"] select').at(-1) as HTMLSelectElement
    const option = [...selector.options].find((o) => o.value === 'session:manual-work')
    expect(option?.textContent).toContain('manual-work')
    expect(option?.parentElement?.getAttribute('label')).toBe('Existing session is idle')
    selector.value = 'session:manual-work'
    selector.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(document.body.textContent).toContain("waits until that session's agent finishes its turn")
    button('Create queue')!.click()
    await flushPromises()
    expect(calls[0].body).toMatchObject({ afterRunId: '', afterSession: 'manual-work' })
    expect($$('[data-testid="queue-dependency"]')[0].textContent).toContain('Waits for session manual-work to be idle')
  })

  it('sets and clears Start after on an existing queue', async () => {
    const sessions = useSessionsStore()
    sessions.byMachine = { host: [{ id: '$1', name: 'manual-work', path: '/home/dev/app', agents: ['claude'], status: 'working', attached: 0, windows: 1, created: '', activity: '' }] }
    const calls = stubFetch((_method, path, body) => ({ status: 200, body: { ...queue([queued], 'running'), ...((body as { afterSession?: string })?.afterSession ? { afterSession: 'manual-work' } : {}) } }))
    await mountPanel(queue([queued], 'running'), false, true)
    const form = $$('form[aria-label="Start after"]')[0]
    const selector = form.querySelector('select') as HTMLSelectElement
    expect(selector.value).toBe('')
    expect(form.querySelector('button[aria-label="Save Start after"]')).toBeNull()
    selector.value = 'session:manual-work'
    selector.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(form.textContent).toContain('The next item to start waits')
    ;(form.querySelector('button[aria-label="Save Start after"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(calls.at(-1)).toEqual({ method: 'PUT', path: '/api/queues/q1/link', body: { afterRunId: '', afterSession: 'manual-work' } })
    expect($$('[data-testid="queue-dependency"]')[0].textContent).toContain('Waits for session manual-work to be idle before its next item')
    selector.value = ''
    selector.dispatchEvent(new Event('change'))
    await flushPromises()
    ;(form.querySelector('button[aria-label="Save Start after"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(calls.at(-1)).toEqual({ method: 'PUT', path: '/api/queues/q1/link', body: { afterRunId: '', afterSession: '' } })
  })

  it('with the switch off, shows what exists and explains the switch', async () => {
    await mountPanel([queue([queued], 'idle'), second({ status: 'idle' })], false, false)
    expect($$('nav[aria-label="Queues"] button')).toHaveLength(2)
    const newButton = $$('button').find((b) => b.getAttribute('aria-label') === 'New queue') as HTMLButtonElement
    expect(newButton.disabled).toBe(false)
    expect($$('[data-testid="parallel-off"]')[0].textContent).toContain('One queue runs at a time')
    expect($$('[data-testid="parallel-off"]')[0].textContent).toContain('still create queues')
    expect(($$('[data-testid="parallel-toggle"]')[0] as HTMLInputElement).checked).toBe(false)
  })

  it('with the switch off, creates another queue but blocks running it while another queue runs', async () => {
    const created = second({ items: [queued], status: 'idle' })
    const calls = stubFetch(() => ({ status: 201, body: created }))
    await mountPanel(queue([{ ...queued, status: 'running' }], 'running'), false, false)
    button('New queue')!.click()
    await flushPromises()
    const name = $$('form[aria-label="Create queue"] input')[0] as HTMLInputElement
    name.value = 'Docs'
    name.dispatchEvent(new Event('input'))
    button('Create queue')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'POST', path: '/api/queues', body: { projectId: 'p1', name: 'Docs', afterRunId: '', afterSession: '' } }])
    expect(button('Show queue Docs')!.getAttribute('aria-current')).toBe('true')
    const start = $$('button').find((b) => b.getAttribute('aria-label') === 'Start') as HTMLButtonElement
    expect(start.disabled).toBe(true)
    expect(start.title).toContain('Run queues in parallel')
    expect($$('[data-testid="queue-run-blocked"]')[0].textContent).toContain("Can't run yet")
    // Turning the switch on lifts the block.
    useQueuesStore().parallelQueues = true
    await flushPromises()
    expect(start.disabled).toBe(false)
    expect($$('[data-testid="queue-run-blocked"]')).toHaveLength(0)
  })

  it('turns parallel queues on and off from the panel after an in-app confirmation', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm')
    const calls = stubFetch((_m, _p, body) => ({ status: 200, body }))
    await mountPanel([queue([queued], 'idle')], false, false);
    const toggle = () => $$('[data-testid="parallel-toggle"]')[0] as HTMLInputElement
    const dialogButton = (label: string) => $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === label)!
    toggle().click()
    await flushPromises()
    expect(calls).toEqual([])
    expect(toggle().checked).toBe(false)
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain('Run queues in parallel?')
    dialogButton('Turn on').click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'PUT', path: '/api/machines/host/parallel-queues', body: { parallelQueues: true } }])
    expect(useQueuesStore().parallelQueues).toBe(true)
    expect(toggle().checked).toBe(true)
    expect($$('[data-testid="parallel-off"]')).toHaveLength(0)
    expect(($$('button').find((b) => b.getAttribute('aria-label') === 'New queue') as HTMLButtonElement).disabled).toBe(false)
    toggle().click()
    await flushPromises()
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain('Turn off parallel queues?')
    dialogButton('Turn off').click()
    await flushPromises()
    expect(calls[1]).toEqual({ method: 'PUT', path: '/api/machines/host/parallel-queues', body: { parallelQueues: false } })
    expect(useQueuesStore().parallelQueues).toBe(false)
    expect(confirmSpy).not.toHaveBeenCalled()
  })

  it('leaves the parallel switch unchanged when confirmation is cancelled', async () => {
    const calls = stubFetch((_m, _p, body) => ({ status: 200, body }))
    await mountPanel([queue([queued], 'idle')], false, false);
    ;($$('[data-testid="parallel-toggle"]')[0] as HTMLInputElement).click()
    await flushPromises()
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Cancel')!.click()
    await flushPromises()
    expect(calls).toEqual([])
    expect(useQueuesStore().parallelQueues).toBe(false)
    expect(($$('[data-testid="parallel-toggle"]')[0] as HTMLInputElement).checked).toBe(false)
  })

  it('deletes an item only after confirmation', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    await mountPanel(queue([attention, queued]))
    button('Delete item 2')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain('Delete item 2?')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Cancel')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    button('Delete item 2')!.click()
    await flushPromises()
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Delete item')!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([`DELETE /api/queue-items/${queued.id}`])
  })

  it('renames the queue', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { ...queue([queued]), name: 'Release' } }))
    await mountPanel(queue([queued]), false, true)
    button('Rename queue')!.click()
    await flushPromises()
    const input = $$('form[aria-label="Rename queue"] input')[0] as HTMLInputElement
    expect(input.value).toBe('Milestones')
    input.value = 'Release'
    input.dispatchEvent(new Event('input'))
    $$('form[aria-label="Rename queue"] button').find((b) => b.getAttribute('aria-label') === 'Save')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'PATCH', path: '/api/queues/q1', body: { name: 'Release' } }])
    expect(document.body.textContent).toContain('Release')
    expect($$('form[aria-label="Rename queue"]')).toHaveLength(0)
  })

  it('turns looping on with a runtime limit and shows the pass and when looping stops', async () => {
    const startedAt = '2026-09-29T10:00:00Z'
    const looping = { ...queue([queued], 'running'), loop: { enabled: true, maxRuntimeSeconds: 7200, startedAt, passStartedAt: startedAt, pass: 1 } }
    const calls = stubFetch(() => ({ status: 200, body: looping }))
    await mountPanel(queue([queued]))
    const box = $$('input[aria-label="Loop the queue"]')[0] as HTMLInputElement
    expect(box.checked).toBe(false)
    expect(picked('Loop runtime limit')).toEqual(['5', '0'])
    // Only hours and minutes can be picked; zero is the one invalid choice.
    expect($$('[aria-label="Loop runtime limit"] input')).toHaveLength(0)
    await pick('Loop runtime limit', 0, 0)
    await flushPromises()
    box.click()
    await flushPromises()
    expect(calls).toEqual([])
    expect(box.checked).toBe(false)
    expect(document.body.textContent).toContain('Pick a limit of at least 1 minute.')
    await pick('Loop runtime limit', 2, 0)
    await flushPromises()
    box.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'PUT', path: '/api/queues/q1/loop', body: { enabled: true, maxRuntime: '2h' } }])
    expect(box.checked).toBe(true)
    const status = $$('[data-testid="queue-loop-status"]')[0]
    expect(status.textContent).toContain('Pass 1')
    expect(status.querySelector('time')?.getAttribute('datetime')).toBe('2026-09-29T12:00:00.000Z')
  })

  it('changes the loop limit with Save limit and offers Start on a finished looping queue', async () => {
    const done = { ...queued, status: 'done' as const }
    const finished = { ...queue([done], 'finished'), loop: { enabled: true, maxRuntimeSeconds: 18000, pass: 3 } }
    const calls = stubFetch(() => ({ status: 200, body: { ...finished, loop: { ...finished.loop, maxRuntimeSeconds: 5400 } } }))
    await mountPanel(finished)
    expect(button('Start')?.hasAttribute('disabled')).toBe(false)
    expect(button('Save limit')).toBeFalsy()
    expect(picked('Loop runtime limit')).toEqual(['5', '0'])
    await pick('Loop runtime limit', 1, 30)
    await flushPromises()
    button('Save limit')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'PUT', path: '/api/queues/q1/loop', body: { enabled: true, maxRuntime: '1h30m' } }])
    expect(picked('Loop runtime limit')).toEqual(['1', '30'])
  })

  it('schedules Start with the delay picked as hours and minutes', async () => {
    const calls = stubFetch(() => ({ status: 200, body: queue([queued], 'idle') }))
    await mountPanel(queue([queued], 'idle'))
    expect(picked('Start delay')).toEqual(['0', '0'])
    expect($$('[aria-label="Start delay"] input')).toHaveLength(0)
    button('Start')!.click()
    await flushPromises()
    await pick('Start delay', 4, 14)
    await flushPromises()
    button('Start')!.click()
    await flushPromises()
    expect(calls).toEqual([
      { method: 'POST', path: '/api/queues/q1/start', body: undefined },
      { method: 'POST', path: '/api/queues/q1/start', body: { delay: '4h14m' } },
    ])
  })

  it('keeps Start disabled on a finished queue that does not loop', async () => {
    await mountPanel(queue([{ ...queued, status: 'done' }], 'finished'))
    expect(button('Start')?.hasAttribute('disabled')).toBe(true)
  })

  it('deletes the selected queue only after confirmation', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    await mountPanel([queue([queued], 'idle'), second({ status: 'idle' })], false, true)
    button('Show queue Docs')!.click()
    await flushPromises()
    button('Delete queue')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Delete queue Docs?')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Delete queue')!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['DELETE /api/queues/q2'])
    expect(useQueuesStore().queues.map((q) => q.id)).toEqual(['q1'])
    expect(button('Show queue Milestones')!.getAttribute('aria-current')).toBe('true')
  })

  it('shows the shared-directory warning and "waiting for a free slot"', async () => {
    const warned = queue([{ ...queued, position: 1, waitingForSlot: true }], 'running')
    warned.warnings = [{ code: 'shared_directory', message: 'Queue Docs also runs in /home/dev/app — the agents may edit the same files', queues: [{ id: 'q2', name: 'Docs', projectName: 'app' }] }]
    await mountPanel([warned, second()], false, true)
    const warning = $$('[data-testid="queue-warning"]')
    expect(warning).toHaveLength(1)
    expect(warning[0].getAttribute('role')).toBe('status')
    expect(warning[0].textContent).toContain('Queue Docs also runs in /home/dev/app')
    expect($$('[data-testid="item-status"]')[0].textContent).toBe('Queued · waiting for a free slot')
    // The switcher marks the warned queue.
    expect(button('Show queue Milestones')!.querySelector('svg')).toBeTruthy()
    expect(button('Show queue Docs')!.querySelector('svg')).toBeFalsy()
  })

  it('updates the waiting state live from queue.changed', async () => {
    const waiting = queue([{ ...queued, position: 1, waitingForSlot: true }], 'running')
    await mountPanel(waiting, false, true)
    expect($$('[data-testid="item-status"]')[0].textContent).toBe('Queued · waiting for a free slot')
    useQueuesStore().apply({ type: 'queue.changed', machine: 'host', payload: { action: 'run_started', queueId: 'q1', queue: queue([{ ...queued, position: 1, status: 'running', run: { id: 'r9', status: 'starting', sessionName: 'app-q1', startedAt: '' } }], 'running') } })
    await flushPromises()
    expect($$('[data-testid="item-status"]')[0].textContent).toBe('Running · starting')
  })

  it('opens the item a notification points at: its queue selected, the item highlighted (V2-M3)', async () => {
    const other: Queue = { ...queue([queued]), id: 'q2', name: 'Second' }
    await mountPanel([queue([attention]), other], false, true)
    useQueuesStore().focusItem('q2', 'i2')
    await flushPromises()
    const row = $$('[data-queue-item="i2"]')[0]
    expect(row?.dataset.highlighted).toBe('true')
    expect($$('[data-queue-item="i1"]')).toHaveLength(0)
  })
})

describe('QueuePanel completion gates (V2-M4)', () => {
  const awaiting: Queue['items'][number] = {
    id: 'i3', queueId: 'q1', position: 1, agent: 'claude', flags: '', instruction: '/goal m1', status: 'awaiting_approval',
    verifyCommand: 'make test', requiresApproval: true,
    verify: { attempt: 1, running: false, outcome: 'passed', exitCode: 0, durationMs: 1200, truncated: true, output: '<b>bold</b> ok\n' },
    run: { id: 'r3', status: 'achieved', sessionName: 'app-q1', startedAt: '' },
  }
  const failedVerify: Queue['items'][number] = {
    ...awaiting, id: 'i4', status: 'needs_attention', requiresApproval: false,
    verify: { attempt: 2, running: false, outcome: 'failed', exitCode: 1, durationMs: 900, output: 'FAIL' },
    run: { id: 'r4', status: 'achieved', sessionName: 'app-q1', startedAt: '', detail: 'verify failed (exit 1)' },
  }

  it('shows the gates, the attempt and the output as text, with Approve and Reject', async () => {
    await mountPanel(queue([awaiting], 'running'))
    const text = document.body.textContent ?? ''
    expect(text).toContain('Awaiting approval · goal achieved')
    expect(text).toContain('verify make test · requires approval')
    expect(text).toContain('Verify attempt 1: passed · exit 0 · 1.2 s')
    expect(text).toContain('truncated: the last 16 KiB')
    const out = $$('[data-testid="verify-output"]')[0]
    expect(out.textContent).toBe('<b>bold</b> ok\n')
    expect(out.querySelector('b')).toBeNull()
    for (const label of ['Approve item 1', 'Reject item 1']) expect(button(label), label).toBeTruthy()
    for (const label of ['Retry item 1', 'Skip item 1', 'Mark item 1 done', 'Edit item 1', 'Re-run verify of item 1', 'Edit gates of item 1']) expect(button(label), label).toBeFalsy()
  })

  it('approves at once and asks before Reject', async () => {
    const calls = stubFetch((_m, path) => ({ status: 200, body: queue([{ ...awaiting, status: path.endsWith('approve') ? 'done' : 'needs_attention' }], 'running') }))
    await mountPanel(queue([awaiting], 'running'))
    button('Reject item 1')!.click()
    await flushPromises()
    expect(calls).toHaveLength(0)
    expect(document.body.textContent).toContain('Reject item 1?')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Reject')!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/queue-items/i3/reject'])
  })

  it('shows a 409 from a stale click and refetches', async () => {
    const calls = stubFetch((method) => method === 'POST'
      ? { status: 409, body: { error: 'this item is done; Approve is only for items awaiting approval (approved by a@example.com)', hint: 'Reload the queue and try again.' } }
      : { status: 200, body: { queues: [queue([{ ...awaiting, status: 'done' }], 'running')], parallelQueues: false } })
    await mountPanel(queue([awaiting], 'running'))
    button('Approve item 1')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain('(approved by a@example.com)')
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/queue-items/i3/approve', 'GET /api/queues'])
    expect(button('Approve item 1')).toBeFalsy()
  })

  it('re-runs verify and edits only the gates of a needs-attention item', async () => {
    const calls = stubFetch(() => ({ status: 200, body: queue([failedVerify]) }))
    await mountPanel(queue([failedVerify]))
    expect(document.body.textContent).toContain('verify failed (exit 1)')
    button('Re-run verify of item 1')!.click()
    await flushPromises()
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/queue-items/i4/reverify'])
    calls.length = 0
    button('Edit gates of item 1')!.click()
    await flushPromises()
    const form = $$('form[aria-label="Edit gates of item 1"]')[0]
    expect(form.querySelector('textarea')).toBeNull() // the instruction is locked
    expect(form.querySelector('select')).toBeNull()
    const verify = form.querySelector('[data-testid="verify-command"]') as HTMLInputElement
    expect(verify.value).toBe('make test')
    expect(verify.className).toContain('font-mono')
    verify.value = `make 'x`
    verify.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(document.body.textContent).toContain('The verify command has an unbalanced single quote.')
    verify.value = 'go test ./...'
    verify.dispatchEvent(new Event('input'))
    ;(form.querySelector('[data-testid="requires-approval"]') as HTMLInputElement).click()
    form.querySelector<HTMLButtonElement>('button[type="submit"]')!.click()
    await flushPromises()
    expect(calls).toEqual([{ method: 'PATCH', path: '/api/queue-items/i4', body: { verifyCommand: 'go test ./...', requiresApproval: true } }])
  })

  it('adds an item with gates only when set', async () => {
    const calls = stubFetch(() => ({ status: 201, body: {} }))
    await mountPanel(queue([], 'idle'))
    const form = $$('form[aria-label="Add item"]')[0]
    const instruction = form.querySelector('textarea') as HTMLTextAreaElement
    instruction.value = '/goal ship'
    instruction.dispatchEvent(new Event('input'))
    const verify = form.querySelector('[data-testid="verify-command"]') as HTMLInputElement
    verify.value = '  make test '
    verify.dispatchEvent(new Event('input'))
    ;(form.querySelector('[data-testid="requires-approval"]') as HTMLInputElement).click()
    button('Add item')!.click()
    await flushPromises()
    expect(calls[0].body).toEqual({ agent: 'claude', flags: '--dangerously-skip-permissions', instruction: '/goal ship', executionMode: 'agent', verifyCommand: 'make test', requiresApproval: true })
  })

  it('follows live updates: the buttons go when another device approves', async () => {
    await mountPanel(queue([awaiting], 'running'))
    expect(button('Approve item 1')).toBeTruthy()
    useQueuesStore().put(queue([{ ...awaiting, status: 'done' }], 'running'))
    await flushPromises()
    expect(button('Approve item 1')).toBeFalsy()
    expect(document.body.textContent).toContain('Done · goal achieved')
  })

  it('shows a running verify attempt', async () => {
    await mountPanel(queue([{ ...awaiting, status: 'verifying', verify: { attempt: 3, running: true } }], 'running'))
    expect(document.body.textContent).toContain('Verifying · goal achieved')
    expect(document.body.textContent).toContain('Verify attempt 3: running…')
    for (const label of ['Approve item 1', 'Retry item 1', 'Mark item 1 done', 'Edit gates of item 1']) expect(button(label), label).toBeFalsy()
  })
})
