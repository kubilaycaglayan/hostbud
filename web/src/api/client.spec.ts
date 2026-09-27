import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, copyModeApi, getUIState, projectsApi, putUIState, request, sessionsApi, windowsApi } from './client'
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

describe('copyModeApi', () => {
  it('uses the authenticated session copy-mode route with bounded line actions', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { inMode: true, scrollPosition: 18, historySize: 220 } }))
    expect(await copyModeApi.action('host', 'a-name', 'scroll-up', 12)).toEqual({ inMode: true, scrollPosition: 18, historySize: 220 })
    expect(calls).toEqual([{
      method: 'POST',
      path: '/api/machines/host/sessions/a-name/copy-mode',
      body: { action: 'scroll-up', lines: 12 },
    }])
  })
})

describe('windowsApi', () => {
  it('uses the window listing and selection routes with encoded ids', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { windows: [], truncated: false } }))
    await windowsApi.list('host', 'a session')
    await windowsApi.select('host', 'a session', '@2', '%3')
    expect(calls).toEqual([
      { method: 'GET', path: '/api/machines/host/sessions/a%20session/windows', body: undefined },
      { method: 'POST', path: '/api/machines/host/sessions/a%20session/select', body: { window: '@2', pane: '%3' } },
    ])
  })
})

describe('projectsApi', () => {
  it('loads recent commands for one project and submits command text through project session creation', async () => {
    const calls = stubFetch((method) => ({ status: method === 'POST' ? 201 : 200, body: method === 'GET' ? { commands: ['make test'] } : { name: 'work' } }))
    expect(await projectsApi.recentCommands('project-a')).toEqual({ commands: ['make test'] })
    await projectsApi.createSession('project-a', { startCommand: `printf '$HOME; λ'` })
    expect(calls).toEqual([
      { method: 'GET', path: '/api/projects/project-a/recent-commands', body: undefined },
      { method: 'POST', path: '/api/projects/project-a/sessions', body: { startCommand: `printf '$HOME; λ'` } },
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
