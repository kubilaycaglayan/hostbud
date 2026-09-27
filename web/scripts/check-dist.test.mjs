import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { execFileSync } from 'node:child_process'
import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { checkDist } from './check-dist-lib.mjs'

const dirs = []
afterEach(() => { for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true }) })
function fixture() {
  const dir = mkdtempSync(join(tmpdir(), 'hostbud-dist-'))
  dirs.push(dir)
  const png = (size) => {
    const b = Buffer.alloc(24)
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]).copy(b)
    b.writeUInt32BE(size, 16); b.writeUInt32BE(size, 20)
    return b
  }
  mkdirSync(join(dir, 'icons'))
  for (const [name, size] of [['icon-192.png', 192], ['icon-512.png', 512], ['icon-maskable-512.png', 512], ['apple-touch-icon-180.png', 180]]) {
    writeFileSync(join(dir, 'icons', name), png(size))
  }
  writeFileSync(join(dir, 'favicon.svg'), '<svg/>')
  writeFileSync(join(dir, 'index.html'), '<script>try{const m=localStorage.getItem(\'hostbud.theme\');document.documentElement.dataset.theme=m||\'dark\'}catch{document.documentElement.dataset.theme=\'dark\'}</script><link rel="manifest" href="/manifest.webmanifest"><link rel="icon" href="/favicon.svg"><link rel="apple-touch-icon" href="/icons/apple-touch-icon-180.png">')
  writeFileSync(join(dir, 'manifest.webmanifest'), JSON.stringify({ id: '/', start_url: '/', scope: '/', display: 'standalone', icons: [
    { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
    { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
    { src: '/icons/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
  ] }))
  return dir
}

describe('check-dist PWA validation', () => {
  it('accepts a correct fixture distribution', () => assert.equal(checkDist(fixture()), true))
  it('rejects missing icons', () => {
    const dir = fixture(); rmSync(join(dir, 'icons/icon-192.png'))
    assert.throws(() => checkDist(dir), /missing file/)
  })
  it('rejects icon size mismatches', () => {
    const dir = fixture(); writeFileSync(join(dir, 'icons/icon-192.png'), Buffer.alloc(24))
    assert.throws(() => checkDist(dir), /PNG size mismatch/)
  })
  it('rejects off-origin URLs', () => {
    const dir = fixture(); writeFileSync(join(dir, 'manifest.webmanifest'), JSON.stringify({ id: '/', start_url: '/', scope: '/', display: 'standalone', icons: [{ src: 'https://example.com/icon.png', sizes: '192x192', type: 'image/png' }] }))
    assert.throws(() => checkDist(dir), /off-origin URL/)
  })
  it('rejects a missing manifest link', () => {
    const dir = fixture()
    const html = readFileSync(join(dir, 'index.html'), 'utf8')
    writeFileSync(join(dir, 'index.html'), html.replace(/<link rel="manifest"[^>]*>/, ''))
    assert.throws(() => checkDist(dir), /does not link the manifest/)
  })
  it('requires the small theme boot script before stylesheets', () => {
    const dir = fixture()
    const html = readFileSync(join(dir, 'index.html'), 'utf8')
    writeFileSync(join(dir, 'index.html'), html.replace(/<script>[\s\S]*?<\/script>/, ''))
    assert.throws(() => checkDist(dir), /theme boot script/)
    writeFileSync(join(dir, 'index.html'), html.replace('</script>', '</script><link rel="stylesheet" href="/app.css">').replace(/<script>[\s\S]*?<\/script>/, '<script>document.documentElement.dataset.theme="dark";localStorage.getItem("hostbud.theme")</script>'))
    const late = readFileSync(join(dir, 'index.html'), 'utf8').replace(/<script>[\s\S]*?<\/script>/, '').replace('<link rel="manifest"', '<link rel="stylesheet" href="/app.css"><script>document.documentElement.dataset.theme="dark";localStorage.getItem("hostbud.theme")</script><link rel="manifest"')
    writeFileSync(join(dir, 'index.html'), late)
    assert.throws(() => checkDist(dir), /before stylesheets/)
    writeFileSync(join(dir, 'index.html'), html.replace(/<script>[\s\S]*?<\/script>/, `<script>${'x'.repeat(1024)}</script>`))
    assert.throws(() => checkDist(dir), /exceeds 1 KiB/)
    writeFileSync(join(dir, 'index.html'), html.replace('</script>', ';document.documentElement.style.colorScheme="dark"</script>'))
    assert.throws(() => checkDist(dir), /set only data-theme/)
  })
  it('rejects a second inline script', () => {
    const dir = fixture()
    const html = readFileSync(join(dir, 'index.html'), 'utf8')
    writeFileSync(join(dir, 'index.html'), html.replace('</script>', '</script><script>evil()</script>'))
    assert.throws(() => checkDist(dir), /more than one inline script/)
  })
  it('rejects font data URLs that violate the same-origin CSP', () => {
    const dir = fixture()
    mkdirSync(join(dir, 'assets'))
    writeFileSync(join(dir, 'assets/app.css'), 'src: url(data:font/woff2;base64,AA==)')
    assert.throws(() => checkDist(dir), /inline font asset/)
  })
  it('generates the expected PNG sizes from a fixture SVG', () => {
    const dir = mkdtempSync(join(tmpdir(), 'hostbud-icons-'))
    dirs.push(dir)
    const svg = join(dir, 'fixture.svg')
    writeFileSync(svg, '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>')
    const output = join(dir, 'output')
    execFileSync(process.execPath, [new URL('./icons.mjs', import.meta.url).pathname, output, svg])
    for (const [name, size] of [['icon-192.png', 192], ['icon-512.png', 512], ['icon-maskable-512.png', 512], ['apple-touch-icon-180.png', 180]]) {
      const png = readFileSync(join(output, 'icons', name))
      assert.deepEqual([png.readUInt32BE(16), png.readUInt32BE(20)], [size, size])
    }
  })
  it('keeps safe-area insets on the app shell, drawer and compact sheets', () => {
    const css = readFileSync(join(process.cwd(), 'src/assets/main.css'), 'utf8')
    const app = readFileSync(join(process.cwd(), 'src/App.vue'), 'utf8')
    assert.match(css, /#app\s*\{[^}]*safe-area-inset-top[^}]*safe-area-inset-right[^}]*safe-area-inset-bottom[^}]*safe-area-inset-left/s)
    assert.match(app, /safe-area-inset-top/)
    assert.match(app, /safe-area-inset-bottom/)
    for (const name of ['CreateSessionDialog.vue', 'KillSessionDialog.vue', 'FileBrowserDialog.vue']) {
      assert.match(readFileSync(join(process.cwd(), 'src/components', name), 'utf8'), /safe-area-inset-bottom/)
    }
  })
  it('checks a service worker precache against the exact built shell', () => {
    const dir = fixture()
    mkdirSync(join(dir, 'assets'))
    writeFileSync(join(dir, 'assets/app-hash.js'), 'app')
    const precache = ['/', '/assets/app-hash.js', '/manifest.webmanifest', '/favicon.svg',
      '/icons/apple-touch-icon-180.png', '/icons/icon-192.png', '/icons/icon-512.png', '/icons/icon-maskable-512.png']
    const worker = () => `// hostbud-precache: ${JSON.stringify(precache)}\n// hostbud-cache: hostbud-shell-abc123\nself.addEventListener('install',()=>{});`
    writeFileSync(join(dir, 'sw.js'), worker())
    assert.equal(checkDist(dir), true)
    writeFileSync(join(dir, 'sw.js'), worker().replace('/assets/app-hash.js', '/api/nope'))
    assert.throws(() => checkDist(dir), /precache list does not match/)
    writeFileSync(join(dir, 'sw.js'), worker() + '\nself.skipWaiting()')
    assert.throws(() => checkDist(dir), /must wait for the next launch/)
    writeFileSync(join(dir, 'sw.js'), worker() + '\nself.skipWaiting()')
    assert.throws(() => checkDist(dir), /must wait for the next launch/)
  })
})
