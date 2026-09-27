import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, copyModeApi, filesystemApi, getUIState, projectsApi, putUIState, request, runtimeApi, sessionsApi, setExecTimeoutMs, setSftpTimeoutMs, setUploadTimeoutMs, windowsApi } from './client'
import { stubFetch } from '@/test-utils'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  setExecTimeoutMs(10_000)
  setSftpTimeoutMs(10_000)
  setUploadTimeoutMs(300_000)
})

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

  it('maps an aborted remote request to an actionable timeout error', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new DOMException('timed out', 'TimeoutError') }))
    await expect(request('POST', '/api/machines/host/sessions', {})).rejects.toMatchObject({ status: 504, message: "hostbud didn't answer" })
  })
})

describe('runtime limits', () => {
  it('uses the server exec timeout plus five seconds for tmux-backed requests', async () => {
    const timeout = vi.spyOn(AbortSignal, 'timeout')
    stubFetch((_method, path) => ({ status: 200, body: path === '/api/runtime/limits' ? { execTimeoutMs: 25_000, sftpTimeoutMs: 15_000, uploadTimeoutMs: 45_000 } : { name: 'work' } }))
    await runtimeApi.configureExecTimeout()
    await sessionsApi.create('host', { name: 'work' })
    expect(timeout).toHaveBeenCalledWith(30_000)
    timeout.mockClear()
    await filesystemApi.list('host', '/home/dev', false)
    expect(timeout).toHaveBeenCalledWith(20_000)
    timeout.mockClear()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ path: '/home/dev/a.heic', size: 1 }), { status: 201 })))
    await filesystemApi.uploadPhoto('host', '/home/dev/repo', new File([new Uint8Array([255])], 'a.heic'))
    expect(timeout).toHaveBeenCalledWith(50_000)
  })

  it('uploads the selected File directly as opaque octet-stream bytes', async () => {
    const bytes = new Uint8Array([0, 255, 216, 0, 128, 10])
    const file = new File([bytes], 'IMG 1234.HEIC', { type: 'image/heic', lastModified: 7 })
    let uploadRequest: { url: string; init?: RequestInit } | undefined
    vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      uploadRequest = { url: String(url), init }
      return new Response(JSON.stringify({ path: '/home/dev/repo/IMG 1234.HEIC', size: bytes.length }), { status: 201 })
    }))
    expect(await filesystemApi.uploadPhoto('host', '/home/dev/repo', file)).toEqual({ path: '/home/dev/repo/IMG 1234.HEIC', size: bytes.length })
    expect(uploadRequest?.url).toBe('/api/machines/host/fs/upload?directory=%2Fhome%2Fdev%2Frepo&name=IMG%201234.HEIC')
    expect(uploadRequest?.init).toMatchObject({ method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/octet-stream' }, body: file })
    expect([...new Uint8Array(await (uploadRequest?.init?.body as Blob).arrayBuffer())]).toEqual([...bytes])
  })

  it('adds the next number on a filename conflict without changing photo bytes', async () => {
    const file = new File([new Uint8Array([0, 255, 9])], 'IMG_1234.HEIC', { type: 'image/heic' })
    const requests: { url: string; body: BodyInit | null | undefined }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      requests.push({ url: String(url), body: init?.body })
      if (requests.length === 1) return new Response(JSON.stringify({ error: 'a file or directory with that name already exists' }), { status: 409 })
      return new Response(JSON.stringify({ path: '/home/dev/repo/IMG_1234-1.HEIC', size: file.size }), { status: 201 })
    }))
    const result = await filesystemApi.uploadPhotoUnique('host', '/home/dev/repo', file)
    expect(result.path).toBe('/home/dev/repo/IMG_1234-1.HEIC')
    expect(requests.map((request) => request.url)).toEqual([
      '/api/machines/host/fs/upload?directory=%2Fhome%2Fdev%2Frepo&name=IMG_1234.HEIC',
      '/api/machines/host/fs/upload?directory=%2Fhome%2Fdev%2Frepo&name=IMG_1234-1.HEIC',
    ])
    expect(requests.map((request) => request.body)).toEqual([file, file])
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
