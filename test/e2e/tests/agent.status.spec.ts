import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test('Session status stays in the API without tree indicators or reserved space', async ({ page, ui, target }) => {
  const fresh = newAccount('e2e-agent-status')
  forbidInLogs(fresh.email, fresh.password)
  const session = uniqueName('agent-status')
  try {
    await owner.allow(fresh.email)
    await ui.createAccount(fresh)
    await target.run([
      'mkdir -p /home/dev/.hostbud-test-bin',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/coy',
      `tmux new-session -d -s ${shq(session)} -c /home/dev`,
      `tmux send-keys -t ${shq('=' + session + ':')} 'PATH=/home/dev/.hostbud-test-bin:$PATH coy 60' Enter`,
    ].join(' && '))
    const pane = (await target.tmux('list-panes', '-t', `=${session}:`, '-F', '#{pane_id}')).trim()
    const row = page.locator(`[data-tree-key="session:${session}"]`)
    await ui.showList()

    for (const status of ['working', 'blocked']) {
      await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', status)
      await expect.poll(async () => {
        const response = await page.request.get('/api/machines/host/sessions')
        const body = await response.json() as { sessions: { name: string; status?: string }[] }
        return body.sessions.find((item) => item.name === session)?.status
      }).toBe(status)
      await expect(row).toBeVisible()
      await expect(row.locator('[data-session-status], [data-status-slot]')).toHaveCount(0)
      await expect(row).not.toHaveAttribute('aria-label', /Working|Blocked|Ended/)
      await expect(row.locator('[data-session-prefix]')).not.toHaveClass(/gap-x-1/)
    }

    await target.tmux('send-keys', '-t', pane, 'C-c')
    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'ended')
    await expect.poll(async () => {
      const response = await page.request.get('/api/machines/host/sessions')
      const body = await response.json() as { sessions: { name: string; status?: string }[] }
      return body.sessions.find((item) => item.name === session)?.status
    }).toBe('ended')
    await expect(row.locator('[data-session-status], [data-status-slot]')).toHaveCount(0)

    await page.reload()
    await expect(row.locator('[data-session-status], [data-status-slot]')).toHaveCount(0)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + session)} 2>/dev/null || true`)
    await target.exec('rm -f /home/dev/.hostbud-test-bin/coy')
  }
})
