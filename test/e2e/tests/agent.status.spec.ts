import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test.use({ serviceWorkers: 'allow' })

test('(T13) Agent logos appear before provider hook status on collapsed session rows', async ({ page, ui, target }) => {
  const fresh = newAccount('e2e-agent-status')
  forbidInLogs(fresh.email, fresh.password)
  const session = uniqueName('agent-status')
  try {
    await owner.allow(fresh.email)
    await ui.createAccount(fresh)
    await page.evaluate(async () => { await navigator.serviceWorker.ready })
    await page.reload()
    await expect(page.evaluate(() => navigator.serviceWorker.controller !== null)).resolves.toBe(true)
    await target.run([
      'mkdir -p /home/dev/.hostbud-test-bin',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/coy',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/cly',
      `tmux new-session -d -s ${shq(session)} -c /home/dev`,
      `tmux send-keys -t ${shq('=' + session + ':')} 'PATH=/home/dev/.hostbud-test-bin:$PATH coy 60' Enter`,
    ].join(' && '))

    const row = page.locator(`[data-tree-key="session:${session}"]`)
    await ui.showList()
    const firstPane = (await target.tmux('list-panes', '-t', `=${session}:`, '-F', '#{pane_id}')).trim().split('\n')[0]
    await target.tmux('set-option', '-p', '-t', firstPane, '@hostbud_agent_status', 'working')
    await expect.poll(async () => await row.locator('[data-session-status]').getAttribute('data-status')).toBe('working')
    await expect(row.locator('[data-session-status]')).toHaveText('🟢')
    await expect(row.locator('[data-agent-mark]').first()).toHaveAttribute('data-agent', 'codex')
    await expect(row.locator('[data-agent-mark]').first()).toBeVisible()
    await expect(row.locator('[data-agent-mark]').first().evaluate((mark) => {
      const status = mark.closest('[data-tree-key]')?.querySelector('[data-session-status]')
      return Boolean(status && (mark.compareDocumentPosition(status) & Node.DOCUMENT_POSITION_FOLLOWING))
    })).resolves.toBe(true)
    await expect(row).toHaveAttribute('aria-label', `${session}, Working, Codex running`)
    await expect(row.locator('button[aria-label^="Expand "]')).toHaveCount(0)

    await target.tmux('set-option', '-p', '-t', firstPane, '@hostbud_agent_status', 'ended')
    await expect(row.locator('[data-session-status]')).toHaveAttribute('data-status', 'working')
    await expect(row.locator('[data-session-status]')).toHaveText('🟢')
    await target.tmux('set-option', '-p', '-t', firstPane, '@hostbud_agent_status', 'working')
    await expect(row.locator('[data-session-status]')).toHaveAttribute('data-status', 'working')

    // A stale cache-first navigation shell used to discard the status marker
    // on an ordinary refresh after a hard reload showed the new build.
    await page.evaluate(async () => {
      const name = (await caches.keys()).find((cacheName) => cacheName.startsWith('hostbud-shell-'))
      if (!name) throw new Error('hostbud shell cache is missing')
      await (await caches.open(name)).put('/', new Response('<!doctype html><title>stale shell</title><body>stale shell</body>', {
        headers: { 'Content-Type': 'text/html' },
      }))
    })
    await page.reload()
    await expect(row.locator('[data-session-status]')).toHaveAttribute('data-status', 'working')
    await expect(page.getByText('stale shell')).toHaveCount(0)

    await target.tmux('set-option', '-p', '-t', firstPane, '@hostbud_agent_status', 'blocked')
    await expect.poll(async () => await row.locator('[data-session-status]').getAttribute('data-status')).toBe('blocked')
    await expect(row.locator('[data-session-status]')).toHaveText('🚧')

    await target.tmux('split-window', '-t', `=${session}:`, '-c', '/home/dev', '/home/dev/.hostbud-test-bin/cly 60')
    const panes = (await target.tmux('list-panes', '-t', `=${session}:`, '-F', '#{pane_id}')).trim().split('\n')
    const secondPane = panes.find((pane) => pane !== firstPane)
    if (!secondPane) throw new Error('expected the second agent pane')
    await target.tmux('set-option', '-p', '-t', firstPane, '@hostbud_agent_status', 'working')
    await target.tmux('set-option', '-p', '-t', secondPane, '@hostbud_agent_status', 'ended')
    await expect.poll(async () => await row.locator('[data-session-status]').getAttribute('data-status')).toBe('working')

    await target.tmux('set-option', '-p', '-u', '-t', secondPane, '@hostbud_agent_status')
    await target.tmux('set-option', '-p', '-t', firstPane, '@hostbud_agent_status', 'ended')
    await expect(row.locator('[data-session-status]')).toHaveCount(0)
    await target.tmux('send-keys', '-t', firstPane, 'C-c')
    await expect.poll(async () => await row.locator('[data-session-status]').getAttribute('data-status')).toBe('ended')
    await expect(row.locator('[data-session-status]')).toHaveText('🎯')
    await expect(row.locator('button[aria-label^="Expand "]')).toHaveCount(0)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + session)} 2>/dev/null || true`)
    await target.exec('rm -f /home/dev/.hostbud-test-bin/coy /home/dev/.hostbud-test-bin/cly')
  }
})
