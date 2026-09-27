import assert from 'node:assert/strict'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, it } from 'node:test'
import { keepDistPlaceholder } from './keep-dist.mjs'

const dirs = []
afterEach(() => { for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true }) })

it('creates the embed directory when Docker build context omitted its tracked placeholder', () => {
  const root = mkdtempSync(join(tmpdir(), 'hostbud-empty-dist-'))
  dirs.push(root)
  const dist = join(root, 'web', 'dist')

  assert.doesNotThrow(() => keepDistPlaceholder(dist))
})
