import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { promptLine, textRect } from '../helpers/shell.ts'
import { uniqueName } from '../helpers/target.ts'

// Long-lived terminal contrast (M8 T7): a prompt-like TUI keeps running while
// the OS switches between dark and light (hostbud theme: System). Like Codex,
// it paints its prompt with an explicit background computed for the dark
// theme and writes the prompt text in the default foreground, which the
// switch flips. The terminal must stay attached and the text legible.

const PROMPT_BG = '44;46;50' // the dark default background, ~12 % toward white
const PROMPT_TEXT = 'Ask the prompt to do anything'
const TUI = `clear; b='\\e[48;2;${PROMPT_BG}m'; printf "$b%-60s\\e[0m\\n$b  %-58s\\e[0m\\n$b%-60s\\e[0m\\n" '' '${PROMPT_TEXT}' ''; read -r _`

/** WCAG contrast between the prompt row's background and its most
 * contrasting (text) pixel, measured on a screenshot of the rendered cells. */
async function renderedContrast(page: Page): Promise<number> {
  const r = await textRect(page, PROMPT_TEXT)
  const png = await page.screenshot({ clip: { x: r.x, y: r.y, width: r.width, height: r.height } })
  return page.evaluate(async (base64) => {
    const img = new Image()
    img.src = `data:image/png;base64,${base64}`
    await img.decode()
    const canvas = document.createElement('canvas')
    canvas.width = img.width
    canvas.height = img.height
    const ctx = canvas.getContext('2d')!
    ctx.drawImage(img, 0, 0)
    const data = ctx.getImageData(0, 0, img.width, img.height).data
    const lum = (i: number) => {
      const c = [data[i], data[i + 1], data[i + 2]].map((v) => {
        const s = v / 255
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
      })
      return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
    }
    const counts = new Map<number, number>()
    for (let i = 0; i < data.length; i += 4) {
      const key = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2]
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    const bgKey = [...counts.entries()].sort((a, b) => b[1] - a[1])[0][0]
    let bgIndex = 0
    for (let i = 0; i < data.length; i += 4) if (((data[i] << 16) | (data[i + 1] << 8) | data[i + 2]) === bgKey) { bgIndex = i; break }
    const bg = lum(bgIndex)
    let best = 1
    for (let i = 0; i < data.length; i += 4) {
      const l = lum(i)
      best = Math.max(best, (Math.max(l, bg) + 0.05) / (Math.min(l, bg) + 0.05))
    }
    return best
  }, png.toString('base64'))
}

test.beforeEach(({ isMobile }) => {
  test.skip(isMobile, 'desktop scenario (the phone theme scenarios cover System switching)')
})

test('(T7) Long-lived terminal contrast', async ({ page, ui, target }, testInfo) => {
  test.setTimeout(90_000)
  const fresh = newAccount('e2e-theme-contrast')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await page.emulateMedia({ colorScheme: 'dark' })
  await ui.createAccount(fresh) // new accounts use System
  const session = uniqueName('e2e-contrast')
  await target.tmux('new-session', '-d', '-s', session, '-c', '/home/dev')
  await page.reload()
  await ui.openTerminal(session)
  await expect.poll(() => promptLine(target, session)).toMatch(/\$$/)
  await ui.type(TUI, true)
  await expect.poll(() => target.capture(session)).toContain(PROMPT_TEXT)
  const clients = (await target.tmux('list-clients', '-t', `=${session}`, '-F', '#{client_pid}')).trim()

  const states: [scheme: 'dark' | 'light', background: string][] = [['dark', '#0f1115'], ['light', '#ffffff'], ['dark', '#0f1115']]
  for (const [scheme, background] of states) {
    await page.emulateMedia({ colorScheme: scheme })
    await expect(page.locator('html')).toHaveAttribute('data-theme', scheme)
    await expect.poll(() => page.evaluate((name) => window.__hostbud?.termTheme(name).background, session)).toBe(background)
    await page.waitForTimeout(200) // the renderer repaints on its next frame
    // Before M8 T7 the light state measured about 1.2:1 (dark text on the
    // still-dark prompt). Antialiasing keeps glyph pixels below the 4.5 xterm
    // enforces per cell, hence the lower bound here.
    expect(await renderedContrast(page)).toBeGreaterThanOrEqual(3)
    await testInfo.attach(`contrast-${scheme}.png`, { body: await page.screenshot(), contentType: 'image/png' })
    // Same tmux client all along: nothing detached or re-attached.
    expect((await target.tmux('list-clients', '-t', `=${session}`, '-F', '#{client_pid}')).trim()).toBe(clients)
  }
  await ui.type('\r')
})
