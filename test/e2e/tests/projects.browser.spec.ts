import { expect, test } from '../helpers/fixtures.ts'
import { shq, uniqueName } from '../helpers/target.ts'

for (const profile of ['desktop', 'phone'] as const) {
  test.describe(`project browser ${profile}`, () => {
    test.use(profile === 'phone' ? { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true } : {})

    test('Path autocomplete and invalid paths', async ({ page, target }) => {
      const name = uniqueName('e2e-browser')
      const dir = `/home/dev/${name}`
      await target.run(`mkdir -p ${shq(`${dir}/child`)}`)
      await page.goto('/')
      await page.getByRole('button', { name: 'Browse files' }).click()
      await page.getByLabel('Current path').fill(`${dir}/chi`)
      await page.getByRole('option').filter({ hasText: `${dir}/child` }).click()
      await expect(page.getByLabel('Current path')).toHaveValue(`${dir}/child`)
      await page.getByLabel('Current path').fill(`${dir}/missing`)
      await page.getByRole('button', { name: 'Go' }).click()
      await expect(page.getByRole('alert', { name: "Couldn't open this directory" })).toBeVisible()
      await page.getByLabel('Current path').fill(dir)
      await page.getByRole('button', { name: 'Go' }).click()
      await expect(page.getByRole('button', { name: 'child/' })).toBeVisible()
    })

    test('Create folder and open as project, then create a session here', async ({ page, target, request }) => {
      const name = uniqueName('e2e-project-browser')
      const dir = `/home/dev/${name}`
      await target.run(`mkdir -p ${shq(dir)}`)
      await page.goto('/')
      await page.getByRole('button', { name: 'Browse files' }).click()
      await page.getByLabel('Current path').fill(dir)
      await page.getByRole('button', { name: 'Go' }).click()
      await page.getByLabel('New folder name').fill('created')
      await page.getByRole('button', { name: 'Create folder' }).click()
      await expect(page.getByRole('button', { name: 'created/' })).toBeVisible()
      await page.getByRole('button', { name: 'Open as project' }).click()
      await expect(page.getByText(`Project: created`)).toBeVisible()
      await page.getByRole('button', { name: 'New session here' }).click()
      await expect(page.getByRole('dialog', { name: 'New session here' })).toBeVisible()
      await page.getByLabel('Name').fill(name)
      await page.getByLabel('Start command').fill('printf browser-project')
      await page.getByRole('button', { name: 'Create session' }).click()
      await expect.poll(async () => {
        const res = await request.get('/api/machines/host/sessions')
        const data = await res.json()
        return data.sessions.some((session: { name: string; path: string }) => session.name === name && session.path === `${dir}/created`)
      }).toBe(true)
      await expect.poll(async () => {
        const res = await request.get('/api/projects?machine=host')
        const data = await res.json()
        return data.projects.some((project: { path: string }) => project.path === `${dir}/created`)
      }).toBe(true)
    })
  })
}

test('Project persists and updates live', async ({ page, request, target }) => {
  const name = uniqueName('e2e-live-project')
  const path = `/home/dev/${name}`
  await target.run(`mkdir -p ${shq(path)}`)
  await page.goto('/')
  await page.getByRole('button', { name: 'Browse files' }).click()
  await page.getByLabel('Current path').fill('/home/dev')
  await page.getByRole('button', { name: 'Go' }).click()
  // Create from the browser in another tab context; the first view receives projects.changed.
  const second = await page.context().newPage()
  await second.goto('/')
  await second.getByRole('button', { name: 'Browse files' }).click()
  await second.getByLabel('Current path').fill('/home/dev')
  await second.getByRole('button', { name: 'Go' }).click()
  const row = second.getByRole('list', { name: 'Directory entries' }).getByRole('listitem').filter({ hasText: name })
  await row.getByRole('button', { name: 'Open as project' }).click()
  await expect(second.getByText(`Project: ${name}`)).toBeVisible()
  await expect.poll(async () => {
    const response = await request.get('/api/projects?machine=host')
    const data = await response.json()
    return response.status() === 200 && data.projects.some((project: { path: string }) => project.path === path)
  }).toBe(true)
  await expect(page.getByRole('list', { name: 'Directory entries' }).getByRole('listitem').filter({ hasText: name }).getByRole('button', { name: 'Open project' })).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: 'Browse files' }).click()
  await page.getByLabel('Current path').fill('/home/dev')
  await page.getByRole('button', { name: 'Go' }).click()
  await expect(page.getByRole('button', { name: 'Open project' }).first()).toBeVisible()
  await second.close()
})

test('(T4) Hidden toggle and lazy symlink status in the browser', async ({ page, target }) => {
  const name = uniqueName('e2e-browser-links')
  const dir = `/home/dev/${name}`
  await target.run(`mkdir -p ${shq(dir)}; touch ${shq(`${dir}/.hidden`)}; ln -s missing ${shq(`${dir}/broken-link`)}`)
  try {
    await page.goto('/')
    await page.getByRole('button', { name: 'Browse files' }).click()
    await page.getByLabel('Current path').fill(dir)
    await page.getByRole('button', { name: 'Go' }).click()
    await expect(page.getByText('broken-link (unresolved)')).toBeVisible()
    await expect(page.getByText('.hidden')).toHaveCount(0)
    await page.getByRole('button', { name: 'Check link' }).click()
    await expect(page.getByText('broken-link (broken)')).toBeVisible()
    await page.getByLabel('Show hidden files').check()
    await expect(page.getByText('.hidden')).toBeVisible()
  } finally {
    await target.run(`rm -rf ${shq(dir)}`)
  }
})
