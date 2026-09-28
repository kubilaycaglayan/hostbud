import type { APIRequestContext } from '@playwright/test'
import { LLM_URL, useOrigin } from './api.ts'
import { LLM_STORAGE_STATE } from './auth.ts'
import { llmDb } from './db.ts'
import { test as base } from './fixtures.ts'

export const test = base.extend<{ llm: APIRequestContext }>({
  llm: async ({ playwright }, use) => {
    await llmDb.reset()
    const request = await playwright.request.newContext({ baseURL: LLM_URL, storageState: LLM_STORAGE_STATE })
    await use(useOrigin(request, LLM_URL))
    await request.dispose()
  },
})

export { expect } from './fixtures.ts'
