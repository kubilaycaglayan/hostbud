import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { Project, Session } from '@/api/types'
import { VueDraggable } from 'vue-draggable-plus'
import { useProjectsStore } from '@/stores/projects'
import { useSessionsStore } from '@/stores/sessions'
import { useMachinesStore } from '@/stores/machines'
import { useTreeStore } from '@/stores/tree'
import { useWindowsStore } from '@/stores/windows'
import { ApiError, windowsApi } from '@/api/client'
import SessionTree from './SessionTree.vue'
import TreePanel from './TreePanel.vue'

const fetchMock = vi.fn()
vi.stubGlobal('fetch', fetchMock)
const project = (id: string, path: string): Project => ({ id, machineId: 'host', path, name: id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
const session = (name: string, path: string): Session => ({ id: `$${name}`, name, path, attached: 0, windows: 3, created: '', activity: '' })

beforeEach(async () => {
  setActivePinia(createPinia())
  fetchMock.mockReset()
  fetchMock.mockImplementation(async (url: string) => {
    if (String(url) === '/api/machines/host/sessions/one') {
      return { ok: false, status: 504, headers: new Headers(), json: async () => ({ error: "The host didn't answer within 10s", hint: 'hostbud will retry' }), text: async () => '' }
    }
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
  it('keeps the project tree as the scroll area and anchors the section action outside it', () => {
    const wrapper = mount(TreePanel, { props: { connectionState: 'connected' } })
    const panel = wrapper.get('section[aria-label="Sessions"]')
    const viewport = panel.get('div')
    expect(viewport.classes()).toEqual(expect.arrayContaining(['flex', 'flex-col', 'overflow-hidden', 'flex-1']))
    const nav = viewport.get('nav[aria-label="Project and session tree"]')
    expect(nav.classes()).toContain('flex-1')
    const scroller = nav.get('[role="tree"]')
    expect(scroller.classes()).toContain('overflow-y-auto')
    const footer = nav.get('button[aria-label="Create a new section"]').element.parentElement!
    expect(footer.classList.contains('mt-auto')).toBe(true)
    expect(footer.previousElementSibling).toBe(scroller.element)
    expect(footer.querySelector('[role="tree"]')).toBeNull()
    wrapper.unmount()
  })

  it('starts a new session for the focused project, session, or Other group with N', async () => {
    const wrapper = mount(SessionTree)
    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'N', shiftKey: true })
    expect(wrapper.emitted('sessionInProject')?.at(-1)?.[0]).toMatchObject({ id: 'a' })
    await wrapper.get('[data-tree-key="session:one"]').trigger('keydown', { key: 'n' })
    expect(wrapper.emitted('sessionInProject')?.at(-1)?.[0]).toMatchObject({ id: 'a' })
    await wrapper.get('[data-tree-key="other"]').trigger('keydown', { key: 'n' })
    expect(wrapper.emitted('create')).toHaveLength(1)
    await wrapper.get('button[aria-label="New session in a"]').trigger('click')
    expect(wrapper.emitted('createSessionInProject')?.at(-1)?.[0]).toMatchObject({ id: 'a' })
    wrapper.unmount()
  })

  it('colors a project name green while one of its sessions is working, live from events', async () => {
    const wrapper = mount(SessionTree)
    const name = () => wrapper.get('[data-tree-key="project:a"] span.font-semibold')
    expect(name().classes()).not.toContain('text-ok')
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [{ ...session('one', '/work/a'), status: 'working' }, session('two', '/work/a'), session('loose', '/outside')] } })
    await nextTick()
    expect(name().classes()).toContain('text-ok')
    expect(wrapper.get('[data-tree-key="project:b"] span.font-semibold').classes()).not.toContain('text-ok')
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [{ ...session('one', '/work/a'), status: 'ended' }, session('two', '/work/a'), session('loose', '/outside')] } })
    await nextTick()
    expect(name().classes()).not.toContain('text-ok')
    wrapper.unmount()
  })

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

  it('creates, colors, renames, assigns and deletes a persistent project section', async () => {
    const tree = useTreeStore()
    const wrapper = mount(SessionTree, { attachTo: document.body })
    expect(wrapper.get('button[aria-label="Create a new section"]').classes()).toContain('touch-target')
    await wrapper.get('button[aria-label="Create a new section"]').trigger('click')
    await nextTick()
    const editor = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    const input = editor.querySelector<HTMLInputElement>('input')!
    input.value = 'Research'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    editor.querySelector<HTMLButtonElement>('button[aria-label="orange"]')!.click()
    editor.querySelector<HTMLButtonElement>('button[type="submit"]')!.click()
    await flushPromises()
    expect(tree.order.sections).toHaveLength(1)
    expect(tree.order.sections[0]).toMatchObject({ name: 'Research', color: 'orange' })
    expect(wrapper.get('[role="group"][aria-label="Research section"]').text()).toContain('Empty section')

    await wrapper.get('[data-tree-key="project:a"] button[aria-label="More actions for a"]').trigger('click')
    await nextTick()
    const moveAction = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Move to Research')
    expect(moveAction).toBeTruthy()
    moveAction!.click()
    await nextTick()
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
    expect(tree.order.projectSections.a).toBe(tree.order.sections[0].id)
    expect(wrapper.get('[role="group"][aria-label="Research section"]').attributes('data-project-section-id')).toBe(tree.order.sections[0].id)

    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'p' })
    await flushPromises()
    await nextTick()
    expect(tree.order.pinned).toContain('a')
    expect(wrapper.find('[data-tree-key="project:a"] button[aria-label="Unpin a"]').exists()).toBe(true)
    expect(wrapper.get('[role="group"][aria-label="Research section"]').text()).toContain('Pinned')

    await wrapper.get('button[aria-label="Edit section Research"]').trigger('click')
    await nextTick()
    const edit = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    const editInput = edit.querySelector<HTMLInputElement>('input')!
    editInput.value = 'Planning'
    editInput.dispatchEvent(new Event('input', { bubbles: true }))
    edit.querySelector<HTMLButtonElement>('button[aria-label="purple"]')!.click()
    edit.querySelector<HTMLButtonElement>('button[type="submit"]')!.click()
    await flushPromises()
    expect(tree.order.sections[0]).toMatchObject({ name: 'Planning', color: 'purple' })
    expect(tree.order.projectSections.a).toBe(tree.order.sections[0].id)

    await wrapper.get('button[aria-label="Edit section Planning"]').trigger('click')
    const deleteButton = [...document.body.querySelectorAll('button')].find((button) => button.closest('[role="dialog"]') && button.textContent?.includes('Delete section'))
    expect(deleteButton).toBeTruthy()
    deleteButton!.click()
    expect(tree.order.sections).toEqual([])
    expect(tree.order.projectSections).toEqual({})
    wrapper.unmount()
  })

  it('reorders sections from their drag list and keeps section creation in the gutter footer', async () => {
    const tree = useTreeStore()
    const first = tree.createProjectSection('First', 'blue')
    const second = tree.createProjectSection('Second', 'green')
    const wrapper = mount(SessionTree)
    const sorter = wrapper.findAllComponents(VueDraggable).find((list) => list.attributes('data-section-order-list') !== undefined)
    expect(sorter).toBeTruthy()
    expect(sorter!.classes()).toContain('gap-1')
    expect(wrapper.get('button[aria-label="Drag to reorder section First"]').classes()).toContain('section-drag-handle')
    expect(wrapper.get('[aria-label="First section"] header').findAll('button').map((button) => button.attributes('aria-label'))).toEqual([
      'Collapse section First',
      'Edit section First',
      'Drag to reorder section First',
    ])
    expect(wrapper.get('button[aria-label="Collapse section First"]').classes()).toContain('section-color-button')
    expect(wrapper.get('[aria-label="First section"] [data-section-title]').classes()).toContain('section-title-label')
    await sorter!.vm.$emit('update:modelValue', [{ id: second.id }, { id: first.id }])
    expect(tree.order.sections.map((section) => section.id)).toEqual([second.id, first.id])
    expect(wrapper.get('nav > div.sticky').classes()).toContain('bottom-0')
    expect(wrapper.get('nav > div.sticky').classes()).toContain('mt-auto')
    expect(wrapper.get('nav > div.sticky').classes()).toContain('shrink-0')
    wrapper.unmount()
  })

  it('marks a collapsed parent project as selected and moves the marker with selection', async () => {
    const tree = useTreeStore()
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a'), session('two', '/work/b')] } })
    tree.sync()
    const wrapper = mount(SessionTree, { props: { selected: 'one' } })
    const first = wrapper.get('[data-tree-key="project:a"]')
    await first.get('button[aria-label="Collapse a"]').trigger('click')
    expect(first.attributes('aria-selected')).toBe('true')
    expect(first.get('.tree-row').classes()).toContain('bg-selected')
    expect(wrapper.find('[data-tree-key="session:one"]').exists()).toBe(false)

    await wrapper.setProps({ selected: 'two' })
    expect(first.attributes('aria-selected')).toBeUndefined()
    const second = wrapper.get('[data-tree-key="project:b"]')
    await second.get('button[aria-label="Collapse b"]').trigger('click')
    expect(second.attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-tree-key="session:two"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('toggles a section from its color dot and marks it when it contains the selected session', async () => {
    const tree = useTreeStore()
    const section = tree.createProjectSection('Research', 'blue')
    tree.assignProjectSection('a', section.id)
    const wrapper = mount(SessionTree, { props: { selected: 'one' } })
    const sectionRow = () => wrapper.get(`[data-project-section-id="${section.id}"]`)
    expect(sectionRow().attributes('data-selected-session')).toBeUndefined()
    await sectionRow().get('button[aria-label="Collapse section Research"]').trigger('click')
    expect(sectionRow().get('[data-section-title]').text()).toBe('Research')
    expect(sectionRow().get('[data-section-project-count]').text()).toBe('(1)')
    expect(sectionRow().find('p').exists()).toBe(false)
    expect(tree.order.collapsedSections).toEqual([section.id])
    expect(sectionRow().attributes('data-selected-session')).toBe('true')
    expect(sectionRow().attributes('aria-label')).toContain('contains selected session')
    expect(wrapper.find('[data-tree-key="project:a"]').exists()).toBe(false)
    expect(wrapper.find('[data-tree-key="session:one"]').exists()).toBe(false)
    expect(wrapper.props('selected')).toBe('one')
    await sectionRow().get('button[aria-label="Expand section Research"]').trigger('click')
    expect(tree.order.collapsedSections).toEqual([])
    expect(sectionRow().attributes('data-selected-session')).toBeUndefined()
    expect(wrapper.get('[data-tree-key="session:one"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('preserves saved row order until the first host session snapshot arrives', async () => {
    const sessions = useSessionsStore()
    const machines = useMachinesStore()
    const tree = useTreeStore()
    sessions.reset()
    tree.order.sessions.a = ['two', 'one']
    machines.apply({ type: 'snapshot', machines: [{ id: 'host', label: 'Host', status: 'ok', os: '', home: '/work', tmuxVersion: '', tmuxMissing: false }], sessions: {} })

    tree.sync()
    expect(tree.order.sessions.a).toEqual(['two', 'one'])

    sessions.apply({ type: 'snapshot', machines: [], sessions: { host: [session('one', '/work/a'), session('two', '/work/a')] } })
    tree.sync()
    expect(tree.groups.groups.find((group) => group.project.id === 'a')?.sessions.map((item) => item.name)).toEqual(['two', 'one'])
  })

  it('pins from the project menu, unpins with P, and keeps reordering inside a section', async () => {
    const tree = useTreeStore()
    const wrapper = mount(SessionTree, { attachTo: document.body })
    await wrapper.get('[data-tree-key="project:b"] button[aria-label="More actions for b"]').trigger('keydown', { key: 'Enter' })
    const pinAction = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Pin')
    expect(pinAction).toBeTruthy()
    expect(pinAction?.classList.contains('touch-target')).toBe(true)
    pinAction!.click()
    await flushPromises()
    expect(tree.order.pinned).toEqual(['b'])
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
    expect(wrapper.get('[role="group"][aria-label="Pinned projects"]').text()).toContain('Pinned')
    expect(wrapper.get('[data-tree-key="project:b"] button[aria-label="Unpin b"]').classes()).toContain('touch-target')
    expect([...wrapper.findAll('[data-tree-kind="project"]')].map((row) => row.attributes('data-tree-key'))).toEqual(['project:b', 'project:a'])
    const lists = wrapper.findAllComponents(VueDraggable)
    expect(lists.every((list) => list.props('forceFallback') === true)).toBe(true)
    expect(lists.every((list) => list.props('delay') === 250)).toBe(true)
    const onMove = lists[0].props('onMove') as (event: { from: HTMLElement; to: HTMLElement }, originalEvent: Event) => boolean
    expect(onMove({ from: lists[0].element as HTMLElement, to: lists[0].element as HTMLElement }, new Event('move'))).toBe(true)
    expect(onMove({ from: lists[0].element as HTMLElement, to: lists[1].element as HTMLElement }, new Event('move'))).toBe(false)

    const pinnedRow = wrapper.get('[data-tree-key="project:b"]')
    await pinnedRow.trigger('keydown', { key: 'ArrowDown', altKey: true })
    expect(tree.order.projects).toEqual(['b', 'a'])
    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'p' })
    expect(tree.order.pinned).toEqual(['b', 'a'])
    expect(tree.order.projects).toEqual(['b', 'a'])
    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'ArrowUp', altKey: true })
    await wrapper.get('[data-tree-key="project:b"]').trigger('keydown', { key: 'ArrowDown', altKey: true })
    expect(tree.order.projects).toEqual(['a', 'b'])
    await wrapper.get('[data-tree-key="project:a"] button[aria-label="Unpin a"]').trigger('click')
    expect(tree.order.pinned).toEqual(['b'])
    expect(tree.order.projects).toEqual(['b', 'a'])
    await wrapper.get('[data-tree-key="project:b"]').trigger('keydown', { key: 'p' })
    expect(tree.order.pinned).toEqual([])
    expect(tree.order.projects).toEqual(['a', 'b'])
    expect(wrapper.get('[data-tree-key="project:b"]').find('button[aria-label="Unpin b"]').exists()).toBe(false)
    wrapper.unmount()
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
    expect(wrapper.get('[data-tree-key="other"] [data-other-label]').classes()).toEqual(expect.arrayContaining(['uppercase', 'text-muted']))
    expect(projectRow.classes()).not.toContain('bg-tree-header')
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
    await wrapper.get('[data-tree-key="project:a"]').trigger('keydown', { key: 'Delete' })
    expect(wrapper.emitted('removeProject')).toEqual([['a']])
    wrapper.unmount()
  })

  it('starts inline rename from F2 and the project menu (no project pencil), then restores tree focus on Escape', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const session = wrapper.get('[data-tree-key="session:one"]')
    await session.trigger('keydown', { key: 'F2' })
    expect(wrapper.find('input[aria-label="Rename one"]').exists()).toBe(true)
    await wrapper.get('input[aria-label="Rename one"]').trigger('keydown.esc')
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.get('[data-tree-key="session:one"]').element)

    expect(wrapper.find('button[aria-label="Rename a"]').exists()).toBe(false)
    await wrapper.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const rename = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    expect(rename).toBeTruthy()
    expect(rename!.classList.contains('touch-target')).toBe(true)
    expect([...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].some((node) => node.textContent?.trim() === 'Hide')).toBe(true)
    expect([...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].some((node) => node.textContent?.trim() === 'Remove project…')).toBe(true)
    const remove = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Remove project…')
    remove!.click()
    await flushPromises()
    expect(wrapper.emitted('removeProject')).toEqual([['a']])
    await wrapper.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const renameAgain = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    renameAgain!.click()
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
    expect(wrapper.text()).toContain("The host didn't answer within 10s")
    wrapper.unmount()
  })

  it('turns spaces in an inline rename into hyphens without asking', async () => {
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url) === '/api/machines/host/sessions/one' && init?.method === 'PATCH') {
        return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify({ name: 'new-session' }) }
      }
      return { ok: false, status: 404, headers: new Headers(), text: async () => '' }
    })
    const wrapper = mount(SessionTree, { attachTo: document.body })
    await wrapper.get('[data-tree-key="session:one"]').trigger('keydown', { key: 'F2' })
    const input = wrapper.get('input[aria-label="Rename one"]')
    await input.setValue(' new  session ')
    await input.trigger('keydown.enter')
    await flushPromises()
    const patch = fetchMock.mock.calls.find(([url, init]) => String(url) === '/api/machines/host/sessions/one' && (init as RequestInit | undefined)?.method === 'PATCH')
    expect(JSON.parse(String((patch?.[1] as RequestInit).body))).toEqual({ name: 'new-session' })
    expect(wrapper.text()).not.toContain("Use only letters, digits, '-' and '_'.")
    wrapper.unmount()
  })

  it('refuses a rename to an existing session name without duplicating rows', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const row = wrapper.get('[data-tree-key="session:one"]')
    ;(row.element as HTMLElement).focus()
    await row.trigger('keydown', { key: 'F2' })
    const input = wrapper.get('input[aria-label="Rename one"]')
    await input.setValue('two')
    await input.trigger('keydown.enter')
    await flushPromises()
    await nextTick()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/machines/host/sessions/one', expect.objectContaining({ method: 'PATCH' }))
    expect(wrapper.get('#inline-rename-error').text()).toMatch(/already exists/)
    expect(useTreeStore().order.sessions.a).toEqual(['one', 'two'])
    expect(useSessionsStore().list('host').map((s) => s.name)).toEqual(['one', 'two', 'loose'])
    expect(wrapper.findAll('[data-tree-key="session:one"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-tree-key="session:two"]')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps focus on the terminal the rename selected instead of refocusing the tree', async () => {
    fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url) === '/api/machines/host/sessions/one' && init?.method === 'PATCH') {
        return { ok: true, status: 200, headers: new Headers(), text: async () => JSON.stringify({ name: 'renamed' }) }
      }
      return { ok: false, status: 404, headers: new Headers(), text: async () => '' }
    })
    const terminal = document.createElement('textarea')
    document.body.appendChild(terminal)
    // The app focuses the selected session's terminal (App.openSession).
    const wrapper = mount(SessionTree, { attachTo: document.body, attrs: { onSelect: () => terminal.focus() } })
    const row = wrapper.get('[data-tree-key="session:one"]')
    ;(row.element as HTMLElement).focus()
    await row.trigger('keydown', { key: 'F2' })
    const input = wrapper.get('input[aria-label="Rename one"]')
    await input.setValue('renamed')
    await input.trigger('keydown.enter')
    await flushPromises()
    await nextTick()
    await nextTick()
    expect(document.activeElement).toBe(terminal)
    wrapper.unmount()
    terminal.remove()
  })

  it('renames immediately and returns focus to the selected terminal cursor', async () => {
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
    await nextTick()
    expect(wrapper.find('[data-tree-key="session:renamed"]').exists()).toBe(true)
    expect(wrapper.emitted('select')?.at(-1)).toEqual(['one'])
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith('/api/machines/host/sessions/one', expect.objectContaining({ method: 'PATCH' }))
    expect(useTreeStore().order.sessions.a).toEqual(['renamed', 'two'])
    useSessionsStore().apply({
      type: 'sessions.changed', machine: 'host',
      payload: { sessions: [session('renamed', '/work/a'), session('two', '/work/a'), session('loose', '/outside')] },
    })
    await flushPromises()
    await nextTick()
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
    const wrapper = mount(SessionTree, { attachTo: document.body })
    await wrapper.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    ;[...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')!.click()
    await flushPromises()
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
    const downAgain = new Event('pointerdown', { bubbles: true })
    Object.defineProperties(downAgain, { pointerType: { value: 'touch' }, clientX: { value: 10 }, clientY: { value: 10 } })
    row.element.dispatchEvent(downAgain)
    const move = new Event('pointermove', { bubbles: true })
    Object.defineProperties(move, { pointerType: { value: 'touch' }, clientX: { value: 25 }, clientY: { value: 10 } })
    row.element.dispatchEvent(move)
    await vi.advanceTimersByTimeAsync(500)
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
    row.element.dispatchEvent(down)
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    const rename = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Rename')
    expect(rename).toBeTruthy()
    const hide = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Hide')
    expect(hide?.classList.contains('touch-target')).toBe(true)
    rename!.click()
    await flushPromises()
    expect(wrapper.find('input[aria-label="Rename a"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('closes a session menu on Hide so Show hidden does not reopen it', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    await wrapper.get('[data-tree-key="session:one"] button[aria-label="More actions for one"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    ;[...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Hide')!.click()
    await flushPromises()
    expect(wrapper.find('[data-tree-key="session:one"]').exists()).toBe(false)
    await wrapper.get('button[aria-pressed="false"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-tree-key="session:one"]').attributes('aria-label')).toBe('one, hidden')
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
    wrapper.unmount()
  })

  it('hides and unhides projects and sessions through Show hidden without touching tmux or layout', async () => {
    const tree = useTreeStore()
    const layout = (await import('@/stores/layout')).useLayoutStore()
    const before = JSON.stringify(layout.layout)
    const wrapper = mount(SessionTree, { attachTo: document.body })
    await wrapper.get('[data-tree-key="session:one"] button[aria-label="More actions for one"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const hideSession = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Hide')
    expect(hideSession).toBeTruthy()
    hideSession!.click()
    await flushPromises()
    expect(wrapper.find('[data-tree-key="session:one"]').exists()).toBe(false)
    expect(wrapper.get('button[aria-pressed="false"]').text()).toBe('Show hidden (1)')

    await wrapper.get('button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const hideProject = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Hide')
    hideProject!.click()
    await flushPromises()
    expect(wrapper.find('[data-tree-key="project:a"]').exists()).toBe(false)
    expect(wrapper.get('button[aria-pressed="false"]').text()).toBe('Show hidden (2)')
    useSessionsStore().apply({
      type: 'snapshot', machines: [],
      sessions: { host: [session('one', '/work/a'), session('two', '/work/a'), session('late', '/work/a'), session('loose', '/outside')] },
    })
    tree.sync()
    expect(wrapper.find('[data-tree-key="session:late"]').exists()).toBe(false)

    await wrapper.get('button[aria-pressed="false"]').trigger('click')
    expect(wrapper.get('button[aria-pressed="true"]')).toBeTruthy()
    expect(wrapper.get('[data-tree-key="project:a"]').attributes('aria-label')).toBe('a, hidden')
    expect(wrapper.get('[data-tree-key="project:a"]').classes()).toContain('opacity-50')
    expect(wrapper.get('[data-tree-key="session:one"]').attributes('aria-label')).toBe('one, hidden')
    expect(wrapper.get('[data-tree-key="session:one"]').classes()).toContain('opacity-50')
    expect(wrapper.get('[data-tree-key="session:late"]').attributes('aria-label')).toBe('late, hidden')
    await wrapper.get('[data-tree-key="project:a"] button[aria-label="More actions for a"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const unhideProject = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Unhide')
    unhideProject!.click()
    await flushPromises()
    expect(wrapper.get('[data-tree-key="project:a"]').attributes('aria-label')).toBe('a')
    await wrapper.get('[data-tree-key="session:one"] button[aria-label="More actions for one"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const unhideSession = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((node) => node.textContent?.trim() === 'Unhide')
    unhideSession!.click()
    await flushPromises()
    expect(wrapper.get('[data-tree-key="session:one"]').attributes('aria-label')).toBe('one')

    expect(JSON.stringify(layout.layout)).toBe(before)
    expect(fetchMock.mock.calls.some(([url, init]) => String(url).includes('/sessions/') && ['PATCH', 'DELETE'].includes(String((init as RequestInit | undefined)?.method)))).toBe(false)
    expect(tree.order.sessions.a).toEqual(['one', 'two', 'late'])
    wrapper.unmount()
  })

  it('uses H to hide a focused session and focuses the next visible row', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const row = wrapper.get('[data-tree-key="session:one"]')
    ;(row.element as HTMLElement).focus()
    await row.trigger('keydown', { key: 'h' })
    await flushPromises()
    await nextTick()
    expect(useTreeStore().order.hidden.sessions).toContain('host/one')
    expect((document.activeElement as HTMLElement).dataset.treeKey).toBe('session:two')
    wrapper.unmount()
  })

  it('shows Everything is hidden when the last project and session are hidden', () => {
    const tree = useTreeStore()
    tree.hideProject('a')
    tree.hideProject('b')
    for (const name of ['one', 'two', 'loose']) tree.hideSession('host', name)
    const wrapper = mount(SessionTree)
    expect(wrapper.text()).toContain('Everything is hidden.')
    expect(wrapper.text()).not.toContain('No tmux sessions yet.')
  })

  it('moves focus to Show hidden instead of body when hiding the last visible row', async () => {
    const tree = useTreeStore()
    tree.hideProject('a')
    tree.hideProject('b')
    tree.hideSession('host', 'one')
    tree.hideSession('host', 'two')
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const row = wrapper.get('[data-tree-key="session:loose"]')
    ;(row.element as HTMLElement).focus()
    await row.trigger('keydown', { key: 'H' })
    await flushPromises()
    await nextTick()
    expect(document.activeElement).toBe(wrapper.get('button[aria-pressed="false"]').element)
    expect(wrapper.text()).toContain('Everything is hidden.')
    wrapper.unmount()
  })

  it('compact project rows: no session counts or attachment dots, with grouped row actions', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const project = wrapper.get('[data-tree-key="project:a"]')
    const header = project.element.firstElementChild as HTMLElement
    expect(header.className).toContain('min-h-8')
    expect(header.classList.contains('tree-row')).toBe(true)
    expect(header.querySelector('[data-project-name]')?.classList.contains('cursor-pointer')).toBe(true)
    expect(header.classList.contains('relative')).toBe(true)
    const expand = header.firstElementChild as HTMLElement
    const drag = header.querySelector('[aria-label="Drag to reorder project a"]') as HTMLElement
    expect(header.querySelector('[data-project-actions]')?.contains(drag)).toBe(true)
    expect(expand.getAttribute('aria-label')).toBe('Collapse a')
    expect(drag.getAttribute('aria-label')).toBe('Drag to reorder project a')
    expect(expand.nextElementSibling?.classList.contains('lucide-folder')).toBe(true)
    expect(header.querySelector('[data-project-count]')).toBeNull()
    for (const label of ['Drag to reorder project a', 'More actions for a', 'New session in a']) {
      expect(header.querySelector(`[aria-label="${label}"]`)?.classList.contains('row-action')).toBe(true)
    }
    expect(expand.classList.contains('row-action')).toBe(false)
    // Every control stays a touch target (44 px on phones) without a desktop 44 px minimum.
    const controls = [...project.element.querySelectorAll('button')]
    expect(controls.every((button) => button.classList.contains('touch-target'))).toBe(true)
    expect(project.element.querySelectorAll('.min-h-11, .min-h-12, .min-w-8')).toHaveLength(0)
    const group = project.get('[role="group"]')
    expect(group.classes()).toEqual(expect.arrayContaining(['ml-2', 'border-l', 'pl-1.5']))
    // The project path heads its expanded group.
    expect(group.element.firstElementChild?.getAttribute('title')).toBe('/work/a')
    // Session rows lead with the reserved logo/status slots, then the name; their chevron (3 windows) follows it.
    const row = wrapper.get('[data-tree-key="session:one"]')
    expect(row.element.firstElementChild?.hasAttribute('data-session-prefix')).toBe(true)
    expect(row.element.children[1]?.getAttribute('aria-label')).toBe('one')
    expect(row.find('[data-session-dot]').exists()).toBe(false)
    const buttons = row.findAll('button').map((b) => b.attributes('aria-label'))
    expect(buttons.indexOf('one')).toBeLessThan(buttons.indexOf('Expand one'))
    expect(header.querySelector('[role="img"]')).toBeNull()
    // Project actions are still wired.
    expect(project.find('button[aria-label="More actions for a"]').exists()).toBe(true)
    expect(project.find('button[aria-label="New session in a"]').exists()).toBe(true)
    await project.get('button[aria-label="Collapse a"]').trigger('click')
    await nextTick()
    expect(project.element.querySelector('[data-project-count]')).toBeNull()
    await project.get('button[aria-label="Expand a"]').trigger('click')
    await nextTick()
    expect(project.element.querySelector('[data-project-count]')).toBeNull()
    wrapper.unmount()
  })

  it('arrow keys skip expanding a session with nothing to expand (M8 T2)', async () => {
    useSessionsStore().apply({ type: 'snapshot', machines: [], sessions: { host: [{ ...session('one', '/work/a'), windows: 1 }, session('two', '/work/a'), session('loose', '/outside')] } })
    await nextTick()
    const list = vi.spyOn(windowsApi, 'list').mockResolvedValue({ windows: [], truncated: false })
    const wrapper = mount(SessionTree, { attachTo: document.body })
    const row = wrapper.get('[data-tree-key="session:one"]')
    expect(row.attributes('aria-expanded')).toBeUndefined()
    await row.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(useTreeStore().order.expanded).not.toContain('host/one')
    expect(list).not.toHaveBeenCalled()
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

  it('shows an opened session: expands its collapsed project without taking focus', async () => {
    const wrapper = mount(SessionTree, { attachTo: document.body, props: { selected: 'one' } })
    const tree = useTreeStore()
    tree.setCollapsed('a', true)
    tree.hideSession('host', 'one')
    await flushPromises()
    expect(wrapper.find('[data-tree-key="session:one"]').exists()).toBe(false)
    const before = document.activeElement
    await (wrapper.vm as unknown as { showSession: (name: string) => Promise<void> }).showSession('one')
    await flushPromises()
    expect(tree.order.collapsed).not.toContain('a')
    expect(wrapper.get('[data-tree-key="session:one"]').attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(before)
    // An unknown session changes nothing.
    tree.setCollapsed('a', true)
    await (wrapper.vm as unknown as { showSession: (name: string) => Promise<void> }).showSession('missing')
    expect(tree.order.collapsed).toContain('a')
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

  it('emits createQueue with the session from its actions menu', async () => {
    const wrapper = mount(SessionTree)
    await wrapper.get('button[aria-label="More actions for loose"]').trigger('keydown', { key: 'Enter' })
    await new Promise((r) => setTimeout(r))
    const action = [...document.body.querySelectorAll<HTMLElement>('[role=menuitem]')].find((x) => x.textContent?.trim() === 'Create queue')
    expect(action).toBeDefined()
    action!.click()
    await flushPromises()
    expect(wrapper.emitted('createQueue')?.[0]?.[0]).toMatchObject({ name: 'loose', path: '/outside' })
  })
})
