import type { APIRequestContext } from '@playwright/test'
import { expect } from '@playwright/test'
import { mutate } from './api.ts'
import { Target } from './target.ts'

// V2-M13: the second e2e server (its own sshd and tmux; same user and keys).
export const SERVER_HOST = 'hostbud-e2e-target2'
export const server = new Target(SERVER_HOST)

export interface Machine {
  id: string
  label: string
  source?: string
  address?: string
  status: string
  error?: string
}

export interface HostKey { type: string; key: string; fingerprint: string }

export async function listMachines(request: APIRequestContext): Promise<Machine[]> {
  const res = await request.get('/api/machines')
  expect(res.status()).toBe(200)
  return (await res.json()).machines
}

/** Scans and adds the second server with its confirmed keys; returns it. */
export async function addServer(request: APIRequestContext, label: string): Promise<Machine> {
  const scan = await mutate(request, 'POST', '/api/machines/scan', { host: SERVER_HOST, port: 22 })
  expect(scan.status(), await scan.text()).toBe(200)
  const keys: HostKey[] = (await scan.json()).hostKeys
  expect(keys.length).toBeGreaterThan(0)
  const res = await mutate(request, 'POST', '/api/machines', {
    label, host: SERVER_HOST, port: 22, user: 'dev', hostKeys: keys.map(({ type, key }) => ({ type, key })),
  })
  expect(res.status(), await res.text()).toBe(201)
  return await res.json()
}

/** Waits until the machine reports status. */
export async function waitForStatus(request: APIRequestContext, id: string, status: string): Promise<void> {
  await expect.poll(async () => (await listMachines(request)).find((m) => m.id === id)?.status, { timeout: 20_000 }).toBe(status)
}

/** Removes every added server and its projects (test cleanup). */
export async function removeServers(request: APIRequestContext): Promise<void> {
  const servers = (await listMachines(request)).filter((m) => m.source === 'custom')
  for (const m of servers) {
    const projects = await request.get('/api/projects', { params: { machine: m.id } })
    if (projects.ok()) {
      for (const p of (await projects.json()).projects as { id: string }[]) await mutate(request, 'DELETE', `/api/projects/${p.id}`)
    }
    await mutate(request, 'DELETE', `/api/machines/${m.id}`)
  }
}
