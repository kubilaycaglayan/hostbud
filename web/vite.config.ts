import { createHash } from 'node:crypto'
import { readdirSync, statSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { keepDistPlaceholder } from './scripts/keep-dist.mjs'
import { build as viteBuild, defineConfig, type Plugin } from 'vite'

// Vite empties dist on every build; put the tracked .gitkeep back so the
// Go `//go:embed all:dist` directive always has a directory to embed.
const keepDist: Plugin = {
  name: 'hostbud-keep-dist',
  apply: 'build',
  closeBundle() {
    keepDistPlaceholder(fileURLToPath(new URL('./dist', import.meta.url)))
  },
}

const serviceWorker: Plugin = {
  name: 'hostbud-service-worker',
  apply: 'build',
  async closeBundle() {
    const root = fileURLToPath(new URL('.', import.meta.url))
    const dist = resolve(root, 'dist')
    const assets: string[] = []
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const path = resolve(dir, name)
        if (statSync(path).isDirectory()) walk(path)
        else assets.push(`/${path.slice(dist.length + 1).split('\\').join('/')}`)
      }
    }
    walk(resolve(dist, 'assets'))
    assets.sort()
    const urls = ['/', ...assets, '/manifest.webmanifest', '/favicon.svg',
      '/icons/apple-touch-icon-180.png', '/icons/icon-192.png', '/icons/icon-512.png', '/icons/icon-maskable-512.png']
    const version = createHash('sha256').update(JSON.stringify(urls)).digest('hex').slice(0, 12)
    await viteBuild({
      configFile: false,
      root,
      logLevel: 'warn',
      define: {
        __HOSTBUD_CACHE_NAME__: JSON.stringify(`hostbud-shell-${version}`),
        __HOSTBUD_PRECACHE_URLS__: JSON.stringify(urls),
      },
      build: {
        target: 'es2022',
        outDir: dist,
        emptyOutDir: false,
        minify: false,
        rollupOptions: {
          input: resolve(root, 'src/sw/sw.ts'),
          output: { format: 'iife', entryFileNames: 'sw.js' },
        },
      },
    })
    const sw = resolve(dist, 'sw.js')
    const source = (await import('node:fs/promises')).readFile(sw, 'utf8')
    writeFileSync(sw, `// hostbud-precache: ${JSON.stringify(urls)}\n// hostbud-cache: hostbud-shell-${version}\n${await source}`)
  },
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), keepDist, serviceWorker],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    // `pnpm dev` proxies the backend (make run / make deploy) so the SPA runs with HMR.
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Keep fonts as same-origin files; small font subsets must not be inlined
    // because the application CSP only allows font-src 'self'.
    assetsInlineLimit: 0,
  },
})
