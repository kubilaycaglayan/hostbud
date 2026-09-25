import { request } from '@playwright/test'
import { newAccount, STORAGE_STATE } from './helpers/auth.ts'
import { forbidInLogs, ORIGIN } from './helpers/api.ts'
import { owner } from './helpers/db.ts'
import { Target } from './helpers/target.ts'

// Waits for the app and the target, then creates the account every scenario
// is signed in with (auth scenarios start from empty contexts instead).
export default async function globalSetup() {
  const deadline = Date.now() + 60_000
  const api = await request.newContext({ baseURL: 'http://localhost:9055', extraHTTPHeaders: { Origin: ORIGIN } })
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
  await new Target().run('true')

  await owner.clearRateLimits()
  // The e2e database password must never reach the app's logs either.
  forbidInLogs(new URL(process.env.E2E_DB_URL ?? 'postgres://x:y@z/db').password)
  const account = newAccount('e2e-setup')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  const reg = await api.post('/api/auth/register', { data: account })
  if (reg.status() !== 201) throw new Error(`setup registration: ${reg.status()} ${await reg.text()}`)
  const login = await api.post('/api/auth/login', { data: account })
  if (login.status() !== 200) throw new Error(`setup sign-in: ${login.status()} ${await login.text()}`)
  const state = await api.storageState({ path: STORAGE_STATE })
  for (const c of state.cookies) forbidInLogs(c.value)
  await api.dispose()
}
