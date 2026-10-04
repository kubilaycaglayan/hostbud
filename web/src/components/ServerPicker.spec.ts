import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CreateSessionDialog from './CreateSessionDialog.vue'
import FileBrowserDialog from './FileBrowserDialog.vue'
import type { Machine } from '@/api/types'
import { useMachinesStore } from '@/stores/machines'
import { useProjectsStore } from '@/stores/projects'
import { stubFetch } from '@/test-utils'

// V2-M13 T5: New session and Browse files ask which server.
const hostMachine: Machine = { id: 'host', label: 'Host', source: 'host', status: 'ok', os: '', home: '/home/dev', tmuxVersion: '', tmuxMissing: false }
const server: Machine = { id: 's-abc', label: 'Build box', source: 'custom', status: 'ok', os: '', home: '/home/build', tmuxVersion: '', tmuxMissing: false }

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const select = () => document.body.querySelector<HTMLSelectElement>('select[name="server"]')
async function choose(id: string) {
  select()!.value = id
  select()!.dispatchEvent(new Event('change'))
  await flushPromises()
}
function button(text: string) {
  return [...document.body.querySelectorAll('button')].find((b) => b.textContent?.trim() === text)!
}

describe('server picker', () => {
  it('is absent while the host is the only machine', async () => {
    useMachinesStore().machines = [hostMachine]
    stubFetch(() => ({ status: 201, body: { name: 'dev' } }))
    mount(CreateSessionDialog, { props: { open: true, machine: 'host' }, attachTo: document.body })
    await flushPromises()
    expect(select()).toBeNull()
    expect(document.body.textContent).toContain('on the host')
  })

  it('creates the new session on the chosen server and emits its ref', async () => {
    useMachinesStore().machines = [hostMachine, { ...server, status: 'unreachable' }]
    const calls = stubFetch(() => ({ status: 201, body: { name: 'build' } }))
    const w = mount(CreateSessionDialog, { props: { open: true, machine: 'host' }, attachTo: document.body })
    await flushPromises()
    expect([...select()!.options].map((o) => o.textContent?.trim())).toEqual(['Host (this host)', 'Build box — not connected'])
    expect(select()!.value).toBe('host')
    await choose('s-abc')
    expect(document.body.textContent).toContain('on Build box')
    // The default name follows the server's home.
    expect(document.body.querySelector<HTMLInputElement>('input[name="name"]')!.value).toBe('build')
    button('Create').click()
    await flushPromises()
    expect(calls.find((c) => c.method === 'POST')?.path).toBe('/api/machines/s-abc/sessions')
    expect(w.emitted('created')?.[0]).toEqual(['s-abc/build'])
  })

  it('browses the chosen server and adds projects there', async () => {
    useMachinesStore().machines = [hostMachine, server]
    useProjectsStore().remember({ id: 'p-host', machineId: 'host', path: '/home/build/app', name: 'app', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' })
    const calls = stubFetch((method, path) => {
      if (path.endsWith('/fs/home')) return { status: 200, body: { path: path.includes('s-abc') ? '/home/build' : '/home/dev' } }
      if (path.includes('/fs?')) return { status: 200, body: { path: path.includes('s-abc') ? '/home/build' : '/home/dev', entries: [{ name: 'app', path: path.includes('s-abc') ? '/home/build/app' : '/home/dev/app', kind: 'directory' }] } }
      if (path === '/api/projects?machine=*') return { status: 200, body: { projects: [{ id: 'p-host', machineId: 'host', path: '/home/build/app', name: 'app' }] } }
      if (method === 'POST' && path === '/api/projects') return { status: 201, body: { id: 'p-srv', machineId: 's-abc', path: '/home/build/app', name: 'app' } }
      if (path.includes('recent-commands')) return { status: 200, body: { commands: [] } }
      return { status: 404 }
    })
    mount(FileBrowserDialog, { props: { open: true, machine: 'host' }, attachTo: document.body })
    await flushPromises()
    expect(calls.some((c) => c.path === '/api/machines/host/fs/home')).toBe(true)
    await choose('s-abc')
    expect(calls.some((c) => c.path === '/api/machines/s-abc/fs/home')).toBe(true)
    // The host project at the same path doesn't count on the server.
    const add = document.body.querySelector<HTMLButtonElement>('button[aria-label="Add app as project"]')
    expect(add).not.toBeNull()
    add!.click()
    await flushPromises()
    expect(calls.find((c) => c.method === 'POST' && c.path === '/api/projects')?.body).toMatchObject({ machineId: 's-abc', path: '/home/build/app' })
  })
})
