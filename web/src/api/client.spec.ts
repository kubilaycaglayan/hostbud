import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getUIState, putUIState, request, sessionsApi } from './client'
import { stubFetch } from '@/test-utils'

afterEach(() => vi.unstubAllGlobals())

describe('request', () => {
  it('sends JSON and returns the parsed body', async () => {
    const calls = stubFetch(() => ({ status: 201, body: { name: 'dev' } }))
    expect(await request('POST', '/api/x', { a: 1 })).toEqual({ name: 'dev' })
    expect(calls).toEqual([{ method: 'POST', path: '/api/x', body: { a: 1 } }])
  })

  it('returns undefined for 204', async () => {
    stubFetch(() => ({ status: 204 }))
    expect(await request('DELETE', '/api/x')).toBeUndefined()
  })

  it('throws ApiError with {error, hint} and Retry-After', async () => {
    stubFetch(() => ({ status: 429, body: { error: 'too many attempts', hint: 'wait' }, headers: { 'Retry-After': '8' } }))
    const err = await request('POST', '/api/auth/login', {}).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 429, message: 'too many attempts', hint: 'wait', retryAfter: 8 })
  })

  it('keeps a generic message when the error body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('oops', { status: 502 })))
    const err = await request('GET', '/api/x').catch((e) => e)
    expect(err).toMatchObject({ status: 502, message: 'request failed (502)', hint: undefined })
  })
})

describe('sessionsApi', () => {
  it('builds the documented routes', async () => {
    const calls = stubFetch((method) => ({ status: method === 'DELETE' ? 204 : 200, body: method === 'DELETE' ? undefined : {} }))
    await sessionsApi.machines()
    await sessionsApi.list('host')
    await sessionsApi.create('host', { path: '~/app', startCommand: 'htop' })
    await sessionsApi.rename('host', 'a', 'b')
    await sessionsApi.kill('host', 'b')
    expect(calls).toEqual([
      { method: 'GET', path: '/api/machines', body: undefined },
      { method: 'GET', path: '/api/machines/host/sessions', body: undefined },
      { method: 'POST', path: '/api/machines/host/sessions', body: { path: '~/app', startCommand: 'htop' } },
      { method: 'PATCH', path: '/api/machines/host/sessions/a', body: { name: 'b' } },
      { method: 'DELETE', path: '/api/machines/host/sessions/b', body: undefined },
    ])
  })
})

describe('UI state', () => {
  it('GET returns the stored value, or null before anything was saved', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { version: 1, tabs: [] } }))
    expect(await getUIState('layout')).toEqual({ version: 1, tabs: [] })
    expect(calls).toEqual([{ method: 'GET', path: '/api/ui-state/layout', body: undefined }])
    stubFetch(() => ({ status: 404, body: { error: 'nothing saved yet' } }))
    expect(await getUIState('layout')).toBeNull()
  })

  it('other GET errors are thrown', async () => {
    stubFetch(() => ({ status: 401, body: { error: 'sign in first' } }))
    await expect(getUIState('layout')).rejects.toMatchObject({ status: 401 })
  })

  it('PUT sends the value as JSON', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    await putUIState('layout', { version: 1 })
    expect(calls).toEqual([{ method: 'PUT', path: '/api/ui-state/layout', body: { version: 1 } }])
  })
})
