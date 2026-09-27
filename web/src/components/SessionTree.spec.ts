import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { Project, Session } from '@/api/types'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useMachinesStore } from '@/stores/machines'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'
import { ApiError, windowsApi } from '@/api/client'
import SessionTree from './SessionTree.vue'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)
const project = (id: string, path: string): Project => ({ id, machineId: 'host', path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 3, created: '', activity: '' })

beforeEach(async () => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
  fetchMock.mockImplementation(async (url: string) => {
    const body = String(url) === '/api/projects'
      ? { id: 'saved', machineId: 'host', path: '/outside', name: 'outside' }
      : null
    return { ok: body !== null, status: body !== null ? 200 : 404, headers: new Headers(), text: async () => body ? JSON.stringify(body) : '' }
  })
  useProjectsStore().remember(project('a', '/work/a'))
  useProjectsStore().remember(project('b', '/work/b'))
  useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a'), session('two', '/work/a'), session('loose', '/outside')] } })
  const tree = useTreeStore()
  await tree.load()
  tree.sync()
})

afterEach(() => {
  useTreeStore().reset()
  vi.useRealTimers()
})

describe('SessionTree', () => {
  it('moves groups and session rows in the explicit order', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    expect(wrapper.find('button[aria-label="Move project b up"]').exists()).toBe(false)
    expect(wrapper.get('button[aria-label="Drag to reorder project b"]').attributes('title')).toBe('Drag to reorder projects')
    expect(wrapper.get('button[aria-label="Drag to reorder project b"]').classes()).toContain('touch-target')
    expect(wrapper.get('button[aria-label="New session in a"]').classes()).toContain('touch-target')
    useTreeStore().reorderProjects(['b', 'a'])
    expect(useTreeStore().groups.groups.map((g) => g.project.id)).toEqual(['b', 'a'])
    useTreeStore().reorderSessions('a', ['two', 'one'])
    expect(useTreeStore().groups.groups.find((g) => g.project.id === 'a')?.sessions.map((s) => s.name)).toEqual(['two', 'one'])
  })

  it('renders the accessible tree hierarchy and keeps one roving tab stop', async () => {
    useMachinesStore().apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: '', home: '/work', tmuxVersion: '', tmuxMissing: false }], sessions: { host: [] } })
    const wrapper = mount(SessionTree, { props: { selected: 'one' } })
    expect(wrapper.find('[role="tree"][aria-label="Projects and sessions"]').exists()).toBe(true)
    const projectRow = wrapper.get('[data-tree-key="project:a"]')
    const sessionRow = wrapper.get('[data-tree-key="session:one"]')
    expect(projectRow.attributes()).toMatchObject({ role: 'treeitem', 'aria-level': '1', 'aria-expanded': 'true', tabindex: '0', 'aria-label': 'a' })
    expect(projectRow.text()).toContain('~/a')
    expect(projectRow.find('[title="/work/a"]').exists()).toBe(true)
    expect(projectRow.find('svg.lucide-folder').exists()).toBe(true)
    expect(sessionRow.attributes()).toMatchObject({ role: 'treeitem', 'aria-level': '2', 'aria-selected': 'true', tabindex: '-1' })
    expect(sessionRow.find('button[data-session-row]').classes()).toContain('min-w-[8ch]')
    expect(wrapper.get('[data-tree-key="other"]').classes()).toContain('bg-tree-header')
    expect(wrapper.get('[data-tree-key="other"]').element.querySelector(':scope > div > span[title]')).toBeNull()
    expect(wrapper.find('[role="group"]').exists()).toBe(true)

    const layoutBefore = JSON.stringify((await import('@/stores/layout')).useLayoutStore().layout)
    await projectRow.trigger('keydown', { key: 'Enter' })
    expect(projectRow.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[data-tree-key="session:one"]').exists()).toBe(false)
    expect(JSON.stringify((await import('@/stores/layout')).useLayoutStore().layout)).toBe(layoutBefore)
    expect(useTreeStore().order.collapsed).toContain('a')
  })

  it('moves tree focus with arrows and reorders only within the focused section', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const projectA = wrapper.get('[data-tree-key="project:a"]')
    const projectAElement = projectA.element as HTMLElement
    projectAElement.focus()
    await projectA.trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(wrapper.get('[data-tree-key="session:one"]').element)
    await wrapper.get('[data-tree-key="session:one"]').trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(projectA.element)
    await projectA.trigger('keydown', { key: 'ArrowUp', altKey: true })
    expect(useTreeStore().groups.groups.map((g) => g.project.id)).toEqual(['a', 'b'])
    await projectA.trigger('keydown', { key: 'ArrowDown', altKey: true })
    expect(useTreeStore().groups.groups.map((g) => g.project.id)).toEqual(['b', 'a'])
    const first = wrapper.get('[data-tree-key="session:one"]')
    await first.trigger('keydown', { key: 'ArrowUp', altKey: true })
    expect(useTreeStore().groups.groups.find((g) => g.project.id === 'a')?.sessions.map((s) => s.name)).toEqual(['one', 'two'])
    await first.trigger('keydown', { key: 'ArrowDown', altKey: true })
    expect(useTreeStore().groups.groups.find((g) => g.project.id === 'a')?.sessions.map((s) => s.name)).toEqual(['two', 'one'])
    await wrapper.get('[data-tree-key="session:two"]').trigger('keydown', { key: 'ArrowUp', altKey: true })
    expect(useTreeStore().groups.groups.find((g) => g.project.id === 'a')?.sessions.map((s) => s.name)).toEqual(['two', 'one'])
    await wrapper.get('[data-tree-key="project:b"]').trigger('keydown', { key: 'ArrowUp', altKey: true })
    expect(useTreeStore().groups.groups.map((g) => g.project.id)).toEqual(['b', 'a'])
    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'ArrowDown', altKey: true })
    expect(useTreeStore().groups.groups.map((g) => g.project.id)).toEqual(['b', 'a'])
    wrapper.unmount()
  })

  it('supports Home, End, Enter and Delete from tree rows', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const loose = wrapper.get('[data-tree-key="session:loose"]')
    await loose.trigger('keydown', { key: 'Home' })
    expect(document.activeElement).toBe(wrapper.get('[data-tree-key="project:a"]').element)
    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(loose.element)
    await loose.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')).toEqual([['loose']])
    await loose.trigger('keydown', { key: 'Delete' })
    expect(wrapper.emitted('kill')).toEqual([['loose']])
    wrapper.unmount()
  })

  it('starts inline rename from F2, project pencil and the project menu, then restores tree focus on Escape', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const session = wrapper.get('[data-tree-key="session:one"]')
    await session.trigger('keydown', { key: 'F2' })
    expect(wrapper.find('input[aria-label="Rename one"]').exists()).toBe(true)
    await wrapper.get('input[aria-label="Rename one"]').trigger('keydown.esc')
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.get('[data-tree-key="session:one"]').element)

    await wrapper.get('button[aria-label="Rename a"]').trigger('click')
    expect(wrapper.find('input[aria-label="Rename a"]').exists()).toBe(true)
    await wrapper.get('input[aria-label="Rename a"]').trigger('keydown.esc')
    await wrapper.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const rename = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    expect(rename).toBeTruthy()
    rename!.click()
    await flushPromises()
    expect(wrapper.find('input[aria-label="Rename a"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps invalid and rejected session names in the inline field with an accessible error', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    await wrapper.get('[data-tree-key="session:one"]').trigger('keydown', { key: 'F2' })
    const input = wrapper.get('input[aria-label="Rename one"]')
    await input.setValue('bad.name')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(input.attributes('aria-describedby')).toBe('inline-rename-error')
    expect(wrapper.text()).toContain("Use only letters, digits, '-' and '_'.")
    expect(fetchMock.mock.calls.some(([url, init]) => String(url).includes('/sessions/one') && (init as RequestInit | undefined)?.method === 'PATCH')).toBe(false)

    await input.setValue('valid-new-name')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(wrapper.find('input[aria-label="Rename one"]').exists()).toBe(true)
    expect(wrapper.get('input[aria-label="Rename one"]').attributes('aria-describedby')).toBe('inline-rename-error')
    expect(wrapper.text()).toContain('Request failed (404).')
    wrapper.unmount()
  })

  it('commits a valid rename, preserves its saved position, and restores focus after the inventory event', async () => {
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url) === '/api/machines/host/sessions/one' && init?.method === 'PATCH') {
        return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify({ name: 'renamed' }) }
      }
      return { ok: false, status: 404, headers: new Headers(), text: async () => '' }
    })
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const oldRow = wrapper.get('[data-tree-key="session:one"]')
    ;(oldRow.element as HTMLElement).focus()
    await oldRow.trigger('keydown', { key: 'F2' })
    const input = wrapper.get('input[aria-label="Rename one"]')
    await input.setValue('renamed')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/machines/host/sessions/one', expect.objectContaining({ method: 'PATCH' }))
    expect(useTreeStore().order.sessions.a).toEqual(['renamed', 'two'])
    useSessionsStore().apply({
      type: 'sessions.changed', machine: 'host',
      payload: { sessions: [session('renamed', '/work/a'), session('two', '/work/a'), session('loose', '/outside')] },
    })
    await flushPromises()
    await nextTick()
    expect(wrapper.get('[data-tree-key="session:renamed"]').attributes('tabindex')).toBe('0')
    expect((document.activeElement as HTMLElement).dataset.treeKey).toBe('session:renamed')
    wrapper.unmount()
  })

  it('trims a project name before PATCH and keeps the project path and session placement', async () => {
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url) === '/api/projects/a' && init?.method === 'PATCH') {
        const renamedProject = { ...project('a', '/work/a'), name: 'renamed project' }
        return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify(renamedProject) }
      }
      return { ok: false, status: 404, headers: new Headers(), text: async () => '' }
    })
    const wrapper = mount(SessionTree)
    await wrapper.get('button[aria-label="Rename a"]').trigger('click')
    const input = wrapper.get('input[aria-label="Rename a"]')
    await input.setValue('  renamed project  ')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/a', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ name: 'renamed project' }) }))
    expect(useProjectsStore().byPath('/work/a')?.name).toBe('renamed project')
    expect(useTreeStore().groups.groups.find((group) => group.project.id === 'a')?.sessions.map((row) => row.name)).toEqual(['one', 'two'])
    wrapper.unmount()
  })

  it('starts double-click rename only when the pointer is fine', async () => {
    const original = window.matchMedia
    window.matchMedia = vi.fn().mockReturnValue({ matches: false }) as unknown as typeof window.matchMedia
    const wrapper = mount(SessionTree)
    const title = wrapper.get('[data-tree-key="session:one"] [data-session-row]')
    await title.trigger('dblclick')
    expect(wrapper.find('input[aria-label="Rename one"]').exists()).toBe(false)
    window.matchMedia = vi.fn().mockReturnValue({ matches: true }) as unknown as typeof window.matchMedia
    await title.trigger('dblclick')
    expect(wrapper.find('input[aria-label="Rename one"]').exists()).toBe(true)
    wrapper.unmount()
    window.matchMedia = original
  })

  it('opens the project Rename action from its long-press menu', async () => {
    vi.useFakeTimers()
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const row = wrapper.get('[data-tree-key="project:a"] > div')
    const down = new Event('pointerdown', { bubbles: true })
    Object.defineProperties(down, { pointerType: { value: 'touch' }, clientX: { value: 10 }, clientY: { value: 10 } })
    row.element.dispatchEvent(down)
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    const rename = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    expect(rename).toBeTruthy()
    rename!.click()
    await flushPromises()
    expect(wrapper.find('input[aria-label="Rename a"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders lazy window and pane rows and emits their selection ids', async () => {
    const tree = useTreeStore()
    tree.order.expanded = ['host/one']
    const windows = useWindowsStore()
    windows.bySession['host/one'] = {
      status: 'ok', truncated: true, windows: [
        { id: '@1', index: 0, name: 'shell', active: true, panes: [{ id: '%1', index: 0, active: true, command: 'bash', width: 80, height: 24 }] },
        { id: '@2', index: 1, name: 'editor', active: false, panes: [
          { id: '%2', index: 0, active: true, command: 'vim', width: 40, height: 24 },
          { id: '%3', index: 1, active: false, command: 'bash', width: 40, height: 24 },
        ] },
      ],
    }
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const session = wrapper.get('[data-tree-key="session:one"]')
    expect(session.attributes('aria-expanded')).toBe('true')
    const firstWindow = wrapper.get('[data-tree-key="window:host/one/@1"]')
    const splitWindow = wrapper.get('[data-tree-key="window:host/one/@2"]')
    expect(firstWindow.attributes('aria-level')).toBe('3')
    expect(firstWindow.text()).toContain('1: shell')
    expect(firstWindow.find('button').classes()).toContain('touch-target')
    expect(splitWindow.attributes('aria-expanded')).toBe('false')
    expect(splitWindow.find('button').classes()).toContain('touch-target')
    expect(wrapper.findAll('[role="treeitem"][tabindex="0"]')).toHaveLength(1)
    await splitWindow.trigger('keydown', { key: 'ArrowRight' })
    await splitWindow.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const pane = wrapper.get('[data-tree-key="pane:host/one/@2/%3"]')
    expect(pane.attributes('aria-level')).toBe('4')
    expect(pane.text()).toContain('Pane 2 — bash')
    expect(pane.find('button').classes()).toContain('touch-target')
    expect(document.activeElement).toBe(wrapper.get('[data-tree-key="pane:host/one/@2/%2"]').element)
    expect(wrapper.findAll('[role="treeitem"][tabindex="0"]')).toHaveLength(1)
    await pane.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('selectWindow')).toEqual([['one', '@2', '%3']])
    await firstWindow.find('button').trigger('click')
    expect(wrapper.emitted('selectWindow')).toEqual([['one', '@2', '%3'], ['one', '@1', undefined]])
    expect(wrapper.text()).toContain('More windows not shown')
    wrapper.unmount()
  })

  it('shows loading and actionable error rows with retry', async () => {
    const tree = useTreeStore()
    tree.order.expanded = ['host/one']
    const windows = useWindowsStore()
    windows.bySession['host/one'] = { status: 'loading', windows: [], truncated: false }
    const wrapper = mount(SessionTree)
    expect(wrapper.get('[role="treeitem"][aria-disabled="true"]').text()).toContain('Loading windows…')
    windows.bySession['host/one'] = { status: 'error', windows: [], truncated: false, error: new ApiError(404, 'tmux not found on the host', 'Install tmux.') }
    await flushPromises()
    expect(wrapper.get('[role="treeitem"][aria-disabled="true"]').text()).toContain('Tmux not found on the host. Install tmux.')
    const list = vi.spyOn(windowsApi, 'list').mockResolvedValue({ windows: [], truncated: false })
    await wrapper.findAll('button').find((button) => button.text() === 'Retry')!.trigger('click')
    await flushPromises()
    expect(list).toHaveBeenCalledWith('host', 'one')
  })

  it('lets keyboard users tab into row actions without stealing button keys', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const row = wrapper.get('[data-tree-key="project:a"]')
    const rowElement = row.element as HTMLElement
    rowElement.focus()
    await row.trigger('keydown', { key: 'Tab' })
    expect(document.activeElement).toBe(row.element.querySelector('button'))
    const enter = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    row.element.querySelector('button')?.dispatchEvent(enter)
    expect(enter.defaultPrevented).toBe(false)
    expect(useTreeStore().order.collapsed).not.toContain('a')
    const shiftTab = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true })
    row.element.querySelector('button')?.dispatchEvent(shiftTab)
    expect(document.activeElement).toBe(row.element)
    expect(shiftTab.defaultPrevented).toBe(true)
    wrapper.unmount()
  })

  it('moves focus to a visible parent when the focused session disappears', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const sessionRow = wrapper.get('[data-tree-key="session:one"]')
    const sessionElement = sessionRow.element as HTMLElement
    sessionElement.focus()
    useTreeStore().setCollapsed('a', true)
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.get('[data-tree-key="project:a"]').element)
    expect(wrapper.get('[data-tree-key="project:a"]').attributes('tabindex')).toBe('0')
    wrapper.unmount()
  })

  it('shows one empty-state line when there are no projects or sessions', async () => {
    useProjectsStore().reset()
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [] } })
    const wrapper = mount(SessionTree)
    expect(wrapper.findAll('p').filter((line) => line.text() === 'No tmux sessions yet.')).toHaveLength(1)
    expect(wrapper.find('[data-tree-key="other"]').exists()).toBe(false)
  })

  it('hides an empty Other group and restores it when an unmatched session arrives', async () => {
    const sessions = useSessionsStore()
    sessions.apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a'), session('loose', '/outside')] } })
    const tree = useTreeStore()
    tree.setCollapsed('__other__', true)
    const wrapper = mount(SessionTree)
    expect(wrapper.find('[data-tree-key="other"]').exists()).toBe(true)
    sessions.apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a')] } })
    await flushPromises()
    expect(wrapper.find('[data-tree-key="other"]').exists()).toBe(false)
    expect(tree.order.collapsed).toContain('__other__')
    sessions.apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a'), session('new', '/outside')] } })
    await flushPromises()
    expect(wrapper.find('[data-tree-key="other"]').exists()).toBe(true)
    expect(wrapper.find('[data-tree-key="other"]').attributes('aria-expanded')).toBe('false')
  })

  it('makes unmatched sessions clickable and saves as project from their actions menu', async () => {
    const before = useSessionsStore().list('host').find((s) => s.name === 'loose')
    const wrapper = mount(SessionTree)
    expect(wrapper.find('[aria-label="Unmatched session actions"]').exists()).toBe(false)
    await wrapper.get('button[aria-label="loose"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['loose']])
    await wrapper.get('button[aria-label="More actions for loose"]').trigger('keydown', { key: 'Enter' })
    await new Promise((r) => setTimeout(r))
    const saveAction = [...document.body.querySelectorAll<HTMLElement>('[role=menuitem]')].find((x) => x.textContent?.trim() === 'Save as project')
    expect(saveAction).toBeDefined()
    saveAction!.click()
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/projects', expect.objectContaining({ method: 'POST', body: JSON.stringify({ machineId: 'host', path: '/outside', name: '' }) }))
    expect(useTreeStore().groups.groups.some((group) => group.project.id === 'saved' && group.sessions.some((s) => s.name === 'loose'))).toBe(true)
    expect(useSessionsStore().list('host').find((s) => s.name === 'loose')).toEqual(before)
  })
})
