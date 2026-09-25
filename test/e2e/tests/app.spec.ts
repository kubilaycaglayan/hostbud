import { expect, test } from '../helpers/fixtures.ts'

// Open the app (T5): the shell loads through Caddy with no console errors.
test('open the app: shell loads through Caddy and /api/health is ok', async ({ ui, request }) => {
  await ui.open()
  const res = await request.get('/api/health')
  expect(res.ok()).toBe(true)
  expect(await res.json()).toEqual({ status: 'ok' })
})
