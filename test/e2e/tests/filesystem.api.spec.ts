import { request as playwrightRequest } from '@playwright/test'
import { ctl } from '../helpers/ctl.ts'
import { FOREIGN_ORIGIN, MACHINE, ORIGIN, mutate, forbidInLogs } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { shq, uniqueName } from '../helpers/target.ts'

const fsPath = `/api/machines/${MACHINE}/fs`
const homePath = `/api/machines/${MACHINE}/fs/home`
const statPath = `/api/machines/${MACHINE}/fs/stat`
const mkdirPath = `/api/machines/${MACHINE}/fs/mkdir`

test('filesystem API: browse home, literal paths, hidden files and symlink state', async ({ target, request }) => {
  const dir = uniqueName('e2e-fs')
  const base = `/home/dev/${dir}`
  const odd = `${base}/space quote ' 雪;$()`
  forbidInLogs(base, odd)
  await target.run(
    `mkdir -p ${shq(`${base}/z-dir`)} ${shq(`${base}/a-dir`)} ${shq(odd)}; ` +
      `touch ${shq(`${base}/.hidden`)} ${shq(`${base}/a-file`)} ${shq(`${base}/z-file`)}; ` +
      `ln -s a-file ${shq(`${base}/valid-link`)}; ln -s missing ${shq(`${base}/broken-link`)}; ` +
      `mkdir ${shq(`${base}/private`)}; touch ${shq(`${base}/private/target`)}; chmod 000 ${shq(`${base}/private`)}; ` +
      `ln -s private/target ${shq(`${base}/unreadable-link`)}; ` +
      `ln -s loop-b ${shq(`${base}/loop-a`)}; ln -s loop-a ${shq(`${base}/loop-b`)}`,
  )
  try {
    const home = await request.get(homePath)
    expect(home.status(), await home.text()).toBe(200)
    expect(await home.json()).toEqual({ path: '/home/dev' })

    const visible = await request.get(fsPath, { params: { path: base } })
    expect(visible.status(), await visible.text()).toBe(200)
    const listing = await visible.json()
    expect(listing.path).toBe(base)
    expect(listing.entries.map((e: { name: string }) => e.name)).toEqual([
      'a-dir',
      "space quote ' 雪;$()",
      'z-dir',
      'a-file',
      'broken-link',
      'loop-a',
      'loop-b',
      'private',
      'unreadable-link',
      'valid-link',
      'z-file',
    ])
    expect(listing.entries.find((e: { name: string }) => e.name === 'valid-link').symlinkState).toBe('unresolved')
    expect(listing.entries.some((e: { name: string }) => e.name === '.hidden')).toBe(false)

    const hidden = await request.get(fsPath, { params: { path: base, hidden: 'true' } })
    expect((await hidden.json()).entries.some((e: { name: string }) => e.name === '.hidden')).toBe(true)

    const valid = await request.get(statPath, { params: { path: `${base}/valid-link` } })
    expect(valid.status()).toBe(200)
    expect(await valid.json()).toMatchObject({ symlink: true, symlinkState: 'resolved' })
    const broken = await request.get(statPath, { params: { path: `${base}/broken-link` } })
    expect(broken.status()).toBe(200)
    expect(await broken.json()).toMatchObject({ symlink: true, symlinkState: 'broken' })
    const loop = await request.get(statPath, { params: { path: `${base}/loop-a` } })
    expect(loop.status()).toBe(200)
    expect(await loop.json()).toMatchObject({ symlink: true, symlinkState: 'loop' })
    const unreadable = await request.get(statPath, { params: { path: `${base}/unreadable-link` } })
    expect(unreadable.status()).toBe(200)
    expect(await unreadable.json()).toMatchObject({ symlink: true, symlinkState: 'unreadable' })

    const oddListing = await request.get(fsPath, { params: { path: odd } })
    expect(oddListing.status(), await oddListing.text()).toBe(200)
  } finally {
    await target.run(`chmod 700 ${shq(`${base}/private`)} 2>/dev/null || true; rm -rf ${shq(base)}`)
  }
})

test('(T1) filesystem API: auth and Origin checks, with no delete or remote rename', async ({ request, target, baseURL }) => {
  const dir = uniqueName('e2e-fs-safe')
  const path = `/home/dev/${dir}`
  forbidInLogs(path)
  await target.run(`mkdir -p ${shq(path)}`)
  const anonymous = await playwrightRequest.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  try {
    expect((await anonymous.get(homePath)).status()).toBe(401)
    const unauthenticatedMkdir = await anonymous.post(mkdirPath, {
      data: { path, name: 'unauthorized' },
      headers: { Origin: ORIGIN },
    })
    expect(unauthenticatedMkdir.status()).toBe(401)
    expect((await mutate(request, 'POST', mkdirPath, { path, name: 'foreign' }, FOREIGN_ORIGIN)).status()).toBe(403)
    expect((await mutate(request, 'POST', mkdirPath, { path, name: 'allowed' }, ORIGIN)).status()).toBe(201)

    expect((await mutate(request, 'DELETE', fsPath, undefined)).status()).toBe(405)
    expect((await mutate(request, 'PATCH', fsPath, { path, name: 'renamed' })).status()).toBe(405)
    expect([404, 405]).toContain(
      (await mutate(request, 'POST', `${fsPath}/rename`, { from: path, to: `${path}-renamed` })).status(),
    )
    const stillThere = await request.get(fsPath, { params: { path: '/home/dev' } })
    const homeEntries = (await stillThere.json()).entries as { name: string }[]
    expect(homeEntries.some((entry) => entry.name === dir)).toBe(true)
    const createdEntries = await request.get(fsPath, { params: { path } })
    const names = ((await createdEntries.json()).entries as { name: string }[]).map((entry) => entry.name)
    expect(names).toContain('allowed')
    expect(names).not.toContain('unauthorized')
    expect(names).not.toContain('foreign')
    const allowed = await request.get(fsPath, { params: { path: `${path}/allowed` } })
    expect(allowed.status()).toBe(200)
  } finally {
    await anonymous.dispose()
    await target.run(`rm -rf ${shq(path)}`)
  }
})

test('(T1) SFTP unavailable recovery: failure is actionable and browsing recovers', async ({ request }) => {
  test.setTimeout(90_000)
  await ctl.stopSshd()
  try {
    const failed = await request.get(homePath, { timeout: 20_000 })
    expect([503, 504]).toContain(failed.status())
    expect((await failed.json()).error).toMatch(/filesystem (unavailable|request timed out)/)
  } finally {
    await ctl.startSshd()
  }
  await expect
    .poll(async () => {
      const recovered = await request.get(homePath)
      return recovered.status() === 200
    }, { timeout: 60_000, intervals: [500, 1_000, 2_000] })
    .toBe(true)
})
