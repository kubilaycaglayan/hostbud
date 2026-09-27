import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'
import { MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

test('(T2) stalled tmux returns a bounded timeout and inventory recovers', async ({ request }) => {
  const name = uniqueName('e2e-timeout')
  await ctl.stallTmux()
  try {
    const res = await mutate(request, 'POST', `/api/machines/${MACHINE}/sessions`, {
      name, path: '/home/dev', env: {}, startCommand: '',
    }, ORIGIN)
    const body = await res.text()
    expect(res.status(), body).toBe(504)
    expect(body).toContain("The host didn't answer within 10s")
  } finally {
    await ctl.unstallTmux()
  }
  await expect.poll(async () => {
    const response = await request.get('/api/machines')
    const { machines } = await response.json() as { machines: { status: string }[] }
    return machines[0]?.status
  }, { timeout: 20_000 }).toBe('ok')
  const recovered = await mutate(request, 'POST', `/api/machines/${MACHINE}/sessions`, {
    name, path: '/home/dev', env: {}, startCommand: '',
  }, ORIGIN)
  expect(recovered.status(), await recovered.text()).toBe(201)
})

test('(T3) stalled SFTP returns a bounded timeout and recovers', async ({ request }) => {
  await ctl.stallSftp(60)
  const start = Date.now()
  try {
    const res = await request.get(`/api/machines/${MACHINE}/fs?path=~`, { timeout: 20_000 })
    const body = await res.json()
    expect(res.status(), JSON.stringify(body)).toBe(504)
    expect(body).toMatchObject({ error: "The host's file service didn't answer within 10s" })
    expect(Date.now() - start).toBeLessThan(15_000)
  } finally {
    await ctl.unstallSftp()
  }
  await expect.poll(async () => (await request.get(`/api/machines/${MACHINE}/fs?path=~`)).status(), { timeout: 15_000 }).toBe(200)
})
