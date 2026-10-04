import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ServersDialog from './ServersDialog.vue'
import type { Machine } from '@/api/types'
import { useMachinesStore } from '@/stores/machines'
import { stubFetch } from '@/test-utils'

const hostMachine: Machine = { id: 'host', label: 'Host', source: 'host', status: 'ok', os: 'Linux', home: '/home/dev', tmuxVersion: 'tmux 3.4', tmuxMissing: false }
const server: Machine = { id: 's-0011223344', label: 'Build box', source: 'custom', address: 'dev@server-a.example.com:22', status: 'unreachable', error: 'the server refused hostbud\'s SSH key (permission denied)', hint: 'Add the public key', os: '', home: '', tmuxVersion: '', tmuxMissing: false }
const scanned = { hostKeys: [{ type: 'ssh-ed25519', key: 'AAAAC3', fingerprint: 'SHA256:abcdef' }] }

beforeEach(() => {
  setActivePinia(createPinia())
  useMachinesStore().machines = [hostMachine, server]
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const $$ = (sel: string) => [...document.body.querySelectorAll<HTMLElement>(sel)]
const button = (label: string) => $$('button').find((b) => b.textContent?.trim() === label)
const field = (name: string) => document.body.querySelector<HTMLInputElement>(`input[name="${name}"]`)!
function type(name: string, value: string) {
  field(name).value = value
  field(name).dispatchEvent(new Event('input'))
}

async function mountDialog(route: Parameters<typeof stubFetch>[0]) {
  const calls = stubFetch(route)
  mount(ServersDialog, { props: { open: true }, attachTo: document.body })
  await flushPromises()
  return calls
}

describe('ServersDialog (V2-M13 T3)', () => {
  it('lists the host and servers with their status and hint', async () => {
    await mountDialog(() => ({ status: 200 }))
    const rows = $$('[data-testid="server-row"]').map((r) => r.textContent ?? '')
    expect(rows[0]).toContain('Host')
    expect(rows[0]).toContain('(this host)')
    expect(rows[1]).toContain('Build box')
    expect(rows[1]).toContain('dev@server-a.example.com:22')
    expect(rows[1]).toContain('permission denied')
    expect(rows[1]).toContain('Add the public key')
    // Only servers can be removed, never the host.
    expect($$('button[aria-label^="Remove "]').map((b) => b.getAttribute('aria-label'))).toEqual(['Remove Build box'])
  })

  it('adds a server only after its host-key fingerprints are shown and trusted', async () => {
    const calls = await mountDialog((method, path) => {
      if (path === '/api/machines/scan') return { status: 200, body: scanned }
      if (method === 'POST' && path === '/api/machines') return { status: 201, body: { ...server, id: 's-new', status: 'unknown' } }
      return { status: 404 }
    })
    type('label', 'Build box 2')
    type('host', ' server-b.example.com ')
    type('port', '2222')
    type('user', 'dev')
    button('Check host key')!.click()
    await flushPromises()
    expect(calls.find((c) => c.path === '/api/machines/scan')?.body).toEqual({ host: 'server-b.example.com', port: 2222 })
    expect(calls.some((c) => c.path === '/api/machines' && c.method === 'POST')).toBe(false)
    expect($$('[data-testid="fingerprint"]').map((f) => f.textContent?.trim())).toEqual(['ssh-ed25519 SHA256:abcdef'])

    // Changing the port invalidates the scan: the keys belong to the old target.
    type('port', '22')
    await flushPromises()
    expect($$('[data-testid="fingerprint"]')).toHaveLength(0)
    expect(button('Trust and add')).toBeUndefined()
    type('port', '2222')
    await flushPromises()

    button('Trust and add')!.click()
    await flushPromises()
    expect(calls.find((c) => c.path === '/api/machines' && c.method === 'POST')?.body).toEqual({
      label: 'Build box 2', host: 'server-b.example.com', port: 2222, user: 'dev', hostKeys: [{ type: 'ssh-ed25519', key: 'AAAAC3' }],
    })
    expect(field('host').value).toBe('')
  })

  it('shows scan and add errors with their hints', async () => {
    let scanStatus = 502
    await mountDialog((_m, path) => path === '/api/machines/scan'
      ? (scanStatus === 502 ? { status: 502, body: { error: 'the server sent no SSH host keys', hint: 'Check the host name and port' } } : { status: 200, body: scanned })
      : { status: 409, body: { error: 'another server already has this nickname', hint: 'Pick a different nickname.' } })
    button('Check host key')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain("Enter the server's host name")
    type('host', 'server-a')
    type('port', '99999')
    button('Check host key')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain('Use a port from 1 to 65535.')
    type('port', '22')
    button('Check host key')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain('he server sent no SSH host keys')
    expect(document.body.textContent).toContain('Check the host name and port')
    scanStatus = 200
    button('Check host key')!.click()
    await flushPromises()
    button('Trust and add')!.click()
    await flushPromises()
    expect(document.body.textContent).toContain('nother server already has this nickname')
  })

  it('removes a server only after the in-app confirmation', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm')
    const calls = await mountDialog(() => ({ status: 204 }))
    $$('button[aria-label="Remove Build box"]')[0].click()
    await flushPromises()
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain('Remove Build box?')
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain('tmux sessions keep running')
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Cancel')!.click()
    await flushPromises()
    expect(calls.filter((c) => c.method === 'DELETE')).toEqual([])
    $$('button[aria-label="Remove Build box"]')[0].click()
    await flushPromises()
    $$('[role="alertdialog"] button').find((b) => b.textContent?.trim() === 'Remove server')!.click()
    await flushPromises()
    expect(calls.filter((c) => c.method === 'DELETE').map((c) => c.path)).toEqual(['/api/machines/s-0011223344'])
    expect(confirmSpy).not.toHaveBeenCalled()
  })
})

describe('machines and sessions stores on machine.removed', () => {
  it('drops the machine and its sessions', async () => {
    const { applyMachines } = await import('@/stores/machines')
    const { applySessions } = await import('@/stores/sessions')
    const e = { type: 'machine.removed' as const, machine: server.id, payload: { id: server.id } }
    expect(applyMachines([hostMachine, server], e).map((m) => m.id)).toEqual(['host'])
    expect(Object.keys(applySessions({ host: [], [server.id]: [] }, e))).toEqual(['host'])
  })
})
