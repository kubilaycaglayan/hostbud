import { expect, test } from '../helpers/fixtures.ts'

test('(T7) Manifest and icons load through Caddy with no external requests', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop-chromium')
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
