import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AuthView from '@/components/AuthView.vue'
import CreateSessionDialog from '@/components/CreateSessionDialog.vue'
import FileBrowser from '@/components/FileBrowser.vue'
import InlineRename from '@/components/InlineRename.vue'
import ProjectSessionDialog from '@/components/ProjectSessionDialog.vue'
import { stubFetch } from '@/test-utils'

// Browser autocomplete policy (M8 T3): every application input says
// autocomplete="off", except the login screen's password input, whose
// markup stays exactly as it was.

const LOGIN_PASSWORD = `:autocomplete="m === 'signin' ? 'current-password' : 'new-password'"`

/** Every form control in the app's Vue sources: file and opening tag. */
const sources = import.meta.glob('../**/*.vue', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
const controls = Object.entries(sources).flatMap(([file, text]) =>
  [...text.matchAll(/<(input|textarea|select|ComboboxInput)\b(?:[^>"']|"[^"]*"|'[^']*')*>/g)].map((m) => ({ file: file.replace('../', ''), tag: m[0] })),
)

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('autocomplete policy (source audit)', () => {
  it('finds the app\'s form controls', () => {
    const files = new Set(controls.map((c) => c.file))
    for (const f of ['App.vue', 'components/AuthView.vue', 'components/FileBrowser.vue', 'components/CommandPalette.vue', 'components/ProjectSessionDialog.vue', 'components/InlineRename.vue', 'components/TerminalSearch.vue', 'components/CreateSessionDialog.vue'])
      expect(files).toContain(f)
  })

  it('every control but the login password declares autocomplete="off"', () => {
    const offenders = controls.filter((c) => !c.tag.includes(LOGIN_PASSWORD) && !/\sautocomplete="off"/.test(c.tag))
    expect(offenders).toEqual([])
  })

  it('the login password keeps its markup: the only exception, unchanged', () => {
    const passwords = controls.filter((c) => c.tag.includes(LOGIN_PASSWORD))
    expect(passwords.map((c) => c.file)).toEqual(['components/AuthView.vue'])
    const tag = passwords[0].tag.replace(/\s+/g, ' ')
    expect(tag).toBe(`<input v-model="password" type="password" name="password" ${LOGIN_PASSWORD} required class="rounded border border-border bg-bg px-2 py-2 text-base" >`)
  })
})

const rendered = (root: ParentNode) => [...root.querySelectorAll<HTMLInputElement>('input, textarea, select')]

describe('autocomplete policy (rendered forms)', () => {
  it('login screen: email off; password current-password on sign-in, new-password on register', async () => {
    const wrapper = mount(AuthView, { attachTo: document.body })
    const password = () => wrapper.get('input[type=password]').element as HTMLInputElement
    expect(wrapper.get('input[type=email]').attributes('autocomplete')).toBe('off')
    expect(password().getAttribute('autocomplete')).toBe('current-password')
    expect(password().name).toBe('password')
    expect(password().required).toBe(true)
    await wrapper.findAll('[role=tab]')[1].trigger('mousedown')
    await flushPromises()
    await wrapper.findAll('[role=tab]')[1].trigger('click')
    await flushPromises()
    const register = wrapper.findAll('input[type=password]').map((i) => i.attributes('autocomplete'))
    expect(register).toContain('new-password')
    wrapper.unmount()
  })

  it('file browser, new-session, project-session and rename forms render autocomplete="off"', async () => {
    stubFetch((_method, path) => {
      if (path.endsWith('/fs/home')) return { status: 200, body: { path: '/home/dev' } }
      if (path.includes('/fs?')) return { status: 200, body: { path: '/home/dev', entries: [] } }
      if (path.startsWith('/api/projects?')) return { status: 200, body: { projects: [] } }
      if (path.endsWith('/recent-commands')) return { status: 200, body: { commands: [] } }
      return { status: 200, body: {} }
    })
    const project = { id: 'p1', machineId: 'host', path: '/home/dev/work', name: 'work', sortOrder: 0, pinned: false, createdAt: '', updatedAt: '' }
    const wrappers = [
      mount(FileBrowser, { props: { machine: 'host' }, attachTo: document.body }),
      mount(CreateSessionDialog, { props: { open: true, machine: 'host' }, attachTo: document.body }),
      mount(ProjectSessionDialog, { props: { project }, attachTo: document.body }),
      mount(InlineRename, { props: { name: 'work', commit: async () => undefined }, attachTo: document.body }),
    ]
    await flushPromises()
    const inputs = rendered(document.body)
    expect(inputs.length).toBeGreaterThanOrEqual(8)
    expect(inputs.filter((i) => i.getAttribute('autocomplete') !== 'off').map((i) => i.outerHTML)).toEqual([])
    for (const w of wrappers) w.unmount()
  })
})
