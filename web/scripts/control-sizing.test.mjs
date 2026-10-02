import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const css = readFileSync(resolve(import.meta.dirname, '../src/assets/main.css'), 'utf8')

test('touch sizing keeps native choice controls compact', () => {
  assert.match(css, /input:not\(\[type='checkbox'\], \[type='radio'\]\),\s*textarea,\s*select\s*\{\s*min-width: 2\.75rem;\s*min-height: 2\.75rem;/)
  assert.match(css, /input:is\(\[type='checkbox'\], \[type='radio'\]\)\s*\{\s*min-width: 0;\s*min-height: 0;/)
})
