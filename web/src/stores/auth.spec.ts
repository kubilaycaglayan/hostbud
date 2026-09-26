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

  it.each([502, 503, 504])('treats gateway status %i as unreachable', async (status) => {
    stubFetch(() => ({ status, body: { error: 'unavailable' } }))
    const auth = useAuthStore()
    await auth.check()
    expect(auth.status).toBe('unreachable')
  })

  it('treats network errors and timeout as unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')))
    const auth = useAuthStore()
    await auth.check()
    expect(auth.status).toBe('unreachable')
  })

  it('shows a server error instead of the sign-in state for an unexpected 500', async () => {
    stubFetch(() => ({ status: 500, body: { error: 'broken' } }))
    const auth = useAuthStore()
    await auth.check()
    expect(auth.status).toBe('server-error')
    expect(auth.serverError).toContain('hostbud returned an error')
  })

  it('keeps a signed-in state during a reconnect auth check unless the server answers 401', async () => {
    const auth = useAuthStore()
    auth.status = 'authenticated'
    stubFetch(() => ({ status: 503, body: { error: 'unavailable' } }))
    expect(await auth.stillAuthorized()).toBe(true)
    expect(auth.status).toBe('authenticated')

    stubFetch(() => ({ status: 401, body: { error: 'sign in required' } }))
    expect(await auth.stillAuthorized()).toBe(false)
    expect(auth.status).toBe('anonymous')
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
