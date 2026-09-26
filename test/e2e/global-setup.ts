import { request, type APIRequestContext } from '@playwright/test'
import { DOMAIN_STORAGE_STATE, newAccount, STORAGE_STATE } from './helpers/auth.ts'
import { DOMAIN_URL, forbidInLogs, ORIGIN } from './helpers/api.ts'
import { owner } from './helpers/db.ts'
import { Target } from './helpers/target.ts'

// Polls GET /api/health until it answers ok.
async function waitHealthy(api: APIRequestContext, what: string) {
  const deadline = Date.now() + 60_000
  let last: string
  for (;;) {
    try {
      const res = await api.get('/api/health')
      if (res.ok()) return
      last = `HTTP ${res.status()}`
    } catch (err) {
      last = String(err)
    }
    if (Date.now() > deadline) throw new Error(`hostbud never became healthy on ${what}: ${last}`)
    await new Promise((r) => setTimeout(r, 500))
  }
}

async function signIn(api: APIRequestContext, account: { email: string; password: string }, file: string) {
  const login = await api.post('/api/auth/login', { data: account })
  if (login.status() !== 200) throw new Error(`setup sign-in: ${login.status()} ${await login.text()}`)
  const state = await api.storageState({ path: file })
  for (const c of state.cookies) forbidInLogs(c.value)
}

// Waits for the app (on both access paths) and the target, then creates the
// account every scenario is signed in with (auth scenarios start from empty
// contexts instead), signed in on the loopback site and on the domain.
export default async function globalSetup() {
  const api = await request.newContext({ baseURL: 'http://localhost:9055', extraHTTPHeaders: { Origin: ORIGIN } })
  const domain = await request.newContext({
    baseURL: DOMAIN_URL,
    ignoreHTTPSErrors: true, // Caddy's internal CA
    extraHTTPHeaders: { Origin: DOMAIN_URL },
  })
  await waitHealthy(api, 'the loopback site')
  await waitHealthy(domain, 'the domain')
  await new Target().run('true')

  await owner.clearRateLimits()
  // The e2e database password must never reach the app's logs either.
  forbidInLogs(new URL(process.env.E2E_DB_URL ?? 'postgres://x:y@z/db').password)
  const account = newAccount('e2e-setup')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  const reg = await api.post('/api/auth/register', { data: account })
  if (reg.status() !== 201) throw new Error(`setup registration: ${reg.status()} ${await reg.text()}`)
  await signIn(api, account, STORAGE_STATE)
  await signIn(domain, account, DOMAIN_STORAGE_STATE)
  await api.dispose()
  await domain.dispose()
}
