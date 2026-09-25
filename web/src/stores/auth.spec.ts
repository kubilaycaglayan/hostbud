import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from './auth'
import { stubFetch } from '@/test-utils'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

describe('auth store', () => {
  it('is anonymous when /me answers 401', async () => {
    stubFetch(() => ({ status: 401, body: { error: 'sign in required' } }))
    const auth = useAuthStore()
    await auth.check()
    expect(auth.status).toBe('anonymous')
  })

  it('is authenticated with the account email', async () => {
    stubFetch(() => ({ status: 200, body: { email: 'person@example.com' } }))
    const auth = useAuthStore()
    await auth.check()
    expect(auth.status).toBe('authenticated')
    expect(auth.email).toBe('person@example.com')
  })

  it('registers, then signs in', async () => {
    const calls = stubFetch((method, path) =>
      path === '/api/auth/me' ? { status: 200, body: { email: 'person@example.com' } } : { status: method === 'POST' && path.endsWith('register') ? 201 : 200, body: {} },
    )
    const auth = useAuthStore()
    await auth.register('person@example.com', 'long enough pw')
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([
      'POST /api/auth/register',
      'POST /api/auth/login',
      'GET /api/auth/me',
    ])
    expect(auth.status).toBe('authenticated')
  })

  it('logs out even if the request fails', async () => {
    stubFetch(() => ({ status: 500, body: { error: 'x' } }))
    const auth = useAuthStore()
    auth.status = 'authenticated'
    await auth.logout().catch(() => {})
    expect(auth.status).toBe('anonymous')
  })
})
