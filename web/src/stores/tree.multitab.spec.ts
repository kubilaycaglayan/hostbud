import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTreeStore } from './tree'
import { useProjectsStore } from './projects'
import { emptyTreeState, type TreeState } from '@/lib/tree'

// Independent Pinia roots model browser tabs; fetch models their shared account.
let saved: TreeState
let holdSave: boolean
let releaseSave: (() => void) | undefined
const tabs: ReturnType<typeof useTreeStore>[] = []
const copy = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T

beforeEach(() => {
  vi.useFakeTimers()
  saved = {
    ...emptyTreeState(), projects: ['p', 'q'],
    sessions: { p: ['one', 'two'], q: ['three', 'four'] },
    sections: [{ id: 'a', name: 'A', color: 'red' }, { id: 'b', name: 'B', color: 'blue' }],
  }
  holdSave = false
  releaseSave = undefined
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as TreeState
      if (holdSave) await new Promise<void>((resolve) => { releaseSave = resolve })
      saved = copy(body)
      return new Response(null, { status: 204 })
    }
    return new Response(JSON.stringify(saved), { status: 200 })
  }))
})
afterEach(() => {
  for (const tab of tabs.splice(0)) tab.reset()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
async function tab() {
  setActivePinia(createPinia())
  const projects = useProjectsStore()
  projects.items = ['p', 'q'].map((id) => ({ id, name: id, machineId: 'host', path: '/' + id, sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }))
  projects.loaded = true
  const tree = useTreeStore()
  tabs.push(tree)
  await tree.load()
  return tree
}
const settleSave = () => vi.advanceTimersByTimeAsync(501)

describe('tree edits across tabs', () => {
  it.each(['sections', 'projects', 'sessions'] as const)('a stale collapse edit preserves another tab’s %s order', async (level) => {
    const a = await tab()
    const b = await tab()
    if (level === 'sections') a.reorderProjectSections(['b', 'a'])
    if (level === 'projects') a.reorderProjects(['q', 'p'])
    if (level === 'sessions') a.order.sessions.p = ['two', 'one']
    await settleSave()
    const expected = copy(saved)
    b.toggleCollapsed('p')
    await settleSave()
    expect(saved.collapsed).toContain('p')
    expect(saved.sections).toEqual(expected.sections)
    expect(saved.projects).toEqual(expected.projects)
    expect(saved.sessions.p).toEqual(expected.sessions.p)
    await a.refresh()
    await b.refresh()
    expect(a.order).toEqual(b.order)
    expect(a.order.collapsed).toContain('p')
  })

  it('preserves session reorders in different projects from stale tabs', async () => {
    const a = await tab()
    const b = await tab()
    a.order.sessions.p = ['two', 'one']
    await settleSave()
    b.order.sessions.q = ['four', 'three']
    await settleSave()
    expect(saved.sessions).toMatchObject({ p: ['two', 'one'], q: ['four', 'three'] })
  })

  it('refresh cannot revert an edit while its save is in flight', async () => {
    const a = await tab()
    holdSave = true
    a.reorderProjectSections(['b', 'a'])
    await settleSave()
    expect(releaseSave).toBeTypeOf('function')
    await a.refresh()
    const duringSave = a.order.sections.map((section) => section.id)
    holdSave = false
    releaseSave!()
    await settleSave()
    expect(duringSave).toEqual(['b', 'a'])
    expect(a.order.sections.map((section) => section.id)).toEqual(['b', 'a'])
    expect(saved.sections.map((section) => section.id)).toEqual(['b', 'a'])
  })
})
