import { expect, test as base } from '../helpers/fixtures.ts'
import { test as multiTest } from '../helpers/multi.ts'
import { test as llmTest } from '../helpers/llm.ts'
import { createQueue, addItem, control, getQueue, itemOf, newProject } from '../helpers/queues.ts'
import { Stubs } from '../helpers/stubs.ts'
import { llmDb, queues as mainQueues } from '../helpers/db.ts'
import { newDevice, pushfake, received, subscribe } from '../helpers/push.ts'
import { putNotificationSettings } from '../helpers/notifications.ts'
import { expect as rootExpect } from '../helpers/fixtures.ts'

const stubs = new Stubs()

base('(V2-M5 T1) Supervisor status is off without a provider and never exposes a key', async ({ request }) => {
  const response = await request.get('/api/supervisor')
  expect(response.status()).toBe(200)
  expect(await response.json()).toMatchObject({ enabled: false })
})

base('(V2-M5 T1) No provider means no calls or LLM events', async ({ request, target }) => {
  await target.resetTmux()
  await stubs.reset()
  await request.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  const project = await newProject(request, target, 'e2e-no-llm')
  const queue = await createQueue(request, project.id)
  await stubs.setBehavior('e2e no provider quiet', 'silent', 0.1)
  const item = await addItem(request, queue.id, { instruction: '/goal e2e no provider quiet' })
  await control(request, queue.id, 'start')
  await rootExpect.poll(async () => (await itemOf(request, queue.id, item.id)).status, { timeout: 30_000 }).toBe('needs_attention')
  rootExpect(await mainQueues.events((await itemOf(request, queue.id, item.id)).run!.id)).not.toContainEqual(rootExpect.objectContaining({ source: 'llm' }))
  rootExpect(await (await request.get('http://hostbud-e2e-llmfake:8080/ctl/requests')).json()).toEqual([])
})

multiTest('(V2-M5 T1) Partial provider configuration reports an actionable reason and app stays healthy', async ({ multi }) => {
  const response = await multi.get('/api/supervisor')
  expect(response.status()).toBe(200)
  const status = await response.json() as { enabled: boolean; reason?: string }
  expect(status.enabled).toBe(false)
  expect(status.reason).toContain('OPENAI_API_KEY')
  expect(JSON.stringify(status)).not.toContain('placeholder')
  expect((await multi.get('/api/health')).status()).toBe(200)
})

llmTest('(V2-M5 T1/T2/T3/T4) Quiet run is flagged from a scrubbed capture and queue does not advance', async ({ llm, target }) => {
  await target.resetTmux()
  await stubs.reset()
  await llm.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  const status = await (await llm.get('/api/supervisor')).json()
  expect(status).toMatchObject({ enabled: true, provider: 'openai', maxPerRunHour: 2, scrub: true })
  expect(JSON.stringify(status)).not.toContain('e2e-placeholder-key')
  const project = await newProject(llm, target, 'e2e-llm-quiet')
  const queue = await createQueue(llm, project.id)
  await stubs.setBehavior('e2e llm quiet', 'quiet-print', 0.1)
  const item = await addItem(llm, queue.id, { instruction: '/goal e2e llm quiet' })
  await control(llm, queue.id, 'start')
  await expect.poll(async () => (await itemOf(llm, queue.id, item.id)).run?.flag?.label, { timeout: 40_000 }).toBe('waiting_input')
  const view = await getQueue(llm, queue.id)
  expect(view.status).toBe('paused')
  expect(view.items).toHaveLength(1)
  expect(view.items[0].status).toBe('needs_attention')
  expect(view.items[0].run?.flag?.reason).toContain('waiting for input')
  const captured = await (await llm.get('http://hostbud-e2e-llmfake:8080/ctl/requests')).json() as { pane: string }[]
  expect(captured).toHaveLength(1)
  expect(captured[0].pane).toContain('[redacted]')
  for (const secret of ['example-secret-value', 'example-bearer-value', 'e2e-placeholder-key']) expect(captured[0].pane).not.toContain(secret)
  const events = await llmDb.events(view.items[0].run!.id)
  expect(events.filter((e) => e.source === 'llm').map((e) => e.kind)).toEqual(['llm_started', 'llm_result'])
  expect(JSON.stringify(events)).not.toContain('API_KEY=')
})

llmTest('(V2-M5 T5) Flag notifies once per run and label with the allowlisted payload', async ({ llm, target }) => {
  await target.resetTmux()
  await stubs.reset()
  await llm.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  await pushfake.reset()
  const device = newDevice()
  await putNotificationSettings(llm, { enabled: true, onAttention: true })
  expect((await subscribe(llm, device)).status()).toBe(204)
  const project = await newProject(llm, target, 'e2e-llm-notify')
  const queue = await createQueue(llm, project.id)
  await stubs.setBehavior('e2e llm notify', 'quiet-print', 0.1)
  const item = await addItem(llm, queue.id, { instruction: '/goal e2e llm notify' })
  await control(llm, queue.id, 'start')
  await expect.poll(async () => (await itemOf(llm, queue.id, item.id)).run?.flag?.label, { timeout: 40_000 }).toBe('waiting_input')
  await expect.poll(async () => received(device), { timeout: 15_000 }).toHaveLength(1)
  const updated = await itemOf(llm, queue.id, item.id)
  const [payload] = await received(device)
  expect(payload).toMatchObject({ kind: 'attention', key: `run:${updated.run!.id}:llm:waiting_input`, outcome: 'waiting_input', project: project.name, position: 1 })
  expect(JSON.stringify(payload)).not.toMatch(/reason|pane|instruction|example-secret|API_KEY/)
})

llmTest('(V2-M5 T2) Hourly classification budget is respected', async ({ llm, target }) => {
  await target.resetTmux(); await stubs.reset(); await llm.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  const project = await newProject(llm,target,'e2e-llm-budget'); const queue=await createQueue(llm,project.id)
  await stubs.setBehavior('e2e llm budget','quiet-print',0.1)
  const item=await addItem(llm,queue.id,{instruction:'/goal e2e llm budget'}); await control(llm,queue.id,'start')
  await expect.poll(async()=>await (await llm.get('http://hostbud-e2e-llmfake:8080/ctl/requests')).json(),{timeout:40_000}).toHaveLength(2)
  await new Promise((resolve)=>setTimeout(resolve,1500))
  expect(await (await llm.get('http://hostbud-e2e-llmfake:8080/ctl/requests')).json()).toHaveLength(2)
  expect((await itemOf(llm,queue.id,item.id)).status).toBe('needs_attention')
})

llmTest('(V2-M5 T2) Gated items are never classified', async ({ llm, target }) => {
  await target.resetTmux(); await stubs.reset(); await llm.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  const project=await newProject(llm,target,'e2e-llm-gated'); const queue=await createQueue(llm,project.id)
  await stubs.setBehavior('e2e llm gated','achieve:1',0.1)
  const item=await addItem(llm,queue.id,{instruction:'/goal e2e llm gated',requiresApproval:true}); await control(llm,queue.id,'start')
  await expect.poll(async()=>(await itemOf(llm,queue.id,item.id)).status,{timeout:15_000}).toBe('awaiting_approval')
  await new Promise((resolve)=>setTimeout(resolve,11_000))
  expect(await (await llm.get('http://hostbud-e2e-llmfake:8080/ctl/requests')).json()).toEqual([])
})

llmTest('(V2-M5 T3) Completed label never advances the queue', async ({ llm, target }) => {
  await target.resetTmux(); await stubs.reset(); await llm.post('http://hostbud-e2e-llmfake:8080/ctl/reset')
  await llm.post('http://hostbud-e2e-llmfake:8080/ctl/script',{data:{label:'completed',reason:'Looks done.'}})
  const project=await newProject(llm,target,'e2e-llm-completed'); const queue=await createQueue(llm,project.id)
  await stubs.setBehavior('e2e llm completed','quiet-print',0.1)
  const first=await addItem(llm,queue.id,{instruction:'/goal e2e llm completed'}); const second=await addItem(llm,queue.id,{instruction:'/goal e2e llm never advance'})
  await control(llm,queue.id,'start')
  await expect.poll(async()=>(await itemOf(llm,queue.id,first.id)).run?.flag?.label,{timeout:40_000}).toBe('completed')
  const view=await getQueue(llm,queue.id)
  expect(view.status).toBe('paused'); expect(view.items[0].status).toBe('needs_attention'); expect(view.items[1]).toMatchObject({id:second.id,status:'queued'})
  expect(view.items[0].run?.flag?.reason).toBe('Looks done.')
})
