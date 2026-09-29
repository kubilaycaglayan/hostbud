import { ORIGIN } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { clickNotification, getNotificationSettings, openSettings, permissionRequests, putNotificationSettings, shownNotifications } from '../helpers/notifications.ts'
import { notifications } from '../helpers/db.ts'
import { mutate } from '../helpers/api.ts'
import { newDevice, pushfake, received, subscribe } from '../helpers/push.ts'
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
    await context.grantPermissions(['notifications'])
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
    const settingsLoaded = page.waitForResponse(response =>
      response.request().method() === 'GET' && new URL(response.url()).pathname === '/api/notifications/settings',
    )
    const liveSnapshot = new Promise<void>((resolve) => {
      page.on('websocket', socket => {
        if (new URL(socket.url()).pathname !== '/ws/events') return
        socket.on('framereceived', data => {
          try {
            if ((JSON.parse(String(data)) as { type?: string }).type === 'snapshot') resolve()
          } catch { /* ignore non-JSON websocket frames */ }
        })
      })
    })
    await ui.open()
    await Promise.all([settingsLoaded, liveSnapshot])
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

// V2-M3 T4: Settings → Notifications on desktop Chromium. The phone
// variant is notifications.phone.spec.ts.
test.describe('Notification settings (desktop)', () => {
  test.describe.configure({ timeout: 60_000 })
  test.skip(({ isMobile }) => isMobile, 'the phone variant is notifications.phone.spec.ts')

  test('(V2-M3 T4) Notification settings', async ({ page, ui, request }) => {
    await ui.open()
    const dialog = await openSettings(page)
    await expect(dialog.getByTestId('notifications-toggle')).not.toBeChecked()
    const deviceStatus = dialog.getByTestId('notification-device')
    if (await page.evaluate(() => Notification.permission) === 'denied') {
      await expect(deviceStatus).toContainText('blocked for hostbud in this browser')
    } else {
      await expect(deviceStatus).toHaveText('Not set up on this device yet.')
    }
    expect(await permissionRequests(page)).toBe(0)
    // A per-event choice saves at once, without asking for permission.
    await dialog.getByRole('checkbox', { name: 'A queue has finished' }).uncheck()
    await expect.poll(async () => (await getNotificationSettings(request)).onFinished).toBe(false)
    expect((await getNotificationSettings(request)).enabled).toBe(false)
    expect(await permissionRequests(page)).toBe(0)
  })

  test('(V2-M3 T4) Permission denied', async ({ page, ui, request }) => {
    await ui.open()
    const dialog = await openSettings(page)
    await dialog.getByTestId('notifications-toggle').click()
    await expect.poll(() => permissionRequests(page)).toBe(1)
    // Headless Chromium answers the prompt with "denied" (nothing granted).
    await expect(dialog.getByTestId('notification-device')).toContainText('blocked for hostbud in this browser')
    await expect(dialog.getByTestId('notifications-toggle')).not.toBeChecked()
    expect((await getNotificationSettings(request)).enabled).toBe(false)
  })

  test('(V2-M3 T4) Permission revoked', async ({ page, ui, request, context }) => {
    await context.grantPermissions(['notifications'], { origin: ORIGIN })
    await ui.open()
    let dialog = await openSettings(page)
    await dialog.getByTestId('notifications-toggle').click()
    await expect(dialog.getByTestId('notifications-toggle')).toBeChecked()
    await expect(dialog.getByTestId('notification-device')).toHaveText('This device: notifications while hostbud is open.')
    expect((await getNotificationSettings(request)).enabled).toBe(true)

    // The owner takes the permission back in the browser, then comes back.
    await context.clearPermissions()
    await page.reload()
    await expect(ui.tree()).toBeVisible()
    dialog = await openSettings(page)
    await expect(dialog.getByTestId('notification-device')).toHaveText('Notifications are blocked on this device.')
    // This device has no subscription left; the account stays on for others.
    expect(await notifications.subscriptions()).toEqual([])
    expect((await getNotificationSettings(request)).enabled).toBe(true)
  })

  test('(V2-M3 T4) Test notification', async ({ page, ui, request, context }) => {
    // In the page (no push subscription on this browser): shown right here.
    await ui.open()
    await context.grantPermissions(['notifications'], { origin: new URL(page.url()).origin })
    const dialog = await openSettings(page)
    await dialog.getByTestId('notifications-toggle').click()
    await expect(dialog.getByTestId('notifications-toggle')).toBeChecked()
    await dialog.getByRole('button', { name: 'Send test notification' }).click()
    await expect.poll(async () => (await shownNotifications(page)).map((n) => n.title)).toEqual(['hostbud: test notification'])

    // Through push: only to this account's own device, through pushfake.
    await pushfake.reset()
    const d = newDevice()
    expect((await subscribe(request, d)).status()).toBe(204)
    expect((await mutate(request, 'POST', '/api/notifications/test', { endpoint: d.endpoint })).status()).toBe(202)
    await expect.poll(async () => (await received(d)).map((p) => p.title), { timeout: 15_000 }).toEqual(['hostbud: test notification'])
    expect((await mutate(request, 'POST', '/api/notifications/test', { endpoint: 'http://hostbud-e2e-pushfake:8080/push/not-mine' })).status()).toBe(404)
  })
})
