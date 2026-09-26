import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CreateSessionDialog from './CreateSessionDialog.vue'
import KillSessionDialog from './KillSessionDialog.vue'
import RenameSessionDialog from './RenameSessionDialog.vue'
import ToastRegion from './ToastRegion.vue'
import { sessionNameError } from '@/lib/names'
import { stubFetch } from '@/test-utils'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const $ = (sel: string) => document.body.querySelector(sel) as HTMLElement | null
const input = (name: string) => $(`input[name=${name}]`) as HTMLInputElement
async function type(name: string, value: string) {
  const el = input(name)
  el.value = value
  el.dispatchEvent(new Event('input'))
  el.dispatchEvent(new Event('blur'))
  await flushPromises()
}
async function click(text: string) {
  const b = [...document.body.querySelectorAll('button')].find((x) => x.textContent?.trim() === text)
  if (!b) throw new Error(`no button "${text}"`)
  b.click()
  await flushPromises()
}

function mountOpen<T>(component: T, props: Record<string, unknown>) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return mount(component as any, { props: { open: true, ...props }, attachTo: document.body })
}

describe('session name rule', () => {
  it('matches the backend regex', () => {
    for (const ok of ['a', 'acc-a', 'My_Session-2', 'x'.repeat(64)]) expect(sessionNameError(ok)).toBe('')
    for (const bad of ['a.b', 'a:b', 'a b', 'ä', 'x'.repeat(65)]) expect(sessionNameError(bad)).not.toBe('')
    expect(sessionNameError('')).toBe('')
    expect(sessionNameError('', true)).toBe('Enter a name.')
  })
})

describe('CreateSessionDialog', () => {
  it('defaults the directory to ~ and sends only what was given', async () => {
    const calls = stubFetch(() => ({ status: 201, body: { name: 'dev' } }))
    const w = mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    expect(input('path').value).toBe('~')
    expect(input('name').value).toBe('')
    await click('Create')
    expect(calls).toEqual([{ method: 'POST', path: '/api/machines/host/sessions', body: { path: '~' } }])
    expect(w.emitted('created')).toEqual([['dev']])
    expect(w.emitted('update:open')).toEqual([[false]])
  })

  it('passes name, directory and start command', async () => {
    const calls = stubFetch(() => ({ status: 201, body: { name: 'web' } }))
    mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await type('path', '~/some/dir')
    await type('name', 'web')
    await type('startCommand', 'htop')
    await click('Create')
    expect(calls[0].body).toEqual({ name: 'web', path: '~/some/dir', startCommand: 'htop' })
  })

  it('rejects invalid names inline without calling the API', async () => {
    const calls = stubFetch(() => ({ status: 201, body: { name: 'x' } }))
    mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    for (const bad of ['a.b', 'a:b', 'a b']) {
      await type('name', bad)
      await click('Create')
      expect($('#create-name-error')?.textContent).toContain("Use only letters, digits, '-' and '_'.")
      expect(input('name').getAttribute('aria-invalid')).toBe('true')
    }
    expect(calls).toEqual([])
  })

  it('shows the backend error and hint in the dialog and stays open', async () => {
    stubFetch(() => ({
      status: 409,
      body: { error: 'a session named "web" already exists', hint: 'Pick another name, or open the existing session.' },
    }))
    const w = mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await type('name', 'web')
    await click('Create')
    const toast = $('[role=dialog] [role=alert]')!
    expect(toast.textContent).toContain("Couldn't create the session")
    expect(toast.textContent).toContain('A session named "web" already exists.')
    expect(toast.textContent).toContain('Pick another name')
    expect(w.emitted('update:open')).toBeUndefined()
  })
})

describe('RenameSessionDialog', () => {
  it('prefills the name and renames', async () => {
    const calls = stubFetch(() => ({ status: 200, body: { name: 'new' } }))
    const w = mountOpen(RenameSessionDialog, { machine: 'host', session: 'old' })
    await flushPromises()
    expect(input('name').value).toBe('old')
    await type('name', 'new')
    await click('Rename')
    expect(calls).toEqual([{ method: 'PATCH', path: '/api/machines/host/sessions/old', body: { name: 'new' } }])
    expect(w.emitted('renamed')).toEqual([['old', 'new']])
  })

  it('blocks invalid names', async () => {
    const calls = stubFetch(() => ({ status: 200, body: {} }))
    mountOpen(RenameSessionDialog, { machine: 'host', session: 'old' })
    await flushPromises()
    await type('name', 'x.y')
    expect(($('button[type=submit]') as HTMLButtonElement).disabled).toBe(true)
    await type('name', '')
    expect($('#rename-name-error')?.textContent).toBe('Enter a name.')
    expect(calls).toEqual([])
  })
})

describe('KillSessionDialog', () => {
  it('reports a failed kill in a toast (the dialog has closed)', async () => {
    stubFetch(() => ({ status: 503, body: { error: 'tmux not found on the host', hint: 'Install it with `sudo apt install tmux`' } }))
    mount(ToastRegion, { attachTo: document.body })
    mountOpen(KillSessionDialog, { machine: 'host', session: 'acc-a' })
    await flushPromises()
    await click('Kill session')
    const toast = $('section[aria-label=Notifications] [role=alert]')!
    expect(toast.textContent).toContain("Couldn't kill the session")
    expect(toast.textContent).toContain('Tmux not found on the host.')
    expect(toast.textContent).toContain('sudo apt install tmux')
  })

  it('Cancel makes no API call', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    const w = mountOpen(KillSessionDialog, { machine: 'host', session: 'acc-a' })
    await flushPromises()
    expect($('[role=alertdialog]')?.textContent).toContain('Kill session acc-a?')
    await click('Cancel')
    expect(calls).toEqual([])
    expect(w.emitted('update:open')).toEqual([[false]])
    expect(w.emitted('killed')).toBeUndefined()
  })

  it('Confirm calls DELETE', async () => {
    const calls = stubFetch(() => ({ status: 204 }))
    const w = mountOpen(KillSessionDialog, { machine: 'host', session: 'acc-a' })
    await flushPromises()
    await click('Kill session')
    expect(calls).toEqual([{ method: 'DELETE', path: '/api/machines/host/sessions/acc-a', body: undefined }])
    expect(w.emitted('killed')).toEqual([['acc-a']])
  })
})
