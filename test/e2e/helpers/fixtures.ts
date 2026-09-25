import { test as base, expect } from '@playwright/test'
import { Target } from './target.ts'
import { UI } from './ui.ts'

interface Fixtures {
  target: Target
  ui: UI
  /** Browser problems a test expects (matched against the report line). */
  allowedBrowserErrors: RegExp[]
}

export const test = base.extend<Fixtures>({
  allowedBrowserErrors: [[], { option: true }],

  // Playwright requires a destructuring pattern even with no dependencies.
  // eslint-disable-next-line no-empty-pattern
  target: async ({}, use) => {
    await use(new Target())
  },

  // Fails the test on console errors, uncaught exceptions, failed requests
  // and HTTP error responses the page ran into.
  page: async ({ page, allowedBrowserErrors }, use) => {
    const problems: string[] = []
    const report = (line: string) => {
      if (!allowedBrowserErrors.some((re) => re.test(line))) problems.push(line)
    }
    page.on('console', (m) => {
      if (m.type() === 'error') report(`console error: ${m.text()}`)
    })
    page.on('pageerror', (e) => report(`uncaught: ${e.message}`))
    page.on('requestfailed', (r) =>
      report(`request failed: ${r.method()} ${r.url()} (${r.failure()?.errorText})`),
    )
    page.on('response', (r) => {
      if (r.status() >= 400) report(`HTTP ${r.status()}: ${r.request().method()} ${r.url()}`)
    })
    await use(page)
    expect(problems, 'browser console errors or failed requests').toEqual([])
  },

  ui: async ({ page }, use) => {
    await use(new UI(page))
  },
})

export { expect }
