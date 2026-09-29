import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SettingsDialog from './SettingsDialog.vue'
import type { SupervisorStatus } from '@/api/types'
import { useQueuesStore } from '@/stores/queues'
import { stubFetch } from '@/test-utils'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const $$ = (sel: string) => [...document.body.querySelectorAll<HTMLElement>(sel)]
const input = () => $$('form[aria-label="Queue runs"] input')[0] as HTMLInputElement
const save = () => $$('button').find((b) => b.textContent?.trim() === 'Save')!

async function mountSettings(cap: number | null, parallel = true, answer?: (body: unknown) => { status: number; body?: unknown }, supervisor: SupervisorStatus = { enabled: false, reason: 'LLM supervisor is off: set HOSTBUD_LLM_PROVIDER=openai to enable' }) {
  const store = useQueuesStore()
  store.loaded = true
  store.parallelQueues = parallel
  const calls = stubFetch((method, path, body) => path === '/api/supervisor' ? { status: 200, body: supervisor } : method === 'GET' ? { status: 200, body: { maxConcurrentRuns: cap } } : answer ? answer(body) : { status: 200, body })
  mount(SettingsDialog, { props: { open: true, machine: 'host' }, attachTo: document.body })
  await flushPromises()
  return calls
}

function type(value: string) {
  input().value = value
  input().dispatchEvent(new Event('input'))
}

describe('SettingsDialog', () => {
  it('does not save a changed cap when confirmation is declined', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const calls = await mountSettings(null)
    type('5')
    save().click()
    await flushPromises()
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([])
    expect(input().value).toBe('5')
  })

  it('shows the supervisor off reason and privacy details when enabled', async () => {
    await mountSettings(null)
    expect($$('[data-testid="supervisor-settings"]')[0].textContent).toContain('set HOSTBUD_LLM_PROVIDER=openai')
    document.body.innerHTML = ''
    setActivePinia(createPinia())
    await mountSettings(null, true, undefined, { enabled: true, provider: 'openai', model: 'test-model', scrub: true, quietAfter: '10s', maxPerRunHour: 3 })
    const details = $$('[data-testid="supervisor-settings"]')[0].textContent ?? ''
    expect(details).toContain('openai / test-model')
    expect(details).toContain('Recent pane text leaves this host for the provider')
    expect(details).toContain('Common secrets are scrubbed first')
    expect(details).toContain('Flags are advisory and never advance a queue')
  })

  it('loads a configured cap; empty uses the parallel default', async () => {
    await mountSettings(3)
    expect(input().value).toBe('3')
    expect(input().getAttribute('autocomplete')).toBe('off')
    document.body.innerHTML = ''
    setActivePinia(createPinia())
    await mountSettings(null)
    expect(input().value).toBe('2')
  })

  it('validates before sending: whole numbers 1–32 or empty', async () => {
    const calls = await mountSettings(null)
    for (const bad of ['0', '33', '1.5', 'two', '-1']) {
      type(bad)
      save().click()
      await flushPromises()
      expect(document.body.textContent, bad).toContain('Enter a whole number from 1 to 32')
      expect(input().getAttribute('aria-invalid')).toBe('true')
    }
    expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(0)
    type(' 2 ')
    save().click()
    await flushPromises()
    type('')
    save().click()
    await flushPromises()
    expect(calls.filter((c) => c.method === 'PUT')).toEqual([
      { method: 'PUT', path: '/api/machines/host/capacity', body: { maxConcurrentRuns: 2 } },
      { method: 'PUT', path: '/api/machines/host/capacity', body: { maxConcurrentRuns: null } },
    ])
    expect($$('form[aria-label="Queue runs"] [role="status"]')[0].textContent).toContain('default limit')
  })

  it('shows the server error', async () => {
    await mountSettings(null, true, () => ({ status: 400, body: { error: 'the run cap must be a whole number from 1 to 32, or empty for no cap', hint: 'Leave it empty for no cap.' } }))
    type('5')
    save().click()
    await flushPromises()
    expect($$('[role="alert"]').map((a) => a.textContent).join(' ')).toContain('he run cap must be a whole number')
  })

  it('explains that the cap waits for the parallel-queues switch', async () => {
    await mountSettings(null, false)
    expect($$('[data-testid="parallel-off"]')[0].textContent).toContain('Run queues in parallel')
  })
})
