import { appendFileSync, mkdirSync } from 'node:fs'
import { request as httpRequest } from 'node:http'
import { request as httpsRequest } from 'node:https'
import { randomBytes } from 'node:crypto'
import type { Duplex } from 'node:stream'
import type { APIRequestContext } from '@playwright/test'

// The allowed Origin for the port-forward path (the page's own origin).
export const ORIGIN = 'http://localhost:9055'
export const FOREIGN_ORIGIN = 'http://evil.example.com'
// The domain path: HOSTBUD_DOMAIN of the e2e app, served over HTTPS by the
// e2e Caddy with a certificate from its internal CA.
export const DOMAIN = 'hostbud.example.test'
export const DOMAIN_URL = `https://${DOMAIN}`
export const TS_DOMAIN = 'hostbud-ts.example.test'
export const TS_DOMAIN_URL = `https://${TS_DOMAIN}`
export const TS_LOOPBACK_URL = 'http://localhost:9057'
export const MACHINE = 'host'
// HOSTBUD_POLL_INTERVAL in compose.yml.
export const POLL_INTERVAL_MS = 1_000

/** Read and write opaque per-account UI state through the authenticated API. */
export async function getUIState(request: APIRequestContext, key: 'layout' | 'tree' | 'theme'): Promise<unknown> {
  const res = await request.get(`/api/ui-state/${key}`)
  if (res.status() === 404) return null
  if (!res.ok()) throw new Error(`GET UI state ${key}: ${res.status()} ${await res.text()}`)
  return await res.json()
}

export async function putUIState(request: APIRequestContext, key: 'layout' | 'tree' | 'theme', value: unknown): Promise<void> {
  const res = await mutate(request, 'PUT', `/api/ui-state/${key}`, value)
  if (!res.ok()) throw new Error(`PUT UI state ${key}: ${res.status()} ${await res.text()}`)
}

export interface Session {
  id: string
  name: string
  path: string
  attached: number
  windows: number
}

export async function listSessions(request: APIRequestContext): Promise<Session[]> {
  const res = await request.get(`/api/machines/${MACHINE}/sessions`)
  if (!res.ok()) throw new Error(`GET sessions: ${res.status()} ${await res.text()}`)
  return (await res.json()).sessions
}

// V2-M2: the multi app's loopback site (HOSTBUD_PARALLEL_QUEUES=true).
export const MULTI_URL = 'http://localhost:9058'

// The allowed Origin of request contexts made for another app (the multi
// app); everything else is the loopback site's.
const requestOrigins = new WeakMap<APIRequestContext, string>()

/** Registers the Origin mutate() sends for this request context. */
export function useOrigin(request: APIRequestContext, origin: string): APIRequestContext {
  requestOrigins.set(request, origin)
  return request
}

/** State-changing request with the page's Origin, like the UI sends. */
export function mutate(
  request: APIRequestContext,
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  data?: unknown,
  origin = requestOrigins.get(request) ?? ORIGIN,
) {
  return request.fetch(path, { method, data, headers: { Origin: origin } })
}

/**
 * Records a value (path, command, marker) that must never show up in the
 * app's info-level logs; run.sh checks them after the run ("Logs clean").
 */
export function forbidInLogs(...values: string[]): void {
  mkdirSync('results', { recursive: true })
  appendFileSync('results/log-markers.txt', values.map((v) => v + '\n').join(''))
}

/** Raw WebSocket upgrade with an arbitrary Origin (and optional Cookie
 * header) to `base` (the loopback site unless given); resolves the HTTP
 * status. The domain's internal-CA certificate isn't verified. */
export function upgradeStatus(path: string, origin: string, cookie?: string, base = ORIGIN): Promise<number> {
  const url = new URL(path, base)
  const request = url.protocol === 'https:' ? httpsRequest : httpRequest
  return new Promise((resolve, reject) => {
    const req = request({
      host: url.hostname,
      port: url.port || (url.protocol === 'https:' ? 443 : 80),
      path: url.pathname + url.search,
      rejectUnauthorized: false,
      headers: {
        Connection: 'Upgrade',
        Upgrade: 'websocket',
        'Sec-WebSocket-Version': '13',
        'Sec-WebSocket-Key': randomBytes(16).toString('base64'),
        Origin: origin,
        ...(cookie ? { Cookie: cookie } : {}),
      },
    })
    req.on('response', (res) => {
      res.resume()
      resolve(res.statusCode ?? 0)
    })
    req.on('upgrade', (_res, socket) => {
      socket.destroy()
      resolve(101)
    })
    req.on('error', reject)
    req.end()
  })
}

/** Upgrades and leaves the raw socket open for API level terminal scenarios. */
export function upgradeSocket(path: string, origin: string, cookie: string, base = ORIGIN): Promise<{ status: number; socket?: Duplex; body?: string }> {
  const url = new URL(path, base)
  const request = url.protocol === 'https:' ? httpsRequest : httpRequest
  return new Promise((resolve, reject) => {
    const req = request({
      host: url.hostname,
      port: url.port || (url.protocol === 'https:' ? 443 : 80),
      path: url.pathname + url.search,
      rejectUnauthorized: false,
      headers: {
        Connection: 'Upgrade', Upgrade: 'websocket', 'Sec-WebSocket-Version': '13',
        'Sec-WebSocket-Key': randomBytes(16).toString('base64'), Origin: origin, Cookie: cookie,
      },
    })
    req.on('response', (res) => {
      const chunks: Buffer[] = []
      res.on('data', (chunk: Buffer) => chunks.push(chunk))
      res.on('end', () => resolve({ status: res.statusCode ?? 0, body: Buffer.concat(chunks).toString() }))
    })
    req.on('upgrade', (res, socket) => {
      socket.pause()
      resolve({ status: res.statusCode ?? 101, socket })
    })
    req.on('error', reject)
    req.end()
  })
}
