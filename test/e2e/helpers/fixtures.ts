import { test as base, expect, type Page } from '@playwright/test'
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
export const EMPTY_TREE = {
  version: 2,
  projects: [],
  sessions: {},
  pinned: [],
  hidden: { projects: [], sessions: [] },
  collapsed: [],
  expanded: [],
  showHidden: false,
}
const SYSTEM_THEME = { version: 1, mode: 'system' }

// The browser tests share one account: projects, tabs, tree customizations
// and the theme left by an earlier test would change what the next one sees.
// Signed out (the auth scenarios) the first request answers 401: nothing to reset.
async function resetAccountState(page: Page, baseURL: string | undefined): Promise<void> {
  const headers = { Origin: new URL(baseURL!).origin }
  const response = await page.request.get('/api/projects?machine=host')
  if (response.status() === 401) return
  if (!response.ok()) throw new Error(`reset e2e projects: ${response.status()} ${await response.text()}`)
  const { projects } = await response.json() as { projects: { id: string }[] | null }
  for (const project of projects ?? []) {
    const deleted = await page.request.delete(`/api/projects/${encodeURIComponent(project.id)}`, { headers })
    if (!deleted.ok()) throw new Error(`reset e2e project ${project.id}: ${deleted.status()} ${await deleted.text()}`)
  }
  for (const [key, data] of [['layout', EMPTY_LAYOUT], ['tree', EMPTY_TREE], ['theme', SYSTEM_THEME]] as const) {
    const saved = await page.request.put(`/api/ui-state/${key}`, { data, headers })
    if (!saved.ok()) throw new Error(`reset e2e ${key}: ${saved.status()} ${await saved.text()}`)
  }
}

// A new account has no saved layout yet: the app's first GET answers 404
// (M3 T6), which is the API's normal answer, not a problem.
const NEW_ACCOUNT_UI_STATE =
  /^HTTP 404: GET https?:\/\/[^/]+\/api\/ui-state\/(layout|tree|theme)$|^console error: Failed to load resource: the server responded with a status of 404 .*@ https?:\/\/[^/]+\/api\/ui-state\/(layout|tree|theme)$/

export const test = base.extend<Fixtures>({
  allowedBrowserErrors: [undefined, { option: true }],

  // Playwright requires a destructuring pattern even with no dependencies.
  // eslint-disable-next-line no-empty-pattern
  target: async ({}, use) => {
    await use(new Target())
  },

  // Fails the test on console errors, uncaught exceptions, failed requests
  // and HTTP error responses the page ran into. Every test starts with the
  // shared account's default state (resetAccountState).
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
    page.on('requestfailed', (r) => {
      const error = r.failure()?.errorText ?? ''
      // Cancelled by the page itself (a reload, or the app's own timeout
      // abort while the server is unreachable), not refused by the server.
      if (/^net::ERR_ABORTED$|^Load request cancelled$|^cancelled$/.test(error)) return
      report(`request failed: ${r.method()} ${r.url()} (${error})`)
    })
    page.on('response', (r) => {
      if (r.status() >= 400) report(`HTTP ${r.status()}: ${r.request().method()} ${r.url()}`)
    })
    await resetAccountState(page, baseURL)
    await use(page)
    expect(problems, 'browser console errors or failed requests').toEqual([])
  },

  ui: async ({ page }, use) => {
    await use(new UI(page))
  },
})

export { expect }
