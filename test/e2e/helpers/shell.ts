import { expect, type Page } from '@playwright/test'
import { uniqueName, type Target } from './target.ts'
import type { UI } from './ui.ts'

/** The last non-empty line of the pane: the shell's current prompt line. */
export async function promptLine(target: Target, name: string): Promise<string> {
  const lines = (await target.capture(name)).split('\n').map((l) => l.trimEnd()).filter(Boolean)
  return lines.at(-1) ?? ''
}

/** Creates a session on the target (as from a real terminal), runs `setup`
 * (tmux settings that must precede the attach), opens it in the browser and
 * waits for bash's prompt. Returns the session name. */
export async function openShell(
  ui: UI,
  target: Target,
  prefix: string,
  setup?: (name: string) => Promise<unknown>,
): Promise<string> {
  const name = uniqueName(prefix)
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev')
  await setup?.(name)
  await ui.open()
  await ui.openTerminal(name)
  // The first attach after the stack was (re)built can take a few seconds
  // on a busy machine.
  await expect.poll(() => target.display(name, '#{session_attached}'), { timeout: 15_000 }).toBe('1')
  await expect.poll(() => promptLine(target, name)).toMatch(/\$$/)
  return name
}

/** Drags the mouse across the last on-screen occurrence of `text` in the
 * browser terminal, with Shift held if asked. */
export async function dragAcross(page: Page, text: string, opts: { shift?: boolean } = {}): Promise<void> {
  const r = await textRect(page, text)
  const y = r.y + r.height / 2
  if (opts.shift) await page.keyboard.down('Shift')
  await page.mouse.move(r.x + 2, y)
  await page.mouse.down()
  await page.mouse.move(r.x + r.width / 2, y, { steps: 3 })
  await page.mouse.move(r.x + r.width - 2, y, { steps: 3 })
  await page.mouse.up()
  if (opts.shift) await page.keyboard.up('Shift')
}

/** The browser terminal's selected text. */
export function termSelection(page: Page): Promise<string> {
  return page.evaluate(() => window.__hostbud?.termSelection() ?? '')
}

export const clipboard = {
  read: (page: Page) => page.evaluate(() => navigator.clipboard.readText()),
  write: (page: Page, text: string) => page.evaluate((t) => navigator.clipboard.writeText(t), text),
}

/** The page rectangle of the last on-screen occurrence of `text`. */
export async function textRect(page: Page, text: string) {
  await expect.poll(() => page.evaluate((t) => window.__hostbud?.termTextRect(t) ?? null, text)).not.toBeNull()
  return (await page.evaluate((t) => window.__hostbud!.termTextRect(t), text))!
}
