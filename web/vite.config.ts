import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig, type Plugin } from 'vite'

// Vite empties dist on every build; put the tracked .gitkeep back so the
// Go `//go:embed all:dist` directive always has a directory to embed.
const keepDist: Plugin = {
  name: 'hostbud-keep-dist',
  apply: 'build',
  closeBundle() {
    writeFileSync(fileURLToPath(new URL('./dist/.gitkeep', import.meta.url)), '')
  },
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), keepDist],
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
  },
})
