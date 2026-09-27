import { expect, test } from '../helpers/fixtures.ts'
import { getUIState, listSessions, mutate, ORIGIN } from '../helpers/api.ts'
import { uniqueName } from '../helpers/target.ts'

const EMPTY_TREE = {
  version: 2,
  projects: [],
  sessions: {},
  pinned: [],
  hidden: { projects: [], sessions: [] },
  collapsed: [],
  expanded: [],
  showHidden: false,
}

test.describe.configure({ mode: 'serial' })

test('seed account and target state for the following isolation check', async ({ request, target }) => {
  const name = uniqueName('e2e-fixture-project')
  const path = `/home/dev/${name}`
  await target.run(`mkdir -p '${path}'`)
  const created = await mutate(request, 'POST', '/api/projects', { machineId: 'host', path, name }, ORIGIN)
  expect(created.status(), await created.text()).toBe(201)
  const project = await created.json() as { id: string }
  await target.tmux('new-session', '-d', '-s', name)
  await mutate(request, 'PUT', '/api/ui-state/tree', { ...EMPTY_TREE, projects: [project.id] }, ORIGIN)
  await mutate(request, 'PUT', '/api/ui-state/theme', { version: 1, mode: 'dark' }, ORIGIN)
})

test('each browser scenario starts with clean shared account and target state', async ({ page, request, target }) => {
  await page.goto('/')
  await expect(page.getByRole('navigation', { name: 'Project and session tree' })).toBeVisible()
  expect((await request.get('/api/projects?machine=host')).status()).toBe(200)
  expect((await (await request.get('/api/projects?machine=host')).json()).projects).toEqual([])
  expect(await listSessions(request)).toEqual([])
  expect(await getUIState(page.request, 'tree')).toEqual(EMPTY_TREE)
  expect(await getUIState(page.request, 'theme')).toEqual({ version: 1, mode: 'system' })
  expect(await getUIState(request, 'tree')).toEqual(EMPTY_TREE)
  expect(await getUIState(request, 'theme')).toEqual({ version: 1, mode: 'system' })
  expect(await target.sessions()).toEqual([])
})
