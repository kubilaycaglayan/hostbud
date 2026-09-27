import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

export function checkDist(dist) {
  const fail = (message) => { throw new Error(`check-dist: ${message}`) }
  const file = (name) => join(dist, name)
  const exists = (name) => {
    try { return statSync(file(name)).isFile() } catch { return false }
  }
  if (!exists('index.html')) fail('missing index.html')
  if (!exists('manifest.webmanifest')) fail('missing manifest.webmanifest')
  const html = readFileSync(file('index.html'), 'utf8')
  const inlineScripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script\s*>/gi)]
    .filter((match) => !/\bsrc\s*=/i.test(match[1]))
  if (inlineScripts.length > 1) fail('index.html has more than one inline script')
  const boot = html.match(/<script>([\s\S]*?)<\/script>/)
  const firstStylesheet = html.search(/<link[^>]+rel=["']stylesheet["']/i)
  if (!boot || boot[1].length >= 1024) fail('theme boot script is missing or exceeds 1 KiB')
  if (firstStylesheet >= 0 && boot.index > firstStylesheet) fail('theme boot script must run before stylesheets')
  if (!/dataset\.theme/.test(boot[1]) || !/hostbud\.theme/.test(boot[1])) fail('theme boot script does not apply the saved theme')
  if (/document\.documentElement\.(?!dataset\.theme)|\bfetch\s*\(|XMLHttpRequest|WebSocket/.test(boot[1])) fail('theme boot script must set only data-theme and make no request')
  if (!/rel=["']manifest["'][^>]*href=["']\/manifest\.webmanifest["']|href=["']\/manifest\.webmanifest["'][^>]*rel=["']manifest["']/.test(html)) fail('index.html does not link the manifest')
  if (!/rel=["']icon["'][^>]*href=["']\/favicon\.svg["']|href=["']\/favicon\.svg["'][^>]*rel=["']icon["']/.test(html)) fail('index.html does not link the favicon')
  if (!/rel=["']apple-touch-icon["'][^>]*href=["']\/icons\/apple-touch-icon-180\.png["']|href=["']\/icons\/apple-touch-icon-180\.png["'][^>]*rel=["']apple-touch-icon["']/.test(html)) fail('index.html does not link the apple-touch-icon')
  const manifest = JSON.parse(readFileSync(file('manifest.webmanifest'), 'utf8'))
  if (manifest.id !== '/' || manifest.start_url !== '/' || manifest.scope !== '/' || manifest.display !== 'standalone') fail('manifest identity or display settings are invalid')
  const refs = [...(manifest.icons ?? []).map((icon) => icon.src), '/favicon.svg', '/icons/apple-touch-icon-180.png']
  for (const url of [...refs, '/manifest.webmanifest']) {
    if (!url.startsWith('/') || url.startsWith('//') || /^[a-z][a-z\d+.-]*:/i.test(url)) fail(`off-origin URL: ${url}`)
    if (!exists(url.slice(1))) fail(`missing file: ${url}`)
  }
  for (const icon of manifest.icons ?? []) {
    if (icon.type !== 'image/png') fail(`non-PNG icon: ${icon.src}`)
    const [w, h] = icon.sizes.split('x').map(Number)
    const png = readFileSync(file(icon.src.slice(1)))
    if (png.length < 24 || png.toString('hex', 0, 8) !== '89504e470d0a1a0a' || png.readUInt32BE(16) !== w || png.readUInt32BE(20) !== h) fail(`PNG size mismatch: ${icon.src}`)
  }
  for (const name of ['icons/apple-touch-icon-180.png']) {
    const png = readFileSync(file(name))
    if (png.length < 24 || png.toString('hex', 0, 8) !== '89504e470d0a1a0a' || png.readUInt32BE(16) !== 180 || png.readUInt32BE(20) !== 180) fail(`PNG size mismatch: ${name}`)
  }
  for (const href of [...html.matchAll(/(?:href|src)=["']([^"']+)["']/g)].map((match) => match[1])) {
    if (/^[a-z][a-z\d+.-]*:/i.test(href) || href.startsWith('//')) fail(`off-origin HTML URL: ${href}`)
  }
  if (exists('sw.js')) {
    const sw = readFileSync(file('sw.js'), 'utf8')
    const marker = sw.match(/^\/\/ hostbud-precache: (.+)$/m)
    if (!marker) fail('service worker has no injected precache list')
    const precache = JSON.parse(marker[1])
    const assets = productionFiles(dist).filter((name) => name.startsWith('assets/')).map((name) => `/${name}`)
    const expected = ['/', ...assets, '/manifest.webmanifest', '/favicon.svg', ...readdirSync(file('icons')).map((name) => `/icons/${name}`)].sort()
    if (JSON.stringify([...precache].sort()) !== JSON.stringify(expected)) fail('service worker precache list does not match shell files')
    if (precache.some((url) => !url.startsWith('/') || url.startsWith('/api') || url.startsWith('/ws'))) fail('invalid service worker precache URL')
    if (/skipWaiting\s*\(|clients\.claim\s*\(/.test(sw)) fail('service worker must wait for the next launch')
    if (!/^\/\/ hostbud-cache: hostbud-shell-[a-f\d]+$/m.test(sw)) fail('service worker cache version is missing')
  }
  return true
}

export function productionFiles(dist) {
  const files = []
  const visit = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name)
      if (statSync(path).isDirectory()) visit(path)
      else files.push(relative(dist, path))
    }
  }
  visit(dist)
  return files
}
