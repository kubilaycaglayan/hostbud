import { expect, test } from '../helpers/fixtures.ts'
import { mutate, ORIGIN, FOREIGN_ORIGIN } from '../helpers/api.ts'

test('Window nudge API configures the singleton and arms only a future UTC+03:00 slot', async ({ request }) => {
  const initial = await request.get('/api/window-nudge')
  expect(initial.status()).toBe(200)
  expect(await initial.json()).toMatchObject({ enabled: false, machineId: 'host', provider: 'codex', schedule: '01-06-11-16-21' })

  const schedule = await mutate(request, 'PUT', '/api/window-nudge', {
    enabled: true, machineId: 'host', directory: '/home/dev', provider: 'claude', schedule: '04-09-14-19-00',
  }, ORIGIN)
  expect(schedule.status(), await schedule.text()).toBe(200)
  const saved = await schedule.json() as { enabled: boolean; nextRunAt: string; schedule: string; provider: string }
  expect(saved).toMatchObject({ enabled: true, schedule: '04-09-14-19-00', provider: 'claude' })
  expect(Date.parse(saved.nextRunAt)).toBeGreaterThan(Date.now())

  const skipped = await mutate(request, 'PUT', '/api/window-nudge', {
    enabled: true, machineId: 'host', directory: '/home/dev', provider: 'claude', schedule: '04-09-14-19-00', skipFirst: true,
  }, ORIGIN)
  expect(skipped.status(), await skipped.text()).toBe(200)
  const skippedSettings = await skipped.json() as { nextRunAt: string; skipFirst?: boolean }
  expect(skippedSettings).not.toHaveProperty('skipFirst')
  expect(Date.parse(skippedSettings.nextRunAt)).toBeGreaterThan(Date.parse(saved.nextRunAt))

  expect((await mutate(request, 'PUT', '/api/window-nudge', {
    enabled: false, machineId: 'host', directory: '/home/dev', provider: 'claude', schedule: '04-09-14-19-00',
  }, FOREIGN_ORIGIN)).status()).toBe(403)
  const unauthorized = await request.get('/api/window-nudge', { headers: { Cookie: '' } })
  expect(unauthorized.status()).toBe(401)
  const disabled = await mutate(request, 'PUT', '/api/window-nudge', {
    enabled: false, machineId: 'host', directory: '/home/dev', provider: 'claude', schedule: '04-09-14-19-00',
  }, ORIGIN)
  expect(disabled.status(), await disabled.text()).toBe(200)
  expect((await disabled.json()).enabled).toBe(false)
})
