import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import ServerStatusIcon from './ServerStatusIcon.vue'
import { useMachinesStore } from '@/stores/machines'
import { useLiveStore } from '@/stores/live'
import type { Machine, MachineStatus } from '@/api/types'

const machine = (id: string, status: MachineStatus) => ({ id, status } as Machine)

describe('server header health (V2-M13 T8)', () => {
  it('updates both server interiors from aggregate live health without showing stale success', async () => {
    setActivePinia(createPinia())
    const machines = useMachinesStore()
    const live = useLiveStore()
    const icon = mount(ServerStatusIcon)
    const health = () => icon.attributes('data-server-health')
    expect(health()).toBe('unknown')
    live.state = 'open'
    machines.machines = [machine('host', 'ok'), machine('server-a', 'ok')]
    await nextTick()
    expect(health()).toBe('ok')
    expect(icon.findAll('rect')).toHaveLength(2)
    expect(icon.text()).toBe('All servers connected')
    for (const status of ['unreachable', 'tmux_missing', 'unknown'] as const) {
      machines.apply({ type: 'machine.status', machine: 'server-a', payload: machine('server-a', status) })
      await nextTick()
      expect(health()).toBe(status === 'unknown' ? 'unknown' : 'error')
    }
    machines.apply({ type: 'machine.removed', machine: 'server-a', payload: { id: 'server-a' } })
    await nextTick()
    expect(health()).toBe('ok')
    live.state = 'reconnecting'
    await nextTick()
    expect(health()).toBe('unknown')
    live.state = 'open'
    machines.machines = []
    await nextTick()
    expect(health()).toBe('unknown')
  })
})
