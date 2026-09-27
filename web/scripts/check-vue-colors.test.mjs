import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { findHardcodedColors } from './check-vue-colors.mjs'

const dirs = []
afterEach(() => { for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true }) })

describe('Vue color lint', () => {
  it('accepts token classes and rejects hex/rgb colors in Vue files', () => {
    const dir = mkdtempSync(join(tmpdir(), 'hostbud-colors-'))
    dirs.push(dir)
    mkdirSync(join(dir, 'components'))
    writeFileSync(join(dir, 'components/Good.vue'), '<div class="bg-surface text-fg"/>')
    assert.deepEqual(findHardcodedColors(dir), [])
    writeFileSync(join(dir, 'components/Bad.vue'), '<div style="color: #fff; background: rgb(0, 0, 0)"/>')
    assert.equal(findHardcodedColors(dir).length, 1)
  })
})
