import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { addItem, control, createQueue, getQueue, itemOf, waitItem, newProject, override, type Queue, type QueueItem } from '../helpers/queues.ts'
import { Stubs, type StubBehavior } from '../helpers/stubs.ts'
import { shq, type Target } from '../helpers/target.ts'

// V2-M1 T9: the dispatcher end to end with the stub clients on the
// throwaway target. The injected hook command runs for real: tmux env →
// curl → hostbud-e2e-caddy's loopback site → app → adapter read → next
// session. The e2e app's stale window is 10 s.

const stubs = new Stubs()
const HOOK_URL = 'http://hostbud-e2e-caddy:9055'

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
  await stubs.reset()
})

type Setup = { agent?: 'claude' | 'codex'; condition: string; behavior: StubBehavior }

async function queueWith(request: Parameters<typeof newProject>[0], target: Target, items: Setup[]) {
  const project = await newProject(request, target, 'e2e-runs')
  const queue = await createQueue(request, project.id)
  const added: QueueItem[] = []
  for (const it of items) {
    await stubs.setBehavior(it.condition, it.behavior, 0.5)
    added.push(await addItem(request, queue.id, { agent: it.agent ?? 'claude', instruction: `/goal ${it.condition}` }))
  }
  return { project, queue, items: added }
}

function statuses(q: Queue) {
  return q.items.map((i) => i.status)
}

/** The Claude transcript of a stub run holds its top-level achieved record. */
async function achievedRecordWritten(target: Target, cwd: string, session: string): Promise<boolean> {
  const dir = `/home/dev/.claude/projects/${cwd.replace(/[^A-Za-z0-9]/g, '-')}`
  const r = await target.exec(`grep -cE '^[{]"type":"attachment","attachment":[{]"type":"goal_status",[^}]*"met":true' ${shq(`${dir}/${session}.jsonl`)}`)
  return r.code === 0 && Number(r.stdout.trim()) >= 1
}

test.describe('queue runs', () => {
  test.describe.configure({ timeout: 120_000 })

  test('(V2-M7 T2) Queue lifecycle timestamps', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { condition: 'e2e lifecycle timestamps', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const started = await waitItem(request, queue.id, items[0].id, 'running')
    expect(started.startedAt).toBeTruthy()
    expect(started.endedAt).toBeFalsy()
    expect((await getQueue(request, queue.id)).startedAt).toBeTruthy()
    const done = await waitItem(request, queue.id, items[0].id, 'done')
    expect(done.endedAt).toBeTruthy()
    await expect.poll(async () => (await getQueue(request, queue.id)).status).toBe('finished')
    const finished = await getQueue(request, queue.id)
    expect(finished.startedAt).toBeTruthy()
    expect(finished.endedAt).toBeTruthy()
  })

  test('(V2-M1 T9) Three items in order', async ({ request, target }) => {
    const { project, queue, items } = await queueWith(request, target, [
      { condition: 'e2e order m1', behavior: 'achieve:2' },
      { condition: 'e2e order m2', behavior: 'achieve:1' },
      { condition: 'e2e order m3', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    for (let n = 0; n < 3; n++) {
      const item = await waitItem(request, queue.id, items[n].id, 'running')
      const current = await getQueue(request, queue.id)
      // At most one run is active; later items are still queued.
      expect(statuses(current)).toEqual(items.map((_, i) => (i < n ? 'done' : i === n ? 'running' : 'queued')))
      const run = item.run!
      expect(run.sessionName).toBe(`${project.name}-q${n + 1}`)
      await expect.poll(() => stubs.log(run.id, 'claude')).not.toBeNull()
      const log = (await stubs.log(run.id, 'claude'))!
      expect(log.cwd).toBe(project.path)
      expect(log.env).toMatchObject({ HOSTBUD_URL: HOOK_URL, HOSTBUD_RUN_ID: run.id })
      expect(log.env.HOSTBUD_RUN_TOKEN_sha256).toMatch(/^[0-9a-f]{64}$/)
      expect(log.argv).toContain('--settings')
      expect(log.argv.at(-1)).toBe(`/goal e2e order m${n + 1}`)
      const env = await stubs.sessionEnv(run.sessionName)
      expect(env.HOSTBUD_RUN_ID).toBe(run.id)
      expect(env.HOSTBUD_URL).toBe(HOOK_URL)
      expect(env.HOSTBUD_RUN_TOKEN).toHaveLength(43)
      expect(log.argv.join(' ')).not.toContain(env.HOSTBUD_RUN_TOKEN)
      const done = await waitItem(request, queue.id, items[n].id, 'done')
      expect(done.run!.status).toBe('achieved')
      if (n < 2) {
        // The next session appears only after this stub wrote its achieved record.
        const next = await waitItem(request, queue.id, items[n + 1].id, 'running')
        await expect.poll(async () => (await target.sessions()).includes(next.run!.sessionName)).toBe(true)
        expect(await achievedRecordWritten(target, project.path, log.sessions[0])).toBe(true)
      }
    }
    await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 15_000 }).toBe('finished')
    expect(statuses(await getQueue(request, queue.id))).toEqual(['done', 'done', 'done'])
    // hostbud closed no session.
    expect(await target.sessions()).toEqual(expect.arrayContaining([1, 2, 3].map((n) => `${project.name}-q${n}`)))
  })

  test('(V2-M1 T9) Three items in order: app restart during item 2', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { condition: 'e2e restart m1', behavior: 'achieve:1' },
      { condition: 'e2e restart m2', behavior: 'slow:8' },
      { condition: 'e2e restart m3', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[1].id, 'running')
    await ctl.appRestart()
    await waitItem(request, queue.id, items[1].id, 'done', 45_000)
    await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 30_000 }).toBe('finished')
    expect(statuses(await getQueue(request, queue.id))).toEqual(['done', 'done', 'done'])
  })

  test('(V2-M1 T9) Decoy text does not advance', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { condition: 'e2e decoy m1', behavior: 'decoy' },
      { condition: 'e2e decoy m2', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const item = await waitItem(request, queue.id, items[0].id, 'running')
    await expect.poll(async () => (await itemOf(request, queue.id, items[0].id)).run?.status).toBe('running')
    // Past the stub's turn and every follow-up read (2 s and 5 s).
    await new Promise((resolve) => setTimeout(resolve, 8_000))
    const now = await getQueue(request, queue.id)
    expect(statuses(now)).toEqual(['running', 'queued'])
    expect(now.status).toBe('running')
    expect(await target.sessions()).toContain(item.run!.sessionName)
  })

  test('(V2-M1 T9) Codex camelCase goal limits keep the queue running', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { agent: 'codex', condition: 'e2e codex wire limits', behavior: 'codex-wire-limits' },
      { agent: 'codex', condition: 'e2e codex wire limits next', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'done', 45_000)
    await waitItem(request, queue.id, items[1].id, 'done', 45_000)
    await expect.poll(async () => (await getQueue(request, queue.id)).status).toBe('finished')
  })

  for (const [behavior, runStatus, detail] of [
    ['fail', 'failed', "can't be achieved"],
    ['exit', 'exited', 'ended without achieving'],
    ['silent', 'stale', 'no signal'],
    ['clear', 'exited', '/clear'],
  ] as const) {
    test(`(V2-M1 T9) Needs attention: ${behavior}`, async ({ request, target }) => {
      const { queue, items } = await queueWith(request, target, [
        { condition: `e2e attention ${behavior}`, behavior },
        { condition: `e2e attention ${behavior} next`, behavior: 'achieve:1' },
      ])
      expect((await control(request, queue.id, 'start')).status()).toBe(200)
      const item = await waitItem(request, queue.id, items[0].id, 'needs_attention', 40_000)
      expect(item.run!.status).toBe(runStatus)
      expect(item.run!.detail).toContain(detail)
      const now = await getQueue(request, queue.id)
      expect(now.status).toBe('paused')
      expect(statuses(now)).toEqual(['needs_attention', 'queued'])
      // The session keeps running: hostbud never closes it.
      expect(await target.sessions()).toContain(item.run!.sessionName)
    })
  }

  test('(V2-M1 T9) Late achieved after stale', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { condition: 'e2e late m1', behavior: 'slow:16' },
      { condition: 'e2e late m2', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const stale = await waitItem(request, queue.id, items[0].id, 'needs_attention', 30_000)
    expect(stale.run!.status).toBe('stale')
    expect((await getQueue(request, queue.id)).status).toBe('paused')
    // The long turn ends: its Stop hook still counts (a stale run keeps its
    // token); the item is done, the queue stays paused.
    const done = await waitItem(request, queue.id, items[0].id, 'done', 30_000)
    expect(done.run!.status).toBe('achieved')
    await new Promise((resolve) => setTimeout(resolve, 2_000))
    const paused = await getQueue(request, queue.id)
    expect(paused.status).toBe('paused')
    expect(statuses(paused)).toEqual(['done', 'queued'])
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    await waitItem(request, queue.id, items[1].id, 'done', 30_000)
    await expect.poll(async () => (await getQueue(request, queue.id)).status).toBe('finished')
  })

  test('(V2-M1 T9) Late achieved without a hook is seen on Resume', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { condition: 'e2e late silent m1', behavior: 'silent-then-achieve:14' },
      { condition: 'e2e late silent m2', behavior: 'achieve:1' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'needs_attention', 30_000)
    await new Promise((resolve) => setTimeout(resolve, 6_000)) // the record is written, no hook
    expect((await itemOf(request, queue.id, items[0].id)).status).toBe('needs_attention')
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'done')
    await waitItem(request, queue.id, items[1].id, 'done', 30_000)
  })

  test('(V2-M1 T9) Retry and skip', async ({ request, target }) => {
    const { project, queue, items } = await queueWith(request, target, [
      { condition: 'e2e retry m1', behavior: 'fail' },
      { condition: 'e2e retry m2', behavior: 'exit' },
      { condition: 'e2e retry m3', behavior: 'exit' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    const failed = await waitItem(request, queue.id, items[0].id, 'needs_attention')
    const oldSession = failed.run!.sessionName
    // Owner actions only on needs-attention items.
    expect((await override(request, items[1].id, 'skip')).status()).toBe(409)
    // Retry: queued again, queue stays paused, the old session stays open.
    await stubs.setBehavior('e2e retry m1', 'achieve:1')
    const retried = await override(request, items[0].id, 'retry')
    expect(retried.status(), await retried.text()).toBe(200)
    expect(((await retried.json()) as Queue).status).toBe('paused')
    expect((await itemOf(request, queue.id, items[0].id)).status).toBe('queued')
    expect(await target.sessions()).toContain(oldSession)
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    const again = await waitItem(request, queue.id, items[0].id, 'done')
    expect(again.run!.id).not.toBe(failed.run!.id)
    expect(again.run!.sessionName).toBe(`${project.name}-q1-1`)
    expect(await target.sessions()).toEqual(expect.arrayContaining([oldSession, again.run!.sessionName]))
    // Skip, then Mark done (overrides the evaluator).
    await waitItem(request, queue.id, items[1].id, 'needs_attention')
    expect((await override(request, items[1].id, 'skip')).status()).toBe(200)
    expect((await itemOf(request, queue.id, items[1].id)).status).toBe('skipped')
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    await waitItem(request, queue.id, items[2].id, 'needs_attention')
    expect((await override(request, items[2].id, 'mark-done')).status()).toBe(200)
    expect((await control(request, queue.id, 'resume')).status()).toBe(200)
    await expect.poll(async () => (await getQueue(request, queue.id)).status).toBe('finished')
    expect(statuses(await getQueue(request, queue.id))).toEqual(['done', 'skipped', 'done'])
  })

  test('(V2-M1 T9) Mixed clients', async ({ request, target }) => {
    const { queue, items } = await queueWith(request, target, [
      { agent: 'claude', condition: 'e2e mixed m1', behavior: 'achieve:1' },
      { agent: 'codex', condition: 'e2e mixed m2', behavior: 'achieve:2' },
    ])
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await waitItem(request, queue.id, items[0].id, 'done')
    const codex = await waitItem(request, queue.id, items[1].id, 'done', 40_000)
    expect(codex.run!.clientVersion).toBe('0.157.1')
    const log = (await stubs.log(codex.run!.id, 'codex'))!
    expect(log.argv.filter((a) => a.startsWith('hooks.'))).toHaveLength(3)
    expect(log.argv.join(' ')).not.toContain('notify')
    expect(log.argv.at(-1)).toBe('e2e mixed m2') // the plain condition; hostbud set the goal
    await expect.poll(async () => (await getQueue(request, queue.id)).status).toBe('finished')
  })
})
