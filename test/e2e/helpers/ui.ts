import { expect, type Page } from '@playwright/test'

// Page helpers. They drive the UI only through roles, labels and visible text.
export class UI {
  constructor(readonly page: Page) {}

  /** Opens the app and waits for the shell. */
  async open(): Promise<void> {
    await this.page.goto('/')
    await expect(this.page.getByRole('heading', { name: 'hostbud' })).toBeVisible()
  }
}
