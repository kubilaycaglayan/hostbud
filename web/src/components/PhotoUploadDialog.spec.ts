import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ApiError, filesystemApi } from '@/api/client'
import PhotoUploadDialog from './PhotoUploadDialog.vue'

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

async function openDialog() {
  setActivePinia(createPinia())
  wrapper = mount(PhotoUploadDialog, {
    attachTo: document.body,
    props: { open: true, machine: 'host', directory: '/home/dev/repo' },
  })
  await flushPromises()
}

async function choose(files: File[]) {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]')!
  Object.defineProperty(input, 'files', { configurable: true, value: files })
  input.dispatchEvent(new Event('change', { bubbles: true }))
  await flushPromises()
}

function button(name: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('button')].find((element) => element.textContent?.trim() === name)!
}

describe('photo upload dialog', () => {
  it('submits the selected photo File objects unchanged and reports destination and byte counts', async () => {
    const upload = vi.spyOn(filesystemApi, 'uploadPhoto').mockImplementation(async (_machine, _directory, file) => ({ path: `/home/dev/repo/${file.name}`, size: file.size }))
    await openDialog()
    const first = new File([new Uint8Array([0, 255, 216, 10])], 'first.heic', { type: 'image/heic' })
    const second = new File([new Uint8Array([255, 217, 0])], 'second.dng', { type: 'image/x-adobe-dng' })
    await choose([first, second])
    button('Send photos').click()
    await flushPromises()
    expect(upload.mock.calls).toEqual([
      ['host', '/home/dev/repo', first, 'first.heic'],
      ['host', '/home/dev/repo', second, 'second.dng'],
    ])
    expect(document.querySelector('[aria-label="Photo destination"]')?.textContent).toBe('/home/dev/repo')
    expect(document.querySelector('[aria-label="Sent photos"]')?.textContent).toContain('first.heic · 4 bytes')
    expect(document.querySelector('[aria-label="Sent photos"]')?.textContent).toContain('second.dng · 3 bytes')
    expect(wrapper!.emitted('uploaded')).toEqual([['/home/dev/repo/first.heic'], ['/home/dev/repo/second.dng']])
  })

  it('keeps only failed and not-yet-sent files selected after a partial error so retry skips completed uploads', async () => {
    const first = new File([new Uint8Array([1])], 'first.heic', { type: 'image/heic' })
    const second = new File([new Uint8Array([2])], 'second.heic', { type: 'image/heic' })
    const upload = vi.spyOn(filesystemApi, 'uploadPhoto')
      .mockResolvedValueOnce({ path: '/home/dev/repo/first.heic', size: 1 })
      .mockRejectedValueOnce(new ApiError(504, 'upload timed out', 'Retry the original photo.'))
      .mockResolvedValueOnce({ path: '/home/dev/repo/second.heic', size: 1 })
    await openDialog()
    await choose([first, second])
    button('Send photos').click()
    await flushPromises()
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('upload timed out')
    const pending = document.querySelector('[aria-label="Selected photos"]')!.textContent!
    expect(pending).not.toContain('first.heic')
    expect(pending).toContain('second.heic')
    button('Send photos').click()
    await flushPromises()
    expect(upload.mock.calls.map((call) => call[2].name)).toEqual(['first.heic', 'second.heic', 'second.heic'])
    expect(document.querySelector('[role="alert"]')).toBeNull()
  })
})
