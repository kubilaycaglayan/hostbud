import type { APIRequestContext } from '@playwright/test'
import { MULTI_URL, useOrigin } from './api.ts'
import { MULTI_STORAGE_STATE } from './auth.ts'
import { multiDb } from './db.ts'
import { test as base } from './fixtures.ts'

// V2-M2: scenarios against hostbud-e2e-app-multi (HOSTBUD_PARALLEL_QUEUES=true,
// its own database, Caddy :9058) on the shared throwaway target. The V2-M1
// specs keep running against the switch-off app.

export const test = base.extend<{ multi: APIRequestContext }>({
  multi: async ({ playwright }, use) => {
    await multiDb.reset()
    const request = await playwright.request.newContext({ baseURL: MULTI_URL, storageState: MULTI_STORAGE_STATE })
    await use(useOrigin(request, MULTI_URL))
    await request.dispose()
  },
})

export { expect } from './fixtures.ts'
