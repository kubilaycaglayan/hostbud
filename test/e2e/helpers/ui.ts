import { expect, type Page } from '@playwright/test'

declare global {
  interface Window {
    __hostbud?: {
      termText: () => string
      termSize: () => { cols: number; rows: number }
      termSelection: () => string
      termViewport: () => string
      termTextRect: (needle: string) => { x: number; y: number; width: number; height: number } | null
    }
  }
}

// Page helpers. They drive the UI only through roles, labels and visible text.
export class UI {
  constructor(readonly page: Page) {}

  /** Opens the app and waits for the signed-in shell. */
  async open(): Promise<void> {
    await this.page.goto('/')
    await expect(this.page.getByRole('complementary', { name: 'Sessions' })).toBeVisible()
  }

  /** The list item of a session in the sidebar (exact name). Call
   * showList() first if a terminal may be open on a narrow screen. */
  session(name: string) {
    const p = this.page
    return p.getByRole('listitem').filter({ has: p.getByRole('button', { name, exact: true }) })
  }

  /** Session names shown in the sidebar list. */
  async sessionNames(): Promise<string[]> {
    const list = this.page.getByRole('list', { name: 'tmux sessions' })
    if ((await list.count()) === 0) return []
    // The first button of each row is the session itself (then rename, kill).
    return list
      .getByRole('listitem')
      .evaluateAll((items) => items.map((li) => li.querySelector('button')?.getAttribute('aria-label') ?? ''))
  }

  /** The host problem banner (unreachable / tmux missing). */
  banner() {
    return this.page.getByRole('alert', { name: /Host unreachable|tmux not found on the host/ })
  }

  /** On narrow screens an open terminal replaces the list: go back to it. */
  async showList(): Promise<void> {
    const back = this.page.getByRole('button', { name: 'Back to sessions' })
    if (await back.isVisible()) await back.click()
    await expect(this.page.getByRole('complementary', { name: 'Sessions' })).toBeVisible()
  }

  /** Clicks a session and waits for its terminal (e2e build hook ready). */
  async openTerminal(name: string): Promise<void> {
    await this.showList()
    await this.page.getByRole('button', { name, exact: true }).click()
    await expect(this.page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
    await this.page.waitForFunction(() => window.__hostbud !== undefined)
  }

  /** What the browser terminal shows (xterm buffer, via the e2e hook). */
  termText(): Promise<string> {
    return this.page.evaluate(() => window.__hostbud?.termText() ?? '')
  }

  /** Types into the terminal (focusing its input, without clicking: a
   * click would reach mouse-aware programs like vim), then Enter if asked. */
  async type(text: string, enter = false): Promise<void> {
    await this.page.getByRole('textbox', { name: 'Terminal input' }).focus()
    await this.page.keyboard.type(text)
    if (enter) await this.page.keyboard.press('Enter')
  }

  /** The terminal's exit/disconnect state. */
  termStatus() {
    return this.page.getByRole('region', { name: /^Terminal: / }).getByRole('status')
  }

  /** An error toast, by its title. */
  toast(title: string) {
    return this.page.getByRole('region', { name: 'Notifications' }).getByRole('alert', { name: title })
  }

  /** Opens the create dialog, fills the given fields and submits. */
  async createSession(fields: { directory?: string; name?: string; startCommand?: string }): Promise<void> {
    const p = this.page
    await this.showList()
    await p.getByRole('button', { name: 'New session' }).click()
    const dialog = p.getByRole('dialog', { name: 'New session' })
    if (fields.directory !== undefined) await dialog.getByLabel('Directory').fill(fields.directory)
    if (fields.name !== undefined) await dialog.getByLabel('Name').fill(fields.name)
    if (fields.startCommand !== undefined) await dialog.getByLabel('Start command').fill(fields.startCommand)
    await dialog.getByRole('button', { name: 'Create' }).click()
  }

  /** The sign-in / registration screen. */
  authForm() {
    const p = this.page
    return {
      tab: (name: 'Sign in' | 'Create account') => p.getByRole('tab', { name }),
      email: p.getByLabel('Email'),
      password: p.getByLabel('Password'),
      submit: (name: 'Sign in' | 'Create account') => p.getByRole('button', { name, exact: true }),
      alert: p.getByRole('alert'),
    }
  }

  async signIn(email: string, password: string): Promise<void> {
    const f = this.authForm()
    await f.email.fill(email)
    await f.password.fill(password)
    await f.submit('Sign in').click()
  }
}
