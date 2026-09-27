import { writeFileSync } from 'node:fs'
import { join } from 'node:path'

export function keepDistPlaceholder(dist) {
  writeFileSync(join(dist, '.gitkeep'), '')
}
