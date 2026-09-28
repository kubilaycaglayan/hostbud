import { createDecipheriv, createECDH, hkdfSync, randomBytes, type ECDH } from 'node:crypto'
import type { APIRequestContext } from '@playwright/test'
import { mutate } from './api.ts'

// V2-M3 Web Push against hostbud-e2e-pushfake: devices are key pairs the
// scenario holds (like a browser's), subscribed through the API; bodies are
// decrypted here (RFC 8291 aes128gcm) as the browser would.

export const PUSHFAKE = 'http://hostbud-e2e-pushfake:8080'

export interface Device {
  id: string
  endpoint: string
  path: string
  p256dh: string
  auth: string
  ecdh: ECDH
  secret: Buffer
}

export function newDevice(): Device {
  const ecdh = createECDH('prime256v1')
  ecdh.generateKeys()
  const secret = randomBytes(16)
  const id = randomBytes(6).toString('hex')
  const path = `/push/${id}`
  return {
    id, path, endpoint: PUSHFAKE + path, ecdh, secret,
    p256dh: ecdh.getPublicKey().toString('base64url'), auth: secret.toString('base64url'),
  }
}

export async function subscribe(request: APIRequestContext, d: Device, origin?: string) {
  return await mutate(request, 'POST', '/api/notifications/subscriptions', { endpoint: d.endpoint, keys: { p256dh: d.p256dh, auth: d.auth } }, origin)
}

export async function unsubscribe(request: APIRequestContext, d: Device) {
  return await mutate(request, 'DELETE', '/api/notifications/subscriptions', { endpoint: d.endpoint })
}

export interface PushPost {
  path: string
  headers: Record<string, string>
  body: string // base64
}

export const pushfake = {
  async posts(path?: string): Promise<PushPost[]> {
    const res = await fetch(`${PUSHFAKE}/ctl/posts${path ? `?path=${encodeURIComponent(path)}` : ''}`)
    return await res.json() as PushPost[]
  },
  async answer(path: string, statuses: number[], delayMs = 0): Promise<void> {
    const res = await fetch(`${PUSHFAKE}/ctl/answer`, { method: 'POST', body: JSON.stringify({ path, statuses, delayMs }) })
    if (!res.ok) throw new Error(`pushfake answer: ${res.status}`)
  },
  async reset(): Promise<void> {
    await fetch(`${PUSHFAKE}/ctl/reset`, { method: 'POST' })
  },
}

/** Decrypts one push body for the device (RFC 8291) and parses its JSON. */
export function decrypt(d: Device, body64: string): Record<string, unknown> {
  const body = Buffer.from(body64, 'base64')
  const salt = body.subarray(0, 16)
  const idlen = body[20]
  const serverPublic = body.subarray(21, 21 + idlen)
  const ciphertext = body.subarray(21 + idlen)
  const shared = d.ecdh.computeSecret(serverPublic)
  const info = Buffer.concat([Buffer.from('WebPush: info\0'), d.ecdh.getPublicKey(), serverPublic])
  const ikm = Buffer.from(hkdfSync('sha256', shared, d.secret, info, 32))
  const cek = Buffer.from(hkdfSync('sha256', ikm, salt, Buffer.from('Content-Encoding: aes128gcm\0'), 16))
  const nonce = Buffer.from(hkdfSync('sha256', ikm, salt, Buffer.from('Content-Encoding: nonce\0'), 12))
  const decipher = createDecipheriv('aes-128-gcm', cek, nonce)
  decipher.setAuthTag(ciphertext.subarray(ciphertext.length - 16))
  const plain = Buffer.concat([decipher.update(ciphertext.subarray(0, ciphertext.length - 16)), decipher.final()])
  const end = plain.lastIndexOf(2)
  return JSON.parse(plain.subarray(0, end).toString('utf8')) as Record<string, unknown>
}

/** The decrypted payloads a device received. */
export async function received(d: Device): Promise<Record<string, unknown>[]> {
  return (await pushfake.posts(d.path)).map((p) => decrypt(d, p.body))
}
