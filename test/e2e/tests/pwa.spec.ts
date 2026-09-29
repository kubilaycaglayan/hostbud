import { expect, test } from '../helpers/fixtures.ts'
import { ctl } from '../helpers/ctl.ts'

test.use({
  serviceWorkers: 'allow',
  allowedBrowserErrors: /^(?:HTTP 502: (?:GET|PUT) .*\/api\/|console error: Failed to load resource: the server responded with a status of 502 .*\/api\/)/,
})
test.afterEach(async () => { await ctl.appStart() })

test('(T7) Manifest and icons load through Caddy with no external requests', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => requests.push(request.url()))
  await page.goto('/')
  const origin = new URL(page.url()).origin
  const link = page.locator('link[rel="manifest"]')
  const href = await link.getAttribute('href')
  expect(href).toBe('/manifest.webmanifest')
  const manifestResponse = await page.request.get(href!)
  expect(manifestResponse.headers()['content-type']).toContain('application/manifest+json')
  const manifest = await manifestResponse.json()
  expect(manifest).toMatchObject({ id: '/', name: 'hostbud', short_name: 'hostbud', start_url: '/', scope: '/', display: 'standalone' })
  expect(manifest.theme_color).toBeTruthy()
  expect(manifest.background_color).toBeTruthy()
  for (const icon of manifest.icons as { src: string; sizes: string }[]) {
    const response = await page.request.get(icon.src)
    expect(response.headers()['content-type']).toContain('image/png')
    const bytes = await response.body()
    expect(bytes.toString('hex', 0, 8)).toBe('89504e470d0a1a0a')
    const [width, height] = icon.sizes.split('x').map(Number)
    expect([bytes.readUInt32BE(16), bytes.readUInt32BE(20)]).toEqual([width, height])
  }
  const appleIcon = await page.locator('link[rel="apple-touch-icon"]').getAttribute('href')
  expect(appleIcon).toBe('/icons/apple-touch-icon-180.png')
  expect((await page.request.get(appleIcon!)).headers()['content-type']).toContain('image/png')
  expect(await page.locator('link[rel="icon"]').getAttribute('href')).toBe('/favicon.svg')
  expect(requests.every((url) => new URL(url).origin === origin)).toBe(true)
})

test('(T8) Service worker controls the page after reload and preserves other caches', async ({ page }) => {
  await page.addInitScript(async () => {
    if (!sessionStorage.getItem('seeded-test-caches')) {
      await caches.open('other-app-cache')
      await caches.open('hostbud-shell-stale')
      sessionStorage.setItem('seeded-test-caches', 'yes')
    }
  })
  await page.goto('/')
  expect(await page.evaluate(() => navigator.serviceWorker.controller === null)).toBe(true)
  const registration = await page.evaluate(async () => {
    const value = await navigator.serviceWorker.ready
    return value.scope
  })
  await page.reload()
  expect(await page.evaluate(() => navigator.serviceWorker.controller !== null)).toBe(true)
  expect(new URL(registration).pathname).toBe('/')
  const keys = await page.evaluate(() => caches.keys())
  expect(keys).toContain('other-app-cache')
  expect(keys).not.toContain('hostbud-shell-stale')
  expect(keys.filter((name) => name.startsWith('hostbud-shell-'))).toHaveLength(1)
})

test('(T8) Offline start serves the app shell and recovers after hostbud starts', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(async () => { await navigator.serviceWorker.ready })
  await page.reload()
  await expect(page.getByRole('heading', { name: "Can't reach hostbud" })).toHaveCount(0)
  await ctl.appStop()
  const documentResponse = await page.reload()
  expect(await documentResponse?.fromServiceWorker()).toBe(true)
  await expect(page.getByRole('heading', { name: "Can't reach hostbud" })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Sign in' })).toHaveCount(0)
  await expect(page.getByText(/session/i)).toHaveCount(0)
  await ctl.appStart()
  await page.getByRole('button', { name: 'Try again' }).click()
  await expect(page.getByRole('tree', { name: 'Projects and sessions' })).toBeVisible()
})

test('(T8) API and WebSocket paths never enter the service worker cache', async ({ page, ui }) => {
  const apiServiceWorker: boolean[] = []
  page.on('response', (response) => {
    if (new URL(response.url()).pathname.startsWith('/api/')) apiServiceWorker.push(response.fromServiceWorker())
  })
  await page.goto('/')
  await page.evaluate(async () => { await navigator.serviceWorker.ready })
  await page.reload()
  await ui.tree().waitFor()
  expect(apiServiceWorker.length).toBeGreaterThan(0)
  expect(apiServiceWorker.every((value) => !value)).toBe(true)
  const cachedUrls = await page.evaluate(async () => {
    const names = await caches.keys()
    const requests = (await Promise.all(names.map(async (name) => (await caches.open(name)).keys()))).flat()
    return requests.map((request) => new URL(request.url).pathname)
  })
  expect(cachedUrls.some((url) => url.startsWith('/api/') || url.startsWith('/ws/'))).toBe(false)
})
