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
