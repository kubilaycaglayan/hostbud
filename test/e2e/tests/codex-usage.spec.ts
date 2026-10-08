import { expect, test } from '../helpers/fixtures.ts'
import { MACHINE, mutate, ORIGIN } from '../helpers/api.ts'
import { shq, uniqueName } from '../helpers/target.ts'

// M8 T28: metadata travels through the real inventory/API/events and header.
// Transcript parsing and the hook-to-tmux path also run in integration tests.
test('(T28, T31) Active session header aligns project and token usage', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-codex-usage')
  const path = `/home/dev/${uniqueName('header-project')}`
  const projectName = uniqueName('Project')
  await target.run(`mkdir -p ${shq(path)} /home/dev/.hostbud-test-bin && ln -sf /bin/sleep /home/dev/.hostbud-test-bin/coy`)
  const projectResponse = await mutate(page.request, 'POST', '/api/projects', { machineId: MACHINE, path, name: projectName }, ORIGIN)
  expect(projectResponse.status(), await projectResponse.text()).toBe(201)
  await target.tmux('new-session', '-d', '-s', name, '-c', path, '/home/dev/.hostbud-test-bin/coy 120')
  try {
    await ui.open()
    await ui.openTerminal(name)
    const header = page.getByRole('region', { name: `Terminal: ${name}`, exact: true }).locator('[data-terminal-header]')
    const usage = header.locator('[data-agent-usage]')
    const project = header.locator('[data-terminal-project]')
    await expect(project).toHaveText(projectName)
    await expect(project).toHaveAttribute('aria-label', `Project: ${projectName}`)
    await expect(usage).toHaveCount(0)
    await target.tmux('set-option', '-p', '-t', `=${name}:.0`, '@hostbud_codex_usage', '12000,345678,200000')
    await expect(usage).toHaveText('12K ctx · 345.7K used')
    await expect(usage).toHaveClass(/text-center/)
    const headerBox = await header.boundingBox()
    const usageBox = await usage.boundingBox()
    expect(Math.abs((usageBox!.x + usageBox!.width / 2) - (headerBox!.x + headerBox!.width / 2))).toBeLessThan(80)
    await expect(usage).toHaveAttribute('aria-label', /12,000 of 200,000 context tokens; 345,678 total tokens consumed/)
    await target.tmux('set-option', '-p', '-t', `=${name}:.0`, '@hostbud_codex_usage', '2000,360000,200000')
    await expect(usage).toHaveText('2K ctx · 360K used')
    await page.reload()
    await expect(usage).toHaveText('2K ctx · 360K used')
    await target.tmux('split-window', '-t', `=${name}:`)
    await expect(usage).toHaveCount(0)
    await target.tmux('select-pane', '-t', `=${name}:.0`)
    await expect(usage).toHaveText('2K ctx · 360K used')
    await target.tmux('set-option', '-p', '-t', `=${name}:.0`, '@hostbud_codex_usage', '')
    await expect(usage).toHaveCount(0)
    // Zero is a reported value, distinct from unavailable.
    await target.tmux('set-option', '-p', '-t', `=${name}:.0`, '@hostbud_codex_usage', '0,0,0')
    await expect(usage).toHaveText('0 ctx · 0 used')
    const bounds = await header.boundingBox()
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + name)} 2>/dev/null || true`)
  }
})

// M8 T30: Claude Code sessions use the same header via @hostbud_claude_usage.
test('Claude Code header shows context and consumed tokens', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-claude-usage')
  await target.run('mkdir -p /home/dev/.hostbud-test-bin && ln -sf /bin/sleep /home/dev/.hostbud-test-bin/cly')
  await target.tmux('new-session', '-d', '-s', name, '-c', '/home/dev', '/home/dev/.hostbud-test-bin/cly 120')
  try {
    await ui.open()
    await ui.openTerminal(name)
    const header = page.getByRole('region', { name: `Terminal: ${name}`, exact: true }).locator('[data-terminal-header]')
    const usage = header.locator('[data-agent-usage]')
    await expect(usage).toHaveCount(0)
    await target.tmux('set-option', '-p', '-t', `=${name}:.0`, '@hostbud_claude_usage', '74243,417626,0')
    await expect(usage).toHaveText('74.2K ctx · 417.6K used')
    await expect(usage).toHaveAttribute('aria-label', /^Claude Code: 74,243 context tokens; 417,626 total tokens consumed/)
    await target.tmux('set-option', '-p', '-t', `=${name}:.0`, '@hostbud_claude_usage', '')
    await expect(usage).toHaveCount(0)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + name)} 2>/dev/null || true`)
  }
})

test('(T38) Adaptive title bar expands only on overflow and shows branch context', async ({ page, ui, target }) => {
  const name = uniqueName('e2e-branch-header')
  const second = uniqueName('e2e-nogit-header')
  const path = `/home/dev/${uniqueName('header-git')}`
  await target.run(`mkdir -p ${shq(path)} && git -C ${shq(path)} init -q -b feature/adaptive-title-bar-with-a-long-branch-name && touch ${shq(path)}/one.txt ${shq(path)}/two.txt`)
  await target.tmux('new-session', '-d', '-s', name, '-c', path)
  try {
    await ui.open()
    await ui.openTerminal(name)
    const header = page.getByRole('region', { name: `Terminal: ${name}`, exact: true }).locator('[data-terminal-header]')
    const branch = header.locator('[data-git-branch]')
    await expect(branch).toContainText('feature/adaptive-title-bar-with-a-long-branch-name')
    await expect(branch.locator('[data-git-changed-files]')).toHaveText('(2)')
    await page.setViewportSize({ width: 320, height: 720 })
    await expect(header).toHaveAttribute('data-overflow', 'true')
    await expect(header).not.toHaveAttribute('data-expanded', 'true')
    const compactHeader = await header.boundingBox()
    expect(compactHeader!.height).toBeGreaterThan(44)
    await header.click()
    await expect(header).toHaveAttribute('data-expanded', 'true')
    // The phone-width sidebar covers the top-left body click target; Escape
    // dismisses the expanded header reliably in every viewport profile.
    await page.keyboard.press('Escape')
    await expect(header).not.toHaveAttribute('data-expanded', 'true')
    // An unused directory has no branch label.
    const noGit = `/home/dev/${uniqueName('header-nogit')}`
    await target.run(`mkdir -p ${shq(noGit)}`)
    await target.tmux('new-session', '-d', '-s', second, '-c', noGit)
    await ui.openTerminal(second)
    await expect(page.getByRole('region', { name: `Terminal: ${second}`, exact: true }).locator('[data-git-branch]')).toHaveCount(0)
  } finally {
    await target.exec(`tmux kill-session -t ${shq('=' + name)} 2>/dev/null || true`)
    await target.exec(`tmux kill-session -t ${shq('=' + second)} 2>/dev/null || true`)
  }
})
