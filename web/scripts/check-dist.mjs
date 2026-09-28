// Fails a production build (VITE_E2E unset) whose output still contains the
// e2e test hooks. Runs after `vite build`.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { checkDist, productionFiles } from './check-dist-lib.mjs'

if (process.env.VITE_E2E === '1') process.exit(0)

const dist = new URL('../dist', import.meta.url).pathname
checkDist(dist)
const hits = productionFiles(dist).filter((name) => /\.(js|html)$/.test(name) && /__hostbud|__notifications|__clickNotification|__notificationPermissionRequests/.test(readFileSync(resolve(dist, name), 'utf8')))
if (hits.length) {
  console.error(`check-dist: e2e test hooks in a production build: ${hits.join(', ')}`)
  process.exit(1)
}
