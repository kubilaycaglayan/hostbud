import { expect, test } from '../helpers/fixtures.ts'

test('the account menu helper opens the summary menu without matching Create account', async ({ page, ui }) => {
  test.setTimeout(3_000)
  await page.setContent('<form><button type="submit" disabled>Create account</button></form>')
  await page.evaluate(() => setTimeout(() => {
    document.body.insertAdjacentHTML('beforeend', `
      <details>
        <summary aria-label="Account">Account</summary>
        <button>Sign out</button>
      </details>
    `)
  }, 100))

  await ui.openAccountMenu()

  await expect(page.getByRole('button', { name: 'Create account', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
})
