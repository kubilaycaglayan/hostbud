import { expect, test } from '../helpers/fixtures.ts'
import { newAccount } from '../helpers/auth.ts'
import { owner } from '../helpers/db.ts'
import { forbidInLogs, MACHINE, mutate, ORIGIN, putUIState } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

// A project whose session is working (🟢) shows its name in green, live.

test('Project name turns green while one of its sessions is working', async ({ page, ui, target }) => {
  const fresh = newAccount('e2e-project-working')
  forbidInLogs(fresh.email, fresh.password)
  await owner.allow(fresh.email)
  await ui.createAccount(fresh)
  const api = page.context().request
  const name = uniqueName('e2e-working-project')
  const session = uniqueName('e2e-working-session')
  await target.run(`mkdir -p ${shq(`/home/dev/${name}`)}`)
  const res = await mutate(api, 'POST', '/api/projects', { machineId: MACHINE, path: `/home/dev/${name}`, name }, ORIGIN)
  expect(res.status()).toBe(201)
  const id = ((await res.json()) as { id: string }).id
  try {
    await putUIState(api, 'tree', { version: 2, projects: [id], sessions: {}, pinned: [], hidden: { projects: [], sessions: [] }, collapsed: [], expanded: [], showHidden: false })
    // A fake Codex (sleep named coy) so the pane counts as an agent pane.
    await target.run([
      'mkdir -p /home/dev/.hostbud-test-bin',
      'ln -sf /bin/sleep /home/dev/.hostbud-test-bin/coy',
      `tmux new-session -d -s ${shq(session)} -c ${shq(`/home/dev/${name}`)} /home/dev/.hostbud-test-bin/coy 60`,
    ].join(' && '))
    await page.reload()
    await ui.showList()
    const label = ui.treeItem(name).locator('span.font-semibold').first()
    await expect(label).not.toHaveAttribute('data-working', /.*/)

    const pane = (await target.tmux('list-panes', '-t', `=${session}:`, '-F', '#{pane_id}')).trim().split('\n')[0]
    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'working')
    await expect(label).toHaveAttribute('data-working', 'true')
    await expect(label).toHaveClass(/text-ok/)

    await target.tmux('set-option', '-p', '-t', pane, '@hostbud_agent_status', 'ended')
    await expect(label).not.toHaveAttribute('data-working', /.*/)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + session)} 2>/dev/null || true`)
    await target.exec('rm -f /home/dev/.hostbud-test-bin/coy')
    await mutate(api, 'DELETE', `/api/projects/${id}`)
  }
})
