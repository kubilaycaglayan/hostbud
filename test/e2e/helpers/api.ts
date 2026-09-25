import { appendFileSync, mkdirSync } from 'node:fs'
import { request as httpRequest } from 'node:http'
import { randomBytes } from 'node:crypto'
import type { APIRequestContext } from '@playwright/test'

// The allowed Origin for the port-forward path (the page's own origin).
export const ORIGIN = 'http://localhost:9055'
export const FOREIGN_ORIGIN = 'http://evil.example.com'
export const MACHINE = 'host'
// HOSTBUD_POLL_INTERVAL in compose.yml.
export const POLL_INTERVAL_MS = 1_000

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

/** State-changing request with the page's Origin, like the UI sends. */
export function mutate(
  request: APIRequestContext,
  method: 'POST' | 'PATCH' | 'DELETE',
  path: string,
  data?: unknown,
  origin = ORIGIN,
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

/** Raw WebSocket upgrade with an arbitrary Origin; resolves the HTTP status. */
export function upgradeStatus(path: string, origin: string): Promise<number> {
  return new Promise((resolve, reject) => {
    const req = httpRequest({
      host: 'localhost',
      port: 9055,
      path,
      headers: {
        Connection: 'Upgrade',
        Upgrade: 'websocket',
        'Sec-WebSocket-Version': '13',
        'Sec-WebSocket-Key': randomBytes(16).toString('base64'),
        Origin: origin,
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
