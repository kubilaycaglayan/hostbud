import { request } from '@playwright/test'
import { Target } from './helpers/target.ts'

// Waits until the app answers through Caddy and the target accepts SSH.
export default async function globalSetup() {
  const deadline = Date.now() + 60_000
  const api = await request.newContext({ baseURL: 'http://localhost:9055' })
  let last: string
  for (;;) {
    try {
      const res = await api.get('/api/health')
      if (res.ok()) break
      last = `HTTP ${res.status()}`
    } catch (err) {
      last = String(err)
    }
    if (Date.now() > deadline) throw new Error(`hostbud never became healthy: ${last}`)
    await new Promise((r) => setTimeout(r, 500))
  }
  await api.dispose()
  await new Target().run('true')
}
