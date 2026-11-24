import { expect, test } from '../helpers/fixtures.ts'
import { uniqueName } from '../helpers/target.ts'

// M8 T44: actual browser, Caddy, system SSH and throwaway target. The lower
// integration test deterministically holds SSH before raw mode; this scenario
// covers the two UI lifecycle triggers with real xterm and wire-level output.
for (const trigger of ['tab-return', 'ssh-retry'] as const) {
  test(`mouse reports never become visible text after ${trigger}`, { tag: '@desktop' }, async ({ page, ui, target }) => {
    await target.resetTmux()
    const name = uniqueName('e2e-mouse-reconnect')
    await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
    await target.tmux('set-option', '-t', `=${name}`, 'mouse', 'on')
    await page.addInitScript(() => {
      const NativeWebSocket = window.WebSocket
      let attaches = 0
      const reports = '\x1b[<35;124;17M\x1b[<35;134;20M'
      const probe = { attaches: 0, output: '', mouseReportsSent: 0 }
      Object.assign(window, { hostbudMouseProbe: probe })
      window.WebSocket = class extends NativeWebSocket {
        constructor(url: string | URL, protocols?: string | string[]) {
          super(url, protocols)
          if (!String(url).includes('/ws/term?')) return
          window.addEventListener('hostbud-test-drop-terminal', () => {
            if (this.readyState === NativeWebSocket.OPEN) this.close(4000, 'test reconnect')
          })
          probe.attaches = ++attaches
          this.addEventListener('open', () => {
            if (attaches > 1) {
              // Reproduce stale xterm mouse input at the socket-open boundary,
              // before the application can assume SSH/tmux is ready.
              this.send(new TextEncoder().encode(reports))
              probe.mouseReportsSent++
            }
          })
          this.addEventListener('message', (event: MessageEvent) => {
            if (event.data instanceof ArrayBuffer) probe.output += new TextDecoder().decode(event.data)
          })
        }
      }
    })
    const probe = () => page.evaluate(() => (window as unknown as {
      hostbudMouseProbe: { attaches: number; output: string; mouseReportsSent: number }
    }).hostbudMouseProbe)
    await ui.open()
    await ui.openTerminal(name)
    await expect.poll(() => target.display(name, '#{session_attached}')).toBe('1')
    const before = (await probe()).attaches
    if (trigger === 'tab-return') {
      const other = await page.context().newPage()
      try {
        await other.bringToFront()
        await expect.poll(() => page.evaluate(() => document.visibilityState)).toBe('hidden')
        await expect.poll(() => target.display(name, '#{session_attached}')).toBe('0')
        await page.bringToFront()
      } finally {
        await other.close()
      }
    } else {
      await page.evaluate(() => window.dispatchEvent(new Event('hostbud-test-drop-terminal')))
    }
    await expect.poll(async () => (await probe()).attaches).toBeGreaterThan(before)
    await expect.poll(() => target.display(name, '#{session_attached}')).toBe('1')
    await expect.poll(async () => (await probe()).mouseReportsSent).toBeGreaterThan(0)
    const terminal = page.locator('[data-focused="true"] .xterm-screen')
    const bounds = await terminal.boundingBox()
    expect(bounds).not.toBeNull()
    await page.mouse.move(bounds!.x + 30, bounds!.y + 30)
    await page.mouse.move(bounds!.x + 100, bounds!.y + 60, { steps: 10 })
    const marker = uniqueName('mouse-reconnected-ok')
    await ui.type(`echo ${marker}`, true)
    await expect.poll(() => target.capture(name)).toContain(marker)
    await expect.poll(() => ui.termText()).toContain(marker)
    // Assert the wire too: tmux redraws could otherwise erase visible damage.
    expect((await probe()).output).not.toMatch(/\^\[\[<35;|\[<35;124;17M|\[<35;134;20M/)
    expect(await ui.termText()).not.toMatch(/\^\[\[<35;/)
  })
}
