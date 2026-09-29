import { ApiError, sessionsApi } from '@/api/client'
import { describeError } from '@/stores/toasts'

export interface KillSessionsOutcome {
  killed: string[]
  failed: string[]
  /** The first failure, ready to show. */
  error: ReturnType<typeof describeError> | null
}

/**
 * Kills sessions in one request: the server refreshes the session list once
 * instead of once per session. A failure doesn't stop the rest; if the request
 * itself fails, every session counts as failed. Callers must have the user's
 * confirmation.
 */
export async function killSessions(machine: string, names: string[]): Promise<KillSessionsOutcome> {
  try {
    const result = await sessionsApi.killMany(machine, names)
    const first = result.failed[0]
    return {
      killed: result.killed,
      failed: result.failed.map((f) => f.name),
      error: first ? describeError(new ApiError(200, first.error, first.hint)) : null,
    }
  } catch (e) {
    return { killed: [], failed: [...names], error: describeError(e) }
  }
}
