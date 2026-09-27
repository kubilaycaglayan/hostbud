import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SessionList from './SessionList.vue'
import type { Session } from '@/api/types'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'

const s = (name: string, attached = 0, windows = 1): Session => ({
  id: '$1', name, path: '/home/dev', attached, windows, created: '', activity: '',
})

describe('SessionList', () => {
  afterEach(() => vi.useRealTimers())
  it('shows an empty state, not an error', () => {
    const w = mount(SessionList, { props: { sessions: [] } })
    expect(w.text()).toContain('No tmux sessions yet.')
    expect(w.find('[role=alert]').exists()).toBe(false)
  })

  it('lists names and attached state without rendering window counts', () => {
    const w = mount(SessionList, { props: { sessions: [s('acc-a', 1, 3), s('acc-b', 0, 1), s('acc-c', 0, 0)] } })
    const items = w.findAll('li')
    expect(items).toHaveLength(3)
    expect(items[0].get('button').attributes('aria-label')).toBe('acc-a')
    expect(items[0].get('[role=img]').attributes('aria-label')).toBe('attached')
    expect(items[1].get('[role=img]').attributes('aria-label')).toBe('detached')
    expect(items.map((item) => item.text()).join(' ')).not.toMatch(/\b\d+ windows?\b/)
  })

  it('uses a drag handle for reorder and keeps the session row free of move arrows', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')], sortable: true } })
    expect(w.find('button[aria-label="Move session b up"]').exists()).toBe(false)
    expect(w.find('button[aria-label="Move session b down"]').exists()).toBe(false)
    expect(w.get('button[aria-label="Drag to reorder session b"]').attributes('title')).toBe('Drag to reorder sessions')
    expect(w.get('button[aria-label="Drag to reorder session b"]').classes()).toContain('touch-target')
    expect(w.get('button[aria-label="b"]').classes()).toContain('touch-target')
    expect(w.get('button[aria-label="More actions for b"]').classes()).toContain('touch-target')
    // Compact rows (M8 T2): no vertical row padding; touch-target keeps 44 px on phones.
    expect(w.get('li').classes().filter((c) => /^py-/.test(c))).toEqual([])
    expect(w.get('button[aria-label="b"]').classes()).toContain('min-h-7')
  })

  it('leads with the session name, then its status and the actions menu (Kill and Rename live inside it); the drag handle comes last (M8 T2)', () => {
    const w = mount(SessionList, { props: { sessions: [s('a')], sortable: true } })
    const row = w.get('li')
    const first = row.element.firstElementChild as HTMLElement
    expect(first.getAttribute('aria-label')).toBe('a')
    expect(first.hasAttribute('data-session-row')).toBe(true)
    expect(first.className).toContain('font-medium')
    expect(first.className).toContain('text-fg')
    expect(first.className).toContain('flex-1')
    expect(first.nextElementSibling?.getAttribute('role')).toBe('img')
    const actions = first.nextElementSibling?.nextElementSibling
    expect(actions?.tagName).toBe('SPAN')
    expect([...actions!.querySelectorAll('button')].map((button) => button.getAttribute('aria-label'))).toEqual([
      'More actions for a',
    ])
    expect(w.find('button[aria-label="Kill a"]').exists()).toBe(false)
    expect(w.find('button[aria-label="Rename a"]').exists()).toBe(false)
    expect(row.element.lastElementChild?.getAttribute('aria-label')).toBe('Drag to reorder session a')
  })

  it('shows compact agent logos before left-gutter session names without changing the accessible row label', () => {
    const session = { ...s('agent-work'), agents: ['codex', 'claude'] as ('codex' | 'claude')[] }
    setActivePinia(createPinia())
    const w = mount(SessionList, { props: { sessions: [session], treeView: true } })
    const row = w.get('li')
    const marks = row.findAll('[data-agent-mark]')
    expect(marks.map((mark) => mark.attributes('data-agent'))).toEqual(['codex', 'claude'])
    expect(marks.map((mark) => mark.attributes('title'))).toEqual(['Codex running', 'Claude Code running'])
    expect(row.attributes('aria-label')).toBe('agent-work, Codex running, Claude Code running')
    expect(row.get('[data-session-row]').attributes('aria-label')).toBe('agent-work')
    expect(row.element.firstElementChild?.getAttribute('data-agent')).toBe('codex')
  })

  it('keeps agent logos out of non-gutter session lists', () => {
    const w = mount(SessionList, { props: { sessions: [{ ...s('agent-work'), agents: ['codex'] }] } })
    expect(w.find('[data-agent-mark]').exists()).toBe(false)
  })

  it('shows agent logos before hook status and the unchanged name on collapsed gutter rows', () => {
    setActivePinia(createPinia())
    for (const [status, emoji, label] of [
      ['working', '🟢', 'Working'],
      ['blocked', '🚧', 'Blocked or waiting'],
      ['ended', '🎯', 'Ended'],
    ] as const) {
      const w = mount(SessionList, { props: { sessions: [{ ...s('status-session'), agents: ['codex'], status }], treeView: true } })
      const row = w.get('li')
      expect(row.get('[data-session-status]').text()).toBe(emoji)
      expect(row.get('[data-session-status]').attributes('aria-label')).toBe(label)
      expect(row.attributes('aria-label')).toBe(`status-session, ${label}, Codex running`)
      expect(row.get('[data-session-row]').attributes('aria-label')).toBe('status-session')
      expect(row.element.firstElementChild?.getAttribute('data-agent')).toBe('codex')
      expect(row.element.children[1]?.hasAttribute('data-session-status')).toBe(true)
      w.unmount()
    }
  })

  it('keeps status marks out of non-gutter session lists', () => {
    const w = mount(SessionList, { props: { sessions: [{ ...s('status-session'), status: 'working' }] } })
    expect(w.find('[data-session-status]').exists()).toBe(false)
  })

  it('emits select and marks the selected session', async () => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')], selected: 'b' } })
    expect(w.get('button[aria-label="b"]').attributes('aria-current')).toBe('true')
    expect(w.get('button[aria-label="a"]').attributes('aria-current')).toBeUndefined()
    await w.get('button[aria-label="a"]').trigger('click')
    expect(w.emitted('select')).toEqual([['a']])
  })

  it('opens the existing row menu after a long press and cancels when the finger moves', async () => {
    vi.useFakeTimers()
    const w = mount(SessionList, { props: { sessions: [s('a')] }, attachTo: document.body })
    const row = w.get('button[aria-label="a"]')
    const pointer = (type: string, x: number, y: number) => {
      const event = new Event(type, { bubbles: true })
      Object.defineProperties(event, { pointerType: { value: 'touch' }, clientX: { value: x }, clientY: { value: y } })
      return event
    }
    row.element.dispatchEvent(pointer('pointerdown', 20, 20))
    await vi.advanceTimersByTimeAsync(500)
    await w.vm.$nextTick()
    expect(document.body.querySelector('[role="menu"]')).not.toBeNull()
    row.element.dispatchEvent(pointer('pointerup', 20, 20))
    await row.trigger('click')
    expect(w.emitted('select')).toBeUndefined()
    w.unmount()

    const short = mount(SessionList, { props: { sessions: [s('b')] } })
    const shortRow = short.get('button[aria-label="b"]')
    shortRow.element.dispatchEvent(pointer('pointerdown', 20, 20))
    shortRow.element.dispatchEvent(pointer('pointermove', 31, 20))
    await vi.advanceTimersByTimeAsync(600)
    shortRow.element.dispatchEvent(pointer('pointerup', 31, 20))
    await shortRow.trigger('click')
    expect(short.emitted('select')).toEqual([['b']])
  })

  it('offers rename and kill per session in the actions menu', async () => {
    for (const treeView of [false, true]) {
      setActivePinia(createPinia())
      const w = mount(SessionList, { props: { sessions: [s('a')], treeView }, attachTo: document.body })
      for (const label of ['Rename', 'Kill…']) {
        await w.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
        await new Promise((resolve) => setTimeout(resolve))
        const item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === label)
        expect(item).toBeTruthy()
        item!.click()
        await new Promise((resolve) => setTimeout(resolve))
      }
      expect(w.emitted('rename')).toEqual([['a']])
      expect(w.emitted('kill')).toEqual([['a']])
      w.unmount()
    }
  })

  it('offers Rename in the long-press tree menu', async () => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    const w = mount(SessionList, { props: { sessions: [s('a')], treeView: true }, attachTo: document.body })
    const row = w.get('button[aria-label="a"]')
    const down = new Event('pointerdown', { bubbles: true })
    Object.defineProperties(down, { pointerType: { value: 'touch' }, clientX: { value: 10 }, clientY: { value: 10 } })
    row.element.dispatchEvent(down)
    await vi.advanceTimersByTimeAsync(500)
    await w.vm.$nextTick()
    const rename = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    expect(rename).toBeTruthy()
    rename!.click()
    expect(w.emitted('rename')).toEqual([['a']])
    w.unmount()
  })

  it('offers Hide or Unhide in the session actions menu', async () => {
    setActivePinia(createPinia())
    const tree = useTreeStore()
    const w = mount(SessionList, { props: { sessions: [s('a')], treeView: true }, attachTo: document.body })
    await w.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve))
    let item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Hide')
    expect(item).toBeTruthy()
    expect(item!.classList.contains('touch-target')).toBe(true)
    item!.click()
    expect(w.emitted('hide')).toEqual([['a', false]])
    w.unmount()
    tree.hideSession('host', 'a')
    const hidden = mount(SessionList, { props: { sessions: [s('a')], treeView: true }, attachTo: document.body })
    await hidden.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve))
    item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Unhide')
    expect(item).toBeTruthy()
    hidden.unmount()
  })

  it('offers Unhide from the long-press menu for a hidden session', async () => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    useTreeStore().hideSession('host', 'a')
    const w = mount(SessionList, { props: { sessions: [s('a')], treeView: true }, attachTo: document.body })
    const row = w.get('button[aria-label="a"]')
    const down = new Event('pointerdown', { bubbles: true })
    Object.defineProperties(down, { pointerType: { value: 'touch' }, clientX: { value: 10 }, clientY: { value: 10 } })
    row.element.dispatchEvent(down)
    await vi.advanceTimersByTimeAsync(500)
    await w.vm.$nextTick()
    const unhide = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Unhide')
    expect(unhide).toBeTruthy()
    expect(unhide!.classList.contains('touch-target')).toBe(true)
    unhide!.click()
    expect(w.emitted('hide')).toEqual([['a', true]])
    w.unmount()
  })

  it.each([
    ['Open in split right', 'row'],
    ['Open in split down', 'column'],
  ])('the row menu: %s', async (label, dir) => {
    const w = mount(SessionList, { props: { sessions: [s('a'), s('b')] }, attachTo: document.body })
    await w.get('button[aria-label="More actions for b"]').trigger('keydown', { key: 'Enter' })
    await new Promise((r) => setTimeout(r))
    const item = [...document.body.querySelectorAll<HTMLElement>('[role=menuitem]')].find((x) => x.textContent?.trim() === label)
    item!.click()
    expect(w.emitted('split')).toEqual([['b', dir]])
    w.unmount()
  })

  it('shows a session chevron only where the row expands (M8 T2)', async () => {
    setActivePinia(createPinia())
    const w = mount(SessionList, { props: { sessions: [s('single'), s('multi', 0, 2)], treeView: true }, attachTo: document.body })
    const single = w.get('[data-tree-key="session:single"]')
    const multi = w.get('[data-tree-key="session:multi"]')
    // One window, panes unknown: nothing to expand, no inert chevron.
    expect(single.find('button[aria-label="Expand single"]').exists()).toBe(false)
    expect(single.attributes('aria-expanded')).toBeUndefined()
    expect(single.find('svg').exists()).toBe(false)
    // Several windows: the chevron stays, operable and named.
    expect(multi.attributes('aria-expanded')).toBe('false')
    const chevron = multi.get('button[aria-label="Expand multi"]')
    expect(chevron.classes()).toEqual(expect.arrayContaining(['touch-target', 'min-h-7', 'min-w-6']))
    expect(chevron.attributes('title')).toBe('Expand multi')

    // A single window already loaded with split panes does expand.
    useWindowsStore().bySession['host/single'] = {
      status: 'ok', truncated: false, windows: [
        { id: '@1', index: 0, name: 'shell', active: true, panes: [
          { id: '%1', index: 0, active: true, command: 'bash', width: 40, height: 24 },
          { id: '%2', index: 1, active: false, command: 'vim', width: 40, height: 24 },
        ] },
      ],
    }
    await w.vm.$nextTick()
    expect(w.get('[data-tree-key="session:single"]').find('button[aria-label="Expand single"]').exists()).toBe(true)
    // …but a loaded single window with one pane doesn't.
    useWindowsStore().bySession['host/single'].windows[0].panes.pop()
    await w.vm.$nextTick()
    expect(w.get('[data-tree-key="session:single"]').find('button[aria-label="Expand single"]').exists()).toBe(false)
    w.unmount()
  })

  it('an expanded state kept from before hides the group once the row no longer expands (M8 T2)', async () => {
    setActivePinia(createPinia())
    useTreeStore().order.expanded = ['host/single']
    const w = mount(SessionList, { props: { sessions: [s('single')], treeView: true }, attachTo: document.body })
    expect(w.find('[data-tree-key="session:single"] [role="group"]').exists()).toBe(false)
    expect(w.get('[data-tree-key="session:single"]').attributes('aria-expanded')).toBeUndefined()
    w.unmount()
  })
})
