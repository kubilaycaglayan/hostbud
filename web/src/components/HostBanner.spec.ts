import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import HostBanner from './HostBanner.vue'
import type { Machine } from '@/api/types'

const m = (status: Machine['status'], error = '', hint = ''): Machine => ({
  id: 'host', label: 'Host', status, error, hint, os: 'Linux', home: '/home/dev', tmuxVersion: '', tmuxMissing: status === 'tmux_missing',
})

describe('HostBanner', () => {
  it('is hidden while the host is fine or unknown', () => {
    expect(mount(HostBanner, { props: { machine: m('ok') } }).find('[role=alert]').exists()).toBe(false)
    expect(mount(HostBanner, { props: { machine: m('unknown') } }).find('[role=alert]').exists()).toBe(false)
    expect(mount(HostBanner, { props: {} }).find('[role=alert]').exists()).toBe(false)
  })

  it('shows unreachable with the actionable hint', () => {
    const w = mount(HostBanner, {
      props: { machine: m('unreachable', "can't reach sshd on the host (connection refused)", 'Check that sshd is running on the host: `sudo systemctl status ssh`') },
    })
    const alert = w.get('[role=alert]').text()
    expect(alert).toContain('Host unreachable')
    expect(alert).toContain('connection refused')
    expect(alert).toContain('sudo systemctl status ssh')
  })

  it('shows tmux missing with the install hint', () => {
    const w = mount(HostBanner, {
      props: { machine: m('tmux_missing', 'tmux not found on the host', 'Install it with `sudo apt install tmux` (macOS: `brew install tmux`)') },
    })
    const alert = w.get('[role=alert]').text()
    expect(alert).toContain('tmux not found on the host')
    expect(alert).toContain('sudo apt install tmux')
  })
})
