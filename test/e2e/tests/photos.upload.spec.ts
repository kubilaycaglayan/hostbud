import { createHash } from 'node:crypto'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { expect, test } from '../helpers/fixtures.ts'
import { shq, uniqueName } from '../helpers/target.ts'
import type { UI } from '../helpers/ui.ts'

// Phone and desktop profiles exercise the same original-file upload flow.
async function createAccount(ui: UI) {
  const account = newAccount('e2e-photo-upload')
  forbidInLogs(account.email, account.password)
  await owner.allow(account.email)
  await ui.createAccount(account)
}

test('(M8 T10) Send original photo bytes to the active session repo', async ({ page, target, ui }) => {
  test.setTimeout(90_000)
  await createAccount(ui)
  const repo = uniqueName('e2e-photo-repo')
  const repoPath = `/home/dev/${repo}`
  const sessionPath = `${repoPath}/nested`
  const session = uniqueName('e2e-photo-session')
  const filename = 'iphone-sample.heic'
  forbidInLogs(repo, session, filename)
  await target.run(`mkdir -p ${shq(sessionPath)}`)
  const projectResponse = await mutate(page.request, 'POST', '/api/projects', { machineId: MACHINE, path: repoPath, name: repo }, ORIGIN)
  expect(projectResponse.status(), await projectResponse.text()).toBe(201)
  await target.tmux('new-session', '-d', '-s', session, '-c', sessionPath)

  await ui.open()
  await ui.openTerminal(session)
  await page.getByRole('button', { name: 'Terminal actions' }).click()
  await page.getByRole('menuitem', { name: 'Send photos to this repo' }).click()
  const dialog = page.getByRole('dialog', { name: 'Send photos to this repo' })
  await expect(dialog.getByLabel('Photo destination')).toHaveText(`/home/dev/${repo}`)

  // Binary fixture includes nulls and non-UTF8 bytes; the app must pass the
  // selected File object through without image decoding or recompression.
  const bytes = Buffer.from([0, 255, 216, 255, 0, 72, 69, 73, 67, 128, 13, 10, 255])
  const expected = createHash('sha256').update(bytes).digest('hex')
  await dialog.locator('input[type=file]').setInputFiles({ name: filename, mimeType: 'image/heic', buffer: bytes })
  await dialog.getByRole('button', { name: 'Send photos' }).click()
  await expect(dialog.getByText(new RegExp(`Sent ${filename}`))).toBeVisible()
  const targetFile = `${repoPath}/${filename}`
  const actual = (await target.run(`sha256sum ${shq(targetFile)}`)).split(/\s+/)[0]
  expect(actual).toBe(expected)
  expect((await target.run(`stat -c %s ${shq(targetFile)}`)).trim()).toBe(String(bytes.length))

  // Cmd-V with a clipboard image routes the image file to the same repo
  // uploader rather than sending binary bytes through the terminal PTY.
  const pastedName = 'pasted-from-clipboard.png'
  const pastedBytes = [0, 255, 1, 2, 3, 128]
  await page.locator('.xterm textarea').evaluate((textarea, payload) => {
    const file = new File([new Uint8Array(payload.bytes)], payload.name, { type: 'image/png' })
    const data = new DataTransfer()
    data.items.add(file)
    const event = new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data })
    textarea.dispatchEvent(event)
  }, { bytes: pastedBytes, name: pastedName })
  await expect(page.getByRole('region', { name: 'Notifications' }).getByText('Photo added to repo').last()).toBeVisible()
  await expect(page.getByText(new RegExp(`\\.\\/${pastedName}`))).toBeVisible()
  const pastedExpected = createHash('sha256').update(Buffer.from(pastedBytes)).digest('hex')
  expect((await target.run(`sha256sum ${shq(`${repoPath}/${pastedName}`)}`)).split(/\s+/)[0]).toBe(pastedExpected)
  await expect.poll(() => ui.termText(session)).toContain(`../${pastedName}`)

  const numberedFilename = 'iphone-sample-1.heic'
  await dialog.locator('input[type=file]').setInputFiles({ name: filename, mimeType: 'image/heic', buffer: bytes })
  await dialog.getByRole('button', { name: 'Send photos' }).click()
  await expect(dialog.getByLabel('Sent photos')).toContainText(numberedFilename)
  expect((await target.run(`sha256sum ${shq(targetFile)}`)).split(/\s+/)[0]).toBe(expected)
  expect((await target.run(`sha256sum ${shq(`${repoPath}/${numberedFilename}`)}`)).split(/\s+/)[0]).toBe(expected)
  await expect.poll(() => ui.termText(session)).toContain(`../${numberedFilename}`)

  await dialog.getByRole('button', { name: 'Close' }).click()
  await target.tmux('kill-session', '-t', `=${session}`)
})
