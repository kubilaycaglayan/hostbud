import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export function keepDistPlaceholder(dist) {
  mkdirSync(dist, { recursive: true })
  writeFileSync(join(dist, '.gitkeep'), '')
}
