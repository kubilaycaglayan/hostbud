import { expect, test } from '../helpers/fixtures.ts'

test('(T6) security headers on both Caddy sites', async ({ browser }) => {
  const context = await browser.newContext({ ignoreHTTPSErrors: true })
  try {
    for (const site of [
      { base: 'http://localhost:9055', hsts: false },
      { base: 'https://hostbud.example.test', hsts: true },
    ]) {
      for (const path of ['/', '/favicon.svg', '/api/health', '/missing-header-check.js']) {
        const response = await context.request.get(`${site.base}${path}`)
        expect(response.status(), `${site.base}${path}`).toBe(path === '/missing-header-check.js' ? 404 : 200)
        for (const [header, value] of Object.entries({
          'x-content-type-options': 'nosniff',
          'referrer-policy': 'no-referrer',
          'x-frame-options': 'DENY',
          'cross-origin-opener-policy': 'same-origin',
          'cross-origin-resource-policy': 'same-origin',
          'permissions-policy': 'camera=(), microphone=(), geolocation=(), payment=(), usb=()',
        })) expect(response.headers()[header], `${path} ${header}`).toBe(value)
        expect(response.headers()['strict-transport-security'], `${site.base} HSTS`)
          .toBe(site.hsts ? 'max-age=31536000' : undefined)
        if (path === '/') expect(response.headers()['content-security-policy']).toContain("script-src 'self' 'sha256-")
      }
    }
  } finally {
    await context.close()
  }
})
