import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

export function findHardcodedColors(root) {
  const files = []
  const visit = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name)
      if (statSync(path).isDirectory()) visit(path)
      else if (path.endsWith('.vue') && !path.endsWith('/lib/theme.ts')) files.push(path)
    }
  }
  visit(root)
  return files.flatMap((path) => readFileSync(path, 'utf8').split('\n').flatMap((line, index) =>
    /#[\da-f]{3,8}\b|\brgba?\s*\(/i.test(line) ? [`${path}:${index + 1}`] : []))
}

const root = new URL('../src/', import.meta.url)
const violations = findHardcodedColors(root.pathname)
if (violations.length) {
  console.error(`Hard-coded colors in Vue files:\n${violations.join('\n')}`)
  process.exitCode = 1
}
