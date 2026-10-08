import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Icons and the manifest have stable URLs: their bytes must version the cache.
export function shellVersion(dist, urls) {
  const hash = createHash('sha256').update(JSON.stringify(urls))
  for (const url of urls) hash.update(readFileSync(resolve(dist, url === '/' ? 'index.html' : url.slice(1))))
  return hash.digest('hex').slice(0, 12)
}
