import { expect, test } from '../helpers/fixtures.ts'
import { shq } from '../helpers/target.ts'

const schedules = ['01-06-11-16-21', '02-07-12-17-22', '03-08-13-18-23', '04-09-14-19-00', '05-10-15-20-00']

function farthestNextSchedule() {
  const local = new Date(Date.now() + 3 * 60 * 60 * 1000)
  let selected = schedules[0]!
  let farthest = -1
  for (const schedule of schedules) {
    for (const hourText of schedule.split('-')) {
      const hour = Number(hourText)
      for (const day of [0, 1]) {
        const candidate = Date.UTC(local.getUTCFullYear(), local.getUTCMonth(), local.getUTCDate() + day, hour)
        const delta = candidate - local.getTime()
        if (delta > 0 && delta > farthest) { selected = schedule; farthest = delta }
      }
    }
  }
  return selected
}

test('Window nudge UI requires confirmation before enabling and arms the saved schedule', { tag: '@desktop' }, async ({ page, target }) => {
  await target.resetTmux()
  await page.getByRole('button', { name: 'Window nudge' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('UTC+03:00')
  await dialog.getByRole('button', { name: 'Browse' }).click()
  await expect(dialog.getByRole('button', { name: 'Use this directory' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Use this directory' }).click()
  await dialog.getByLabel('Provider').selectOption('claude')
  await dialog.getByLabel('Daily UTC+03:00 times').selectOption(farthestNextSchedule())
  await dialog.getByLabel('Skip the next scheduled time once').check()
  await dialog.getByLabel('Active').check()
  await dialog.getByRole('button', { name: 'Save' }).click()
  const confirmation = page.getByRole('alertdialog', { name: 'Confirm recurring session replacement' })
  await expect(confirmation).toContainText('window-nudge-chore')
  await confirmation.getByRole('button', { name: 'Confirm and enable' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.getByRole('button', { name: 'Window nudge' })).toHaveAttribute('aria-pressed', 'true')
  await target.run(`tmux new-session -d -s ${shq('window-nudge-chore')} -c ${shq('/home/dev')}`)
  await page.getByRole('button', { name: 'Window nudge' }).click()
  await page.getByLabel('Active').uncheck()
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  expect(await target.sessions()).toContain('window-nudge-chore')
  await target.resetTmux()
})

test.describe('Window nudge on iPhone 13 Pro', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

  test('configure, confirm and disable without terminating the current session', { tag: '@phone' }, async ({ page, target }) => {
    await target.resetTmux()
    await page.getByRole('button', { name: 'Window nudge' }).tap()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Browse' }).tap()
    await dialog.getByRole('button', { name: 'Use this directory' }).tap()
    await dialog.getByLabel('Daily UTC+03:00 times').selectOption(farthestNextSchedule())
    await dialog.getByLabel('Skip the next scheduled time once').check()
    await dialog.getByLabel('Active').check()
    await dialog.getByRole('button', { name: 'Save' }).tap()
    const confirmation = page.getByRole('alertdialog', { name: 'Confirm recurring session replacement' })
    await confirmation.getByRole('button', { name: 'Confirm and enable' }).tap()
    await expect(page.getByRole('dialog')).toBeHidden()

    await target.run(`tmux new-session -d -s ${shq('window-nudge-chore')} -c ${shq('/home/dev')}`)
    await page.getByRole('button', { name: 'Window nudge' }).tap()
    await page.getByLabel('Active').uncheck()
    await page.getByRole('button', { name: 'Save' }).tap()
    await expect(page.getByRole('dialog')).toBeHidden()
    expect(await target.sessions()).toContain('window-nudge-chore')
    await target.resetTmux()
  })
})
