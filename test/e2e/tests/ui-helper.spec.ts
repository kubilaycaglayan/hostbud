import { expect, test } from '../helpers/fixtures.ts'

test('the account menu helper ignores the Create account auth button', async ({ page, ui }) => {
  test.setTimeout(3_000)
  await page.setContent('<form><button type="submit" disabled>Create account</button></form>')

  await ui.openAccountMenu()

  await expect(page.getByRole('button', { name: 'Create account', exact: true })).toBeDisabled()
})
