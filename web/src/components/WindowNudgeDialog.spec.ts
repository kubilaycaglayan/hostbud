import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WindowNudgeDialog from './WindowNudgeDialog.vue'
import { stubFetch } from '@/test-utils'

const disabled = { enabled: false, machineId: 'host', directory: '/home/dev', provider: 'codex', schedule: '01-06-11-16-21', updatedAt: '' }
const buttons = () => [...document.body.querySelectorAll<HTMLButtonElement>('button')]
const byText = (text: string) => buttons().find((button) => button.textContent?.trim() === text)

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

describe('WindowNudgeDialog', () => {
  it('asks for confirmation before enabling and sends the chosen fixed schedule only after confirmation', async () => {
    const calls = stubFetch((method, path, body) => method === 'GET' ? { status: 200, body: disabled } : { status: 200, body: { ...disabled, ...(body as object), enabled: true, nextRunAt: '2026-11-27T01:00:00Z' } })
    mount(WindowNudgeDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()
    const provider = document.body.querySelector<HTMLSelectElement>('select')!
    provider.value = 'claude'
    provider.dispatchEvent(new Event('change'))
    const schedule = document.body.querySelectorAll<HTMLSelectElement>('select')[1]!
    schedule.value = '05-10-15-20-00'
    schedule.dispatchEvent(new Event('change'))
    const active = [...document.body.querySelectorAll<HTMLInputElement>('input')].find((input) => input.type === 'checkbox')!
    active.checked = true
    active.dispatchEvent(new Event('change'))
    byText('Save')!.click()
    await flushPromises()
    expect(document.body.querySelector('[role="alertdialog"]')?.textContent).toContain('window-nudge-chore')
    expect(calls.filter((call) => call.method === 'PUT')).toEqual([])
    byText('Confirm and enable')!.click()
    await flushPromises()
    expect(calls.filter((call) => call.method === 'PUT')).toEqual([{ method: 'PUT', path: '/api/window-nudge', body: { enabled: true, machineId: 'host', directory: '/home/dev', provider: 'claude', schedule: '05-10-15-20-00' } }])
  })

  it('can disable without asking to kill or replace the current session', async () => {
    const enabled = { ...disabled, enabled: true, nextRunAt: '2026-11-27T01:00:00Z' }
    const calls = stubFetch((method, path, body) => method === 'GET' ? { status: 200, body: enabled } : { status: 200, body: { ...enabled, ...(body as object), enabled: false } })
    mount(WindowNudgeDialog, { props: { open: true }, attachTo: document.body })
    await flushPromises()
    const active = [...document.body.querySelectorAll<HTMLInputElement>('input')].find((input) => input.type === 'checkbox')!
    active.checked = false
    active.dispatchEvent(new Event('change'))
    byText('Save')!.click()
    await flushPromises()
    expect(document.body.querySelector('[role="alertdialog"]')).toBeNull()
    expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(1)
    expect(calls.filter((call) => call.method === 'PUT')[0].body).toMatchObject({ enabled: false })
  })
})
