import { readFileSync } from 'node:fs'
import { FOREIGN_ORIGIN, ORIGIN, upgradeStatus } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'

interface RouteInfo {
  method: string
  path: string
  stateChanging: boolean
  websocket: boolean
  jsonBody: boolean
  token_auth?: boolean
}

const routes = JSON.parse(readFileSync('/e2e/routes.json', 'utf8')) as RouteInfo[]
const pathFor = (path: string) =>
  path.replaceAll('{machine}', 'host').replaceAll('{name}', 'origin-check').replaceAll('{id}', 'missing').replaceAll('{key}', 'layout')
    .replaceAll('{run}', '01ARZ3NDEKTSV4RRFFQ69G5FAV').replaceAll('{event}', 'turn_end')

test('(T12) Origin allowlist covers every state-changing route and WebSocket', async ({ request }) => {
  const cookie = `hostbud_session=${(await request.storageState()).cookies.find((c) => c.name === 'hostbud_session')?.value ?? ''}`
  const state = async (path: string) => {
    if (path.includes('/sessions')) return await request.get('/api/machines/host/sessions')
    if (path.startsWith('/api/projects')) return await request.get('/api/projects')
    if (path.startsWith('/api/ui-state/')) return await request.get(path)
    if (path.startsWith('/api/auth/')) return await request.get('/api/auth/me')
    return null
  }

  for (const route of routes.filter((entry) => entry.stateChanging || entry.websocket)) {
    const path = pathFor(route.path)
    if (route.token_auth) {
      // V2-M1 T3: run hooks are token-authenticated; the Origin check doesn't
      // apply (an unknown run answers 404 whatever the Origin).
      for (const origin of [FOREIGN_ORIGIN, undefined]) {
        const response = await request.fetch(path, { method: route.method, headers: origin ? { Origin: origin } : {}, data: '{}' })
        expect(response.status(), `${route.method} ${route.path} Origin ${origin}`).toBe(404)
      }
      continue
    }
    const before = route.stateChanging ? await state(path) : null
    const beforeBody = before ? await before.text() : ''
    if (route.websocket) {
      expect(await upgradeStatus(path, FOREIGN_ORIGIN, cookie), `${route.path} foreign Origin`).toBe(403)
      for (const origin of [ORIGIN, 'https://hostbud.example.test']) {
        expect(await upgradeStatus(path, origin, cookie), `${route.path} allowed Origin ${origin}`).not.toBe(403)
      }
      continue
    }

    const headers = { Origin: FOREIGN_ORIGIN, Cookie: cookie }
    const response = await request.fetch(path, {
      method: route.method,
      headers,
      ...(route.jsonBody ? { data: {} } : {}),
    })
    expect(response.status(), `${route.method} ${route.path} foreign Origin`).toBe(403)
    const missingOrigin = await request.fetch(path, {
      method: route.method,
      headers: { Cookie: cookie },
      ...(route.jsonBody ? { data: {} } : {}),
    })
    expect(missingOrigin.status(), `${route.method} ${route.path} missing Origin`).toBe(403)

    const after = await state(path)
    if (before && after) expect(await after.text(), `${route.method} ${route.path} side effect`).toBe(beforeBody)

    // An invalid session cookie lets the request pass Origin middleware while
    // ensuring auth rejects it before any mutation can occur.
    for (const origin of [ORIGIN, 'https://hostbud.example.test']) {
      const allowed = await request.fetch(path, {
        method: route.method,
        headers: { Origin: origin, Cookie: 'hostbud_session=invalid' },
        ...(route.jsonBody ? { data: {} } : {}),
      })
      expect(allowed.status(), `${route.method} ${route.path} allowed Origin ${origin}`).not.toBe(403)
    }
  }
})
