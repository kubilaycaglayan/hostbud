// Fails a production build (VITE_E2E unset) whose output still contains the
// e2e test hooks. Runs after `vite build`.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

if (process.env.VITE_E2E === '1') process.exit(0)

const hits = []
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p)
    else if (/\.(js|html)$/.test(name) && readFileSync(p, 'utf8').includes('__hostbud')) hits.push(p)
  }
}
walk(new URL('../dist', import.meta.url).pathname)
if (hits.length) {
  console.error(`check-dist: e2e test hooks in a production build: ${hits.join(', ')}`)
  process.exit(1)
}
