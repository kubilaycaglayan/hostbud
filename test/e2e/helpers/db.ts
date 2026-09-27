import { createHash, randomBytes } from 'node:crypto'
import pg from 'pg'

// The owner's plain-SQL path to the disposable e2e database
// (docs/ARCHITECTURE.md §8.1): allowlist changes and rate-limit resets.
async function sql(text: string, params: unknown[] = []): Promise<Record<string, unknown>[]> {
  const client = new pg.Client({ connectionString: process.env.E2E_DB_URL })
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
      `INSERT INTO queues (id, machine_id, project_id, name) SELECT $1, machine_id, id, 'e2e' FROM projects WHERE id = $2`,
      [queueId, projectId],
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
  /** The run's audit rows, oldest first. */
  events: (runId: string) =>
    sql(`SELECT source, kind, payload_json FROM run_events WHERE run_id = $1 ORDER BY id`, [runId]) as Promise<
      { source: string; kind: string; payload_json: string }[]
    >,
  /** Deletes every queue with its items, runs and events (the tests' shared
   * reset; the app refuses to delete a project that has a queue). */
  deleteAll: async () => {
    await sql(`DELETE FROM run_events`)
    await sql(`DELETE FROM runs`)
    await sql(`DELETE FROM queue_items`)
    await sql(`DELETE FROM queues`)
  },
}
