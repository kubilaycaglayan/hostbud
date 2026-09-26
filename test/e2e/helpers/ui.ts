import { expect, type Page } from '@playwright/test'

// Page helpers. They drive the UI only through roles, labels and visible text.
export class UI {
  constructor(readonly page: Page) {}

  /** Opens the app and waits for the signed-in shell. */
  async open(): Promise<void> {
    await this.page.goto('/')
    await expect(this.page.getByRole('complementary', { name: 'Sessions' })).toBeVisible()
  }

  /** The list item of a session in the sidebar (exact name). */
  session(name: string) {
    const p = this.page
    return p.getByRole('listitem').filter({ has: p.getByRole('button', { name, exact: true }) })
  }

  /** Session names shown in the sidebar list. */
  async sessionNames(): Promise<string[]> {
    const list = this.page.getByRole('list', { name: 'tmux sessions' })
    if ((await list.count()) === 0) return []
    return list.getByRole('button').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label') ?? ''))
  }

  /** The host problem banner (unreachable / tmux missing). */
  banner() {
    return this.page.getByRole('alert')
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
