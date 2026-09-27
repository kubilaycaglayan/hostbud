import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

test('(T12) Agent logos appear on collapsed session rows', async ({ page, ui, target }) => {
  const fresh = newAccount('e2e-agent-marks')
  forbidInLogs(fresh.email, fresh.password)
  const session = uniqueName('agent-marks')
  const ordinary = uniqueName('agent-shell')
  try {
    await owner.allow(fresh.email)
    await ui.createAccount(fresh)
    await target.run([
      'mkdir -p /home/dev/.hostbud-test-bin',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/codex',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/claude',
      `tmux new-session -d -s ${shq(session)} -c /home/dev /home/dev/.hostbud-test-bin/codex 60`,
      `tmux split-window -t ${shq('=' + session + ':')} -c /home/dev /home/dev/.hostbud-test-bin/claude 60`,
    ].join(' && '))

    await ui.showList()
    const row = page.locator(`[data-tree-key="session:${session}"]`)
    await expect.poll(async () => await row.locator('[data-agent-mark]').count()).toBe(2)
    await expect(row.locator('[data-agent-mark]').first()).toBeVisible()
    await expect(row.locator('[data-agent="codex"]')).toHaveAttribute('title', 'Codex running')
    await expect(row.locator('[data-agent="claude"]')).toHaveAttribute('title', 'Claude Code running')
    await expect(row).toHaveAttribute('aria-label', `${session}, Codex running, Claude Code running`)
    await expect(row.locator('button[aria-label^="Expand "]')).toHaveCount(0)

    await target.run(`tmux new-session -d -s ${shq(ordinary)} -c /home/dev`)
    await expect.poll(async () => await ui.treeItem(ordinary).count()).toBe(1)
    await expect(ui.treeItem(ordinary).locator('[data-agent-mark]')).toHaveCount(0)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + session)} 2>/dev/null || true; tmux kill-session -t ${shq('=' + ordinary)} 2>/dev/null || true`)
    await target.exec('rm -f /home/dev/.hostbud-test-bin/codex /home/dev/.hostbud-test-bin/claude')
  }
})
