import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CreateSessionDialog from './CreateSessionDialog.vue'
import KillSessionDialog from './KillSessionDialog.vue'
import ToastRegion from './ToastRegion.vue'
import { normalizeSessionName, projectNameError, sessionNameError } from '@/lib/names'
import { useToastsStore } from '@/stores/toasts'
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

  it('turns whitespace into hyphens like the backend', () => {
    expect(normalizeSessionName('new session')).toBe('new-session')
    expect(normalizeSessionName('  a \t b\n')).toBe('a-b')
    expect(normalizeSessionName('a   b  c')).toBe('a-b-c')
    expect(normalizeSessionName('   ')).toBe('')
  })
})

describe('project name rule', () => {
  it('trims names and measures the 255-byte limit in UTF-8', () => {
    expect(projectNameError('  project  ')).toBe('')
    expect(projectNameError('界'.repeat(85))).toBe('')
    expect(projectNameError('界'.repeat(86))).toBe('Use 255 bytes or fewer.')
    expect(projectNameError('   ')).toBe('Enter a name.')
  })
})

describe('CreateSessionDialog', () => {
  it('renders compact create and kill dialogs as bottom sheets', async () => {
    const create = mountOpen(CreateSessionDialog, { machine: 'host', compact: true })
    const kill = mountOpen(KillSessionDialog, { machine: 'host', session: 'old', compact: true })
    await flushPromises()
    expect($('[role="dialog"]')?.className).toContain('bottom-0')
    expect($('[role="alertdialog"]')?.className).toContain('bottom-0')
    const dialogs = [...document.body.querySelectorAll('[role="dialog"]')]
    expect(dialogs.length).toBe(1)
    expect(dialogs.every((dialog) => dialog.className.includes('bottom-0'))).toBe(true)
    expect([...document.body.querySelectorAll('button')].every((button) => button.classList.contains('touch-target'))).toBe(true)
    expect([...document.body.querySelectorAll('input')].every((field) => field.classList.contains('text-base'))).toBe(true)
    create.unmount()
    kill.unmount()
  })

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

  it('announces a numbered name only when a typed name changes', async () => {
    stubFetch(() => ({ status: 201, body: { name: 'work-1' } }))
    mount(ToastRegion, { attachTo: document.body })
    mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await type('name', 'work')
    await click('Create')
    expect(useToastsStore().toasts).toMatchObject([{
      title: 'Session name changed',
      message: 'Named "work-1": "work" was already taken.',
      tone: 'info',
    }])
    expect($('[role="status"]')?.textContent).toContain('Named "work-1"')
  })

  it('does not announce an auto-derived name when the name field is empty', async () => {
    stubFetch(() => ({ status: 201, body: { name: 'dev-1' } }))
    mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await click('Create')
    expect(useToastsStore().toasts).toEqual([])
  })

  it('turns spaces in the name into hyphens without asking', async () => {
    const calls = stubFetch(() => ({ status: 201, body: { name: 'new-session' } }))
    const w = mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await type('name', '  new   session ')
    expect(input('name').getAttribute('aria-invalid')).not.toBe('true')
    await click('Create')
    await flushPromises()
    expect((calls[0]?.body as { name?: string }).name).toBe('new-session')
    expect(useToastsStore().toasts).toEqual([])
    expect(w.emitted('created')?.[0]).toEqual(['new-session'])
  })

  it('rejects invalid names inline without calling the API', async () => {
    const calls = stubFetch(() => ({ status: 201, body: { name: 'x' } }))
    mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    for (const bad of ['a.b', 'a:b', 'a b.c']) {
      await type('name', bad)
      await click('Create')
      expect($('#create-name-error')?.textContent).toContain("Use only letters, digits, '-' and '_'.")
      expect(input('name').getAttribute('aria-invalid')).toBe('true')
    }
    expect(calls).toEqual([])
  })

  it('shows the missing-path error and hint in the dialog and stays open', async () => {
    stubFetch(() => ({
      status: 400,
      body: { error: "directory /home/dev/missing doesn't exist on the host", hint: 'Pick an existing directory, or create it first.' },
    }))
    const w = mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await type('name', 'web')
    await click('Create')
    const toast = $('[role=dialog] [role=alert]')!
    expect(toast.textContent).toContain("Couldn't create the session")
    expect(toast.textContent).toContain("Directory /home/dev/missing doesn't exist on the host.")
    expect(toast.textContent).toContain('Pick an existing directory')
    expect(w.emitted('update:open')).toBeUndefined()
  })

  it('shows an exec timeout in the open create dialog and keeps entered values', async () => {
    stubFetch(() => ({ status: 504, body: { error: "The host didn't answer within 10s", hint: 'hostbud will retry' } }))
    const w = mountOpen(CreateSessionDialog, { machine: 'host' })
    await flushPromises()
    await type('name', 'keep-me')
    await click('Create')
    expect(input('name').value).toBe('keep-me')
    expect($('[role=dialog] [role=alert]')?.textContent).toContain("The host didn't answer within 10s")
    expect(w.emitted('update:open')).toBeUndefined()
  })
})

describe('KillSessionDialog', () => {
  it('shows an exec timeout in the kill toast', async () => {
    stubFetch(() => ({ status: 504, body: { error: "The host didn't answer within 10s", hint: 'hostbud will retry' } }))
    mount(ToastRegion, { attachTo: document.body })
    mountOpen(KillSessionDialog, { machine: 'host', session: 'acc-a' })
    await flushPromises()
    await click('Kill session')
    expect($('section[aria-label=Notifications] [role=alert]')?.textContent).toContain("The host didn't answer within 10s")
  })

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
