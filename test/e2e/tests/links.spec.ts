import type { BrowserContext, Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { openShell, textRect } from '../helpers/shell.ts'
import { uniqueName, type Target } from '../helpers/target.ts'

// Links (M3 T4). The browser has no internet: pages on example.com are
// answered by context.route, and the opened page's URL is what counts.

test.beforeEach(async ({ target, context }) => {
  await target.resetTmux()
  await context.route('https://example.com/**', (r) =>
    r.fulfill({ status: 200, contentType: 'text/html', body: '<title>opened</title>opened' }),
  )
})

async function outputLine(target: Target, name: string, line: string) {
  await expect.poll(async () => (await target.capture(name)).split('\n').some((l) => l.trimEnd() === line)).toBe(true)
}

/** Clicks (taps, on phones) the middle of `text` in the terminal. */
async function activate(page: Page, text: string, isMobile: boolean) {
  const r = await textRect(page, text)
  const x = r.x + r.width / 2
  const y = r.y + r.height / 2
  if (isMobile) await page.touchscreen.tap(x, y)
  else await page.mouse.click(x, y)
}

/** Every page the terminal page opens. */
function opened(context: BrowserContext): Page[] {
  const pages: Page[] = []
  context.on('page', (p) => pages.push(p))
  return pages
}

// Click a URL (T4)
test('click a printed URL: it opens in a new page, the terminal stays', async ({ ui, target, page, context, isMobile }) => {
  const name = await openShell(ui, target, 'e2e-url')
  const url = `https://example.com/hostbud-${uniqueName('u')}`
  await ui.type(`echo ${url}`, true)
  await outputLine(target, name, url)
  const before = page.url()

  const pages = opened(context)
  await activate(page, url, isMobile)
  await expect.poll(() => pages.length).toBe(1)
  await expect.poll(() => pages[0].url()).toBe(url)
  expect(page.url()).toBe(before)
  await expect(page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
})

// OSC 8 link (T4)
test('OSC 8 hyperlink: hover shows the target, click opens it; javascript: opens nothing', { tag: '@desktop' }, async ({
  ui,
  target,
  page,
  context,
}) => {
  // Hover: desktop scenario.
  // tmux passes OSC 8 on only to terminals with the hyperlinks feature
  // (not in its defaults): set before the browser attaches.
  const name = await openShell(ui, target, 'e2e-osc8', () =>
    target.tmux('set', '-as', 'terminal-features', ',xterm*:hyperlinks'),
  )
  const m = uniqueName('m')
  const url = `https://example.com/osc8-${m}`
  await ui.type(String.raw`printf '\e]8;;${url}\e\\label-${m}\e]8;;\e\\\n'`, true)
  await outputLine(target, name, `label-${m}`)

  const r = await textRect(page, `label-${m}`)
  await page.mouse.move(r.x + r.width / 2, r.y + r.height / 2)
  await expect(page.getByRole('tooltip')).toHaveText(url)

  const pages = opened(context)
  await page.mouse.click(r.x + r.width / 2, r.y + r.height / 2)
  await expect.poll(() => pages.length).toBe(1)
  await expect.poll(() => pages[0].url()).toBe(url)

  // A javascript: target is not a link at all.
  page.on('dialog', (d) => {
    throw new Error(`unexpected dialog: ${d.message()}`)
  })
  await ui.type(String.raw`printf '\e]8;;javascript:alert(1)\e\\js-${m}\e]8;;\e\\\n'`, true)
  await outputLine(target, name, `js-${m}`)
  await activate(page, `js-${m}`, false)
  await page.waitForTimeout(1_000)
  expect(pages).toHaveLength(1)
})
