import type { Page } from '@playwright/test'
import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'
import { FOREIGN_ORIGIN, MACHINE, forbidInLogs, upgradeStatus } from '../helpers/api.ts'

interface TermWindow {
  e2eTerm: { ws: WebSocket; out: string; open: boolean; closed: boolean; control: unknown[] }
}

// Opens /ws/term from the page (the browser sends its real Origin).
async function openTerm(page: Page, session: string, cols: number, rows: number) {
  await page.goto('/')
  await page.evaluate(
    ({ machine, session, cols, rows }) => {
      const w = window as unknown as TermWindow
      const ws = new WebSocket(
        `ws://${location.host}/ws/term?machine=${machine}&session=${session}&cols=${cols}&rows=${rows}`,
      )
      ws.binaryType = 'arraybuffer'
      w.e2eTerm = { ws, out: '', open: false, closed: false, control: [] }
      const dec = new TextDecoder()
      ws.onopen = () => (w.e2eTerm.open = true)
      ws.onclose = () => (w.e2eTerm.closed = true)
      ws.onmessage = (m) => {
        if (typeof m.data === 'string') w.e2eTerm.control.push(JSON.parse(m.data))
        else w.e2eTerm.out += dec.decode(m.data, { stream: true })
      }
    },
    { machine: MACHINE, session, cols, rows },
  )
  await expect.poll(() => page.evaluate(() => (window as unknown as TermWindow).e2eTerm.open), { timeout: 10_000 }).toBe(true)
}

const typeKeys = (page: Page, text: string) =>
  page.evaluate((t) => (window as unknown as TermWindow).e2eTerm.ws.send(new TextEncoder().encode(t)), text)
const control = (page: Page, msg: object) =>
  page.evaluate((m) => (window as unknown as TermWindow).e2eTerm.ws.send(JSON.stringify(m)), msg)
const output = (page: Page) => page.evaluate(() => (window as unknown as TermWindow).e2eTerm.out)

// The first attach after a cold start also sets up the ssh connection.
const slow = { timeout: 10_000 }

test.beforeEach(async ({ target }) => {
  await target.resetTmux()
})

// Terminal WS (T13)
test('terminal WS: attach, type a marker, resize, close leaves the session', async ({ page, target }) => {
  const name = uniqueName('e2e-term')
  const marker = uniqueName('e2e-mark')
  forbidInLogs(marker)
  await target.tmux('new-session', '-d', '-s', name, '-x', '80', '-y', '24')

  await openTerm(page, name, 100, 30)
  await expect.poll(() => target.display(name, '#{session_attached}'), slow).toBe('1')

  await typeKeys(page, `echo ${marker}\r`)
  await expect.poll(() => target.capture(name), slow).toContain(marker)
  await expect.poll(() => output(page), slow).toContain(marker)

  await control(page, { type: 'resize', cols: 132, rows: 40 })
  await expect
    .poll(() => target.run(`tmux list-clients -t =${name} -F '#{client_width}x#{client_height}'`), slow)
    .toBe('132x40\n')
  await expect.poll(() => target.display(name, '#{window_width}'), slow).toBe('132')

  await page.evaluate(() => (window as unknown as TermWindow).e2eTerm.ws.close())
  await expect.poll(() => target.display(name, '#{session_attached}'), slow).toBe('0')
  expect(await target.sessions()).toContain(name)
})

// Origin (T13)
test('origin: a foreign-Origin /ws/term upgrade is rejected', async ({ target }) => {
  const name = uniqueName('e2e-evil-term')
  await target.tmux('new-session', '-d', '-s', name)
  const path = `/ws/term?machine=${MACHINE}&session=${name}&cols=80&rows=24`
  expect(await upgradeStatus(path, FOREIGN_ORIGIN)).toBe(403)
  expect(await target.display(name, '#{session_attached}')).toBe('0')
})
