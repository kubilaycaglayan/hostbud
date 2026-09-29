import { createHash, randomBytes } from 'node:crypto'
import pg from 'pg'

// The owner's plain-SQL path to the disposable e2e database
// (docs/ARCHITECTURE.md §8.1): allowlist changes and rate-limit resets.
async function sql(text: string, params: unknown[] = [], url = process.env.E2E_DB_URL): Promise<Record<string, unknown>[]> {
  const client = new pg.Client({ connectionString: url })
  await client.connect()
  try {
    return (await client.query(text, params)).rows
  } finally {
    await client.end()
  }
}

export const owner = {
  allow: (email: string) =>
    sql(
      `INSERT INTO email_allowlist (email_normalized) VALUES ($1)
       ON CONFLICT (email_normalized) DO UPDATE SET enabled = TRUE, updated_at = now()`,
      [email],
    ),
  disable: (email: string) =>
    sql(`UPDATE email_allowlist SET enabled = FALSE, updated_at = now() WHERE email_normalized = $1`, [email]),
  deleteUser: (email: string) => sql(`DELETE FROM users WHERE email_normalized = $1`, [email]),
  userExists: async (email: string) =>
    (await sql(`SELECT 1 FROM users WHERE email_normalized = $1`, [email])).length === 1,
  clearRateLimits: () => sql(`DELETE FROM login_rate_limits`),
}

const CROCKFORD = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'

/** A ULID (48-bit ms time + 80 random bits), like store.NewULID. */
export function ulid(at = Date.now()): string {
  const bytes = new Uint8Array(16)
  let ms = BigInt(at)
  for (let i = 5; i >= 0; i--) {
    bytes[i] = Number(ms & 0xffn)
    ms >>= 8n
  }
  bytes.set(randomBytes(10), 6)
  let n = 0n
  for (const b of bytes) n = (n << 8n) | BigInt(b)
  let out = ''
  for (let i = 0; i < 26; i++) {
    out = CROCKFORD[Number(n & 31n)] + out
    n >>= 5n
  }
  return out
}

/** A run token as hostbud makes them: 32 random bytes, base64url. */
export function runToken(): string {
  return randomBytes(32).toString('base64url')
}

export interface SeededRun {
  queueId: string
  itemId: string
  runId: string
  token: string
}

// V2-M1 queue rows written straight to the e2e database (the hook endpoint
// scenarios need runs before the queue API and dispatcher exist).
export const queues = {
  /** Seeds a queue with one item and a run in the given state, with a known
   * token (only its SHA-256 is stored, as in the app). */
  async seedRun(projectId: string, status = 'starting'): Promise<SeededRun> {
    const suffix = randomBytes(8).toString('hex')
    const queueId = `queue_e2e${suffix}`
    const itemId = `item_e2e${suffix}`
    const runId = ulid()
    const token = runToken()
    const hash = createHash('sha256').update(token).digest()
    await sql(
      `INSERT INTO queues (id, machine_id, project_id, name) SELECT $1, machine_id, id, $3 FROM projects WHERE id = $2`,
      [queueId, projectId, `e2e-${suffix}`],
    )
    await sql(
      `INSERT INTO queue_items (id, queue_id, machine_id, position, agent, instruction, status)
       VALUES ($1, $2, 'host', 1, 'claude', '/goal e2e', 'running')`,
      [itemId, queueId],
    )
    const ended = ['achieved', 'failed', 'exited', 'cancelled'].includes(status)
    await sql(
      `INSERT INTO runs (id, item_id, machine_id, token_hash, status, started_at, ended_at)
       VALUES ($1, $2, 'host', $3, $4, now(), CASE WHEN $5 THEN now() END)`,
      [runId, itemId, hash, status, ended],
    )
    return { queueId, itemId, runId, token }
  },
  /** Attaches a failed synthetic run and transitions its item for history coverage. */
  async setItemFailure(itemId: string, detail: string, agentSessionId = '') {
    const runId = ulid()
    const tokenHash = createHash('sha256').update(runToken()).digest()
    await sql(
      `INSERT INTO runs (id, item_id, machine_id, token_hash, status, started_at, ended_at, detail, agent_session_id)
       VALUES ($1, $2, 'host', $3, 'failed', now(), now(), $4, NULLIF($5, ''))`,
      [runId, itemId, tokenHash, detail, agentSessionId],
    )
    await sql(
      `INSERT INTO run_events (run_id, machine_id, source, kind, payload_json)
       VALUES ($1, 'host', 'verify', 'verify_result', $2)`,
      [runId, JSON.stringify({ detail, output: 'HISTORY_PRIVATE_OUTPUT_SENTINEL' })],
    )
    await sql(`UPDATE queue_items SET status = 'needs_attention', updated_at = now() WHERE id = $1`, [itemId])
  },
  /** The run's audit rows, oldest first. */
  events: (runId: string) =>
    sql(`SELECT source, kind, payload_json FROM run_events WHERE run_id = $1 ORDER BY id`, [runId]) as Promise<
      { source: string; kind: string; payload_json: string }[]
    >,
  /** Deletes every queue with its items, runs and events, and clears the
   * stored parallel-queues switch (the tests' shared reset; the app refuses
   * to delete a project that has a queue). */
  deleteAll: async () => {
    await sql(`UPDATE machine_capacity SET parallel_queues = NULL`)
    await sql(`DELETE FROM run_events`)
    await sql(`DELETE FROM runs`)
    await sql(`DELETE FROM queue_items`)
    await sql(`DELETE FROM queues`)
  },
}

// V2-M3: notification settings, push subscriptions and the outbox of the
// main apps' database (every account off again: the tests' shared reset).
export const notifications = {
  reset: async () => {
    await sql(`DELETE FROM notification_deliveries`)
    await sql(`DELETE FROM notification_outbox`)
    await sql(`DELETE FROM push_subscriptions`)
    await sql(`DELETE FROM notification_prefs`)
  },
  /** Outbox rows (dedupe keys) per account email. */
  outbox: async (): Promise<{ email: string; key: string }[]> =>
    (await sql(`SELECT u.email_normalized AS email, o.dedupe_key AS key FROM notification_outbox o JOIN users u ON u.id = o.user_id ORDER BY o.id`)) as unknown as { email: string; key: string }[],
  /** Push subscription endpoints per account email. */
  subscriptions: async (): Promise<{ email: string; endpoint: string }[]> =>
    (await sql(`SELECT u.email_normalized AS email, s.endpoint FROM push_subscriptions s JOIN users u ON u.id = s.user_id ORDER BY s.created_at`)) as unknown as { email: string; endpoint: string }[],
}

// V2-M2: the multi app's own database (HOSTBUD_PARALLEL_QUEUES=true).
const multiSql = (text: string, params: unknown[] = []) => sql(text, params, process.env.E2E_DB_MULTI_URL)

export const multiDb = {
  /** The owner's allowlist on the multi app. */
  allow: (email: string) =>
    multiSql(
      `INSERT INTO email_allowlist (email_normalized) VALUES ($1)
       ON CONFLICT (email_normalized) DO UPDATE SET enabled = TRUE, updated_at = now()`,
      [email],
    ),
  clearRateLimits: () => multiSql(`DELETE FROM login_rate_limits`),
  /** Deletes every queue with its items, runs and events, and every
   * project (the multi scenarios' reset). */
  reset: async () => {
    await multiSql(`DELETE FROM run_events`)
    await multiSql(`DELETE FROM runs`)
    await multiSql(`DELETE FROM queue_items`)
    await multiSql(`DELETE FROM queues`)
    await multiSql(`DELETE FROM session_links`)
    await multiSql(`DELETE FROM recent_commands`)
    await multiSql(`DELETE FROM projects`)
    await multiSql(`DELETE FROM machine_capacity`)
    // V2-M3: every account off again.
    await multiSql(`DELETE FROM notification_deliveries`)
    await multiSql(`DELETE FROM notification_outbox`)
    await multiSql(`DELETE FROM push_subscriptions`)
    await multiSql(`DELETE FROM notification_prefs`)
  },
  /** Marks an active run stale, as the stale timer would (a stale run still
   * holds its slot). */
  markStale: (runId: string) =>
    multiSql(
      `UPDATE runs SET status = 'stale', detail = 'no signal from the agent (seeded by e2e)'
       WHERE id = $1 AND status IN ('starting', 'running')`,
      [runId],
    ),
  /** Sets the host's run cap (machine_capacity); null means the default cap. */
  setCapacity: (maxConcurrentRuns: number | null) =>
    multiSql(
      `INSERT INTO machine_capacity (machine_id, max_concurrent_runs, updated_at) VALUES ('host', $1, now())
       ON CONFLICT (machine_id) DO UPDATE SET max_concurrent_runs = EXCLUDED.max_concurrent_runs, updated_at = now()`,
      [maxConcurrentRuns],
    ),
  /** The runs' start order (ids are ULIDs), with their queue. */
  runOrder: () =>
    multiSql(
      `SELECT r.id, i.queue_id, r.status FROM runs r JOIN queue_items i ON i.id = r.item_id ORDER BY r.id`,
    ) as Promise<{ id: string; queue_id: string; status: string }[]>,
}

// V2-M5: isolated database for the configured supervisor test app.
const llmSql = (text: string, params: unknown[] = []) => sql(text, params, process.env.E2E_DB_LLM_URL)
export const llmDb = {
  allow: (email: string) => llmSql(`INSERT INTO email_allowlist(email_normalized) VALUES($1) ON CONFLICT(email_normalized) DO UPDATE SET enabled=TRUE,updated_at=now()`, [email]),
  clearRateLimits: () => llmSql(`DELETE FROM login_rate_limits`),
  reset: async () => {
    await llmSql(`DELETE FROM run_events`)
    await llmSql(`DELETE FROM runs`)
    await llmSql(`DELETE FROM queue_items`)
    await llmSql(`DELETE FROM queues`)
    await llmSql(`DELETE FROM session_links`)
    await llmSql(`DELETE FROM recent_commands`)
    await llmSql(`DELETE FROM projects`)
    await llmSql(`DELETE FROM machine_capacity`)
    await llmSql(`DELETE FROM notification_deliveries`)
    await llmSql(`DELETE FROM notification_outbox`)
    await llmSql(`DELETE FROM push_subscriptions`)
    await llmSql(`DELETE FROM notification_prefs`)
  },
  events: (runId:string) => llmSql(`SELECT source,kind,payload_json FROM run_events WHERE run_id=$1 ORDER BY id`,[runId]) as Promise<{source:string;kind:string;payload_json:string}[]>,
}
