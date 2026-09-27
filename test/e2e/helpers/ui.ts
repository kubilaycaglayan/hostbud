import { expect, type Page } from '@playwright/test'

// The app's e2e hooks (web/src/lib/e2eHooks.ts). Without `session`, the
// focused pane of the active tab answers.
declare global {
  interface Window {
    __hostbud?: {
      termText: (session?: string) => string
      termSize: (session?: string) => { cols: number; rows: number }
      termSelection: (session?: string) => string
      termViewport: (session?: string) => string
      termTextRect: (
        needle: string,
        session?: string,
      ) => { x: number; y: number; width: number; height: number } | null
      termTheme: (session?: string) => { background: string; foreground: string }
      panes: () => { session: string; active: boolean; focused: boolean }[]
    }
  }
}

// Page helpers. They drive the UI only through roles, labels and visible text.
export class UI {
  constructor(readonly page: Page) {}

  tree() {
    return this.page.getByRole('navigation', { name: 'Project and session tree' })
  }

  treeView() {
    return this.tree().getByRole('tree', { name: 'Projects and sessions' })
  }

  treeItem(name: string) {
    return this.treeView().getByRole('treeitem', { name, exact: true })
  }

  async toggle(name: string): Promise<void> {
    const item = this.treeItem(name)
    const expanded = await item.getAttribute('aria-expanded') === 'true'
    await item.getByRole('button', { name: (expanded ? 'Collapse ' : 'Expand ') + name }).click()
  }

  async waitForSave(key: 'tree' | 'theme'): Promise<void> {
    await this.page.waitForResponse((response) =>
      response.request().method() === 'PUT' &&
      new URL(response.url()).pathname === '/api/ui-state/' + key,
    )
  }

  async openAccountMenu(): Promise<void> {
    const account = this.page.locator('summary[aria-label="Account"]')
    if (await account.isVisible()) {
      const isOpen = await account.evaluate((el) => el.parentElement instanceof HTMLDetailsElement && el.parentElement.open)
      if (!isOpen) await account.click()
    }
  }

  async signOut(): Promise<void> {
    await this.openAccountMenu()
    await this.page.getByRole('button', { name: 'Sign out' }).click()
  }

  async expectAccountEmail(email: string | RegExp): Promise<void> {
    await this.openAccountMenu()
    await expect(this.page.getByText(email, typeof email === 'string' ? { exact: true } : {})).toBeVisible()
  }

  /** Opens the app and waits for the signed-in shell. */
  async open(): Promise<void> {
    await this.page.goto('/')
    await expect(this.tree()).toBeVisible()
  }

  /** The list item of a session in the tree (exact name). Call showList()
   * first if a terminal may be open on a compact screen. */
  session(name: string) {
    return this.treeItem(name)
  }

  /** Picks Rename or Kill… from a session row's ⋯ actions menu. */
  async sessionAction(name: string, action: 'Rename' | 'Kill…', tap = false): Promise<void> {
    const trigger = this.session(name).getByRole('button', { name: `More actions for ${name}` })
    await (tap ? trigger.tap() : trigger.click())
    const item = this.page.getByRole('menu').getByRole('menuitem', { name: action, exact: true })
    await (tap ? item.tap() : item.click())
  }

  /** Session names shown in the project tree. */
  async sessionNames(): Promise<string[]> {
    return this.treeView().locator('[role="treeitem"][data-tree-kind="session"]')
      .evaluateAll((items) => items.map((item) => item.getAttribute('aria-label') ?? ''))
  }

  /** The host problem banner (unreachable / tmux missing). */
  banner() {
    return this.page.getByRole('alert', { name: /Host unreachable|tmux not found on the host/ })
  }

  /** On compact screens, open the project-tree drawer over the terminal. */
  async showList(): Promise<void> {
    const trigger = this.page.locator('header button[aria-controls="sessions-sidebar"]')
    const terminal = this.page.getByRole('region', { name: /^Terminal: / }).first()
    const treeVisible = await this.tree().isVisible()
    if ((await terminal.isVisible() || !treeVisible) && (await trigger.getAttribute('aria-expanded')) !== 'true') await trigger.click()
    await expect(this.tree()).toBeVisible()
  }

  /** Clicks an app-bar action (M8 T1), closing the compact tree drawer first. */
  async headerAction(name: 'New session' | 'Browse files'): Promise<void> {
    const drawer = this.page.getByRole('dialog', { name: 'Project tree' })
    if (await drawer.isVisible()) {
      await this.page.keyboard.press('Escape')
      await expect(drawer).toBeHidden()
    }
    await this.page.getByRole('banner').getByRole('button', { name, exact: true }).click()
  }

  /** Opens the shared file browser from the app bar. */
  async openFileBrowser() {
    await this.headerAction('Browse files')
    const dialog = this.page.getByRole('dialog', { name: 'Browse files' })
    await expect(dialog).toBeVisible()
    return dialog
  }

  /** Clicks a session and waits for its terminal (e2e build hook ready). */
  async openTerminal(name: string): Promise<void> {
    await this.showList()
    await this.treeItem(name).getByRole('button', { name, exact: true }).click()
    await this.waitForTerminal(name)
  }

  /** Waits until the session's terminal is shown and is the focused pane. */
  async waitForTerminal(name: string): Promise<void> {
    await expect(this.page.getByRole('region', { name: `Terminal: ${name}` })).toBeVisible()
    await this.page.waitForFunction(
      (n) => window.__hostbud?.panes().some((p) => p.session === n && p.focused) ?? false,
      name,
    )
  }

  /** What the browser terminal shows (xterm buffer, via the e2e hook): the
   * focused pane's, or the named session's. */
  termText(session?: string): Promise<string> {
    return this.page.evaluate((s) => window.__hostbud?.termText(s) ?? '', session)
  }

  /** The mounted terminals: session, in the active tab, focused. */
  panes(): Promise<{ session: string; active: boolean; focused: boolean }[]> {
    return this.page.evaluate(() => window.__hostbud?.panes() ?? [])
  }

  /** A terminal pane, by session. */
  pane(name: string) {
    return this.page.getByRole('region', { name: `Terminal: ${name}`, exact: true })
  }

  /** Splits a pane (its header's Split right/down) and picks a session. */
  async split(from: string, dir: 'right' | 'down', to: string): Promise<void> {
    await this.pane(from).getByRole('button', { name: `Split ${dir}` }).click()
    await this.page.getByRole('dialog', { name: `Split ${dir}` }).getByRole('button', { name: to, exact: true }).click()
    await this.waitForTerminal(to)
  }

  /** The tab bar's tab of a session (exact label). */
  tab(name: string) {
    return this.page.getByRole('tablist', { name: 'Open terminals' }).getByRole('tab', { name, exact: true })
  }

  /** The tab labels, in order. */
  async tabNames(): Promise<string[]> {
    const tabs = this.page.getByRole('tablist', { name: 'Open terminals' }).getByRole('tab')
    return (await tabs.allTextContents()).map((t) => t.trim())
  }

  /** The label of the selected tab. */
  async activeTabName(): Promise<string> {
    const tab = this.page.getByRole('tablist', { name: 'Open terminals' }).getByRole('tab', { selected: true })
    return ((await tab.textContent()) ?? '').trim()
  }

  /** Types into the terminal (focusing its input, without clicking: a
   * click would reach mouse-aware programs like vim), then Enter if asked. */
  async type(text: string, enter = false): Promise<void> {
    // The focused pane's input (splits show several terminals at once).
    await this.page.locator('section[data-focused="true"]').getByRole('textbox', { name: 'Terminal input' }).focus()
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

  async createAccount(account: { email: string; password: string }): Promise<void> {
    await this.signOut()
    const f = this.authForm()
    await f.tab('Create account').click()
    await f.email.fill(account.email)
    await f.password.fill(account.password)
    await f.submit('Create account').click()
    await expect(this.tree()).toBeVisible()
  }
}
