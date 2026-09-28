import { ORIGIN } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { clickNotification, putNotificationSettings, shownNotifications } from '../helpers/notifications.ts'
import { addItem, control, createQueue, getQueue, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'

// V2-M3 T1: in-app notifications while the app is open (Chromium, permission
// granted; the e2e build records them in window.__notifications). A stub
// item achieves on the throwaway target.

const stubs = new Stubs()

test.describe('In-app notifications (desktop)', () => {
  test.describe.configure({ timeout: 90_000 })
  test.skip(({ isMobile }) => isMobile, 'Chromium grants the notification permission; the phone profile checks the denied state (T4)')

  test.beforeEach(async ({ target, context }) => {
    await target.resetTmux()
    await stubs.reset()
    await context.grantPermissions(['notifications'], { origin: ORIGIN })
  })

  async function runOneItem(request: import('@playwright/test').APIRequestContext, target: import('../helpers/target.ts').Target, condition: string) {
    const project = await newProject(request, target, 'e2e-notify')
    await stubs.setBehavior(condition, 'achieve:1')
    const queue = await createQueue(request, project.id)
    const item = await addItem(request, queue.id, { instruction: `/goal ${condition}` })
    return { project, queue, item }
  }

  test('(V2-M3 T1) In-app notification', async ({ page, ui, request, target }) => {
    await putNotificationSettings(request, { enabled: true })
    const { project, queue, item } = await runOneItem(request, target, 'e2e notify in-app')
    await ui.open()
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 30_000 }).toBe('finished')
    const run = (await getQueue(request, queue.id)).items[0].run!

    await expect.poll(async () => (await shownNotifications(page)).length, { timeout: 10_000 }).toBe(2)
    const shown = await shownNotifications(page)
    expect(shown.filter((n) => n.tag === `run:${run.id}:done`)).toEqual([
      { title: `${project.name}: item 1 done`, body: 'Item 1 reached its goal.', tag: `run:${run.id}:done` },
    ])
    expect(shown.filter((n) => n.tag === `queue:${queue.id}:finished:${run.id}`)).toHaveLength(1)
    // Only the payload's text: no instruction, path or session name.
    const text = JSON.stringify(shown)
    for (const secret of ['e2e notify in-app', project.path, run.sessionName]) expect(text).not.toContain(secret)

    // A click opens the Queue panel on that item.
    await clickNotification(page, shown.findIndex((n) => n.tag.endsWith(':done')))
    const panel = page.getByRole('dialog', { name: 'Queue' })
    await expect(panel).toBeVisible()
    await expect(panel.locator(`[data-queue-item="${item.id}"]`)).toHaveAttribute('data-highlighted', 'true')
  })

  test('(V2-M3 T1) Switch off', async ({ page, ui, request, target }) => {
    const { queue } = await runOneItem(request, target, 'e2e notify off')
    await ui.open()
    expect((await control(request, queue.id, 'start')).status()).toBe(200)
    await expect.poll(async () => (await getQueue(request, queue.id)).status, { timeout: 30_000 }).toBe('finished')
    // The events that would notify have arrived (the panel data is live);
    // give a late notification the same time a shown one takes.
    await page.waitForTimeout(1_000)
    expect(await shownNotifications(page)).toEqual([])
  })
})
