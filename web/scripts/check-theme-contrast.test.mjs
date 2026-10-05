import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'

const css = readFileSync(join(process.cwd(), 'src/assets/main.css'), 'utf8')
function token(section, name) {
  const value = section.match(new RegExp(`--hb-${name}: (#[\\da-f]+)`, 'i'))?.[1]
  assert.ok(value, `missing ${name} token`)
  return value
}
function luminance(hex) {
  const values = hex.slice(1).match(/../g).map((pair) => parseInt(pair, 16) / 255)
    .map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return values[0] * 0.2126 + values[1] * 0.7152 + values[2] * 0.0722
}
function contrast(a, b) {
  const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (high + 0.05) / (low + 0.05)
}

describe('UI theme token contrast', () => {
  it('keeps section yellow title text readable', () => {
    const root = css.slice(css.indexOf(':root {'), css.indexOf('}', css.indexOf(':root {')))
    assert.ok(contrast(token(root, 'section-fg'), token(root, 'section-yellow')) >= 4.5, 'section title foreground/yellow background')
  })

  it('meets text and control contrast on both surfaces', () => {
    for (const selector of [':root {', ":root[data-theme='light'] {", ":root[data-theme='solarized'] {", ":root[data-theme='dimmed'] {"]) {
      const start = css.indexOf(selector)
      assert.notEqual(start, -1, `missing ${selector} token block`)
      const section = css.slice(start, css.indexOf('}', start))
      const bg = token(section, 'bg')
      const surface = token(section, 'surface')
      const fg = token(section, 'fg')
      const muted = token(section, 'muted')
      const border = token(section, 'border')
      const selected = token(section, 'selected')
      const selectedFg = token(section, 'selected-fg')
      const icons = ['accent', 'danger', 'ok'].map((name) => token(section, name))
      assert.ok(contrast(fg, bg) >= 4.5, `${selector} foreground/background`)
      assert.ok(contrast(fg, surface) >= 4.5, `${selector} foreground/surface`)
      assert.ok(contrast(muted, bg) >= 4.5, `${selector} muted/background`)
      assert.ok(contrast(muted, surface) >= 4.5, `${selector} muted/surface`)
      assert.ok(contrast(border, bg) >= 3, `${selector} border/background`)
      assert.ok(contrast(border, surface) >= 3, `${selector} border/surface`)
      assert.ok(contrast(selectedFg, selected) >= 4.5, `${selector} selected foreground/background`)
      assert.ok(contrast(selected, surface) >= 1.5, `${selector} selected background/surface distinction`)
      for (const icon of icons) {
        assert.ok(contrast(icon, bg) >= 3, `${selector} icon/background`)
        assert.ok(contrast(icon, surface) >= 3, `${selector} icon/surface`)
      }
    }
  })
})
