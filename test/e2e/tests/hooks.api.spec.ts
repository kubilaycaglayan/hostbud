import type { APIRequestContext } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { FOREIGN_ORIGIN, forbidInLogs, MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { queues, runToken, type SeededRun } from '../helpers/db.ts'
import { shq, uniqueName } from '../helpers/target.ts'

// V2-M1 T3: POST /api/hooks/{run}/{event} through Caddy's loopback site. The
// bearer token is the only credential: no cookie, no Origin check.

let anon: APIRequestContext

test.beforeAll(async ({ playwright }) => {
  anon = await playwright.request.newContext({ baseURL: ORIGIN, storageState: { cookies: [], origins: [] } })
})

test.afterAll(async () => {
  await anon.dispose()
})

async function seed(request: APIRequestContext, target: { run(command: string): Promise<unknown> }, status = 'starting'): Promise<SeededRun> {
  const path = `/home/dev/${uniqueName('e2e-hooks')}`
  await target.run(`mkdir -p ${shq(path)}`)
  const project = await mutate(request, 'POST', '/api/projects', { machineId: MACHINE, path, name: uniqueName('hooks-project') }, ORIGIN)
  expect(project.status(), await project.text()).toBe(201)
  const seeded = await queues.seedRun((await project.json() as { id: string }).id, status)
  forbidInLogs(seeded.token)
  return seeded
}

function hook(run: string, event: string, token: string | null, data: string, headers: Record<string, string> = {}) {
  return anon.post(`/api/hooks/${run}/${event}`, {
    data,
    headers: { 'Content-Type': 'application/json', ...(token === null ? {} : { Authorization: `Bearer ${token}` }), ...headers },
  })
}

const BODY = JSON.stringify({ session_id: 'e2e-session', transcript_path: '/home/dev/.claude/projects/e2e/e2e-session.jsonl', cwd: '/home/dev', hook_event_name: 'SessionStart' })

test('(V2-M1 T3) Hook endpoint: token-only contract through Caddy', async ({ request, target }) => {
  const run = await seed(request, target)
  const other = await seed(request, target, 'running')

  // Accepted without a cookie; a foreign Origin doesn't matter.
  expect((await hook(run.runId, 'session_start', run.token, BODY)).status()).toBe(204)
  expect((await hook(run.runId, 'turn_end', run.token, BODY, { Origin: FOREIGN_ORIGIN })).status()).toBe(204)
  expect(await queues.events(run.runId)).toEqual([
    { source: 'hook', kind: 'session_start', payload_json: BODY },
    { source: 'hook', kind: 'turn_end', payload_json: BODY },
  ])

  // Wrong, missing and another run's token → 401; nothing is recorded.
  expect((await hook(run.runId, 'turn_end', runToken(), BODY)).status()).toBe(401)
  expect((await hook(run.runId, 'turn_end', null, BODY)).status()).toBe(401)
  expect((await hook(run.runId, 'turn_end', other.token, BODY)).status()).toBe(401)
  // Unknown run or event → 404.
  expect((await hook('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'turn_end', run.token, BODY)).status()).toBe(404)
  expect((await hook(run.runId, 'notify', run.token, BODY)).status()).toBe(404)
  // A body over 64 KiB → 413; not JSON → 400.
  expect((await hook(run.runId, 'turn_end', run.token, JSON.stringify({ a: 'x'.repeat(65 << 10) }))).status()).toBe(413)
  expect((await hook(run.runId, 'turn_end', run.token, 'not json', { 'Content-Type': 'text/plain' })).status()).toBe(400)
  expect(await queues.events(run.runId)).toHaveLength(2)

  // The response carries no cookie and isn't cached.
  const accepted = await hook(run.runId, 'session_end', run.token, BODY)
  expect(accepted.status()).toBe(204)
  expect(accepted.headers()['set-cookie']).toBeUndefined()
  expect(accepted.headers()['cache-control']).toBe('no-store')
})

test('(V2-M1 T3) Hook endpoint: ended runs answer 410, stale runs still 204', async ({ request, target }) => {
  for (const status of ['achieved', 'failed', 'exited', 'cancelled']) {
    const ended = await seed(request, target, status)
    expect((await hook(ended.runId, 'turn_end', ended.token, BODY)).status(), status).toBe(410)
    // A wrong token is still 401 before the run's state is revealed.
    expect((await hook(ended.runId, 'turn_end', runToken(), BODY)).status(), status).toBe(401)
  }
  const stale = await seed(request, target, 'stale')
  expect((await hook(stale.runId, 'turn_end', stale.token, BODY)).status()).toBe(204)
})

test('(V2-M1 T3) Hook endpoint: a burst over the per-run limit gets 429', async ({ request, target }) => {
  const run = await seed(request, target, 'running')
  const other = await seed(request, target, 'running')
  const statuses: number[] = []
  for (let i = 0; i < 25; i++) statuses.push((await hook(run.runId, 'turn_end', run.token, '{}')).status())
  expect(statuses.slice(0, 20)).toEqual(Array(20).fill(204))
  expect(statuses.slice(20)).toContain(429)
  // A refused call records nothing; other runs have their own budget.
  expect((await queues.events(run.runId)).length).toBe(statuses.filter((s) => s === 204).length)
  expect((await hook(other.runId, 'turn_end', other.token, '{}')).status()).toBe(204)
})
