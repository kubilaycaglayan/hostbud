import { test as base, expect } from '@playwright/test'
import { Target } from './target.ts'
import { UI } from './ui.ts'

interface Fixtures {
  target: Target
  ui: UI
  /** Browser problems a test expects, matched against the report line. One
   * RegExp (use alternation): test.use() would read an array as a tuple. */
  allowedBrowserErrors: RegExp | undefined
}

export const EMPTY_LAYOUT = { version: 1, tabs: [], activeTab: null }

// A new account has no saved layout yet: the app's first GET answers 404
// (M3 T6), which is the API's normal answer, not a problem.
const NEW_ACCOUNT_UI_STATE =
  /^HTTP 404: GET https?:\/\/[^/]+\/api\/ui-state\/(layout|tree)$|^console error: Failed to load resource: the server responded with a status of 404 .*@ https?:\/\/[^/]+\/api\/ui-state\/(layout|tree)$/

export const test = base.extend<Fixtures>({
  allowedBrowserErrors: [undefined, { option: true }],

  // Playwright requires a destructuring pattern even with no dependencies.
  // eslint-disable-next-line no-empty-pattern
  target: async ({}, use) => {
    await use(new Target())
  },

  // Fails the test on console errors, uncaught exceptions, failed requests
  // and HTTP error responses the page ran into. Every test starts with no
  // open tabs (the saved layout is per account, and the account is shared).
  page: async ({ page, allowedBrowserErrors, baseURL }, use) => {
    const problems: string[] = []
    const report = (line: string) => {
      if (!allowedBrowserErrors?.test(line) && !NEW_ACCOUNT_UI_STATE.test(line)) problems.push(line)
    }
    page.on('console', (m) => {
      // A failed load names its URL only in the location.
      if (m.type() === 'error') report(`console error: ${m.text()} @ ${m.location().url}`)
    })
    await page.exposeBinding('__hostbudCSPViolation', (_source, details: unknown) => {
      problems.push(`securitypolicyviolation: ${JSON.stringify(details)}`)
    })
    await page.addInitScript(() => {
      window.addEventListener('securitypolicyviolation', (event) => {
        const report = (window as Window & { __hostbudCSPViolation?: (detail: unknown) => void }).__hostbudCSPViolation
        report?.({
          blockedURI: event.blockedURI,
          violatedDirective: event.violatedDirective,
          effectiveDirective: event.effectiveDirective,
          sourceFile: event.sourceFile,
          lineNumber: event.lineNumber,
        })
      })
    })
    page.on('pageerror', (e) => report(`uncaught: ${e.message}`))
    page.on('requestfailed', (r) =>
      report(`request failed: ${r.method()} ${r.url()} (${r.failure()?.errorText})`),
    )
    page.on('response', (r) => {
      if (r.status() >= 400) report(`HTTP ${r.status()}: ${r.request().method()} ${r.url()}`)
    })
    // Signed out (the auth scenarios) this answers 401: nothing to reset.
    await page.request.put('/api/ui-state/layout', {
      data: EMPTY_LAYOUT,
      headers: { Origin: new URL(baseURL!).origin },
    })
    await use(page)
    expect(problems, 'browser console errors or failed requests').toEqual([])
  },

  ui: async ({ page }, use) => {
    await use(new UI(page))
  },
})

export { expect }
